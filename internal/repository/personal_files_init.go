package repository

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

// bootstrapMu 串行化 BootstrapPersonalProjects 调用，防止启动期 goroutine
// 和 HTTP 懒触发并发执行时撞 UNIQUE 约束。
var bootstrapMu sync.Mutex

// V4-Q2 §4.2 个人文件归目管理 — 内置 3 个系统容器的自动初始化
//
// 当前产品定位：SYS-PERSONAL-* 不是用户日常工作的真实项目，也不是
// workspace 目录树，而是个人电子文件数字助理使用的三层级标识编码容器。
//
// 桥接链路：既有"扫描盘点/责任认领/归目保护"链路不变，人工确认后只把
// 文件记录挂账到对应容器，记录来源和规则，不在 workspace 中建目录，也不
// 默认复制或迁移实体文件。

// 内置项目编码（系统保留，禁止用户取相同编码立项）
const (
	PersonalCoreProjectCode      = "SYS-PERSONAL-CORE"
	PersonalImportantProjectCode = "SYS-PERSONAL-IMPORTANT"
	PersonalGeneralProjectCode   = "SYS-PERSONAL-GENERAL"
	// 内置"本人" subject 编码
	PersonalUserSubjectCode = "SYS-PERSONAL-USER"
)

// ensurePersonalFilesContext 启动时检查 3 个内置个人文件归目容器是否存在，缺则建。
//
// 步骤：
//  1. 检查并创建 "本人" subject（type=person，作 3 个项目的三主体共用）
//  2. 检查 TPL-PERSONAL-FILES 模版是否已同步到本地（manage seed 提供）
//     - 没有则跳过此次初始化（manage 服务不可达 / 模版未同步时不阻塞启动）
//  3. 对 3 个内置容器各自检查是否存在，没有则用模版实例化
//
// 幂等：多次调用安全；任何步骤失败仅 log，不阻塞 scan 启动。
// EnsurePersonalContextForTest 给 httpd 包测试用的公开 wrapper（仅测试调用）
func EnsurePersonalContextForTest(db *sqlx.DB) error {
	return ensurePersonalFilesContext(db)
}

// EnsurePersonalContext 公开版 — 给 sync 流程（拉完模版即时建项目，无需重启）调用。
// 与 EnsurePersonalContextForTest 行为一致，名字更通用。
func EnsurePersonalContext(db *sqlx.DB) error {
	return ensurePersonalFilesContext(db)
}

// BootstrapPersonalProjects 启动后台引导：尽可能保证 3 个个人项目在 scan 可用。
//
// 流程：
//  1. 先尝试 ensurePersonalFilesContext — 若本地已有模版，直接建项目就完
//  2. 若 3 个项目仍不存在 + 配了 manage_endpoint，主动从 manage 拉
//     TPL-PERSONAL-FILES（V2.0 优先，回退 V1.0）
//  3. 拉成功后再次调 ensurePersonalFilesContext 把项目建出来
//
// 全程 best-effort：失败仅 log，不返错。设计意图是首次启动也能让用户
// 看到个人三夹，无需手动同步 + 重启的 3 步流程。
func BootstrapPersonalProjects(db *sqlx.DB) {
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()
	// Pass 1：本地若已有模版，立刻建项目
	if err := ensurePersonalFilesContext(db); err != nil {
		fmt.Printf("[bootstrap] ensure personal files context: %v\n", err)
	}
	if personalProjectsAllExist(db) {
		return
	}

	// Pass 2：模版还没有，尝试主动从 manage 拉一次
	configRepo := NewSystemConfigRepository(db)
	endpoint := strings.TrimSpace(configRepo.GetValue(KeyManageEndpoint))
	if endpoint == "" {
		fmt.Println("[bootstrap] manage_endpoint 未配置，跳过个人模版自动同步")
		return
	}
	cacheRepo := NewTemplateCacheRepository(db)
	fetcher := NewTemplateFetcher(cacheRepo, configRepo)
	tried := false
	for _, version := range []string{"V2.0", "V1.0"} {
		if _, err := fetcher.FetchByCode("TPL-PERSONAL-FILES", version); err != nil {
			fmt.Printf("[bootstrap] fetch TPL-PERSONAL-FILES %s failed: %v\n", version, err)
			continue
		}
		tried = true
		break
	}
	if !tried {
		fmt.Println("[bootstrap] 未能从 manage 拉到 TPL-PERSONAL-FILES，等用户在立项向导手动同步")
		return
	}

	// Pass 3：模版已就位，再次确保项目
	if err := ensurePersonalFilesContext(db); err != nil {
		fmt.Printf("[bootstrap] post-sync ensure personal files context: %v\n", err)
		return
	}
	if personalProjectsAllExist(db) {
		fmt.Println("[bootstrap] 已自动同步 TPL-PERSONAL-FILES 并建好 3 个个人项目")
	}
}

