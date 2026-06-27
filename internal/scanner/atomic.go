package scanner

import (
	"data-asset-scan-go/internal/models"
	"data-asset-scan-go/internal/repository"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/jmoiron/sqlx"
)

// featurePrecomputeEnabled checks the system_config flag.
// Default true (when row missing/unparseable); explicitly "false" disables.
func featurePrecomputeEnabled() bool {
	db := repository.TryGetDB()
	if db == nil {
		return true // before DB init (rare), be permissive
	}
	repo := repository.NewSystemConfigRepository(db)
	v, err := repo.Get(repository.KeyFeaturePrecomputeEnabled)
	if err != nil {
		return true // any DB read error: default on
	}
	return v != "false"
}

// ScanMode represents the scanning mode
type ScanMode string

const (
	// FullInventory is the first full inventory scan
	FullInventory ScanMode = "FULL_INVENTORY"
	// DailyCheck is the daily check scan
	DailyCheck ScanMode = "DAILY_CHECK"
	// TargetedScan is a targeted scan of a specific workspace
	TargetedScan ScanMode = "TARGETED_SCAN"
)

// progressReportInterval is the minimum time between progress reports (DB UpdateProgress + emitProgress).
// Decouples report frequency from file count — at most ~2 reports/second regardless of scan speed.
const progressReportInterval = 500 * time.Millisecond

// OnScanCompleteHook is invoked after a scan task finishes successfully.
// Injected by main.go at startup to wire scanner-completion → similarity-analysis.
// nil-safe: not called when not injected.
var OnScanCompleteHook func()

// AtomicScanOptions contains options for atomic scanning
type AtomicScanOptions struct {
	Directory        string   // Directory to scan
	Extensions       []string // File extensions to include
	ExcludeDirs      []string // Directories to exclude (按目录名)
	ExcludePaths     []string // 按全路径前缀排除子树（如数据业务模版项目目录，归主路径）
	Workspace        string   // Workspace directory
	MD5Concurrency   int      // Concurrency for MD5 calculation (default 4)
	BatchSize        int      // Batch size for database writes (default 100)
	ProgressInterval int      // Progress update interval in files (default 50)
	ScanMode         ScanMode // Scan mode
	SaveCode         string   // Security code for re-running full inventory
}

// ScanResult represents the result of a scan operation
type ScanResult struct {
	TaskId          int
	TotalFiles      int
	ScannedFiles    int
	Duration        int64 // milliseconds
	Success         bool
	ErrorMessage    string
	UsedExtensions   []string
	WorkspaceStats  *WorkspaceStats
	FileStatistics  *models.ScanStatistics
}

// DataDistributing represents a data distribution record from the database
type DataDistributing = models.DataDistribution

// DataResources represents a data resources record from the database
type DataResources = models.DataResources

// SurvivalStatusClassification represents the result of survival status classification
type SurvivalStatusClassification struct {
	NewFilePaths      []string          // Paths of new files
	NormalFileRecords []DataDistributing // Normal (unchanged) file records
	DeletedRecords    []DataDistributing // Deleted file records
}

// MD5Statistics represents MD5 statistics collected during scanning
type MD5Statistics struct {
	ContentSign        string
	SourceCount        int
	WorkspaceSourceCount int
	FirstCreateTime    string
	FileMagic          string
	FirstFileName      string
	ShortFileName      string
}

// FileInfo represents information about a scanned file
type FileInfo struct {
	Path       string
	Hash       string
	Size       int64
	Suffix     string
	Magic      string
	CreateTime string
	UpdateTime string
	ReadTime   string
	IsHidden   int
}

// AtomicScanner provides atomic file scanning capabilities
type AtomicScanner struct {
	db          *sqlx.DB
	scanner     *StreamingFileScanner
	taskRepo    *repository.ScanTaskRepository
	dataRepo    *repository.DataDistributingRepository
	resourceRepo *repository.DataResourcesRepository
	configRepo  *repository.SystemConfigRepository
	statsRepo   *repository.FileStatisticsRepository
	localIP     string
	localMAC    string
	progressChan chan ProgressUpdate
	stopChan    chan struct{}
	pausedChan  chan struct{}
	isPaused    bool
	mu          sync.Mutex
}

// NewAtomicScanner creates a new AtomicScanner
func NewAtomicScanner(db *sqlx.DB, batchSize int) *AtomicScanner {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &AtomicScanner{
		db:           db,
		scanner:      NewStreamingFileScanner(),
		taskRepo:     repository.NewScanTaskRepository(db),
		dataRepo:     repository.NewDataDistributingRepository(db, batchSize),
		resourceRepo: repository.NewDataResourcesRepository(db, batchSize),
		configRepo:   repository.NewSystemConfigRepository(db),
		statsRepo:    repository.NewFileStatisticsRepository(db),
		localIP:      getLocalIP(),
		localMAC:     getLocalMAC(),
		progressChan: make(chan ProgressUpdate, 100),
		stopChan:     make(chan struct{}),
		pausedChan:   make(chan struct{}, 1),
	}
}

// ProgressChan returns the channel for progress updates
func (s *AtomicScanner) ProgressChan() <-chan ProgressUpdate {
	return s.progressChan
}

// CloseProgressChan closes the progress channel to signal no more updates
func (s *AtomicScanner) CloseProgressChan() {
	select {
	case <-s.progressChan:
	default:
	}
	close(s.progressChan)
}

// Stop signals the scanner to stop
func (s *AtomicScanner) Stop() {
	close(s.stopChan)
}

// Pause pauses the scanning
func (s *AtomicScanner) Pause() {
	s.mu.Lock()
	s.isPaused = true
	s.mu.Unlock()
	<-s.pausedChan
}

// Resume resumes the scanning
func (s *AtomicScanner) Resume() {
	s.mu.Lock()
	s.isPaused = false
	s.mu.Unlock()
	select {
	case s.pausedChan <- struct{}{}:
	default:
	}
}

