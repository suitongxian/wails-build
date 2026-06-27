package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DefaultExcludedDirs is the set of directories to exclude by default
var DefaultExcludedDirs = []string{
	".git",
	"node_modules",
	".svn",
	".hg",
	"__pycache__",
	".DS_Store",
}

// StreamingScanOptions contains options for streaming directory scan
type StreamingScanOptions struct {
	Directory   string   // Directory to scan
	Extensions  []string // File extensions to include (e.g., [".txt", ".pdf"])
	ExcludeDirs []string // Additional directories to exclude (按目录名匹配)
	// ExcludePaths 按"全路径前缀"排除子树（与 ExcludeDirs 按名匹配互补）。
	// 用于精确排除"数据业务模版项目目录"——这些归主路径自动归档，通用扫描不得触碰，
	// 而项目目录之外的工作空间文件照常被扫到(走认领归档)。按名匹配会误伤/漏排，故用此。
	ExcludePaths []string
	BatchSize    int    // Batch size for callback (default 100)
	Workspace    string // Workspace directory for collecting additional suffixes
	// Optional: 上层已算好的 workspace 后缀统计。如果非 nil，跳过函数内部的
	// CollectWorkspaceSuffixes(workspace) 调用，直接复用。设计用于 atomic.go
	// Scan() 入口算一次后传给 CountFilesWithExtensions + ScanWithCallback，
	// 避免 workspace 被 walk 两遍。
	PrecomputedWorkspaceStats *WorkspaceStats
}

// ScanProgress contains progress information during scanning
type ScanProgress struct {
	ScannedCount int
	CurrentPath  string
}

// WorkspaceStats contains statistics about files in the workspace
type WorkspaceStats struct {
	WorkspacePath      string         // Workspace path
	WorkspaceFileCount int            // Total file count in workspace
	WorkspaceSuffixes  []string       // All file suffixes found in workspace
	SuffixCounts       map[string]int // Count of files per suffix
}

// ScanResultWithSuffixes contains scan results including used extensions
type ScanResultWithSuffixes struct {
	ScannedCount   int
	UsedExtensions []string
	WorkspaceStats *WorkspaceStats
}

// FileCallback is a callback function for each file found
type FileCallback func(filePath string) error

// BatchCallback is a callback function for a batch of files
type BatchCallback func(filePaths []string) error

// ProgressCallback is a callback function for progress updates
type ProgressCallback func(progress ScanProgress)

// StreamingFileScanner provides streaming directory scanning capabilities
type StreamingFileScanner struct {
	excludeDirs []string
}

// NewStreamingFileScanner creates a new StreamingFileScanner
func NewStreamingFileScanner() *StreamingFileScanner {
	return &StreamingFileScanner{}
}

