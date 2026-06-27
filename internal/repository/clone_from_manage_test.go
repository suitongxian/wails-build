package repository

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCloneManageTemplateToLocal 验证从 manage 克隆五层模版到本地可编辑模版：
// 结构完整重建、origin=local、core_secret→core、新 template_code。
func TestCloneManageTemplateToLocal(t *testing.T) {
	db := openTestDB(t)
	repo := NewTemplateAuthoringRepository(db)

	// mock manage GET /api/templates/authoring-tree?code=
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/authoring-tree" || r.URL.Query().Get("code") != "TPL-SRC" {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{
			"template":{"template_name":"婚姻法立法","project_sensitivity_level":"core_secret","scope":"industry","short_code":"HYF","manager":"刘主任","owner":"法工委","approval_basis":"立法计划","description":"测试"},
			"stages":[
				{"stage_name":"起草","manager":"张三","manager_username":"zhangsan","members":"李四","members_usernames":"lisi","description":"起草环节",
					"tasks":[{"task_name":"撰写草案","manager":"张三","sensitivity_level":"core_secret","description":"",
						"file_rules":[{"file_name":"草案稿","data_state":"output","required":1,"allowed_file_types":"PDF,DOCX","naming_pattern":"","drafter":"张三","sensitivity_level":"core_secret"}]}]},
				{"stage_name":"审查","manager":"王五","manager_username":"wangwu","members":"","members_usernames":"","description":"",
					"tasks":[]}
			]}}`))
	}))
	defer srv.Close()

	id, err := repo.CloneManageTemplateToLocal(srv.Client(), srv.URL, "TPL-SRC")
	if err != nil {
		t.Fatalf("克隆失败: %v", err)
	}

	// 本地模版：origin=local、新 code、core_secret→core
	var got struct {
		Origin       string `db:"origin"`
		TemplateCode string `db:"template_code"`
		Name         string `db:"template_name"`
		Sens         string `db:"project_sensitivity_level"`
	}
	if err := db.Get(&got, `SELECT origin, template_code, template_name, project_sensitivity_level FROM data_templates WHERE id = ?`, id); err != nil {
		t.Fatalf("查本地模版失败: %v", err)
	}
	if got.Origin != "local" {
		t.Fatalf("克隆体应 origin=local，实得 %s", got.Origin)
	}
	if got.Name != "婚姻法立法" {
		t.Fatalf("名称应保留，实得 %s", got.Name)
	}
	if got.Sens != "core" {
		t.Fatalf("core_secret 应映射回 core，实得 %s", got.Sens)
	}

	// 结构：2 环节，第一个环节 1 任务 1 标识
	var stageN int
	db.Get(&stageN, `SELECT COUNT(*) FROM template_stages WHERE template_id = ? AND disable = 0`, id)
	if stageN != 2 {
		t.Fatalf("应克隆 2 个环节，实得 %d", stageN)
	}
	var ruleN int
	db.Get(&ruleN, `SELECT COUNT(*) FROM template_file_rules fr
		JOIN template_stages s ON s.id = fr.template_stage_id
		WHERE s.template_id = ? AND fr.disable = 0`, id)
	if ruleN != 1 {
		t.Fatalf("应克隆 1 个文档标识，实得 %d", ruleN)
	}
	// 人员 username 保留
	var mgrU string
	db.Get(&mgrU, `SELECT manager_username FROM template_stages WHERE template_id = ? AND stage_name = '起草'`, id)
	if mgrU != "zhangsan" {
		t.Fatalf("环节责任人 username 应保留，实得 %s", mgrU)
	}
}