// Scan performs the file scan operation
func (s *AtomicScanner) Scan(options AtomicScanOptions) ScanResult {
	startTime := time.Now()
	scanStartTime := startTime.Format(time.RFC3339)

	// Get effective scan directory and workspace
	effectiveDirectory := s.getEffectiveScanDirectory(options)
	effectiveWorkspace := s.getEffectiveWorkspace(options)

	// Set defaults
	md5Concurrency := options.MD5Concurrency
	if md5Concurrency <= 0 {
		md5Concurrency = 4
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	// progressInterval is retained for API compatibility but no longer drives progress-report
	// triggering — that is now controlled by progressReportInterval (time-window 500ms).
	progressInterval := options.ProgressInterval
	if progressInterval <= 0 {
		progressInterval = 50
	}
	_ = progressInterval // suppress unused-variable error; kept for caller compatibility

	// Validate scan mode
	if validationError := s.validateScanMode(options); validationError != "" {
		taskId, _ := s.taskRepo.Create(models.CreateScanTaskParams{
			ScanType:     models.ScanTypeFile,
			FileScanRange: &effectiveDirectory,
			WorkspacePath: &options.Workspace,
			ScanArgs:     strPtr(string(options.ScanMode)),
		})
		s.taskRepo.MarkFailed(int64(taskId), validationError)
		return ScanResult{
			TaskId:       int(taskId),
			TotalFiles:   0,
			ScannedFiles: 0,
			Duration:     time.Since(startTime).Milliseconds(),
			Success:      false,
			ErrorMessage: validationError,
		}
	}

	// Validate directory exists
	if effectiveDirectory != "/" && effectiveDirectory != "\\" {
		if _, err := os.Stat(effectiveDirectory); os.IsNotExist(err) {
			errorMessage := fmt.Sprintf("Path is not a directory: %s", effectiveDirectory)
			taskId, _ := s.taskRepo.Create(models.CreateScanTaskParams{
				ScanType:     models.ScanTypeFile,
				FileScanRange: &effectiveDirectory,
				WorkspacePath: &options.Workspace,
				ScanArgs:     strPtr(string(options.ScanMode)),
			})
			s.taskRepo.MarkFailed(int64(taskId), errorMessage)
			return ScanResult{
				TaskId:       int(taskId),
				TotalFiles:   0,
				ScannedFiles: 0,
				Duration:     time.Since(startTime).Milliseconds(),
				Success:      false,
				ErrorMessage: errorMessage,
			}
		}
	}

	// Validate workspace exists
	if effectiveWorkspace != "" {
		if _, err := os.Stat(effectiveWorkspace); os.IsNotExist(err) {
			errorMessage := fmt.Sprintf("Workspace is not a directory: %s", effectiveWorkspace)
			taskId, _ := s.taskRepo.Create(models.CreateScanTaskParams{
				ScanType:     models.ScanTypeFile,
				FileScanRange: &effectiveDirectory,
				WorkspacePath: &options.Workspace,
				ScanArgs:     strPtr(string(options.ScanMode)),
			})
			s.taskRepo.MarkFailed(int64(taskId), errorMessage)
			return ScanResult{
				TaskId:       int(taskId),
				TotalFiles:   0,
				ScannedFiles: 0,
				Duration:     time.Since(startTime).Milliseconds(),
				Success:      false,
				ErrorMessage: errorMessage,
			}
		}
	}

	// Execute pre-scan actions
	s.executeScanModePreActions(options)

	// Create scan task
	taskId, _ := s.taskRepo.Create(models.CreateScanTaskParams{
		ScanType:     models.ScanTypeFile,
		FileScanRange: &effectiveDirectory,
		WorkspacePath: &options.Workspace,
	})

	// Phase 1: Counting — persist phase to DB so the polling client can show
	// an indeterminate "正在统计文件数量..." state while the walker runs.
	s.taskRepo.UpdateProgress(int64(taskId), models.UpdateProgressParams{
		Heartbeat:        1,
		FileScannedCount: 0,
		TaskPhase:        strPtr("counting"),
	})
	s.emitProgress(ProgressUpdate{
		Type:         string(ProgressUpdateTypeValue),
		ScannedCount: 0,
		TotalCount:   0,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
		Indeterminate: true,
	})

	// 一次性收集 workspace 后缀，避免 CountFilesWithExtensions 和 ScanWithCallback
	// 内部各自重复 walk workspace。失败不阻塞主流程（跟之前的 if err == nil 一样兜底）。
	var precomputedWorkspaceStats *WorkspaceStats
	if effectiveWorkspace != "" {
		if ws, err := s.scanner.CollectWorkspaceSuffixes(effectiveWorkspace, options.ExcludeDirs); err == nil {
			precomputedWorkspaceStats = ws
		}
	}

	countResult, err := s.scanner.CountFilesWithExtensions(StreamingScanOptions{
		Directory:                 effectiveDirectory,
		Extensions:                options.Extensions,
		ExcludeDirs:               options.ExcludeDirs,
		ExcludePaths:               options.ExcludePaths,
		Workspace:                 effectiveWorkspace,
		PrecomputedWorkspaceStats: precomputedWorkspaceStats,
	})
	if err != nil {
		s.taskRepo.MarkFailed(int64(taskId), err.Error())
		return ScanResult{
			TaskId:       int(taskId),
			TotalFiles:   0,
			ScannedFiles: 0,
			Duration:     time.Since(startTime).Milliseconds(),
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	totalFiles := countResult.ScannedCount
	usedExtensions := countResult.UsedExtensions
	workspaceStats := countResult.WorkspaceStats

	// Update workspace info
	if workspaceStats != nil {
		s.taskRepo.UpdateWorkspaceInfo(int64(taskId), models.UpdateWorkspaceParams{
			WorkspacePath:        &options.Workspace,
			FileAllSuffixCount:   intPtr(len(usedExtensions)),
			FileCountSuffixCount: intPtr(len(workspaceStats.WorkspaceSuffixes)),
			WorkspaceCount:       intPtr(workspaceStats.WorkspaceFileCount),
		})
	} else if options.Workspace != "" {
		s.taskRepo.UpdateWorkspaceInfo(int64(taskId), models.UpdateWorkspaceParams{
			WorkspacePath:      &options.Workspace,
			FileAllSuffixCount: intPtr(len(usedExtensions)),
		})
	}

	s.taskRepo.UpdateFileTotal(int64(taskId), totalFiles)
	s.taskRepo.UpdateProgress(int64(taskId), models.UpdateProgressParams{
		Heartbeat:        1,
		FileScannedCount: 0,
		TaskPhase:        strPtr("scanning"),
	})

	// Phase 2: Scanning
	s.emitProgress(ProgressUpdate{
		Type:         string(ProgressUpdateTypeValue),
		ScannedCount: 0,
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
	})

	if totalFiles == 0 {
		return s.handleEmptyScan(options, int64(taskId), effectiveDirectory, effectiveWorkspace, startTime, scanStartTime, usedExtensions, workspaceStats)
	}

	// Check if survival status scan is needed
	if s.shouldPerformSurvivalStatusCheck(options.ScanMode) {
		return s.performSurvivalStatusScan(options, int64(taskId), effectiveDirectory, effectiveWorkspace, usedExtensions, workspaceStats, totalFiles, startTime, scanStartTime, md5Concurrency, batchSize, progressInterval, precomputedWorkspaceStats)
	}

	// Full inventory or normal scan: direct insert logic
	return s.performFullScan(options, int64(taskId), effectiveDirectory, effectiveWorkspace, totalFiles, startTime, scanStartTime, usedExtensions, workspaceStats, md5Concurrency, batchSize, progressInterval, precomputedWorkspaceStats)
}

// performFullScan performs a full inventory or normal scan with direct insert
func (s *AtomicScanner) performFullScan(
	options AtomicScanOptions,
	taskId int64,
	effectiveDirectory string,
	effectiveWorkspace string,
	totalFiles int,
	startTime time.Time,
	scanStartTime string,
	usedExtensions []string,
	workspaceStats *WorkspaceStats,
	md5Concurrency int,
	batchSize int,
	progressInterval int,
	precomputedWorkspaceStats *WorkspaceStats,
) ScanResult {
	scanTime := scanStartTime
	md5StatsMap := make(map[string]*MD5Statistics)
	var scannedCount int
	heartbeat := 1
	lastReportAt := startTime

	isFileFromWorkspace := func(filePath string) bool {
		if effectiveWorkspace == "" {
			return false
		}
		return IsPathWithin(filePath, effectiveWorkspace)
	}

	// Worker pool setup
	numWorkers := md5Concurrency
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}
	if numWorkers < 1 {
		numWorkers = 1
	}
	pathChan := make(chan string, numWorkers*4)
	type fileResult struct {
		info        FileInfo
		path        string // original path (same as info.Path but kept explicit)
		features    CachedFeatures
		hasFeatures bool
	}
	recordChan := make(chan fileResult, numWorkers*4)

	// Launch worker goroutines: each reads paths, computes getFileInfo, sends result
	var workerWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case <-s.stopChan:
					return
				case path, ok := <-pathChan:
					if !ok {
						return
					}
					info, err := s.getFileInfo(path)
					if err != nil {
						// Skip unreadable files (same as serial behavior)
						continue
					}
					res := fileResult{info: info, path: path}
					if featurePrecomputeEnabled() {
						if stat, statErr := os.Stat(path); statErr == nil {
							mimeStr := ""
							if mt, mErr := mimetype.DetectFile(path); mErr == nil {
								mimeStr = mt.String()
							}
							if feat, fErr := ExtractFeaturesForCache(path, mimeStr, stat); fErr != nil {
								log.Printf("[scan] feature extract failed for %s: %v", path, fErr)
							} else {
								res.features = feat
								res.hasFeatures = true
							}
						}
					}
					select {
					case <-s.stopChan:
						return
					case recordChan <- res:
					}
				}
			}
		}()
	}

	// Writer goroutine: collects results, batches InsertBatch, updates md5StatsMap
	var pendingWrites []map[string]interface{}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for res := range recordChan {
			fileInfo := res.info
			filePath := res.path

			record := map[string]interface{}{
				"scan_task_id":     taskId,
				"path":             fileInfo.Path,
				"data_type":        1,
				"content_sign":     fileInfo.Hash,
				"file_suffix":      fileInfo.Suffix,
				"file_magic":       fileInfo.Magic,
				"file_create_time": fileInfo.CreateTime,
				"file_update_time": fileInfo.UpdateTime,
				"file_read_time":   fileInfo.ReadTime,
				"file_size":        fileInfo.Size,
				"file_hide":        fileInfo.IsHidden,
				"ip":               s.localIP,
				"mac_address":      s.localMAC,
				"scan_time":        scanTime,
			}
			if res.hasFeatures {
				feat := res.features
				if feat.ContentHash != "" {
					record["content_hash"] = &feat.ContentHash
				}
				if feat.ExtractedText != "" {
					record["extracted_text"] = &feat.ExtractedText
				}
				if feat.Simhash != 0 {
					record["simhash"] = &feat.Simhash
				}
				if !feat.Mtime.IsZero() {
					record["feature_mtime"] = &feat.Mtime
				}
				if feat.Size > 0 {
					record["feature_size"] = &feat.Size
				}
				// TODO(phash): once image pHash extraction is wired in (Plan B follow-up),
				// also set record["phash"] = &feat.Phash here.
			}
			// suspect 标识：路径/后缀/大小判断是否疑似非个人文件，扫描期顺手打标
			if IsSuspectNonPersonal(fileInfo.Path, fileInfo.Size) {
				record["suspect_non_personal"] = 1
			}
			pendingWrites = append(pendingWrites, record)

			// Update MD5 statistics (writer is sole owner — no mutex needed)
			existingStats := md5StatsMap[fileInfo.Hash]
			isFromWorkspace := isFileFromWorkspace(filePath)
			fileCreateTime := fileInfo.CreateTime
			if fileCreateTime == "" {
				fileCreateTime = scanTime
			}
			fileName := filepath.Base(filePath)

			if existingStats != nil {
				existingStats.SourceCount++
				if isFromWorkspace {
					existingStats.WorkspaceSourceCount++
				}
				if fileCreateTime < existingStats.FirstCreateTime {
					existingStats.FirstCreateTime = fileCreateTime
					existingStats.FirstFileName = fileName
				}
				if fileName < existingStats.ShortFileName {
					existingStats.ShortFileName = fileName
				}
			} else {
				md5StatsMap[fileInfo.Hash] = &MD5Statistics{
					ContentSign:          fileInfo.Hash,
					SourceCount:          1,
					WorkspaceSourceCount: 0,
					FirstCreateTime:      fileCreateTime,
					FileMagic:            fileInfo.Magic,
					FirstFileName:        fileName,
					ShortFileName:        fileName,
				}
				if isFromWorkspace {
					md5StatsMap[fileInfo.Hash].WorkspaceSourceCount = 1
				}
			}

			if len(pendingWrites) >= batchSize {
				s.dataRepo.InsertBatch(pendingWrites)
				pendingWrites = pendingWrites[:0]
			}

			scannedCount++

			if now := time.Now(); now.Sub(lastReportAt) >= progressReportInterval {
				heartbeat++
				s.taskRepo.UpdateProgress(taskId, models.UpdateProgressParams{
					Heartbeat:        heartbeat,
					FileScannedCount: scannedCount,
					TaskPhase:        strPtr("scanning"),
				})
				s.emitProgress(ProgressUpdate{
					Type:         string(ProgressUpdateTypeValue),
					ScannedCount: scannedCount,
					TotalCount:   totalFiles,
					CurrentFile:  filePath,
					ElapsedMs:    time.Since(startTime).Milliseconds(),
				})
				lastReportAt = now
			}
		}
		// Flush remaining batch
		if len(pendingWrites) > 0 {
			s.dataRepo.InsertBatch(pendingWrites)
			pendingWrites = pendingWrites[:0]
		}
	}()

	// Walker: feeds paths into pathChan (single goroutine, not CPU-bound)
	_, err := s.scanner.ScanWithCallback(
		StreamingScanOptions{
			Directory:                 effectiveDirectory,
			Extensions:                options.Extensions,
			ExcludeDirs:               options.ExcludeDirs,
			ExcludePaths:               options.ExcludePaths,
			Workspace:                 effectiveWorkspace,
			PrecomputedWorkspaceStats: precomputedWorkspaceStats,
		},
		func(filePath string) error {
			select {
			case <-s.stopChan:
				return fmt.Errorf("scan stopped")
			case pathChan <- filePath:
				return nil
			}
		},
		nil,
	)

	// Close pathChan to signal workers to finish, then wait for them
	close(pathChan)
	workerWg.Wait()
	// Close recordChan to signal writer to finish, then wait
	close(recordChan)
	<-writerDone

	if err != nil {
		s.taskRepo.MarkFailed(taskId, err.Error())
		return ScanResult{
			TaskId:       int(taskId),
			TotalFiles:   totalFiles,
			ScannedFiles: scannedCount,
			Duration:     time.Since(startTime).Milliseconds(),
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	// Aggregating phase
	heartbeat++
	s.taskRepo.UpdateProgress(taskId, models.UpdateProgressParams{
		Heartbeat:        heartbeat,
		FileScannedCount: scannedCount,
		TaskPhase:        strPtr("aggregating"),
	})
	s.emitProgress(ProgressUpdate{
		Type:         string(ProgressUpdateTypeValue),
		ScannedCount: scannedCount,
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
	})

	// Convert MD5Statistics to the format expected by InsertFromStatistics
	statsMap := make(map[string]interface{})
	for k, v := range md5StatsMap {
		statsMap[k] = &repository.MD5Stats{
			ContentSign:         v.ContentSign,
			SourceCount:         v.SourceCount,
			WorkspaceSourceCount: v.WorkspaceSourceCount,
			FirstCreateTime:     v.FirstCreateTime,
			FileMagic:           v.FileMagic,
			FirstFileName:       v.FirstFileName,
			ShortFileName:       v.ShortFileName,
		}
	}
	resourceCount := s.resourceRepo.InsertFromStatistics(statsMap)
	_ = resourceCount // may want to log this

	// Execute post-scan actions
	s.executeScanModePostActions(options, scanStartTime)

	// Mark task as succeeded
	heartbeat++
	s.taskRepo.UpdateProgress(taskId, models.UpdateProgressParams{
		Heartbeat:        heartbeat,
		FileScannedCount: scannedCount,
		TaskPhase:        strPtr("completed"),
	})
	s.taskRepo.MarkSucceeded(taskId)

	// Set last scan time before emitting complete event
	s.configRepo.SetLastScanTime(scanStartTime)

	s.emitProgress(ProgressUpdate{
		Type:         string(CompleteUpdate),
		ScannedCount: scannedCount,
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
		Success:      true,
	})

	// 扫描结果会改变文件集合，必然影响家族归并 —— 置 dirty，
	// 让用户在认领页看到「重建相似关系」按钮可点。
	s.configRepo.SetValue(repository.KeyFamilyDirty, "1")

	// 可选扩展钩子（默认 nil；保留以便将来挂额外的扫描完成回调）
	if OnScanCompleteHook != nil {
		OnScanCompleteHook()
	}

	// Execute file statistics
	fullInventoryTime := s.configRepo.GetFullInventoryTime()
	fileStatistics := s.statsRepo.ExecuteAndSave(int(taskId), strPtr(s.configRepo.GetWorkspace()), strPtr(fullInventoryTime))

	return ScanResult{
		TaskId:          int(taskId),
		TotalFiles:      totalFiles,
		ScannedFiles:    scannedCount,
		Duration:        time.Since(startTime).Milliseconds(),
		Success:         true,
		UsedExtensions:   usedExtensions,
		WorkspaceStats:  workspaceStats,
		FileStatistics:  fileStatistics,
	}
}

// performSurvivalStatusScan performs a DAILY_CHECK or TARGETED_SCAN with survival status classification
func (s *AtomicScanner) performSurvivalStatusScan(
	options AtomicScanOptions,
	taskId int64,
	effectiveDirectory string,
	effectiveWorkspace string,
	usedExtensions []string,
	workspaceStats *WorkspaceStats,
	totalFiles int,
	startTime time.Time,
	scanStartTime string,
	md5Concurrency int,
	batchSize int,
	progressInterval int,
	precomputedWorkspaceStats *WorkspaceStats,
) ScanResult {
	scanTime := scanStartTime
	isFileFromWorkspace := func(filePath string) bool {
		if effectiveWorkspace == "" {
			return false
		}
		return IsPathWithin(filePath, effectiveWorkspace)
	}

	// Phase: collecting - collect all file paths
	s.emitProgress(ProgressUpdate{
		Type:        string(ProgressUpdateTypeValue),
		Phase:       "collecting",
		ScannedCount: 0,
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
	})

	scannedPaths := make(map[string]bool)
	_, err := s.scanner.ScanWithCallback(
		StreamingScanOptions{
			Directory:                 effectiveDirectory,
			Extensions:                usedExtensions,
			ExcludeDirs:               options.ExcludeDirs,
			ExcludePaths:               options.ExcludePaths,
			Workspace:                 effectiveWorkspace,
			PrecomputedWorkspaceStats: precomputedWorkspaceStats,
		},
		func(filePath string) error {
			scannedPaths[filePath] = true
			return nil
		},
		nil,
	)
	if err != nil {
		s.taskRepo.MarkFailed(taskId, err.Error())
		return ScanResult{
			TaskId:       int(taskId),
			TotalFiles:   totalFiles,
			ScannedFiles: 0,
			Duration:     time.Since(startTime).Milliseconds(),
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	// Phase: loading_existing - load existing records from database
	s.emitProgress(ProgressUpdate{
		Type:        string(ProgressUpdateTypeValue),
		Phase:       "loading_existing",
		ScannedCount: len(scannedPaths),
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
	})

	var existingRecords map[string]DataDistributing
	if options.ScanMode == TargetedScan && effectiveWorkspace != "" {
		existingRecords = s.dataRepo.GetActiveByPathMapWithPrefix(effectiveWorkspace)
	} else {
		existingRecords = s.dataRepo.GetActiveByPathMap()
	}
	existingResources := s.resourceRepo.GetActiveByContentSignMap()

	// Phase: classifying - classify files into new, normal, and deleted
	s.emitProgress(ProgressUpdate{
		Type:        string(ProgressUpdateTypeValue),
		Phase:       "classifying",
		ScannedCount: len(scannedPaths),
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
	})

	classification := s.classifySurvivalStatus(scannedPaths, existingRecords)

	// Track counts for progress
	newFilesCount := len(classification.NewFilePaths)
	normalFilesCount := len(classification.NormalFileRecords)
	deletedFilesCount := len(classification.DeletedRecords)
	modifiedFilesCount := 0
	var scannedCount int
	lastReportAt := startTime

	// Phase: checking_modifications - check for modified files
	if normalFilesCount > 0 {
		s.emitProgress(ProgressUpdate{
			Type:        string(ProgressUpdateTypeValue),
			Phase:       "checking_modifications",
			ScannedCount: scannedCount,
			TotalCount:   totalFiles,
			ElapsedMs:    time.Since(startTime).Milliseconds(),
		})

		var unmodifiedFileIDs []int64
		var modifiedFileUpdates []map[string]interface{}
		var modifiedResourceUpdates []map[string]interface{}

		for _, record := range classification.NormalFileRecords {
			currentInfo, err := os.Stat(record.Path)
			if err != nil {
				unmodifiedFileIDs = append(unmodifiedFileIDs, record.DataDistributionID)
				continue
			}

			currentUpdateTime := currentInfo.ModTime().Format(time.RFC3339)

			if record.FileUpdateTime != nil && record.FileUpdateTime.Format(time.RFC3339) == currentUpdateTime {
				unmodifiedFileIDs = append(unmodifiedFileIDs, record.DataDistributionID)
			} else {
				fileInfo, err := s.getFileInfo(record.Path)
				if err != nil {
					unmodifiedFileIDs = append(unmodifiedFileIDs, record.DataDistributionID)
					continue
				}

				if fileInfo.Hash != record.ContentSign {
					modifiedFileUpdates = append(modifiedFileUpdates, map[string]interface{}{
						"data_distribution_id": record.DataDistributionID,
						"content_sign":         fileInfo.Hash,
						"file_update_time":     fileInfo.UpdateTime,
						"file_read_time":       fileInfo.ReadTime,
						"file_size":           fileInfo.Size,
						"file_magic":          fileInfo.Magic,
					})
					modifiedResourceUpdates = append(modifiedResourceUpdates, map[string]interface{}{
						"old_content_sign": record.ContentSign,
						"new_content_sign": fileInfo.Hash,
						"is_from_workspace": isFileFromWorkspace(record.Path),
						"file_create_time": func() string { return "" }(),
						"file_magic":       fileInfo.Magic,
						"file_name":       filepath.Base(record.Path),
					})
					modifiedFilesCount++
				} else {
					unmodifiedFileIDs = append(unmodifiedFileIDs, record.DataDistributionID)
				}
			}
		}

		// Batch update unmodified files
		if len(unmodifiedFileIDs) > 0 {
			s.dataRepo.BatchIncrementScanFoundCount(unmodifiedFileIDs)
		}

		// Batch update modified files
		if len(modifiedFileUpdates) > 0 {
			s.dataRepo.BatchUpdateModifiedFiles(modifiedFileUpdates)
			s.resourceRepo.BatchUpdateForModifiedFiles(modifiedResourceUpdates, existingResources)
		}

		scannedCount += normalFilesCount
	}

	// Phase: marking_deleted - mark deleted files
	if deletedFilesCount > 0 {
		s.emitProgress(ProgressUpdate{
			Type:        string(ProgressUpdateTypeValue),
			Phase:       "marking_deleted",
			ScannedCount: scannedCount,
			TotalCount:   totalFiles,
			ElapsedMs:    time.Since(startTime).Milliseconds(),
		})

		var deletedIDs []int64
		for _, record := range classification.DeletedRecords {
			deletedIDs = append(deletedIDs, record.DataDistributionID)
		}
		s.dataRepo.BatchMarkAsDeleted(deletedIDs)

		// Update data_resources for deleted files
		var deletedUpdates []map[string]interface{}
		for _, record := range classification.DeletedRecords {
			deletedUpdates = append(deletedUpdates, map[string]interface{}{
				"content_sign":        record.ContentSign,
				"is_from_workspace": isFileFromWorkspace(record.Path),
			})
		}
		s.resourceRepo.BatchUpdateForDeletedFiles(deletedUpdates)
	}

	// Phase: processing_new - process new files
	if newFilesCount > 0 {
		s.emitProgress(ProgressUpdate{
			Type:        string(ProgressUpdateTypeValue),
			Phase:       "processing_new",
			ScannedCount: scannedCount,
			TotalCount:   totalFiles,
			ElapsedMs:    time.Since(startTime).Milliseconds(),
		})

		newMd5StatsMap := make(map[string]*MD5Statistics)

		// Worker pool for parallel hashing of new files
		numSurvivalWorkers := md5Concurrency
		if numSurvivalWorkers <= 0 {
			numSurvivalWorkers = runtime.NumCPU()
		}
		if numSurvivalWorkers < 1 {
			numSurvivalWorkers = 1
		}
		survivalPathChan := make(chan string, numSurvivalWorkers*4)
		type survivalResult struct {
			info        FileInfo
			path        string
			features    CachedFeatures
			hasFeatures bool
		}
		survivalRecordChan := make(chan survivalResult, numSurvivalWorkers*4)

		var survivalWorkerWg sync.WaitGroup
		for i := 0; i < numSurvivalWorkers; i++ {
			survivalWorkerWg.Add(1)
			go func() {
				defer survivalWorkerWg.Done()
				for {
					select {
					case <-s.stopChan:
						return
					case path, ok := <-survivalPathChan:
						if !ok {
							return
						}
						info, err := s.getFileInfo(path)
						if err != nil {
							continue
						}
						res := survivalResult{info: info, path: path}
						if featurePrecomputeEnabled() {
							if stat, statErr := os.Stat(path); statErr == nil {
								mimeStr := ""
								if mt, mErr := mimetype.DetectFile(path); mErr == nil {
									mimeStr = mt.String()
								}
								if feat, fErr := ExtractFeaturesForCache(path, mimeStr, stat); fErr != nil {
									log.Printf("[scan] feature extract failed for %s: %v", path, fErr)
								} else {
									res.features = feat
									res.hasFeatures = true
								}
							}
						}
						select {
						case <-s.stopChan:
							return
						case survivalRecordChan <- res:
						}
					}
				}
			}()
		}

		// Feed paths to workers
		go func() {
			defer close(survivalPathChan)
			for _, fp := range classification.NewFilePaths {
				select {
				case <-s.stopChan:
					return
				case survivalPathChan <- fp:
				}
			}
		}()

		// Writer: collect results, batch inserts, update stats
		var pendingWrites []map[string]interface{}
		survivalWriterDone := make(chan struct{})
		go func() {
			defer close(survivalWriterDone)
			for res := range survivalRecordChan {
				fileInfo := res.info
				filePath := res.path

				record := map[string]interface{}{
					"scan_task_id":     taskId,
					"path":             fileInfo.Path,
					"data_type":        1,
					"content_sign":     fileInfo.Hash,
					"file_suffix":      fileInfo.Suffix,
					"file_magic":       fileInfo.Magic,
					"file_create_time": fileInfo.CreateTime,
					"file_update_time": fileInfo.UpdateTime,
					"file_read_time":   fileInfo.ReadTime,
					"file_size":        fileInfo.Size,
					"file_hide":        fileInfo.IsHidden,
					"ip":               s.localIP,
					"mac_address":      s.localMAC,
					"scan_time":        scanTime,
				}
				if res.hasFeatures {
					feat := res.features
					if feat.ContentHash != "" {
						record["content_hash"] = &feat.ContentHash
					}
					if feat.ExtractedText != "" {
						record["extracted_text"] = &feat.ExtractedText
					}
					if feat.Simhash != 0 {
						record["simhash"] = &feat.Simhash
					}
					if !feat.Mtime.IsZero() {
						record["feature_mtime"] = &feat.Mtime
					}
					if feat.Size > 0 {
						record["feature_size"] = &feat.Size
					}
					// TODO(phash): once image pHash extraction is wired in (Plan B follow-up),
					// also set record["phash"] = &feat.Phash here.
				}
				// suspect 标识（survival/daily 路径）
				if IsSuspectNonPersonal(fileInfo.Path, fileInfo.Size) {
					record["suspect_non_personal"] = 1
				}
				pendingWrites = append(pendingWrites, record)

				isFromWorkspace := isFileFromWorkspace(filePath)
				fileCreateTime := fileInfo.CreateTime
				if fileCreateTime == "" {
					fileCreateTime = scanTime
				}
				fileName := filepath.Base(filePath)

				existingResource, ok := existingResources[fileInfo.Hash]
				existingNewStats := newMd5StatsMap[fileInfo.Hash]

				if existingNewStats != nil {
					existingNewStats.SourceCount++
					if isFromWorkspace {
						existingNewStats.WorkspaceSourceCount++
					}
					if fileCreateTime < existingNewStats.FirstCreateTime {
						existingNewStats.FirstCreateTime = fileCreateTime
						existingNewStats.FirstFileName = fileName
					}
					if fileName < existingNewStats.ShortFileName {
						existingNewStats.ShortFileName = fileName
					}
				} else {
					useExistingResource := ok && !existingResource.FirstCreateTime.IsZero() && existingResource.FirstCreateTime.Format(time.RFC3339) <= fileCreateTime
					firstFileName := fileName
					shortFileName := fileName
					if useExistingResource && existingResource.ResourcesName != nil {
						firstFileName = *existingResource.ResourcesName
						shortFileName = *existingResource.ResourcesName
					}
					newMd5StatsMap[fileInfo.Hash] = &MD5Statistics{
						ContentSign:          fileInfo.Hash,
						SourceCount:          1,
						WorkspaceSourceCount: 0,
						FirstCreateTime:      fileCreateTime,
						FileMagic:            fileInfo.Magic,
						FirstFileName:        firstFileName,
						ShortFileName:        shortFileName,
					}
					if isFromWorkspace {
						newMd5StatsMap[fileInfo.Hash].WorkspaceSourceCount = 1
					}
				}

				if len(pendingWrites) >= batchSize {
					s.dataRepo.InsertBatch(pendingWrites)
					pendingWrites = pendingWrites[:0]
				}

				scannedCount++

				if now := time.Now(); now.Sub(lastReportAt) >= progressReportInterval {
					s.emitProgress(ProgressUpdate{
						Type:         string(ProgressUpdateTypeValue),
						Phase:        "processing_new",
						ScannedCount: scannedCount,
						TotalCount:   totalFiles,
						CurrentFile:  filePath,
						ElapsedMs:    time.Since(startTime).Milliseconds(),
					})
					lastReportAt = now
				}
			}
			// Flush remaining
			if len(pendingWrites) > 0 {
				s.dataRepo.InsertBatch(pendingWrites)
				pendingWrites = pendingWrites[:0]
			}
		}()

		// Wait for workers to finish, then close recordChan, wait for writer
		survivalWorkerWg.Wait()
		close(survivalRecordChan)
		<-survivalWriterDone

		// Update data_resources for new files
		for contentSign, stats := range newMd5StatsMap {
			existingResource, ok := existingResources[contentSign]
			if ok {
				s.resourceRepo.IncrementSourceCount(contentSign, int64(stats.SourceCount))
				if stats.WorkspaceSourceCount > 0 {
					s.resourceRepo.IncrementWorkspaceSourceCount(contentSign, int64(stats.WorkspaceSourceCount))
				}
				_ = existingResource
			} else {
				// Determine content_type from file extension
				contentType := ""
				if stats.ShortFileName != "" {
					if ext := filepath.Ext(stats.ShortFileName); ext != "" {
						contentType = strings.ToLower(strings.TrimPrefix(ext, "."))
					}
				}
				s.resourceRepo.InsertBatch([]map[string]interface{}{
					{
						"content_sign":          stats.ContentSign,
						"source_count":          stats.SourceCount,
						"workspace_source_count": stats.WorkspaceSourceCount,
						"first_create_time":      stats.FirstCreateTime,
						"file_magic":            stats.FileMagic,
						"resources_name":         stats.ShortFileName,
						"content_subject":       "file",
						"content_type":           contentType,
					},
				})
			}
		}
	}

	// Execute post-scan actions
	s.executeScanModePostActions(options, scanStartTime)

	// Update task progress
	s.taskRepo.UpdateProgress(taskId, models.UpdateProgressParams{
		Heartbeat:        999,
		FileScannedCount: scannedCount,
		TaskPhase:        strPtr("completed"),
	})
	s.taskRepo.MarkSucceeded(taskId)

	// Emit complete progress
	s.emitProgress(ProgressUpdate{
		Type:         string(CompleteUpdate),
		ScannedCount: scannedCount,
		TotalCount:   totalFiles,
		ElapsedMs:    time.Since(startTime).Milliseconds(),
		Success:      true,
		NewFiles:     newFilesCount,
		NormalFiles:  normalFilesCount - modifiedFilesCount,
		DeletedFiles: deletedFilesCount,
		ModifiedFiles: modifiedFilesCount,
	})

	// 扫描结果会改变文件集合，必然影响家族归并 —— 置 dirty。
	s.configRepo.SetValue(repository.KeyFamilyDirty, "1")

	// 可选扩展钩子（默认 nil）
	if OnScanCompleteHook != nil {
		OnScanCompleteHook()
	}

	// Execute file statistics
	fullInventoryTime := s.configRepo.GetFullInventoryTime()
	fileStatistics := s.statsRepo.ExecuteAndSave(int(taskId), strPtr(s.configRepo.GetWorkspace()), strPtr(fullInventoryTime))

	return ScanResult{
		TaskId:          int(taskId),
		TotalFiles:      totalFiles,
		ScannedFiles:    scannedCount,
		Duration:        time.Since(startTime).Milliseconds(),
		Success:         true,
		UsedExtensions:   usedExtensions,
		WorkspaceStats:  workspaceStats,
		FileStatistics:  fileStatistics,
	}
}

// handleEmptyScan handles the case when no files are found
func (s *AtomicScanner) handleEmptyScan(
	options AtomicScanOptions,
	taskId int64,
	effectiveDirectory string,
	effectiveWorkspace string,
	startTime time.Time,
	scanStartTime string,
	usedExtensions []string,
	workspaceStats *WorkspaceStats,
) ScanResult {
	if s.shouldPerformSurvivalStatusCheck(options.ScanMode) {
		existingRecords := s.dataRepo.GetActiveByPathMap()
		if len(existingRecords) > 0 {
			var deletedIDs []int64
			for _, record := range existingRecords {
				deletedIDs = append(deletedIDs, record.DataDistributionID)
			}
			s.dataRepo.BatchMarkAsDeleted(deletedIDs)

			var deletedUpdates []map[string]interface{}
			isFileFromWorkspace := func(filePath string) bool {
				if effectiveWorkspace == "" {
					return false
				}
				return IsPathWithin(filePath, effectiveWorkspace)
			}
			for _, record := range existingRecords {
				deletedUpdates = append(deletedUpdates, map[string]interface{}{
					"content_sign":        record.ContentSign,
					"is_from_workspace": isFileFromWorkspace(record.Path),
				})
			}
			s.resourceRepo.BatchUpdateForDeletedFiles(deletedUpdates)
		}
	}

	s.executeScanModePostActions(options, scanStartTime)
	s.taskRepo.MarkSucceeded(taskId)

	fullInventoryTime := s.configRepo.GetFullInventoryTime()
	fileStatistics := s.statsRepo.ExecuteAndSave(int(taskId), strPtr(s.configRepo.GetWorkspace()), strPtr(fullInventoryTime))

	return ScanResult{
		TaskId:          int(taskId),
		TotalFiles:      0,
		ScannedFiles:    0,
		Duration:        time.Since(startTime).Milliseconds(),
		Success:         true,
		UsedExtensions:   usedExtensions,
		WorkspaceStats:  workspaceStats,
		FileStatistics:  fileStatistics,
	}
}

// validateScanMode validates the scan mode and returns an error message if invalid
func (s *AtomicScanner) validateScanMode(options AtomicScanOptions) string {
	if options.ScanMode == "" {
		return ""
	}

	switch options.ScanMode {
	case FullInventory:
		if s.configRepo.HasFullInventory() {
			if options.SaveCode == "" {
				return "重新首次普查需要提供安全操作码 save_code"
			}
			if !s.configRepo.VerifySaveCode(options.SaveCode) {
				return "安全操作码验证失败"
			}
		}
		return ""

	case DailyCheck:
		if !s.configRepo.HasFullInventory() {
			return "日常盘点需要先进行首次普查"
		}
		return ""

	case TargetedScan:
		if options.Workspace == "" {
			return "定点扫描需要指定 workspace 目录"
		}
		return ""

	default:
		return fmt.Sprintf("未知的扫描模式: %s", options.ScanMode)
	}
}

// executeScanModePreActions executes pre-scan actions based on scan mode
func (s *AtomicScanner) executeScanModePreActions(options AtomicScanOptions) {
	if options.ScanMode == "" {
		return
	}

	switch options.ScanMode {
	case FullInventory:
		if s.configRepo.HasFullInventory() {
			s.dataRepo.Truncate()
			s.resourceRepo.Truncate()
		}
	}
}

// executeScanModePostActions executes post-scan actions based on scan mode
func (s *AtomicScanner) executeScanModePostActions(options AtomicScanOptions, scanStartTime string) {
	if options.ScanMode == "" {
		return
	}

	switch options.ScanMode {
	case FullInventory:
		s.configRepo.SetFullInventoryTime(scanStartTime)
	case DailyCheck, TargetedScan:
		// Survival status already handled in performSurvivalStatusScan
	}
}

// getEffectiveScanDirectory returns the effective scan directory
func (s *AtomicScanner) getEffectiveScanDirectory(options AtomicScanOptions) string {
	if options.ScanMode == TargetedScan && options.Workspace != "" {
		return options.Workspace
	}
	return options.Directory
}

// getEffectiveWorkspace returns the effective workspace
func (s *AtomicScanner) getEffectiveWorkspace(options AtomicScanOptions) string {
	return options.Workspace
}

// shouldPerformSurvivalStatusCheck returns true if survival status check is needed
func (s *AtomicScanner) shouldPerformSurvivalStatusCheck(scanMode ScanMode) bool {
	return scanMode == DailyCheck || scanMode == TargetedScan
}

// classifySurvivalStatus classifies files into new, normal, and deleted
func (s *AtomicScanner) classifySurvivalStatus(
	scannedPaths map[string]bool,
	existingRecords map[string]DataDistributing,
) SurvivalStatusClassification {
	var newFilePaths []string
	var normalFileRecords []DataDistributing
	var deletedRecords []DataDistributing

	// Find new and normal files
	for filePath := range scannedPaths {
		if existingRecord, ok := existingRecords[filePath]; ok {
			normalFileRecords = append(normalFileRecords, existingRecord)
		} else {
			newFilePaths = append(newFilePaths, filePath)
		}
	}

	// Find deleted files
	for filePath, record := range existingRecords {
		if !scannedPaths[filePath] {
			deletedRecords = append(deletedRecords, record)
		}
	}

	return SurvivalStatusClassification{
		NewFilePaths:      newFilePaths,
		NormalFileRecords: normalFileRecords,
		DeletedRecords:    deletedRecords,
	}
}

// getFileInfo collects file information including hash
func (s *AtomicScanner) getFileInfo(filePath string) (FileInfo, error) {
	// Calculate hash
	hashResult, err := CalculateFileHash(filePath)
	if err != nil {
		return FileInfo{}, err
	}

	// Get file stats
	stat, err := os.Stat(filePath)
	if err != nil {
		return FileInfo{}, err
	}

	// Read magic number
	magic, _ := ReadFileMagic(filePath)

	suffix := strings.ToLower(filepath.Ext(filePath))
	fileName := filepath.Base(filePath)
	isHidden := 0
	if strings.HasPrefix(fileName, ".") {
		isHidden = 1
	}

	return FileInfo{
		Path:       filePath,
		Hash:       hashResult.Hash,
		Size:       stat.Size(),
		Suffix:     suffix,
		Magic:      magic,
		CreateTime: formatTime(stat.ModTime()), // Use ModTime as fallback for CreateTime
		UpdateTime: formatTime(stat.ModTime()),
		ReadTime:   formatTime(stat.ModTime()), // Use ModTime as fallback for ReadTime
		IsHidden:   isHidden,
	}, nil
}

// emitProgress sends a progress update
func (s *AtomicScanner) emitProgress(update ProgressUpdate) {
	select {
	case s.progressChan <- update:
	default:
	}
}

// formatTime formats time to RFC3339 String, returns empty string if zero
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	// This is a simplified version - in production you'd use the actual implementation
	return ""
}

// getLocalMAC returns the local MAC address
func getLocalMAC() string {
	// This is a simplified version - in production you'd use the actual implementation
	return ""
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

func strPtr(s string) *string {
	return &s
}