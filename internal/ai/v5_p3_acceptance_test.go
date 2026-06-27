package ai

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestV5P3_EndToEnd V5-P3 综合验收：
// 模拟一个真实的归目场景，验证 enricher + 评分新维度联动。
//
// 场景：用户有一份 .docx 报告文件，目录里还有同类文件（sibling），
// 系统应识别 mime + 路径 + 文件名为 OUT-001 "审校意见" 高置信度归目。
func TestV5P3_EndToEnd(t *testing.T) {
	db := setupTestDBForEnricher(t)
	defer db.Close()

	now := time.Now()
	// 主资源
	r, _ := db.Exec(`INSERT INTO data_resources (
		content_sign, source_count, workspace_source_count, first_create_time,
		resources_name, resources_desc, claim_status, importance_level, create_time, update_time, disable
	) VALUES ('E2E_P3_001', 1, 1, ?, '审校意见.docx', '本文件是审校意见初稿', 2, 0, ?, ?, 0)`, now, now, now)
	resID, _ := r.LastInsertId()
	db.Exec(`INSERT INTO data_distributing (
		scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
		file_size, ip, mac_address, scan_time, create_time, update_time, disable
	) VALUES (1, '/work/审校/审校意见.docx', 1, 1, 'E2E_P3_001', 'docx',
		'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
		20480, '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`, now, now, now)

	// 同目录 sibling 6 个（≥5 触发聚合目录信号）
	for i := 1; i <= 6; i++ {
		sign := "SIB_P3_" + string(rune('0'+i))
		db.Exec(`INSERT INTO data_distributing (
			scan_task_id, path, data_type, scan_found_count, content_sign, file_suffix, file_magic,
			file_size, ip, mac_address, scan_time, create_time, update_time, disable
		) VALUES (1, ?, 1, 1, ?, 'docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
			10240, '127.0.0.1', 'aa:bb', ?, ?, ?, 0)`,
			"/work/审校/sib-"+string(rune('0'+i))+".docx", sign, now, now, now)
	}

	// enrich
	in, err := EnrichInputForResource(db, resID)
	if err != nil {
		t.Fatalf("enrich failed: %v", err)
	}
	if in.FileName != "审校意见.docx" {
		t.Errorf("FileName wrong: %s", in.FileName)
	}
	if in.Path != "/work/审校/审校意见.docx" {
		t.Errorf("Path wrong: %s", in.Path)
	}
	if in.Metadata["mime"] != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("mime missing: %s", in.Metadata["mime"])
	}
	if in.Metadata["ext"] != "docx" {
		t.Errorf("ext wrong: %s", in.Metadata["ext"])
	}
	if in.Metadata["sibling_count"] != "6" {
		t.Errorf("sibling_count should be 6, got %s", in.Metadata["sibling_count"])
	}
	if in.Summary != "本文件是审校意见初稿" {
		t.Errorf("Summary should be picked up from resources_desc, got %s", in.Summary)
	}

	// classify
	cat := makeCatalog(ProjectStageRuleSnapshot{
		ProjectID: 1, ProjectCode: "SYS-PERSONAL-IMPORTANT", ProjectName: "个人重要级",
		StageCode: "GR-FINAL", StageName: "个人文件定稿",
		FileRuleCode: "OUT-001", FileName: "审校意见", DataState: "output",
		AllowedFileTypes: []string{"docx"},
	})
	a := NewRuleBasedClassifyAdapter(cat)
	sug, err := a.Classify(context.Background(), in)
	if err != nil {
		t.Fatalf("classify failed: %v", err)
	}
	if len(sug) == 0 {
		t.Fatal("expected suggestions")
	}

	top := sug[0]
	if top.Confidence < 0.50 {
		t.Errorf("multi-dimension match should yield > 0.50 confidence, got %.2f (reason: %s)",
			top.Confidence, top.Reason)
	}
	// reason 至少含一个新维度命中
	if !strings.Contains(top.Reason, "正文含") && !strings.Contains(top.Reason, "MIME") && !strings.Contains(top.Reason, "聚合目录") {
		t.Errorf("reason should mention at least one new dimension, got: %s", top.Reason)
	}
}
