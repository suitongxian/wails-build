package similarity

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"data-asset-scan-go/internal/repository"
)

// AnalyzerOptions controls a similarity-analysis run.
type AnalyzerOptions struct {
	// AnalyzeTaskID is recorded on every family row for traceability.
	AnalyzeTaskID *int64
	// Reset wipes existing family assignments before computing new ones.
	// v1 always uses Reset=true (full rebuild). Incremental is future work.
	Reset bool
	// Cfg overrides BuildFamilies defaults; nil means use defaults.
	Cfg *Config
}

// AnalyzerResult summarizes one run.
type AnalyzerResult struct {
	InputCount  int
	FamilyCount int
	MemberCount int // total members across all families
}

// AnalyzeFromDB loads all active rows from data_distributing, runs
// BuildFamilies on them, and persists the results to data_resource_family
// + data_resources.family_*.
//
// Loaders are abstracted via the Loader interface to keep this function
// unit-testable without a real DB.
func AnalyzeFromDB(ctx context.Context, loader Loader, persist Persister, opt AnalyzerOptions) (*AnalyzerResult, error) {
	inputs, err := loader.LoadInputs()
	if err != nil {
		return nil, fmt.Errorf("load inputs: %w", err)
	}

	if opt.Reset {
		if err := persist.ResetFamilies(); err != nil {
			return nil, fmt.Errorf("reset families: %w", err)
		}
	}

	fams, err := BuildFamilies(ctx, inputs, opt.Cfg)
	if err != nil {
		return nil, fmt.Errorf("build families: %w", err)
	}

	log.Printf("Persisting %d families to DB...", len(fams))
	persistStart := time.Now()
	totalMembers := 0
	persistedCount := 0
	skippedSingleton := 0
	for fi, fam := range fams {
		if (fi+1)%50 == 0 || fi+1 == len(fams) {
			log.Printf("Persisting families: %d/%d (persisted=%d, skipped singletons=%d)",
				fi+1, len(fams), persistedCount, skippedSingleton)
		}
		// Translate FamilyMember → repository.FamilyMemberAssignment.
		// data_resources is keyed by content_sign (one row per hash group),
		// so we de-dup by ContentSign here: if two physical files in the
		// same family share a hash, they map to the same data_resources row.
		// IMPORTANT: process the primary first so its IsPrimary flag isn't
		// lost when a sibling physical file of the same hash iterates earlier.
		ordered := make([]FamilyMember, 0, len(fam.Members))
		for _, m := range fam.Members {
			if m.UniqueID == fam.PrimaryID {
				ordered = append(ordered, m)
			}
		}
		for _, m := range fam.Members {
			if m.UniqueID != fam.PrimaryID {
				ordered = append(ordered, m)
			}
		}

		seen := make(map[string]bool)
		assignments := make([]repository.FamilyMemberAssignment, 0, len(ordered))
		for _, m := range ordered {
			if seen[m.ContentSign] {
				continue
			}
			seen[m.ContentSign] = true
			assignments = append(assignments, repository.FamilyMemberAssignment{
				ContentSign: m.ContentSign,
				Relation:    m.Relation,
				Score:       m.Score,
				IsPrimary:   m.UniqueID == fam.PrimaryID,
			})
		}
		if len(assignments) < 2 {
			// Singleton families don't add value over the existing hash-group
			// view — skip persistence.
			skippedSingleton++
			continue
		}

		// Find primary's content_sign for the family row.
		primaryCS := ""
		for _, m := range fam.Members {
			if m.UniqueID == fam.PrimaryID {
				primaryCS = m.ContentSign
				break
			}
		}
		if primaryCS == "" && len(assignments) > 0 {
			primaryCS = assignments[0].ContentSign
		}

		if err := persist.SaveFamily(repository.FamilyInsert{
			PrimaryContentSign: primaryCS,
			Algorithm:          fam.Algorithm,
			HighestScore:       fam.HighestScore,
			AnalyzeTaskID:      opt.AnalyzeTaskID,
			Members:            assignments,
		}); err != nil {
			return nil, fmt.Errorf("save family: %w", err)
		}
		totalMembers += len(assignments)
		persistedCount++
	}
	log.Printf("Persistence done in %v: persisted=%d, skipped singletons=%d, total members=%d",
		time.Since(persistStart), persistedCount, skippedSingleton, totalMembers)

	return &AnalyzerResult{
		InputCount:  len(inputs),
		FamilyCount: len(fams),
		MemberCount: totalMembers,
	}, nil
}

// Loader abstracts how the analyzer obtains its FileInput slice.
type Loader interface {
	LoadInputs() ([]FileInput, error)
}

// Persister abstracts the storage side of the analyzer.
type Persister interface {
	ResetFamilies() error
	SaveFamily(repository.FamilyInsert) error
}

// DBLoader pulls inputs from data_distributing.
type DBLoader struct {
	Repo *repository.DataDistributingRepository
}

func (l *DBLoader) LoadInputs() ([]FileInput, error) {
	rows, err := l.Repo.GetActive()
	if err != nil {
		return nil, err
	}
	out := make([]FileInput, 0, len(rows))
	for _, r := range rows {
		mt := r.UpdateTime
		if r.FileUpdateTime != nil {
			mt = *r.FileUpdateTime
		}
		out = append(out, FileInput{
			UniqueID:    strconv.FormatInt(r.DataDistributionID, 10),
			Path:        r.Path,
			ContentSign: r.ContentSign,
			Size:        r.FileSize,
			ModTime:     mt,
		})
	}
	return out, nil
}

// DBPersister writes families through repository.FamilyRepository.
type DBPersister struct {
	Repo *repository.FamilyRepository
}

func (p *DBPersister) ResetFamilies() error                          { return p.Repo.ResetFamilies() }
func (p *DBPersister) SaveFamily(in repository.FamilyInsert) error { _, err := p.Repo.InsertFamilyWithMembers(in); return err }