// personalProjectsAllExist 检查 3 个 SYS-PERSONAL-* 项目是否全部存在
func personalProjectsAllExist(db *sqlx.DB) bool {
	for _, code := range []string{PersonalCoreProjectCode, PersonalImportantProjectCode, PersonalGeneralProjectCode} {
		var id int64
		err := db.Get(&id, `SELECT id FROM data_projects WHERE project_code = ? AND disable = 0`, code)
		if err != nil || id == 0 {
			return false
		}
	}
	return true
}

func ensurePersonalFilesContext(db *sqlx.DB) error {
	// Step 1: 先检查模版是否已同步——优先 V2.0（§4.2-6 起草修改 + 定稿 双环节），
	// 回退 V1.0（旧 manage seed），都没有则直接跳过（不污染 subjects 表）。
	cacheRepo := NewTemplateCacheRepository(db)
	tpl, err := cacheRepo.FindTemplateByCode("TPL-PERSONAL-FILES", "V2.0")
	templateVersion := "V2.0"
	if err != nil || tpl == nil {
		tpl, err = cacheRepo.FindTemplateByCode("TPL-PERSONAL-FILES", "V1.0")
		templateVersion = "V1.0"
		if err != nil || tpl == nil {
			// 模版未同步，跳过本次初始化（启动后用户可点立项向导"重新同步"触发）
			return nil
		}
	}

	// Step 2: 模版已同步，确保 "本人" subject 存在
	personalSubjectID, err := ensurePersonalUserSubject(db)
	if err != nil {
		return fmt.Errorf("ensure personal user subject: %w", err)
	}

	// Step 3: 创建 3 个内置项目
	specs := []struct {
		Code      string
		Name      string
		ShortCode string
		Sens      string
	}{
		{PersonalCoreProjectCode, "个人核心级文件归目容器", "PERSONAL-CORE", SensCoreSecret},
		{PersonalImportantProjectCode, "个人重要级文件归目容器", "PERSONAL-IMPORTANT", SensImportant},
		{PersonalGeneralProjectCode, "个人一般级文件归目容器", "PERSONAL-GENERAL", SensGeneral},
	}
	for _, spec := range specs {
		if err := ensureInternalProject(db, spec.Code, spec.Name, spec.ShortCode, spec.Sens, personalSubjectID, templateVersion); err != nil {
			return fmt.Errorf("ensure internal project %s: %w", spec.Code, err)
		}
	}
	return nil
}

