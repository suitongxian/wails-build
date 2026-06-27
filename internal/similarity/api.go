package similarity

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
)

// FileInput is a pre-loaded file descriptor used as input to BuildFamilies.
// Callers (typically the data-asset-scan task runner) populate this from the
// data_distribution table + on-disk stat, so we avoid re-walking the tree.
type FileInput struct {
	UniqueID    string    // stable identifier (e.g. data_distribution_id as string)
	Path        string    // absolute file path
	ContentSign string    // partial-MD5 / sha256 already computed by the scanner
	Size        int64     // bytes
	ModTime     time.Time // mtime
}

// Family is the public family record returned to callers.
type Family struct {
	FamilyID     string
	PrimaryID    string
	Algorithm    string
	MemberCount  int
	HighestScore float64
	Members      []FamilyMember
}

type FamilyMember struct {
	UniqueID    string
	Path        string
	ContentSign string
	Relation    string  // same_content / process_version / derived
	Score       float64
}

// BuildFamilies runs the experiment-derived pipeline on a pre-loaded slice of
// FileInputs and returns the resulting families.
//
// The pipeline mirrors experiment/main.go's main() flow but skips the disk
// walk (callers feed inputs in) and the report writers (no xlsx/json/md output).
func BuildFamilies(ctx context.Context, inputs []FileInput, cfg *Config) ([]Family, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	cfg.applyDefaults()

	records := inputsToRecords(inputs)
	markBackups(records, cfg)

	pairs := selectCandidates(ctx, records, cfg)
	rawFamilies := buildFamilies(ctx, records, pairs, cfg)

	recordByID := make(map[string]*FileRecord, len(records))
	for _, r := range records {
		recordByID[r.FileUniqueID] = r
	}

	out := make([]Family, 0, len(rawFamilies))
	for _, fam := range rawFamilies {
		members := make([]FamilyMember, 0, len(fam.MemberIDs))
		for _, mid := range fam.MemberIDs {
			rec := recordByID[mid]
			if rec == nil {
				continue
			}
			meta := fam.MemberScores[mid]
			members = append(members, FamilyMember{
				UniqueID:    rec.FileUniqueID,
				Path:        rec.FileFullPath,
				ContentSign: rec.FileHash,
				Relation:    string(meta.Relation),
				Score:       meta.Score,
			})
		}
		out = append(out, Family{
			FamilyID:     fam.FamilyID,
			PrimaryID:    fam.PrimaryFileID,
			Algorithm:    fam.Algorithm,
			MemberCount:  len(members),
			HighestScore: fam.Score,
			Members:      members,
		})
	}
	return out, nil
}

// inputsToRecords converts FileInput into the internal *FileRecord shape.
func inputsToRecords(inputs []FileInput) []*FileRecord {
	out := make([]*FileRecord, 0, len(inputs))
	hashGroups := make(map[string]string)
	for _, in := range inputs {
		groupID, ok := hashGroups[in.ContentSign]
		if !ok {
			groupID = newUUID()
			hashGroups[in.ContentSign] = groupID
		}
		uid := in.UniqueID
		if uid == "" {
			uid = newUUID()
		}
		name := filepath.Base(in.Path)
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		mainName := name
		if ext != "" {
			mainName = strings.TrimSuffix(name, "."+strings.ToLower(filepath.Ext(name)[1:]))
		}
		out = append(out, &FileRecord{
			FileUniqueID:    uid,
			FileFullPath:    in.Path,
			FileName:        name,
			FileMainName:    mainName,
			FileExt:         ext,
			FileSizeByte:    in.Size,
			FileCreateTime:  in.ModTime,
			FileModifyTime:  in.ModTime,
			FileDirPath:     filepath.Dir(in.Path),
			FileHash:        in.ContentSign,
			FileHashGroupID: groupID,
			FileMIME:        detectMIME(in.Path, ext),
		})
	}
	return out
}

// detectMIME runs mimetype.DetectFile on the path (matching what experiment's
// scanFiles does) and falls back to extension-based guessing if the file
// cannot be opened. Accurate MIME is critical: mimeBucket() routes files into
// img/doc/code/other and only same-bucket pairs are evaluated as candidates.
func detectMIME(path, ext string) string {
	if mt, err := mimetype.DetectFile(path); err == nil {
		s := mt.String()
		if s != "" {
			return s
		}
	}
	return detectMIMEByExt(ext)
}

// detectMIMEByExt is a fallback used when the file is unreadable.
func detectMIMEByExt(ext string) string {
	switch ext {
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "doc":
		return "application/msword"
	case "rtf":
		return "application/rtf"
	case "txt", "md", "log", "csv":
		return "text/plain"
	case "html", "htm":
		return "text/html"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	}
	return "application/octet-stream"
}

// applyDefaults ensures Config has sane values when caller passed a partial cfg.
func (c *Config) applyDefaults() {
	d := defaultConfig()
	if c.HashAlgorithm == "" {
		c.HashAlgorithm = d.HashAlgorithm
	}
	if c.MaxFileSizeGB == 0 {
		c.MaxFileSizeGB = d.MaxFileSizeGB
	}
	if c.SizeFloatThreshold == 0 {
		c.SizeFloatThreshold = d.SizeFloatThreshold
	}
	if c.FileNameSimilarityThresh == 0 {
		c.FileNameSimilarityThresh = d.FileNameSimilarityThresh
	}
	if c.FeatureSimilarityThresh == 0 {
		c.FeatureSimilarityThresh = d.FeatureSimilarityThresh
	}
	if c.ModifyTimeIntervalDay == 0 {
		c.ModifyTimeIntervalDay = d.ModifyTimeIntervalDay
	}
	if c.SameContentThreshold == 0 {
		c.SameContentThreshold = d.SameContentThreshold
	}
	if c.ProcessVersionThreshold == 0 {
		c.ProcessVersionThreshold = d.ProcessVersionThreshold
	}
	if c.DerivedFileThreshold == 0 {
		c.DerivedFileThreshold = d.DerivedFileThreshold
	}
	if c.ImageSimilarityThreshold == 0 {
		c.ImageSimilarityThreshold = d.ImageSimilarityThreshold
	}
	if len(c.BackupKeywords) == 0 {
		c.BackupKeywords = d.BackupKeywords
	}
	if c.excludeSystemExtSet == nil {
		c.excludeSystemExtSet = d.excludeSystemExtSet
	}
	if c.workFileExtSet == nil {
		c.workFileExtSet = d.workFileExtSet
	}
}
