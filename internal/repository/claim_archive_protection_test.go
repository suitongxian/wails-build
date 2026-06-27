package repository

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 认领归档保护·归类级别 → 文件敏感级 映射。
func TestImportanceLevelToSensitivity(t *testing.T) {
	cases := map[int]string{1: "core", 2: "important", 3: "general", 5: "", 0: "", 9: ""}
	for in, want := range cases {
		if got := ImportanceLevelToSensitivity(in); got != want {
			t.Fatalf("ImportanceLevelToSensitivity(%d)=%q，期望 %q", in, got, want)
		}
	}
}

// 方案甲：归类保护后把文件复制进本机「个人{级别}文件夹」，按 group 分组，幂等、不删原件。
func TestArchiveResourceToPersonalFolder(t *testing.T) {
	root := t.TempDir()
	// 造一个"原始扫描文件"
	srcDir := filepath.Join(root, "扫描源")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "历史台账.xlsx")
	if err := os.WriteFile(src, []byte("DATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	archived, _, errs := ArchiveResourceToPersonalFolder(root, "二季度核算", src, "important")
	if archived != 1 || len(errs) > 0 {
		t.Fatalf("应复制 1 个文件，实得 archived=%d errs=%v", archived, errs)
	}
	dst := filepath.Join(root, "个人重要文件夹", "二季度核算", "历史台账.xlsx")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("应复制到 %s：%v", dst, err)
	}
	// 原件不动
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("原始文件不应被删除/移动：%v", err)
	}
	// 复制后应能被「档案在线阅卷·个人」列出
	list := ListPersonalArchiveFiles(root, "important")
	if len(list) != 1 || list[0].FileName != "历史台账.xlsx" || list[0].ProjectName != "二季度核算" {
		t.Fatalf("应能在个人重要文件夹列出，实得 %+v", list)
	}

	// 幂等：再次复制同文件 → 跳过
	a2, s2, _ := ArchiveResourceToPersonalFolder(root, "二季度核算", src, "important")
	if a2 != 0 || s2 != 1 {
		t.Fatalf("二次应跳过（archived=0 skipped=1），实得 archived=%d skipped=%d", a2, s2)
	}

	// group 为空 → 回落「认领归档保护」
	if _, _, e := ArchiveResourceToPersonalFolder(root, "", src, "general"); len(e) > 0 {
		t.Fatalf("空 group 复制失败：%v", e)
	}
	if _, err := os.Stat(filepath.Join(root, "个人一般文件夹", "认领归档保护", "历史台账.xlsx")); err != nil {
		t.Fatalf("空 group 应回落到「认领归档保护」目录：%v", err)
	}

	// 缺路径 → 报错不复制
	if _, _, e := ArchiveResourceToPersonalFolder(root, "x", "", "core"); len(e) == 0 {
		t.Fatal("缺少源路径应返回错误")
	}
}

// GetResourceArchiveInfo 取代表性文件路径（最早入库那条）+ 工作事项/资源名。
func TestGetResourceArchiveInfo(t *testing.T) {
	db := openMigratedTestDB(t)
	defer db.Close()

	now := time.Now()
	older := now.Add(-2 * time.Hour)
	// 同 content_sign 两条 distributing，primary 取最早入库（按 data_distribution_id ASC）
	seedDist(t, db, "/data/first.docx", "sign-A", &older, 1)
	seedDist(t, db, "/data/second.docx", "sign-A", &now, 1)
	res, err := db.Exec(`
		INSERT INTO data_resources
			(content_sign, source_count, workspace_source_count, first_create_time,
			 resources_name, content_subject, claim_status, importance_level,
			 create_time, update_time, disable, data_origin)
		VALUES (?, 2, 2, ?, '历史台账', '二季度核算', 2, 0, ?, ?, 0, 'scan')`,
		"sign-A", now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	repo := NewDataResourcesRepository(db, 100)
	info, err := repo.GetResourceArchiveInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if info.PrimaryPath != "/data/first.docx" {
		t.Fatalf("primary 应取最早入库 /data/first.docx，实得 %q", info.PrimaryPath)
	}
	if info.ContentSubject != "二季度核算" || info.ResourcesName != "历史台账" {
		t.Fatalf("工作事项/资源名不符：%+v", info)
	}
}