// ensurePersonalUserSubject 确保"本人"内置 subject 存在
func ensurePersonalUserSubject(db *sqlx.DB) (int64, error) {
	var id int64
	err := db.Get(&id, `SELECT id FROM subjects WHERE code = ? AND disable = 0`, PersonalUserSubjectCode)
	if err == nil && id > 0 {
		return id, nil
	}
	now := time.Now()
	res, err := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
		VALUES (?, ?, ?, 'active', ?, ?, 0)`,
		PersonalUserSubjectCode, "本人", "person", now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ensureInternalProject 检查并创建一个内置归目容器
//
// 内置容器特征：
//   - project_code 是系统保留前缀 SYS-*
//   - 三主体都是 "本人" subject（个人级文件不涉及部门/单位主体）
//   - status = active（绕过 draft 流程，开箱即用）
//   - project_root 为空，不创建 workspace 目录树
//   - 没有 project_members（个人归目容器无需多人协作）；
//     权限校验时 system 操作直接放行
func ensureInternalProject(db *sqlx.DB, projectCode, projectName, shortCode, sensitivity string, subjectID int64, templateVersion string) error {
	// 幂等
	var existing int64
	err := db.Get(&existing, `SELECT id FROM data_projects WHERE project_code = ? AND disable = 0`, projectCode)
	if err == nil && existing > 0 {
		return nil
	}

	// 找模版本地 id（前面已 Find 过，这里直接查；不复用避免循环依赖）
	var tplID int64
	if err := db.Get(&tplID,
		`SELECT id FROM data_templates WHERE template_code = ? AND template_version = ? AND disable = 0`,
		"TPL-PERSONAL-FILES", templateVersion); err != nil {
		return fmt.Errorf("template not synced: %w", err)
	}

	now := time.Now()
	taskSummary := "scan 端" + projectName + "，用于个人电子文件归目、标识、挂账，不默认迁移实体文件"
	taskSummaryPtr := &taskSummary
	objectShortPtr := &shortCode
	projectRoot := "" // 个人归目容器不指定 root，避免在 workspace 中建目录或暗示实体迁移。
	rootPtr := &projectRoot

	res, err := db.Exec(`INSERT INTO data_projects (
		project_code, project_name, object_short_code, template_id, template_code, template_version,
		task_summary, approval_basis, planned_start_date, planned_end_date,
		sensitivity_level, management_mode,
		owner_subject_id, custodian_subject_id, security_subject_id,
		status, project_root, created_by, created_by_user_id, create_time, update_time, disable
	) VALUES (?, ?, ?, ?, 'TPL-PERSONAL-FILES', ?, ?, NULL, NULL, NULL, ?, 'independent', ?, ?, ?, 'active', ?, 'system', NULL, ?, ?, 0)`,
		projectCode, projectName, objectShortPtr, tplID, templateVersion, taskSummaryPtr, sensitivity,
		subjectID, subjectID, subjectID, rootPtr, now, now)
	if err != nil {
		return err
	}
	projectID, _ := res.LastInsertId()

	// 复制模版 stages → project_stages
	type stageRow struct {
		ID        int64  `db:"id"`
		StageCode string `db:"stage_code"`
		StageName string `db:"stage_name"`
		StageType string `db:"stage_type"`
		SortOrder int    `db:"sort_order"`
	}
	var stages []stageRow
	if err := db.Select(&stages,
		`SELECT id, stage_code, stage_name, stage_type, sort_order FROM template_stages WHERE template_id = ? AND disable = 0 ORDER BY sort_order`,
		tplID); err != nil {
		return err
	}
	for _, s := range stages {
		_, _ = db.Exec(`INSERT INTO project_stages (project_id, template_stage_id, stage_code, stage_name, stage_type, sort_order, status, create_time, update_time, disable)
			VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?, 0)`,
			projectID, s.ID, s.StageCode, s.StageName, s.StageType, s.SortOrder, now, now)
	}

	// 复制模版 file_rules → file_versions（planned）+ 底账草稿
	type ruleRow struct {
		ID           int64  `db:"id"`
		StageID      int64  `db:"template_stage_id"`
		FileRuleCode string `db:"file_rule_code"`
		FileName     string `db:"file_name"`
		DataState    string `db:"data_state"`
		Required     int    `db:"required"`
	}
	var rules []ruleRow
	if err := db.Select(&rules,
		`SELECT id, template_stage_id, file_rule_code, file_name, data_state, required
		 FROM template_file_rules WHERE template_stage_id IN (SELECT id FROM template_stages WHERE template_id = ?)
		   AND disable = 0`,
		tplID); err != nil {
		return err
	}
	stageIDByTplID := map[int64]int64{}
	stageCodeByTplID := map[int64]string{}
	{
		// 重新查刚插的 project_stages 与 template_stage_id 的映射
		type psRow struct {
			ID        int64  `db:"id"`
			TplStage  int64  `db:"template_stage_id"`
			StageCode string `db:"stage_code"`
		}
		var pss []psRow
		_ = db.Select(&pss, `SELECT id, template_stage_id, stage_code FROM project_stages WHERE project_id = ?`, projectID)
		for _, ps := range pss {
			stageIDByTplID[ps.TplStage] = ps.ID
			stageCodeByTplID[ps.TplStage] = ps.StageCode
		}
	}
	for _, r := range rules {
		psID, ok := stageIDByTplID[r.StageID]
		if !ok {
			continue
		}
		stageCode := stageCodeByTplID[r.StageID]
		// file_version_code 编入实际 stage_code，V1.0 = GR-DA / V2.0 = GR-DRAFT|GR-FINAL
		fvCode := fmt.Sprintf("%s-%s-%s", projectCode, stageCode, r.FileRuleCode)
		ruleID := r.ID
		fvRes, err := db.Exec(`INSERT INTO file_versions (
			project_id, project_stage_id, template_file_rule_id, file_version_code, local_code,
			display_name, data_state, version_no, required, lifecycle_status,
			create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'V1.0', ?, 'planned', ?, ?, 0)`,
			projectID, psID, &ruleID, fvCode, r.FileRuleCode, r.FileName, r.DataState, r.Required, now, now)
		if err != nil {
			continue
		}
		fvID, _ := fvRes.LastInsertId()

		// 创建底账草稿
		ledgerCode, err := GenerateLedgerCode(db, now)
		if err != nil {
			continue
		}
		_, _ = db.Exec(`INSERT INTO asset_ledgers (
			ledger_code, file_version_id, project_code, stage_code, file_version_code, asset_name,
			owner_subject_id, custodian_subject_id, security_subject_id,
			sensitivity_level, marking_method, lifecycle_status,
			create_time, update_time, disable
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'reference', 'planned', ?, ?, 0)`,
			ledgerCode, fvID, projectCode, stageCode, fvCode, r.FileName,
			subjectID, subjectID, subjectID, sensitivity, now, now)

		_ = json.Marshal // 留存 json 包以备扩展
	}
	return nil
}
