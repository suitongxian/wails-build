package httpd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_CentralizedProjects_CreateAndList(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三") // 负责人必须已注册

	// 创建一条（带定数权）
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":      "客户合同管理项目",
		"data_owner":        "第一研究院",
		"owner_name":        "张三",
		"sensitivity_level": "important",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["id"] == nil {
		t.Error("create response should contain id")
	}
	// 2026-06-02 去审核：立项即 approved（可承接），不再是 pending
	if d["status"] != "approved" {
		t.Errorf("status = %v, want approved", d["status"])
	}

	// 列表应能看到，且带定数权
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	if len(items) < 1 {
		t.Fatalf("list items = %d, want ≥ 1", len(items))
	}
	first := items[0].(map[string]interface{})
	if first["project_name"] != "客户合同管理项目" || first["owner_name"] != "张三" {
		t.Errorf("first item content mismatch: %+v", first)
	}
	if first["data_owner"] != "第一研究院" {
		t.Errorf("data_owner = %v, want 第一研究院", first["data_owner"])
	}
	if first["status"] != "approved" {
		t.Errorf("list status = %v, want approved", first["status"])
	}
}

func TestHTTP_CentralizedProjects_ListIsolatedByCurrentSubmitter(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	// 先种 owner-a 避免 GetActiveUser (id DESC) 把它当成活跃用户
	seedRegisteredUser(t, db, "owner-a")
	withActiveUser(t, db, "alice")

	// alice 提交一条
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":      "alice 的项目",
		"owner_name":        "owner-a",
		"sensitivity_level": "general",
	})
	successOk(t, status, resp)

	// 模拟另一台终端 bob 提交的条目（直接 SQL 插）
	_, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, submitted_by, status, sync_status, create_time, update_time, disable)
		VALUES ('bob 的项目', 'owner-b', 'bob', 'pending', 'synced', datetime('now'), datetime('now'), 0)`)
	if err != nil {
		t.Fatal(err)
	}

	// alice 视角：只能看到自己的一条
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects")
	successOk(t, status, resp)
	items, _ := dataMap(t, resp)["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("alice should see 1 item, got %d", len(items))
	}
	if first := items[0].(map[string]interface{}); first["submitted_by"] != "alice" {
		t.Errorf("submitted_by = %v, want alice", first["submitted_by"])
	}
}

func TestHTTP_CentralizedProjects_RejectsEmpty(t *testing.T) {
	r, _, cleanup := setupTestServer(t)
	defer cleanup()
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "",
		"owner_name":   "",
	})
	expectFailure(t, status, resp)
}

func TestHTTP_CentralizedProjects_RejectsMissingSensitivity(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	seedRegisteredUser(t, db, "张三")
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name": "缺敏感等级项目",
		"owner_name":   "张三",
	})
	expectFailure(t, status, resp)
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "敏感等级") {
		t.Errorf("expected '敏感等级' in error: %v", msg)
	}
}

func TestHTTP_CentralizedProjects_RejectsUnregisteredOwner(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")
	// 故意不 seed 这个 owner — 应被拒
	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":      "x",
		"owner_name":        "不存在的用户",
		"sensitivity_level": "general",
	})
	expectFailure(t, status, resp)
	if errMsg, _ := resp["error"].(string); !strings.Contains(errMsg, "未在系统中注册") {
		t.Errorf("expected '未在系统中注册' in error, got: %v", errMsg)
	}
}

func TestHTTP_CentralizedProjects_PushesToManageWhenEndpointConfigured(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	// mock manage 端 /api/centralized-projects/submit
	var receivedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/centralized-projects/submit" {
			http.NotFound(w, req)
			return
		}
		_ = json.NewDecoder(req.Body).Decode(&receivedBody)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    0,
			"message": "success",
			"data":    map[string]interface{}{"id": 999, "status": "pending"},
		})
	}))
	defer srv.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)
	seedRegisteredUser(t, db, "李四") // 负责人必须已注册

	status, resp := jsonReq(t, r, "POST", "/centralized-projects", map[string]interface{}{
		"project_name":      "推送测试项目",
		"data_owner":        "档案处",
		"owner_name":        "李四",
		"sensitivity_level": "core",
	})
	successOk(t, status, resp)

	d := dataMap(t, resp)
	if d["sync_status"] != "synced" {
		t.Errorf("sync_status = %v, want synced; resp=%+v", d["sync_status"], resp)
	}
	if receivedBody == nil {
		t.Fatal("manage 端没收到推送")
	}
	if receivedBody["project_name"] != "推送测试项目" {
		t.Errorf("project_name 没有按预期推送: %v", receivedBody)
	}
	if receivedBody["data_owner"] != "档案处" {
		t.Errorf("data_owner should be propagated: %v", receivedBody)
	}
	if receivedBody["sensitivity_level"] != "core" {
		t.Errorf("sensitivity_level should be propagated: %v", receivedBody)
	}

	// 本地的 manage_remote_id 应被回写
	var remoteID int64
	db.Get(&remoteID, `SELECT COALESCE(manage_remote_id, 0) FROM centralized_project_applications ORDER BY id DESC LIMIT 1`)
	if remoteID != 999 {
		t.Errorf("manage_remote_id = %d, want 999", remoteID)
	}
}

func TestHTTP_CentralizedProjects_AssignedAndAccept(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lisi")

	// mock manage：assigned 调 list?status=approved&owner_name=lisi 返一条；accept 调 OK
	var lastAcceptID string
	var lastAcceptBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/centralized-projects/list":
			status := req.URL.Query().Get("status")
			owner := req.URL.Query().Get("owner_name")
			if owner != "lisi" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []any{}})
				return
			}
			if status == "approved" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"code": 0,
					"data": []map[string]interface{}{
						{"id": 7, "project_name": "市场分析项目", "owner_name": "lisi", "status": "approved"},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []any{}})
		case "/api/templates/ingest":
			// accept 现在也会先把模版结构 ingest 进 manage，返回 manage 侧 id=99
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]interface{}{"template_id": 99}})
		case "/api/centralized-projects/accept":
			lastAcceptID = req.URL.Query().Get("id")
			_ = json.NewDecoder(req.Body).Decode(&lastAcceptBody)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "data": map[string]interface{}{"id": 7, "status": "accepted"},
			})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	// accept 会 ingest 所选模版，先建一个最简本地模版供其读取
	authRepo := repository.NewTemplateAuthoringRepository(db)
	localTpl, err := authRepo.CreateLocalTemplate(repository.CreateTemplateInput{
		Scope: "unit", ClassCode: "IND-001", TemplateName: "测试模版", ShortCode: "TST",
		Manager: "lisi", Owner: "院", SensitivityLevel: "general", ApprovalBasis: "x", Description: "d",
	})
	if err != nil {
		t.Fatalf("建本地模版失败: %v", err)
	}
	st, err := authRepo.CreateStage(localTpl.ID, repository.StageInput{Name: "立项", Manager: "lisi"})
	if err != nil {
		t.Fatalf("建环节失败: %v", err)
	}
	if _, err := authRepo.CreateTask(st.ID, repository.TaskInput{Name: "录入", SensitivityLevel: "general"}); err != nil {
		t.Fatalf("建任务失败: %v", err)
	}

	// assigned: 应返 1 条
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/assigned")
	successOk(t, status, resp)
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("assigned items = %d, want 1", len(list))
	}
	first := list[0].(map[string]interface{})
	if first["project_name"] != "市场分析项目" {
		t.Errorf("first project = %v", first)
	}

	// accept: 应转发到 manage 并 success，携带 template + stage_assignments
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/7/accept", map[string]interface{}{
		"template_id":      localTpl.ID,
		"template_code":    localTpl.TemplateCode,
		"template_version": localTpl.TemplateVersion,
		"source":           "local",
		"stage_assignments": []map[string]interface{}{
			{"stage_code": "GR-01", "stage_name": "立项", "assignee_username": "u1"},
			{"stage_code": "GR-02", "stage_name": "采集", "assignee_username": "u2"},
		},
	})
	successOk(t, status, resp)
	if lastAcceptID != "7" {
		t.Errorf("manage accept id = %q, want 7", lastAcceptID)
	}
	if lastAcceptBody["acceptor"] != "lisi" {
		t.Errorf("acceptor = %v, want lisi", lastAcceptBody["acceptor"])
	}
	// accept 应把 template_id 换成 manage 侧 id=99（不是 scan 本地 id），否则任务层查不到
	if tid, _ := lastAcceptBody["template_id"].(float64); int(tid) != 99 {
		t.Errorf("accept 应使用 manage 侧 template_id=99，实得 %v", lastAcceptBody["template_id"])
	}
	if sas, _ := lastAcceptBody["stage_assignments"].([]interface{}); len(sas) != 2 {
		t.Errorf("stage_assignments count = %d, want 2", len(sas))
	}
}

func TestHTTP_CentralizedProjects_MyStages_AndStartStage(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")

	var lastStartActor string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/centralized-projects/my-stages":
			if req.URL.Query().Get("assignee") != "u2" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 1, "stage_code": "GR-01", "stage_name": "立项", "project_name": "P1", "status": "pending", "assignee_username": "u2"},
				},
			})
		case "/api/centralized-projects/application-stages":
			// MyStages 用例只有一个 stage（无前置），放行
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 1, "stage_code": "GR-01", "stage_name": "立项", "sort_order": 0, "assignee_username": "u2", "status": "pending"},
				},
			})
		case "/api/centralized-projects/start-stage":
			var b map[string]interface{}
			_ = json.NewDecoder(req.Body).Decode(&b)
			lastStartActor, _ = b["actor"].(string)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "data": map[string]interface{}{"id": 1, "status": "in_progress"},
			})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)
	// 启动 stage 需要 project_root 已设置
	tmpRoot := t.TempDir()
	cfg.SetValue(repository.KeyProjectRoot, tmpRoot)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/my-stages")
	successOk(t, status, resp)
	if list, _ := resp["data"].([]interface{}); len(list) != 1 {
		t.Fatalf("my-stages items = %d, want 1", len(list))
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/stages/1/start", map[string]interface{}{
		"application_id":  100,
		"stage_code":      "GR-01",
		"all_stage_codes": []string{"GR-01", "GR-02"},
	})
	successOk(t, status, resp)
	if lastStartActor != "u2" {
		t.Errorf("actor sent to manage = %q, want u2", lastStartActor)
	}
	d := dataMap(t, resp)
	if d["virtual_project_code"] != "CPA-100" {
		t.Errorf("virtual_project_code = %v, want CPA-100", d["virtual_project_code"])
	}
	// 目录树应实际建出来
	stageDir, _ := d["stage_dir"].(string)
	if stageDir == "" || !strings.Contains(stageDir, "CPA-100") {
		t.Errorf("stage_dir = %q, expect contains CPA-100", stageDir)
	}
	if _, err := os.Stat(stageDir); err != nil {
		t.Errorf("stage_dir not created on disk: %v", err)
	}
}

func TestHTTP_CentralizedProjects_StartStage_BlockedByUncompletedPrereq(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/centralized-projects/application-stages" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 1, "stage_code": "GR-01", "stage_name": "立项", "sort_order": 0, "assignee_username": "u1", "status": "in_progress"},
					{"id": 2, "stage_code": "GR-02", "stage_name": "采集", "sort_order": 1, "assignee_username": "u2", "status": "pending"},
				},
			})
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	// u2 想启动 GR-02，但前置 GR-01 还在 in_progress → 应被拒
	status, resp := jsonReq(t, r, "POST", "/centralized-projects/stages/2/start", map[string]interface{}{
		"application_id": 50,
		"stage_code":     "GR-02",
	})
	expectFailure(t, status, resp)
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "前置环节") {
		t.Errorf("expected error to mention 前置环节, got: %v", msg)
	}
}

func TestHTTP_CentralizedProjects_StartStage_AllowedAfterPrereqCompleted(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, t.TempDir())

	startCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/centralized-projects/application-stages":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 1, "stage_code": "GR-01", "stage_name": "立项", "sort_order": 0, "assignee_username": "u1", "status": "completed"},
					{"id": 2, "stage_code": "GR-02", "stage_name": "采集", "sort_order": 1, "assignee_username": "u2", "status": "pending"},
				},
			})
		case "/api/centralized-projects/start-stage":
			startCalled = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]interface{}{"id": 2, "status": "in_progress"}})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/stages/2/start", map[string]interface{}{
		"application_id": 51, "stage_code": "GR-02",
	})
	successOk(t, status, resp)
	if !startCalled {
		t.Error("start-stage 应被调用")
	}
}

func TestHTTP_CentralizedProjects_StartStage_RequiresProjectRoot(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, "http://placeholder")
	// 故意不设 workspace（项目根已与 workspace 合并）—— 应该被拒
	status, resp := jsonReq(t, r, "POST", "/centralized-projects/stages/1/start", map[string]interface{}{
		"application_id": 1, "stage_code": "GR-01",
	})
	expectFailure(t, status, resp)
	if msg, _ := resp["error"].(string); !strings.Contains(msg, "工作空间目录") {
		t.Errorf("expected error about 工作空间目录, got: %v", msg)
	}
}

func TestHTTP_CentralizedProjects_DeliverStage_CopiesOutputToNextInput(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")
	tmpRoot := t.TempDir()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, tmpRoot)

	// 五层落盘：建一个含两环节、各一文件任务的模版。
	repo := repository.NewTemplateAuthoringRepository(db)
	tpl, _ := repo.CreateLocalTemplate(repository.CreateTemplateInput{Scope: "unit", TemplateName: "交付链", SensitivityLevel: "general"})
	st1, _ := repo.CreateStage(tpl.ID, repository.StageInput{Name: "采集"})
	tk1, _ := repo.CreateTask(st1.ID, repository.TaskInput{Name: "采集任务", SensitivityLevel: "general"})
	st2, _ := repo.CreateStage(tpl.ID, repository.StageInput{Name: "整理"})
	tk2, _ := repo.CreateTask(st2.ID, repository.TaskInput{Name: "整理任务", SensitivityLevel: "general"})

	// 当前环节(st1)的文件任务(tk1) output/ 放两个定稿，模拟已产出。
	ws := repository.NewProjectWorkspace(tmpRoot)
	projectCode := "CPA-200"
	if _, err := ws.CreateTaskDir(projectCode, st1.StageCode, tk1.TaskCode); err != nil {
		t.Fatal(err)
	}
	outDir := ws.TaskStateDir(projectCode, st1.StageCode, tk1.TaskCode, "output")
	for _, name := range []string{"产出A.txt", "产出B.txt"} {
		f, _ := os.Create(outDir + "/" + name)
		f.WriteString("demo")
		f.Close()
	}

	var completedActor string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/centralized-projects/application-stages":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 1, "stage_code": st1.StageCode, "stage_name": "采集", "sort_order": 0, "assignee_username": "u2"},
					{"id": 2, "stage_code": st2.StageCode, "stage_name": "整理", "sort_order": 1, "assignee_username": "u3"},
				},
			})
		case "/api/centralized-projects/complete-stage":
			var b map[string]interface{}
			_ = json.NewDecoder(req.Body).Decode(&b)
			completedActor, _ = b["actor"].(string)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "data": map[string]interface{}{"id": 1, "status": "completed"},
			})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/stages/1/deliver", map[string]interface{}{
		"application_id":     200,
		"current_stage_code": st1.StageCode,
		"template_code":      tpl.TemplateCode,
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	// 2 个定稿 × 下一环节 1 个任务 = 2 份工作依据
	if dc, _ := d["delivered_count"].(float64); int(dc) != 2 {
		t.Errorf("delivered_count = %v, want 2", dc)
	}
	if d["next_stage_code"] != st2.StageCode {
		t.Errorf("next_stage_code = %v, want %s", d["next_stage_code"], st2.StageCode)
	}
	if completedActor != "u2" {
		t.Errorf("completedActor = %q, want u2", completedActor)
	}
	// 物理验证：下一环节(st2)的文件任务(tk2) input/ 应有那两个文件
	for _, name := range []string{"产出A.txt", "产出B.txt"} {
		next := ws.TaskStateDir(projectCode, st2.StageCode, tk2.TaskCode, "input") + "/" + name
		if _, err := os.Stat(next); err != nil {
			t.Errorf("expected %s to exist: %v", next, err)
		}
	}
}

func TestHTTP_CentralizedProjects_DeliverStage_LastStage(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")
	tmpRoot := t.TempDir()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyProjectRoot, tmpRoot)
	ws := repository.NewProjectWorkspace(tmpRoot)
	_ = ws.CreateProjectTree("CPA-300", []string{"GR-FINAL"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/centralized-projects/application-stages":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 5, "stage_code": "GR-FINAL", "stage_name": "归档", "sort_order": 0, "assignee_username": "u2"},
				},
			})
		case "/api/centralized-projects/complete-stage":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]interface{}{"id": 5, "status": "completed"}})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/stages/5/deliver", map[string]interface{}{
		"application_id": 300, "current_stage_code": "GR-FINAL",
	})
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if d["is_last_stage"] != true {
		t.Errorf("is_last_stage = %v, want true", d["is_last_stage"])
	}
}

func TestHTTP_CentralizedProjects_ListApplicationStages_Proxy(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u2")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/centralized-projects/application-stages" {
			http.NotFound(w, req)
			return
		}
		if req.URL.Query().Get("application_id") != "42" {
			http.Error(w, "wrong app id", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0,
			"data": []map[string]interface{}{
				{"id": 1, "stage_code": "GR-01", "stage_name": "立项", "assignee_username": "u1", "status": "completed", "sort_order": 0},
				{"id": 2, "stage_code": "GR-02", "stage_name": "采集", "assignee_username": "u2", "status": "in_progress", "sort_order": 1},
			},
		})
	}))
	defer srv.Close()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/42/stages")
	successOk(t, status, resp)
	list, _ := resp["data"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("stages count = %d, want 2", len(list))
	}
}

func TestHTTP_CentralizedProjects_ClosureFlow(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "submitter")

	var closeCallerID, closeCloser, closeSummary string
	mySubmissionsCalledFor := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/centralized-projects/list":
			mySubmissionsCalledFor = req.URL.Query().Get("submitted_by")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0,
				"data": []map[string]interface{}{
					{"id": 11, "project_name": "可结项项目", "submitted_by": "submitter", "status": "accepted",
						"owner_name": "lisi", "accepted_at": "2026-05-22T10:00:00Z",
						"template_code": "TPL-X", "template_version": "V1.0"},
				},
			})
		case "/api/centralized-projects/close":
			closeCallerID = req.URL.Query().Get("id")
			var b map[string]interface{}
			_ = json.NewDecoder(req.Body).Decode(&b)
			closeCloser, _ = b["closer"].(string)
			closeSummary, _ = b["closure_summary"].(string)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"code": 0, "data": map[string]interface{}{"id": 11, "status": "closed"},
			})
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()
	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	// my-submissions：应带 submitted_by=submitter
	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/my-submissions")
	successOk(t, status, resp)
	if mySubmissionsCalledFor != "submitter" {
		t.Errorf("manage list submitted_by = %q, want submitter", mySubmissionsCalledFor)
	}
	list, _ := resp["data"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(list))
	}

	// close：透传 closer=currentOperator + closure_summary
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/11/close", map[string]interface{}{
		"closure_summary": "全部环节交付完毕",
	})
	successOk(t, status, resp)
	if closeCallerID != "11" {
		t.Errorf("close id = %q, want 11", closeCallerID)
	}
	if closeCloser != "submitter" {
		t.Errorf("closer = %q, want submitter", closeCloser)
	}
	if closeSummary != "全部环节交付完毕" {
		t.Errorf("closure_summary = %q", closeSummary)
	}
}

func TestHTTP_CentralizedProjects_Refresh_PullsReviewResult(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "u1")

	// 先建一条 pending 申请（不走 push，直接 SQL）
	_, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, submitted_by, status, sync_status, create_time, update_time, disable)
		VALUES ('被驳回的项目', 'wangwu', 'u1', 'pending', 'synced', datetime('now'), datetime('now'), 0)`)
	if err != nil {
		t.Fatal(err)
	}
	var localID int64
	_ = db.Get(&localID, `SELECT id FROM centralized_project_applications ORDER BY id DESC LIMIT 1`)

	// mock manage 返回该 origin_id 已被驳回
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/centralized-projects/by-origins" {
			http.NotFound(w, req)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 0,
			"data": []map[string]interface{}{
				{
					"id":             42,
					"scan_origin_id": localID,
					"owner_name":     "wangwu",
					"status":         "rejected",
					"reject_reason":  "项目目标不明确",
					"reviewed_at":    "2026-05-21T10:00:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	cfg := repository.NewSystemConfigRepository(db)
	cfg.SetValue(repository.KeyManageEndpoint, srv.URL)

	status, resp := jsonReq(t, r, "POST", "/centralized-projects/refresh", nil)
	successOk(t, status, resp)
	d := dataMap(t, resp)
	if v, _ := d["updated"].(float64); int(v) != 1 {
		t.Errorf("updated = %v, want 1", v)
	}

	var (
		gotStatus string
		gotReason *string
	)
	db.Get(&gotStatus, `SELECT status FROM centralized_project_applications WHERE id = ?`, localID)
	db.Get(&gotReason, `SELECT reject_reason FROM centralized_project_applications WHERE id = ?`, localID)
	if gotStatus != "rejected" {
		t.Errorf("local status = %q, want rejected", gotStatus)
	}
	if gotReason == nil || *gotReason != "项目目标不明确" {
		t.Errorf("reject_reason = %v, want 项目目标不明确", gotReason)
	}
}
