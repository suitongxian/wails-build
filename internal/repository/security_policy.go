package repository

import (
	"github.com/jmoiron/sqlx"

	"data-asset-scan-go/internal/models"
)

// SecurityPolicyRepository security_policies 表
type SecurityPolicyRepository struct {
	DB *sqlx.DB
}

func NewSecurityPolicyRepository(db *sqlx.DB) *SecurityPolicyRepository {
	return &SecurityPolicyRepository{DB: db}
}

// FindByLevelAndState 按安全等级和文件状态查找策略
func (r *SecurityPolicyRepository) FindByLevelAndState(level, fileState string) (*models.SecurityPolicy, error) {
	var p models.SecurityPolicy
	err := r.DB.Get(&p, `SELECT * FROM security_policies WHERE sensitivity_level = ? AND file_state = ? AND disable = 0 LIMIT 1`,
		level, fileState)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByID 按主键查
func (r *SecurityPolicyRepository) FindByID(id int64) (*models.SecurityPolicy, error) {
	var p models.SecurityPolicy
	err := r.DB.Get(&p, `SELECT * FROM security_policies WHERE id = ? AND disable = 0`, id)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByCode 按 policy_code 查询
func (r *SecurityPolicyRepository) FindByCode(code string) (*models.SecurityPolicy, error) {
	var p models.SecurityPolicy
	err := r.DB.Get(&p, `SELECT * FROM security_policies WHERE policy_code = ? AND disable = 0`, code)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListByLevel 按等级列出
func (r *SecurityPolicyRepository) ListByLevel(level string) ([]models.SecurityPolicy, error) {
	var list []models.SecurityPolicy
	err := r.DB.Select(&list, `SELECT * FROM security_policies WHERE sensitivity_level = ? AND disable = 0 ORDER BY file_state`, level)
	return list, err
}

// ListAll 全部
func (r *SecurityPolicyRepository) ListAll() ([]models.SecurityPolicy, error) {
	var list []models.SecurityPolicy
	err := r.DB.Select(&list, `SELECT * FROM security_policies WHERE disable = 0 ORDER BY sensitivity_level, file_state`)
	return list, err
}

// SensitivityLevelOrder 安全等级序数（就高不就低用）
//
//	general < important < core_secret
func SensitivityLevelOrder(level string) int {
	switch level {
	case SensGeneral:
		return 1
	case SensImportant:
		return 2
	case SensCoreSecret:
		return 3
	default:
		return 0
	}
}

// HigherSensitivityLevel 返回二者中较高的安全等级
func HigherSensitivityLevel(a, b string) string {
	if SensitivityLevelOrder(a) >= SensitivityLevelOrder(b) {
		return a
	}
	return b
}
