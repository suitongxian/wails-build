package repository

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// SubjectRepository 三主体（人/部门/组织）数据访问
type SubjectRepository struct {
	DB *sqlx.DB
}

// NewSubjectRepository 创建仓储实例
func NewSubjectRepository(db *sqlx.DB) *SubjectRepository {
	return &SubjectRepository{DB: db}
}

// CreateSubjectInput 创建主体输入
type CreateSubjectInput struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Type     string  `json:"type"` // person / department / organization
	ParentID *int64  `json:"parent_id"`
	Contact  *string `json:"contact"`
}

// UpdateSubjectInput 更新主体输入
type UpdateSubjectInput struct {
	Name     *string `json:"name"`
	Type     *string `json:"type"`
	ParentID *int64  `json:"parent_id"`
	Contact  *string `json:"contact"`
	Status   *string `json:"status"`
}

// Create 创建主体
func (r *SubjectRepository) Create(in CreateSubjectInput) (*models.Subject, error) {
	now := time.Now()
	res, err := r.DB.Exec(`INSERT INTO subjects (code, name, type, parent_id, contact, status, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, 'active', ?, ?, 0)`,
		in.Code, in.Name, in.Type, in.ParentID, in.Contact, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.FindByID(id)
}

// FindByID 按 ID 查找
func (r *SubjectRepository) FindByID(id int64) (*models.Subject, error) {
	var s models.Subject
	err := r.DB.Get(&s, `SELECT * FROM subjects WHERE id = ? AND disable = 0`, id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FindByCode 按业务编码查找
func (r *SubjectRepository) FindByCode(code string) (*models.Subject, error) {
	var s models.Subject
	err := r.DB.Get(&s, `SELECT * FROM subjects WHERE code = ? AND disable = 0`, code)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// List 查询主体列表
func (r *SubjectRepository) List(filterType string, keyword string) ([]models.Subject, error) {
	return r.ListWithOptions(filterType, keyword, false)
}

func (r *SubjectRepository) ListWithOptions(filterType string, keyword string, includeSystem bool) ([]models.Subject, error) {
	q := `SELECT * FROM subjects WHERE disable = 0`
	args := []interface{}{}
	if !includeSystem {
		q += ` AND code NOT LIKE 'SYS-%'`
	}
	if filterType != "" {
		q += ` AND type = ?`
		args = append(args, filterType)
	}
	if keyword != "" {
		q += ` AND (code LIKE ? OR name LIKE ?)`
		args = append(args, "%"+keyword+"%", "%"+keyword+"%")
	}
	// 默认按创建时间倒序：刚刚添加的排在最上面（更符合用户直觉）
	// 如需按 type 分组查看，可以用 type 筛选下拉
	q += ` ORDER BY create_time DESC, id DESC`
	var list []models.Subject
	if err := r.DB.Select(&list, q, args...); err != nil {
		return nil, err
	}
	return list, nil
}

// Update 更新主体
func (r *SubjectRepository) Update(id int64, in UpdateSubjectInput) (*models.Subject, error) {
	cur, err := r.FindByID(id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		cur.Name = *in.Name
	}
	if in.Type != nil {
		cur.Type = *in.Type
	}
	if in.ParentID != nil {
		cur.ParentID = in.ParentID
	}
	if in.Contact != nil {
		cur.Contact = in.Contact
	}
	if in.Status != nil {
		cur.Status = *in.Status
	}
	now := time.Now()
	if _, err := r.DB.Exec(`UPDATE subjects SET name = ?, type = ?, parent_id = ?, contact = ?, status = ?, update_time = ? WHERE id = ?`,
		cur.Name, cur.Type, cur.ParentID, cur.Contact, cur.Status, now, id); err != nil {
		return nil, err
	}
	return r.FindByID(id)
}

// SoftDelete 软删除主体
func (r *SubjectRepository) SoftDelete(id int64) error {
	_, err := r.DB.Exec(`UPDATE subjects SET disable = 1, update_time = ? WHERE id = ?`, time.Now(), id)
	return err
}

// UpsertByCode 用 manage 端主体编码作为跨系统稳定标识，同步到本地缓存。
func (r *SubjectRepository) UpsertByCode(in CreateSubjectInput, status string) (*models.Subject, error) {
	if status == "" {
		status = "active"
	}
	now := time.Now()
	_, err := r.DB.Exec(`INSERT INTO subjects (code, name, type, parent_id, contact, status, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(code) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			parent_id = excluded.parent_id,
			contact = excluded.contact,
			status = excluded.status,
			update_time = excluded.update_time,
			disable = 0`,
		strings.TrimSpace(in.Code), strings.TrimSpace(in.Name), strings.TrimSpace(in.Type),
		in.ParentID, in.Contact, status, now, now)
	if err != nil {
		return nil, err
	}
	return r.FindByCode(strings.TrimSpace(in.Code))
}

type ManageSubject struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	ParentID *int64  `json:"parent_id"`
	Contact  *string `json:"contact"`
	Status   string  `json:"status"`
}

type ManageSubjectListResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    []ManageSubject `json:"data"`
}

type SubjectSyncResult struct {
	TotalRemote int      `json:"total_remote"`
	Synced      int      `json:"synced"`
	Errors      []string `json:"errors"`
}

type SubjectSyncer struct {
	DB         *sqlx.DB
	configRepo *SystemConfigRepository
	httpClient *http.Client
}

func NewSubjectSyncer(db *sqlx.DB, config *SystemConfigRepository) *SubjectSyncer {
	return &SubjectSyncer{
		DB:         db,
		configRepo: config,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SubjectSyncer) SyncAll() (*SubjectSyncResult, error) {
	endpoint := strings.TrimRight(s.configRepo.GetValue(KeyManageEndpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("未配置 manage_endpoint，请在 Settings 中设置")
	}

	req, err := http.NewRequest("GET", endpoint+"/api/subjects/list?include_inactive=1", nil)
	if err != nil {
		return nil, err
	}
	// manage_token 已废弃，不再发送 X-Sync-Token 头

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 manage 主体列表失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取主体列表响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manage 主体列表返回非 200: %d, body=%s", resp.StatusCode, string(body))
	}
	var raw ManageSubjectListResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析主体列表失败: %w", err)
	}
	if raw.Code != 0 {
		return nil, fmt.Errorf("manage 主体列表返回错误 code=%d, msg=%s", raw.Code, raw.Message)
	}

	repo := NewSubjectRepository(s.DB)
	result := &SubjectSyncResult{TotalRemote: len(raw.Data), Errors: []string{}}
	for _, subj := range raw.Data {
		if subj.Code == "" || subj.Name == "" || subj.Type == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("跳过非法主体: code=%q name=%q type=%q", subj.Code, subj.Name, subj.Type))
			continue
		}
		if _, err := repo.UpsertByCode(CreateSubjectInput{
			Code:     subj.Code,
			Name:     subj.Name,
			Type:     subj.Type,
			ParentID: subj.ParentID,
			Contact:  subj.Contact,
		}, subj.Status); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", subj.Code, err))
			continue
		}
		result.Synced++
	}
	return result, nil
}
