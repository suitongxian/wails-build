package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

// seedTestTemplate 在测试 DB 中写入一个最简书目印刷模版（2 个环节，少量文件规则）
func seedTestTemplate(t *testing.T, db *sqlx.DB) (templateCode, templateVersion string) {
	t.Helper()
	cache := NewTemplateCacheRepository(db)

	// business class
	desc := "印刷"
	if err := cache.SaveBusinessClass(1, "C23", "出版印刷", "industry", &desc); err != nil {
		t.Fatal(err)
	}

	// template
	classCode := "C23"
	scenario := "图书印刷"
	publisher := "provider"
	endpoint := "http://test"
	tplID, err := cache.SaveTemplate(SaveTemplateInput{
		RemoteID:                1,
		TemplateCode:            "TPL-PRINT-BOOK",
		TemplateName:            "书目印刷",
		TemplateVersion:         "V2.1",
		ClassCode:               &classCode,
		Scenario:                &scenario,
		Publisher:               &publisher,
		Status:                  "active",
		ProjectSensitivityLevel: "important",
		SourceEndpoint:          &endpoint,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2 个环节
	sgID, err := cache.SaveTemplateStage(SaveTemplateStageInput{
		TemplateID: tplID, StageCode: "MZ-SG", StageName: "收稿登记", StageType: "process", SortOrder: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	pbID, err := cache.SaveTemplateStage(SaveTemplateStageInput{
		TemplateID: tplID, StageCode: "MZ-PB", StageName: "排版", StageType: "process", SortOrder: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 文件规则
	rules := []SaveTemplateFileRuleInput{
		{TemplateStageID: sgID, FileRuleCode: "IN-001", FileName: "客户原稿", DataState: "input", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 1},
		{TemplateStageID: sgID, FileRuleCode: "OUT-001", FileName: "收稿凭证", DataState: "output", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 2},
		{TemplateStageID: pbID, FileRuleCode: "IN-001", FileName: "原稿文件", DataState: "input", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 1},
		{TemplateStageID: pbID, FileRuleCode: "PRC-001", FileName: "排版临时文件", DataState: "process", Required: 0, AllowedFileTypes: `["PSD"]`, SortOrder: 2},
		{TemplateStageID: pbID, FileRuleCode: "OUT-001", FileName: "排版完成稿", DataState: "output", Required: 1, AllowedFileTypes: `["PDF"]`, SortOrder: 3},
	}
	for _, r := range rules {
		if _, err := cache.SaveTemplateFileRule(r); err != nil {
			t.Fatal(err)
		}
	}

	return "TPL-PRINT-BOOK", "V2.1"
}

func seedTestSubjects(t *testing.T, db *sqlx.DB) (owner, custodian, security int64) {
	t.Helper()
	repo := NewSubjectRepository(db)
	o, _ := repo.Create(CreateSubjectInput{Code: "U-PM", Name: "项目经理", Type: "person"})
	c, _ := repo.Create(CreateSubjectInput{Code: "D-EDIT", Name: "编辑部", Type: "department"})
	s, _ := repo.Create(CreateSubjectInput{Code: "D-SEC", Name: "合规部", Type: "department"})
	return o.ID, c.ID, s.ID
}

func TestInstantiate_HappyPath(t *testing.T) {
	db := openTestDB(t)

	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)

	tmpRoot := t.TempDir()
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, tmpRoot)

	svc := NewProjectInstantiationService(db)
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "明朝那些事儿印刷计划",
		ObjectShortCode:    "MC-NSXS",
		TaskSummary:        "印刷《明朝那些事儿》共计 5000 册",
		ApprovalBasis:      "HT-2024-0088",
		SensitivityLevel:   "important",
		ManagementMode:     "independent",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		CreatedBy:          "tester",
		Activate:           true,
		Members: []MemberInput{
			{
				SubjectID:         owner,
				RoleCode:          "项目经理",
				StageCodes:        []string{"MZ-SG", "MZ-PB"},
				PermissionActions: []string{"read", "write", "submit", "archive", "close"},
			},
		},
	})
	if err != nil {
		t.Fatalf("instantiate failed: %v", err)
	}

	// 1. 项目编码符合格式
	if !strings.HasPrefix(out.Project.ProjectCode, "MC-NSXS-") {
		t.Fatalf("wrong project code: %s", out.Project.ProjectCode)
	}
	if !strings.HasSuffix(out.Project.ProjectCode, "-001") {
		t.Fatalf("first project should end with -001: %s", out.Project.ProjectCode)
	}

	// 2. 模版版本锁定
	if out.Project.TemplateCode != tplCode || out.Project.TemplateVersion != tplVer {
		t.Fatalf("template lock failed: %s %s", out.Project.TemplateCode, out.Project.TemplateVersion)
	}

	// 3. 安全等级符合就高不就低
	if out.Project.SensitivityLevel != "important" {
		t.Fatalf("sensitivity expected important, got %s", out.Project.SensitivityLevel)
	}

	// 4. 工作环节都被实例化
	if len(out.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(out.Stages))
	}

	// 5. 文件版本数量匹配
	totalFV := 0
	for _, s := range out.Stages {
		totalFV += len(s.FileVersions)
	}
	if totalFV != 5 {
		t.Fatalf("expected 5 file versions, got %d", totalFV)
	}

	// 6. file_version_code 格式正确
	for _, s := range out.Stages {
		for _, fv := range s.FileVersions {
			expected := out.Project.ProjectCode + "-" + s.StageCode + "-" + fv.LocalCode
			if fv.FileVersionCode != expected {
				t.Fatalf("wrong fv code: got %s, expected %s", fv.FileVersionCode, expected)
			}
			if fv.LifecycleStatus != "planned" {
				t.Fatalf("expected planned status, got %s", fv.LifecycleStatus)
			}
		}
	}

	// 7. 底账数量匹配（每个 file_version 一条）
	ledgerRepo := NewAssetLedgerRepository(db)
	ledgers, err := ledgerRepo.Search(LedgerSearchInput{ProjectCode: out.Project.ProjectCode})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledgers) != 5 {
		t.Fatalf("expected 5 ledgers, got %d", len(ledgers))
	}
	// 底账三主体均填
	for _, l := range ledgers {
		if l.OwnerSubjectID != owner || l.CustodianSubjectID != custodian || l.SecuritySubjectID != security {
			t.Fatalf("ledger %s has wrong subjects", l.LedgerCode)
		}
	}

	// 8. 项目目录树创建
	projectDir := filepath.Join(tmpRoot, "项目文件管理", out.Project.ProjectCode)
	expectedDirs := []string{
		projectDir,
		// 2026-06-02 不再建 metadata/ archive/ 占位目录，只建 stages/
		filepath.Join(projectDir, "stages", "MZ-SG", "input"),
		filepath.Join(projectDir, "stages", "MZ-SG", "output"),
		filepath.Join(projectDir, "stages", "MZ-PB", "input"),
		filepath.Join(projectDir, "stages", "MZ-PB", "process"),
		filepath.Join(projectDir, "stages", "MZ-PB", "output"),
	}
	for _, d := range expectedDirs {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatalf("missing directory %s: %v", d, err)
		}
		if !st.IsDir() {
			t.Fatalf("not a dir: %s", d)
		}
	}

	// 9. project_stages 写入了 directory_path
	for _, s := range out.Stages {
		if s.DirectoryPath == nil || !strings.Contains(*s.DirectoryPath, s.StageCode) {
			t.Fatalf("stage %s missing directory_path", s.StageCode)
		}
	}

	// 10. 成员权限写入
	if len(out.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(out.Members))
	}
	memberRepo := NewProjectMemberRepository(db)
	hasClose, _ := memberRepo.HasCloseAuthority(out.Project.ID)
	if !hasClose {
		t.Fatal("expected close authority present")
	}
}

