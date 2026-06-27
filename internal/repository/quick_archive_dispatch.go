package repository

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// ProjectArchiveCtx 一键归档所需的项目上下文。
type ProjectArchiveCtx struct {
	ProjectCode string `db:"project_code"`
	ProjectName string `db:"project_name"`
	Scope       string `db:"scope"`       // 来自模版：industry/unit/department/person
	Sensitivity string `db:"sensitivity"` // 项目敏感级别 core/important/general
}

// GetProjectArchiveContext 按项目 id 取归档上下文：项目编码/名称/敏感级 + 模版 scope。
func GetProjectArchiveContext(db *sqlx.DB, projectID int64) (*ProjectArchiveCtx, error) {
	var ctx ProjectArchiveCtx
	err := db.Get(&ctx, `
		SELECT p.project_code, p.project_name,
		       COALESCE(p.sensitivity_level,'') AS sensitivity,
		       COALESCE(t.scope,'') AS scope
		FROM data_projects p
		LEFT JOIN data_templates t
		       ON t.template_code = p.template_code AND t.template_version = p.template_version
		WHERE p.id = ? AND p.disable = 0`, projectID)
	if err != nil {
		return nil, err
	}
	return &ctx, nil
}

// IsPersonalSystemProject 个人内置系统容器（SYS-PERSONAL-*）不参与一键归档。
func IsPersonalSystemProject(projectCode string) bool {
	return strings.HasPrefix(projectCode, "SYS-PERSONAL-")
}

// ArchiveProjectByScope 一键归档总入口：按【桶】分流（不是按整项目分流）。
//   - 工作依据(input)：不归档
//   - 参考(reference)、过程(process)：一律复制到本机个人夹（按文件级别 → 个人{保密/档案/资料}夹）
//   - 定稿(output)：个人(person)→本地个人夹；部门/单位→上报云端柜/室；行业/未知→跳过（无柜室）
//
// 定稿保管层级规则（2026-06 调整）：单位级项目的定稿统一归「部门柜」，不再区分单位室/部门柜，
//   也不再由用户在承接时选择。outputCustodyScope 参数保留仅为向后兼容，已不影响路由。
// custodyNote：归档归属说明（选填），随定稿上报，云端柜室列表展示。
// 因此一个部门/单位项目会同时产生：本地个人夹副本(参考/过程) + 云端上报(定稿)。
func ArchiveProjectByScope(db *sqlx.DB, root, projectCode, projectName, scope, sensitivity, operator, outputCustodyScope, custodyNote string) (*QuickArchiveResult, error) {
	res := &QuickArchiveResult{Route: RouteLocal}
	if root == "" {
		return res, fmt.Errorf("未配置工作空间目录")
	}
	ws := NewProjectWorkspace(root)
	items := collectArchivableFiles(ws, projectCode, sensitivity)

	// 定稿实际保管层级：单位级项目定稿统一归「部门柜」（不再区分单位室/部门柜）；其余层级按项目层级。
	_ = outputCustodyScope // 已废弃，不再据此区分室/柜（保留入参仅为向后兼容）
	effOutputScope := scope
	if scope == "unit" {
		effOutputScope = "department"
	}
	outRoute, _, _ := ScopeRoute(effOutputScope)

	// 分桶：参考/过程 → 个人夹；定稿 → 看定稿保管层级
	var localItems, cloudItems []archiveItem
	for _, it := range items {
		if it.Bucket == "output" {
			switch outRoute {
			case RouteLocal:
				localItems = append(localItems, it) // 个人级定稿 → 个人夹
			case RouteCloud:
				cloudItems = append(cloudItems, it) // 部门/单位定稿 → 上报柜/室
				// RouteSkip(行业)：定稿无柜室，丢弃
			}
		} else {
			localItems = append(localItems, it) // 参考/过程 一律入个人夹
		}
	}

	// 本地：参考/过程(+个人级定稿) → 个人夹（默认落工作空间根，可由 personal_archive_root 覆盖）
	personalRoot := root
	if len(localItems) > 0 {
		if v := strings.TrimSpace(NewSystemConfigRepository(db).GetValue(KeyPersonalArchiveRoot)); v != "" {
			personalRoot = v
		}
		la, ls, lerr := copyItemsToPersonalFolders(personalRoot, projectName, localItems)
		res.Archived += la
		res.Skipped += ls
		res.Errors = append(res.Errors, lerr...)
	}

	// 云端：定稿(部门/单位) → 柜/室（用定稿实际保管层级 effOutputScope 定柜室）
	cloudN := 0
	if len(cloudItems) > 0 {
		payload := buildCloudPayloadFromItems(projectCode, projectName, effOutputScope, sensitivity, operator, cloudItems)
		payload.CustodyNote = strings.TrimSpace(custodyNote)
		if _, err := uploadQuickArchiveCloud(db, payload); err != nil {
			res.Errors = append(res.Errors, err.Error())
		} else {
			cloudN = len(cloudItems)
			res.Archived += cloudN
		}
	}

	// 结果文案
	switch {
	case cloudN > 0:
		res.Route = RouteCloud
		res.RouteTip = fmt.Sprintf("参考/过程入个人夹，定稿上报云端 %d 个", cloudN)
	default:
		res.Route = RouteLocal
		res.RouteTip = "已归档到本机个人夹：" + personalRoot
	}
	return res, nil
}
