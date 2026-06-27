package repository

import (
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// FamilyRow mirrors data_resource_family.
type FamilyRow struct {
	FamilyID           int64     `db:"family_id" json:"family_id"`
	PrimaryContentSign string    `db:"primary_content_sign" json:"primary_content_sign"`
	PrimaryResourceID  *int64    `db:"primary_resource_id" json:"primary_resource_id"`
	MemberCount        int       `db:"member_count" json:"member_count"`
	Algorithm          *string   `db:"algorithm" json:"algorithm"`
	HighestScore       *float64  `db:"highest_score" json:"highest_score"`
	AnalyzeTaskID      *int64    `db:"analyze_task_id" json:"analyze_task_id"`
	CreateTime         time.Time `db:"create_time" json:"create_time"`
	UpdateTime         time.Time `db:"update_time" json:"update_time"`
	Disable            int       `db:"disable" json:"disable"`
	// 2026-05-21 三级分流：人工裁定的权威源 resource id（NULL = 未确权）
	AuthoritativeResourceID *int64 `db:"authoritative_resource_id" json:"authoritative_resource_id"`
}

// FamilyMemberDetail joins data_resources + data_distributing for a family.
// One row is returned per physical file copy (data_distributing row), so the
// same content_sign may appear multiple times when source_count > 1.
// ClaimStatus / ClaimantName / ClaimTime are included so the UI can grey-out
// already-claimed members in the batch-claim dialog.
type FamilyMemberDetail struct {
	DataResourcesID    int64    `db:"data_resources_id" json:"data_resources_id"`
	ContentSign        string   `db:"content_sign" json:"content_sign"`
	ResourcesName      *string  `db:"resources_name" json:"resources_name"`
	SourceCount        int      `db:"source_count" json:"source_count"`
	FamilyID           *int64   `db:"family_id" json:"family_id"`
	FamilyRelation     *string  `db:"family_relation" json:"family_relation"`
	FamilyScore        *float64 `db:"family_score" json:"family_score"`
	DataDistributionID *int64   `db:"data_distribution_id" json:"data_distribution_id"`
	Path               *string  `db:"path" json:"path"`
	IP                 *string  `db:"ip" json:"ip"`
	// Claim fields — used by the "已认领灰掉" UI in the batch-claim dialog.
	ClaimStatus  *int    `db:"claim_status" json:"claim_status"`
	ClaimantName *string `db:"claimant_name" json:"claimant_name"`
	ClaimTime    *string `db:"claim_time" json:"claim_time"`
}

// FamilyRepository handles CRUD on data_resource_family + family columns of data_resources.
type FamilyRepository struct {
	DB *sqlx.DB
}

func NewFamilyRepository(db *sqlx.DB) *FamilyRepository {
	return &FamilyRepository{DB: db}
}

// ResetFamilies wipes all family assignments. Used by the analyzer when it
// rebuilds families from scratch (v1 strategy).
func (r *FamilyRepository) ResetFamilies() error {
	tx, err := r.DB.Beginx()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE data_resources SET family_id = NULL, family_relation = NULL, family_score = NULL`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM data_resource_family`); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// FamilyInsert is the input used by InsertFamilyWithMembers.
type FamilyInsert struct {
	PrimaryContentSign string
	Algorithm          string
	HighestScore       float64
	AnalyzeTaskID      *int64
	Members            []FamilyMemberAssignment
}

type FamilyMemberAssignment struct {
	ContentSign string
	Relation    string // same_content / process_version / derived / primary
	Score       float64
	IsPrimary   bool
}

// InsertFamilyWithMembers inserts a family row and updates the linked data_resources
// rows (matched by content_sign) with family_id / family_relation / family_score.
// Returns the new family_id.
func (r *FamilyRepository) InsertFamilyWithMembers(in FamilyInsert) (int64, error) {
	now := time.Now()
	tx, err := r.DB.Beginx()
	if err != nil {
		return 0, err
	}

	// Resolve primary_resource_id by content_sign (best-effort; may be nil if
	// data_resources hasn't been written yet).
	var primaryRID *int64
	{
		var rid int64
		err := tx.Get(&rid, `SELECT data_resources_id FROM data_resources WHERE content_sign = ? AND disable = 0 LIMIT 1`, in.PrimaryContentSign)
		if err == nil {
			primaryRID = &rid
		}
	}

	res, err := tx.Exec(`INSERT INTO data_resource_family
		(primary_content_sign, primary_resource_id, member_count, algorithm, highest_score, analyze_task_id, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		in.PrimaryContentSign, primaryRID, len(in.Members), in.Algorithm, in.HighestScore, in.AnalyzeTaskID, now, now)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	famID, err := res.LastInsertId()
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	for _, m := range in.Members {
		rel := m.Relation
		if m.IsPrimary {
			rel = "primary"
		}
		if _, err := tx.Exec(`UPDATE data_resources
			SET family_id = ?, family_relation = ?, family_score = ?, update_time = ?
			WHERE content_sign = ? AND disable = 0`,
			famID, rel, m.Score, now, m.ContentSign); err != nil {
			tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return famID, nil
}

// GetFamilyByID returns one family row.
func (r *FamilyRepository) GetFamilyByID(id int64) (*FamilyRow, error) {
	var f FamilyRow
	err := r.DB.Get(&f, `SELECT * FROM data_resource_family WHERE family_id = ? AND disable = 0`, id)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListFamilyMembers returns one row per physical file copy belonging to a family.
// Each data_resources member is expanded via a JOIN on data_distributing so that
// files sharing the same content_sign appear as separate rows with Path and IP.
func (r *FamilyRepository) ListFamilyMembers(familyID int64) ([]FamilyMemberDetail, error) {
	var members []FamilyMemberDetail
	err := r.DB.Select(&members, `
		SELECT
			dr.data_resources_id, dr.content_sign, dr.resources_name, dr.source_count,
			dr.family_id, dr.family_relation, dr.family_score,
			dd.data_distribution_id, dd.path, dd.ip,
			dr.claim_status, dr.claimant_name, dr.claim_time
		FROM data_resources dr
		LEFT JOIN data_distributing dd
			ON dd.content_sign = dr.content_sign AND dd.disable = 0
		WHERE dr.family_id = ? AND dr.disable = 0
		ORDER BY dr.family_score DESC, dr.data_resources_id ASC, dd.data_distribution_id ASC`,
		familyID)
	return members, err
}

// IDsInFamily returns the data_resources_id list for all rows in a family —
// used by BatchClaim to expand the selection from the primary to all members.
func (r *FamilyRepository) IDsInFamily(familyID int64) ([]int64, error) {
	var ids []int64
	err := r.DB.Select(&ids, `SELECT data_resources_id FROM data_resources WHERE family_id = ? AND disable = 0`, familyID)
	return ids, err
}

// BatchListFamilyMembersByContentSigns 给定一组 content_sign，返回每个 sign 对应 family
// 的全部成员清单（map 的 key 是入参 content_sign，value 是该 family 所有成员）。
// 入参中不属于任何 family 的 content_sign 不会出现在结果 map 里。
// 避免前端批量场景按 family_id 多次往返产生 N+1。
func (r *FamilyRepository) BatchListFamilyMembersByContentSigns(
	contentSigns []string,
) (map[string][]FamilyMemberDetail, error) {
	if len(contentSigns) == 0 {
		return map[string][]FamilyMemberDetail{}, nil
	}

	placeholders := make([]string, len(contentSigns))
	args := make([]interface{}, len(contentSigns))
	for i, cs := range contentSigns {
		placeholders[i] = "?"
		args[i] = cs
	}
	query := `SELECT content_sign, family_id FROM data_resources
		WHERE content_sign IN (` + strings.Join(placeholders, ",") + `)
		  AND family_id IS NOT NULL AND disable = 0`

	type mapping struct {
		ContentSign string `db:"content_sign"`
		FamilyID    int64  `db:"family_id"`
	}
	var mappings []mapping
	if err := r.DB.Select(&mappings, query, args...); err != nil {
		return nil, fmt.Errorf("query content_sign -> family_id: %w", err)
	}

	familyIDs := make(map[int64]bool)
	for _, m := range mappings {
		familyIDs[m.FamilyID] = true
	}
	if len(familyIDs) == 0 {
		return map[string][]FamilyMemberDetail{}, nil
	}

	famPH := make([]string, 0, len(familyIDs))
	famArgs := make([]interface{}, 0, len(familyIDs))
	for fid := range familyIDs {
		famPH = append(famPH, "?")
		famArgs = append(famArgs, fid)
	}

	membersQuery := `SELECT data_resources_id, family_id, family_relation, family_score,
		content_sign, resources_name, source_count, claim_status, claimant_name, claim_time
		FROM data_resources
		WHERE family_id IN (` + strings.Join(famPH, ",") + `) AND disable = 0
		ORDER BY family_id, family_relation`

	var allMembers []FamilyMemberDetail
	if err := r.DB.Select(&allMembers, membersQuery, famArgs...); err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}

	membersByFamily := make(map[int64][]FamilyMemberDetail)
	for _, m := range allMembers {
		if m.FamilyID != nil {
			membersByFamily[*m.FamilyID] = append(membersByFamily[*m.FamilyID], m)
		}
	}

	result := make(map[string][]FamilyMemberDetail, len(mappings))
	for _, mp := range mappings {
		result[mp.ContentSign] = membersByFamily[mp.FamilyID]
	}
	return result, nil
}