func TestInstantiate_HighSecurity_OverridesTemplate(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db) // template is important
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	// 用户填 core_secret（高于模版的 important），最终应取 core_secret
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "P",
		ObjectShortCode:    "MC-A",
		SensitivityLevel:   "core_secret",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"close"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Project.SensitivityLevel != "core_secret" {
		t.Fatalf("expected core_secret, got %s", out.Project.SensitivityLevel)
	}
}

func TestInstantiate_LowerSecurity_StillTakesTemplate(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db) // template is important
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	// 用户填 general（低于模版 important），最终应仍是 important
	out, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "P",
		ObjectShortCode:    "MC-B",
		SensitivityLevel:   "general",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"close"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Project.SensitivityLevel != "important" {
		t.Fatalf("expected important (template baseline), got %s", out.Project.SensitivityLevel)
	}
}

func TestInstantiate_RejectsInternalSensitivity(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	_, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "P",
		ObjectShortCode:    "MC-IN",
		SensitivityLevel:   "internal",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"close"}},
		},
	})
	if err == nil {
		t.Fatal("expected internal sensitivity to be rejected")
	}
	if !strings.Contains(err.Error(), "sensitivity_level 非法") {
		t.Fatalf("expected invalid sensitivity error, got %v", err)
	}
}

func TestInstantiate_RequiresCloseAuthority(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	_, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "P",
		ObjectShortCode:    "MC-NOPE",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			// 没有 close 权限
			{SubjectID: owner, RoleCode: "USER", PermissionActions: []string{"read", "write"}},
		},
	})
	if err == nil {
		t.Fatal("expected error when no member has close authority")
	}
	if !strings.Contains(err.Error(), "close") {
		t.Fatalf("expected close-authority error, got %v", err)
	}
}