// ParseExtensions parses a comma-separated extension string into a normalized slice
func ParseExtensions(extensionsStr string) []string {
	if extensionsStr == "" {
		return nil
	}

	var result []string
	parts := strings.Split(extensionsStr, ",")
	for _, ext := range parts {
		ext = strings.TrimSpace(ext)
		if len(ext) == 0 {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		result = append(result, strings.ToLower(ext))
	}
	return result
}

// isExcludedDir checks if a directory name should be excluded
func (s *StreamingFileScanner) isExcludedDir(dirName string, excludeSet map[string]bool) bool {
	lowerName := strings.ToLower(dirName)

	// Check if starts with dot (hidden directory)
	if strings.HasPrefix(dirName, ".") {
		return true
	}

	// Check default excludes
	for _, excluded := range DefaultExcludedDirs {
		if strings.ToLower(excluded) == lowerName {
			return true
		}
	}

	// Check custom excludes
	if excludeSet[lowerName] {
		return true
	}

	return false
}

// isExcludedPath 按"全路径前缀"判断目录是否应排除（精确到具体子树，不会因目录重名误伤）。
func (s *StreamingFileScanner) isExcludedPath(path string, excludePaths []string) bool {
	for _, ep := range excludePaths {
		if ep == "" {
			continue
		}
		if IsPathWithin(path, ep) {
			return true
		}
	}
	return false
}

// IsPathWithin checks if childPath is within parentPath
func IsPathWithin(childPath, parentPath string) bool {
	normalizedChild := filepath.ToSlash(filepath.Clean(childPath))
	normalizedParent := filepath.ToSlash(filepath.Clean(parentPath))

	if normalizedChild == normalizedParent {
		return true
	}

	if !strings.HasSuffix(normalizedParent, "/") {
		normalizedParent += "/"
	}

	return strings.HasPrefix(normalizedChild, normalizedParent)
}

// CollectWorkspaceSuffixes collects all file suffixes in a workspace directory
func (s *StreamingFileScanner) CollectWorkspaceSuffixes(workspacePath string, excludeDirs []string) (*WorkspaceStats, error) {
	excludeSet := make(map[string]bool)
	for _, d := range excludeDirs {
		excludeSet[strings.ToLower(d)] = true
	}
	var excludePaths []string // 后缀收集不需要按路径排除，占位以统一回调判断

	suffixCounts := make(map[string]int)
	var totalFiles int

	err := filepath.WalkDir(workspacePath, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		if info.IsDir() {
			if s.isExcludedDir(info.Name(), excludeSet) || s.isExcludedPath(path, excludePaths) {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			suffixCounts[ext]++
			totalFiles++
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	workspaceSuffixes := make([]string, 0, len(suffixCounts))
	for ext := range suffixCounts {
		workspaceSuffixes = append(workspaceSuffixes, ext)
	}
	sort.Strings(workspaceSuffixes)

	return &WorkspaceStats{
		WorkspacePath:      workspacePath,
		WorkspaceFileCount: totalFiles,
		WorkspaceSuffixes:  workspaceSuffixes,
		SuffixCounts:       suffixCounts,
	}, nil
}

// MergeExtensions merges two extension slices, removing duplicates
func MergeExtensions(baseExtensions, additionalExtensions []string) []string {
	extSet := make(map[string]bool)

	for _, ext := range baseExtensions {
		extSet[strings.ToLower(ext)] = true
	}
	for _, ext := range additionalExtensions {
		extSet[strings.ToLower(ext)] = true
	}

	result := make([]string, 0, len(extSet))
	for ext := range extSet {
		result = append(result, ext)
	}
	sort.Strings(result)
	return result
}

// ScanWithCallback scans a directory and calls the callback for each file
func (s *StreamingFileScanner) ScanWithCallback(
	options StreamingScanOptions,
	onFile FileCallback,
	onProgress ProgressCallback,
) (*ScanResultWithSuffixes, error) {
	excludeSet := make(map[string]bool)
	for _, d := range options.ExcludeDirs {
		excludeSet[strings.ToLower(d)] = true
	}
	excludePaths := options.ExcludePaths

	var workspaceStats *WorkspaceStats
	finalExtensions := options.Extensions

	// If workspace is specified, collect suffixes first
	if options.PrecomputedWorkspaceStats != nil {
		workspaceStats = options.PrecomputedWorkspaceStats
		finalExtensions = MergeExtensions(options.Extensions, workspaceStats.WorkspaceSuffixes)
	} else if options.Workspace != "" {
		ws, err := s.CollectWorkspaceSuffixes(options.Workspace, options.ExcludeDirs)
		if err == nil {
			workspaceStats = ws
			finalExtensions = MergeExtensions(options.Extensions, ws.WorkspaceSuffixes)
		}
	}

	extSet := make(map[string]bool)
	for _, ext := range finalExtensions {
		extSet[strings.ToLower(ext)] = true
	}

	var scannedCount int
	scannedPaths := make(map[string]bool) // For deduplication
	visitedInodes := make(map[uint64]bool)
	var mu sync.Mutex

	scanDir := func(dir string) error {
		return filepath.WalkDir(dir, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip errors, continue walking
			}

			if info.IsDir() {
				if s.isExcludedDir(info.Name(), excludeSet) || s.isExcludedPath(path, excludePaths) {
					return filepath.SkipDir
				}
				return nil
			}

			// Track inodes to avoid loops (handle symlinks properly)
			if info.Type()&os.ModeSymlink != 0 {
				// For symlinks, use the resolved path for inode tracking
				realPath, err := filepath.EvalSymlinks(path)
				if err == nil {
					stat, err := os.Stat(realPath)
					if err == nil {
						if ino, ok := getInode(stat); ok {
							mu.Lock()
							if visitedInodes[ino] {
								mu.Unlock()
								return nil
							}
							visitedInodes[ino] = true
							mu.Unlock()
						}
					}
				}
			}

			// Check extension filter
			ext := strings.ToLower(filepath.Ext(path))
			if len(extSet) > 0 && !extSet[ext] {
				return nil
			}

			// Deduplicate by normalized path
			normalizedPath := strings.ToLower(filepath.Clean(path))
			mu.Lock()
			if scannedPaths[normalizedPath] {
				mu.Unlock()
				return nil
			}
			scannedPaths[normalizedPath] = true
			mu.Unlock()

			if err := onFile(path); err != nil {
				// Log error but continue processing
			}
			scannedCount++

			if onProgress != nil && scannedCount%50 == 0 {
				onProgress(ScanProgress{
					ScannedCount: scannedCount,
					CurrentPath:  path,
				})
			}

			return nil
		})
	}

	// Scan main directory
	if err := scanDir(options.Directory); err != nil {
		return nil, err
	}

	// If workspace is not within directory range, scan workspace separately
	if options.Workspace != "" {
		workspaceNeedsScanning := true
		if IsPathWithin(options.Workspace, options.Directory) {
			workspaceNeedsScanning = false
		}
		if workspaceNeedsScanning {
			if err := scanDir(options.Workspace); err != nil {
				return nil, err
			}
		}
	}

	return &ScanResultWithSuffixes{
		ScannedCount:   scannedCount,
		UsedExtensions: finalExtensions,
		WorkspaceStats: workspaceStats,
	}, nil
}

// ScanWithBatchCallback scans a directory and calls the callback for batches of files
func (s *StreamingFileScanner) ScanWithBatchCallback(
	options StreamingScanOptions,
	onBatch BatchCallback,
	onProgress ProgressCallback,
) (*ScanResultWithSuffixes, error) {
	excludeSet := make(map[string]bool)
	for _, d := range options.ExcludeDirs {
		excludeSet[strings.ToLower(d)] = true
	}
	excludePaths := options.ExcludePaths

	var workspaceStats *WorkspaceStats
	finalExtensions := options.Extensions
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	// If workspace is specified, collect suffixes first
	if options.Workspace != "" {
		ws, err := s.CollectWorkspaceSuffixes(options.Workspace, options.ExcludeDirs)
		if err == nil {
			workspaceStats = ws
			finalExtensions = MergeExtensions(options.Extensions, ws.WorkspaceSuffixes)
		}
	}

	extSet := make(map[string]bool)
	for _, ext := range finalExtensions {
		extSet[strings.ToLower(ext)] = true
	}

	var scannedCount int
	var batch []string
	scannedPaths := make(map[string]bool)
	visitedInodes := make(map[uint64]bool)
	var mu sync.Mutex

	processBatch := func(currentBatch []string, lastPath string) error {
		if len(currentBatch) == 0 {
			return nil
		}
		if err := onBatch(currentBatch); err != nil {
			return err
		}
		scannedCount += len(currentBatch)
		if onProgress != nil {
			onProgress(ScanProgress{
				ScannedCount: scannedCount,
				CurrentPath:  lastPath,
			})
		}
		return nil
	}

	scanDir := func(dir string) error {
		return filepath.WalkDir(dir, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if info.IsDir() {
				if s.isExcludedDir(info.Name(), excludeSet) || s.isExcludedPath(path, excludePaths) {
					return filepath.SkipDir
				}
				return nil
			}

			// Track inodes for symlinks
			if info.Type()&os.ModeSymlink != 0 {
				realPath, err := filepath.EvalSymlinks(path)
				if err == nil {
					stat, err := os.Stat(realPath)
					if err == nil {
						if ino, ok := getInode(stat); ok {
							mu.Lock()
							if visitedInodes[ino] {
								mu.Unlock()
								return nil
							}
							visitedInodes[ino] = true
							mu.Unlock()
						}
					}
				}
			}

			// Check extension filter
			ext := strings.ToLower(filepath.Ext(path))
			if len(extSet) > 0 && !extSet[ext] {
				return nil
			}

			// Deduplicate
			normalizedPath := strings.ToLower(filepath.Clean(path))
			mu.Lock()
			if scannedPaths[normalizedPath] {
				mu.Unlock()
				return nil
			}
			scannedPaths[normalizedPath] = true
			mu.Unlock()

			batch = append(batch, path)

			if len(batch) >= batchSize {
				lastPath := batch[len(batch)-1]
				currentBatch := batch
				batch = batch[:0]
				if err := processBatch(currentBatch, lastPath); err != nil {
					return err
				}
			}

			return nil
		})
	}

	// Scan main directory
	if err := scanDir(options.Directory); err != nil {
		return nil, err
	}

	// If workspace is not within directory range, scan workspace separately
	if options.Workspace != "" {
		workspaceNeedsScanning := true
		if IsPathWithin(options.Workspace, options.Directory) {
			workspaceNeedsScanning = false
		}
		if workspaceNeedsScanning {
			if err := scanDir(options.Workspace); err != nil {
				return nil, err
			}
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		if err := processBatch(batch, batch[len(batch)-1]); err != nil {
			return nil, err
		}
	}

	return &ScanResultWithSuffixes{
		ScannedCount:   scannedCount,
		UsedExtensions: finalExtensions,
		WorkspaceStats: workspaceStats,
	}, nil
}

// ScanAsGenerator yields files one by one via a channel
func (s *StreamingFileScanner) ScanAsGenerator(options StreamingScanOptions) (<-chan string, <-chan error) {
	fileChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(fileChan)
		defer close(errChan)

		excludeSet := make(map[string]bool)
		for _, d := range options.ExcludeDirs {
			excludeSet[strings.ToLower(d)] = true
		}

		finalExtensions := options.Extensions

		// If workspace is specified, collect suffixes first
		if options.Workspace != "" {
			ws, err := s.CollectWorkspaceSuffixes(options.Workspace, options.ExcludeDirs)
			if err == nil {
				finalExtensions = MergeExtensions(options.Extensions, ws.WorkspaceSuffixes)
			}
		}

		extSet := make(map[string]bool)
		for _, ext := range finalExtensions {
			extSet[strings.ToLower(ext)] = true
		}

		scannedPaths := make(map[string]bool)
		visitedInodes := make(map[uint64]bool)
		var mu sync.Mutex

		scanDir := func(dir string) error {
			return filepath.WalkDir(dir, func(path string, info os.DirEntry, err error) error {
				if err != nil {
					return nil
				}

				if info.IsDir() {
					if s.isExcludedDir(info.Name(), excludeSet) {
						return filepath.SkipDir
					}
					return nil
				}

				// Track inodes for symlinks
				if info.Type()&os.ModeSymlink != 0 {
					realPath, err := filepath.EvalSymlinks(path)
					if err == nil {
						stat, err := os.Stat(realPath)
						if err == nil {
							if ino, ok := getInode(stat); ok {
								mu.Lock()
								if visitedInodes[ino] {
									mu.Unlock()
									return nil
								}
								visitedInodes[ino] = true
								mu.Unlock()
							}
						}
					}
				}

				// Check extension filter
				ext := strings.ToLower(filepath.Ext(path))
				if len(extSet) > 0 && !extSet[ext] {
					return nil
				}

				// Deduplicate
				normalizedPath := strings.ToLower(filepath.Clean(path))
				mu.Lock()
				if scannedPaths[normalizedPath] {
					mu.Unlock()
					return nil
				}
				scannedPaths[normalizedPath] = true
				mu.Unlock()

				select {
				case fileChan <- path:
				case <-fileChan:
				}

				return nil
			})
		}

		// Scan main directory
		if err := scanDir(options.Directory); err != nil {
			errChan <- err
			return
		}

		// If workspace is not within directory range, scan workspace separately
		if options.Workspace != "" {
			workspaceNeedsScanning := true
			if IsPathWithin(options.Workspace, options.Directory) {
				workspaceNeedsScanning = false
			}
			if workspaceNeedsScanning {
				if err := scanDir(options.Workspace); err != nil {
					errChan <- err
					return
				}
			}
		}
	}()

	return fileChan, errChan
}

// CountFiles counts the number of files matching the criteria
func (s *StreamingFileScanner) CountFiles(options StreamingScanOptions) (int, error) {
	excludeSet := make(map[string]bool)
	for _, d := range options.ExcludeDirs {
		excludeSet[strings.ToLower(d)] = true
	}
	excludePaths := options.ExcludePaths

	finalExtensions := options.Extensions

	// If workspace is specified, collect suffixes first
	if options.Workspace != "" {
		ws, err := s.CollectWorkspaceSuffixes(options.Workspace, options.ExcludeDirs)
		if err == nil {
			finalExtensions = MergeExtensions(options.Extensions, ws.WorkspaceSuffixes)
		}
	}

	extSet := make(map[string]bool)
	for _, ext := range finalExtensions {
		extSet[strings.ToLower(ext)] = true
	}

	var totalCount int
	visitedInodes := make(map[uint64]bool)
	var mu sync.Mutex

	countInDirectory := func(dir string) error {
		return filepath.WalkDir(dir, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if info.IsDir() {
				if s.isExcludedDir(info.Name(), excludeSet) || s.isExcludedPath(path, excludePaths) {
					return filepath.SkipDir
				}
				return nil
			}

			// Track inodes for symlinks
			if info.Type()&os.ModeSymlink != 0 {
				realPath, err := filepath.EvalSymlinks(path)
				if err == nil {
					stat, err := os.Stat(realPath)
					if err == nil {
						if ino, ok := getInode(stat); ok {
							mu.Lock()
							if visitedInodes[ino] {
								mu.Unlock()
								return nil
							}
							visitedInodes[ino] = true
							mu.Unlock()
						}
					}
				}
			}

			// Check extension filter
			ext := strings.ToLower(filepath.Ext(path))
			if len(extSet) > 0 && !extSet[ext] {
				return nil
			}

			totalCount++
			return nil
		})
	}

	// Count in main directory
	if err := countInDirectory(options.Directory); err != nil {
		return 0, err
	}

	// If workspace is not within directory range, count workspace separately
	if options.Workspace != "" {
		workspaceNeedsScanning := true
		if IsPathWithin(options.Workspace, options.Directory) {
			workspaceNeedsScanning = false
		}
		if workspaceNeedsScanning {
			if err := countInDirectory(options.Workspace); err != nil {
				return 0, err
			}
		}
	}

	return totalCount, nil
}

