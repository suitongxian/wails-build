package repository

import (
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	SimTaskStatePending = "pending"
	SimTaskStateRunning = "running"
	SimTaskStateSucceed = "succeed"
	SimTaskStateFailed  = "failed"
)

type SimilarityTaskRow struct {
	TaskID       int64      `db:"task_id" json:"task_id"`
	TaskState    string     `db:"task_state" json:"task_state"`
	Phase        *string    `db:"phase" json:"phase"`
	InputCount   int        `db:"input_count" json:"input_count"`
	FamilyCount  int        `db:"family_count" json:"family_count"`
	MemberCount  int        `db:"member_count" json:"member_count"`
	ErrorMessage *string    `db:"error_message" json:"error_message"`
	StartTime    time.Time  `db:"start_time" json:"start_time"`
	EndTime      *time.Time `db:"end_time" json:"end_time"`
	CreateTime   time.Time  `db:"create_time" json:"create_time"`
	UpdateTime   time.Time  `db:"update_time" json:"update_time"`
}

type SimilarityTaskRepository struct {
	DB *sqlx.DB
}

func NewSimilarityTaskRepository(db *sqlx.DB) *SimilarityTaskRepository {
	return &SimilarityTaskRepository{DB: db}
}

func (r *SimilarityTaskRepository) Create() (int64, error) {
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO similarity_task
		(task_state, phase, input_count, family_count, member_count, start_time, create_time, update_time)
		VALUES (?, 'pending', 0, 0, 0, ?, ?, ?)`,
		SimTaskStatePending, now, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r *SimilarityTaskRepository) MarkRunning(id int64, phase string) error {
	now := time.Now()
	_, err := r.DB.Exec(`UPDATE similarity_task
		SET task_state = ?, phase = ?, update_time = ?
		WHERE task_id = ?`, SimTaskStateRunning, phase, now, id)
	return err
}

func (r *SimilarityTaskRepository) UpdatePhase(id int64, phase string) error {
	now := time.Now()
	_, err := r.DB.Exec(`UPDATE similarity_task
		SET phase = ?, update_time = ? WHERE task_id = ?`, phase, now, id)
	return err
}

func (r *SimilarityTaskRepository) MarkSucceeded(id int64, inputCount, familyCount, memberCount int) error {
	now := time.Now()
	_, err := r.DB.Exec(`UPDATE similarity_task
		SET task_state = ?, phase = 'done', input_count = ?, family_count = ?, member_count = ?,
		    end_time = ?, update_time = ?
		WHERE task_id = ?`, SimTaskStateSucceed, inputCount, familyCount, memberCount, now, now, id)
	return err
}

func (r *SimilarityTaskRepository) MarkFailed(id int64, msg string) error {
	now := time.Now()
	_, err := r.DB.Exec(`UPDATE similarity_task
		SET task_state = ?, error_message = ?, end_time = ?, update_time = ?
		WHERE task_id = ?`, SimTaskStateFailed, msg, now, now, id)
	return err
}

func (r *SimilarityTaskRepository) GetByID(id int64) (*SimilarityTaskRow, error) {
	var t SimilarityTaskRow
	if err := r.DB.Get(&t, `SELECT * FROM similarity_task WHERE task_id = ?`, id); err != nil {
		return nil, err
	}
	return &t, nil
}

// Latest returns the most recently created task or nil if none exist.
func (r *SimilarityTaskRepository) Latest() (*SimilarityTaskRow, error) {
	var t SimilarityTaskRow
	err := r.DB.Get(&t, `SELECT * FROM similarity_task ORDER BY task_id DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// LatestSucceeded returns the most recent task with task_state='succeed', or nil if none.
func (r *SimilarityTaskRepository) LatestSucceeded() (*SimilarityTaskRow, error) {
	var t SimilarityTaskRow
	err := r.DB.Get(&t, `SELECT * FROM similarity_task
		WHERE task_state = 'succeed' ORDER BY end_time DESC LIMIT 1`)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// HasRunning returns true if any task is currently in pending/running state.
func (r *SimilarityTaskRepository) HasRunning() (bool, error) {
	var n int
	err := r.DB.Get(&n, `SELECT COUNT(*) FROM similarity_task WHERE task_state IN (?, ?)`,
		SimTaskStatePending, SimTaskStateRunning)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