func TestInstantiate_TemplateNotActive(t *testing.T) {
	db := openTestDB(t)
	cache := NewTemplateCacheRepository(db)
	// 模版状态为 draft
	if _, err := cache.SaveTemplate(SaveTemplateInput{
		TemplateCode:            "TPL-DRAFT",
		TemplateName:            "草稿",
		TemplateVersion:         "V0.1",
		Status:                  "draft",
		ProjectSensitivityLevel: "general",
	}); err != nil {
		t.Fatal(err)
	}
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	_, err := svc.Instantiate(InstantiateInput{
		TemplateCode:       "TPL-DRAFT",
		TemplateVersion:    "V0.1",
		ProjectName:        "P",
		ObjectShortCode:    "MC-D",
		SensitivityLevel:   "general",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"close"}},
		},
	})
	if err == nil {
		t.Fatal("expected error for draft template")
	}
}

func TestInstantiate_Idempotent_DifferentProjects(t *testing.T) {
	db := openTestDB(t)
	tplCode, tplVer := seedTestTemplate(t, db)
	owner, custodian, security := seedTestSubjects(t, db)
	configRepo := NewSystemConfigRepository(db)
	configRepo.SetValue(KeyProjectRoot, t.TempDir())

	svc := NewProjectInstantiationService(db)
	mk := func(short string) string {
		out, err := svc.Instantiate(InstantiateInput{
			TemplateCode:       tplCode,
			TemplateVersion:    tplVer,
			ProjectName:        "P-" + short,
			ObjectShortCode:    short,
			SensitivityLevel:   "important",
			OwnerSubjectID:     owner,
			CustodianSubjectID: custodian,
			SecuritySubjectID:  security,
			Activate:           true,
			Members: []MemberInput{
				{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"close"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return out.Project.ProjectCode
	}
	c1 := mk("MC-A")
	c2 := mk("MC-A")
	if c1 == c2 {
		t.Fatal("two projects with same short_code should have different sequence numbers")
	}
	if !strings.HasSuffix(c1, "-001") || !strings.HasSuffix(c2, "-002") {
		t.Fatalf("expected -001 and -002, got %s and %s", c1, c2)
	}
}