// CountFilesWithExtensions counts files and returns extension info
func (s *StreamingFileScanner) CountFilesWithExtensions(options StreamingScanOptions) (*ScanResultWithSuffixes, error) {
	excludeSet := make(map[string]bool)
	for _, d := range options.ExcludeDirs {
		excludeSet[strings.ToLower(d)] = true
	}
	excludePaths := options.ExcludePaths

	var workspaceStats *WorkspaceStats
	finalExtensions := options.Extensions

	// If workspace is specified, collect suffixes first
	if options.PrecomputedWorkspaceStats != nil {
		workspaceStats = options.PrecomputedWorkspaceStats
		finalExtensions = MergeExtensions(options.Extensions, workspaceStats.WorkspaceSuffixes)
	} else if options.Workspace != "" {
		ws, err := s.CollectWorkspaceSuffixes(options.Workspace, options.ExcludeDirs)
		if err == nil {
			workspaceStats = ws
			finalExtensions = MergeExtensions(options.Extensions, ws.WorkspaceSuffixes)
		}
	}

	extSet := make(map[string]bool)
	for _, ext := range finalExtensions {
		extSet[strings.ToLower(ext)] = true
	}

	var totalCount int
	visitedInodes := make(map[uint64]bool)
	var mu sync.Mutex

	countInDirectory := func(dir string) error {
		return filepath.WalkDir(dir, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if info.IsDir() {
				if s.isExcludedDir(info.Name(), excludeSet) || s.isExcludedPath(path, excludePaths) {
					return filepath.SkipDir
				}
				return nil
			}

			// Track inodes for symlinks
			if info.Type()&os.ModeSymlink != 0 {
				realPath, err := filepath.EvalSymlinks(path)
				if err == nil {
					stat, err := os.Stat(realPath)
					if err == nil {
						if ino, ok := getInode(stat); ok {
							mu.Lock()
							if visitedInodes[ino] {
								mu.Unlock()
								return nil
							}
							visitedInodes[ino] = true
							mu.Unlock()
						}
					}
				}
			}

			// Check extension filter
			ext := strings.ToLower(filepath.Ext(path))
			if len(extSet) > 0 && !extSet[ext] {
				return nil
			}

			totalCount++
			return nil
		})
	}

	// Count in main directory
	if err := countInDirectory(options.Directory); err != nil {
		return nil, err
	}

	// If workspace is not within directory range, count workspace separately
	if options.Workspace != "" {
		workspaceNeedsScanning := true
		if IsPathWithin(options.Workspace, options.Directory) {
			workspaceNeedsScanning = false
		}
		if workspaceNeedsScanning {
			if err := countInDirectory(options.Workspace); err != nil {
				return nil, err
			}
		}
	}

	return &ScanResultWithSuffixes{
		ScannedCount:   totalCount,
		UsedExtensions: finalExtensions,
		WorkspaceStats: workspaceStats,
	}, nil
}
