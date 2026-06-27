package httpd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"data-asset-scan-go/internal/repository"
)

// setupTestServer 起一个 httpd test server：
//   - 临时 SQLite DB（含完整迁移 + 安全策略基线）
//   - 注入到 repository 包级 db
//   - 一个干净的 gin engine 已注册全部路由
//   - 返回的 cleanup 关 db + 复位 GetDB()
func setupTestServer(t *testing.T) (*gin.Engine, *sqlx.DB, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	currentAuthSession.Lock()
	currentAuthSession.session = nil
	currentAuthSession.Unlock()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "v1.db")

	sqlDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// 与生产 openDB 保持一致：单连接池
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite3")
	if err := repository.RunMigrationsForTest(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	// 2026-05-22 测试环境把三个 endpoint 设成不可达的本地端口，避免：
	//   - 代码路径里 effective* 有 valueOrDefault 兜底到默认 URL，
	//     清空 DB 值也会回落到真实公网地址
	//   - 这里用 127.0.0.1:1（端口 1 系统保留，几乎一定 connection refused），
	//     mock server 用例会显式覆盖为自己的 httptest URL
	configRepo := repository.NewSystemConfigRepository(db)
	const unreachable = "http://127.0.0.1:1"
	configRepo.SetValue(repository.KeyManageEndpoint, unreachable)
	configRepo.SetValue(repository.KeyArchiveEndpoint, unreachable)
	configRepo.SetValue(repository.KeyUploadServerURL, unreachable)
	restore := repository.SetTestDB(db)

	r := gin.New()
	RegisterRoutes(r)

	cleanup := func() {
		currentAuthSession.Lock()
		currentAuthSession.session = nil
		currentAuthSession.Unlock()
		_ = db.Close()
		restore()
	}
	return r, db, cleanup
}

// withProjectRoot 配置 system_configs.project_root = tmp 目录
func withProjectRoot(t *testing.T, db *sqlx.DB) string {
	t.Helper()
	root := t.TempDir()
	configRepo := repository.NewSystemConfigRepository(db)
	configRepo.SetValue(repository.KeyProjectRoot, root)
	return root
}

// withActiveUser 设置一个活跃用户（让 currentOperator 不再 fallback 到 "system"）
func withActiveUser(t *testing.T, db *sqlx.DB, name string) {
	t.Helper()
	now := "now"
	_, err := db.Exec(`INSERT INTO user_info (company_name, user_name, department, ip, mac_address, work_address, phone, create_time, update_time, disable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"测试单位", name, "测试部门", "127.0.0.1", "00:00:00:00:00:00", "测试地址", "10000000000", now, now)
	if err != nil {
		t.Fatalf("insert active user: %v", err)
	}
	// 同时在 users 表 mirror 一份（业务上 owner 校验依赖 users 表，
	// 测试里默认假设当前活跃用户也是注册用户）
	seedRegisteredUser(t, db, name)
}

// seedRegisteredUser 仅在 users 表插一行（mock 已注册的 active 用户），
// 用于测试 "owner 必须已注册" 校验路径
func seedRegisteredUser(t *testing.T, db *sqlx.DB, username string) {
	t.Helper()
	now := time.Now()
	var existing int64
	_ = db.Get(&existing, `SELECT id FROM users WHERE username = ? AND disable = 0`, username)
	if existing > 0 {
		return
	}
	_, err := db.Exec(`INSERT INTO users (
		username, display_name, company_name, department, ip, mac_address,
		work_address, phone, status, create_time, update_time, disable
	) VALUES (?, ?, '测试单位', '测试部门', '127.0.0.1', '00:00:00:00:00:00', '测试地址', '10000000000', 'active', ?, ?, 0)`,
		username, username, now, now)
	if err != nil {
		t.Fatalf("seed registered user %s: %v", username, err)
	}
}

// seedUserAndProjectMember 在 users 表里建一个 username=name 的用户，并在指定项目里
// 给该用户分配 actions（如 ["read","write"]）的权限。配合 RequireFileVersionProjectAction
// 之类的写权限中间件使用。
//
// 返回 users.id。
func seedUserAndProjectMember(t *testing.T, db *sqlx.DB, username string, projectID int64, actions []string) int64 {
	t.Helper()
	now := time.Now()

	// 1) users 表：先查再插，避免重复
	var userID int64
	_ = db.Get(&userID, `SELECT id FROM users WHERE username = ? AND disable = 0`, username)
	if userID == 0 {
		res, err := db.Exec(`INSERT INTO users (
			username, display_name, company_name, department, ip, mac_address,
			work_address, phone, status, create_time, update_time, disable
		) VALUES (?, ?, '测试单位', '测试部门', '127.0.0.1', '00:00:00:00:00:00', '测试地址', '10000000000', 'active', ?, ?, 0)`,
			username, username, now, now)
		if err != nil {
			t.Fatalf("insert users %s: %v", username, err)
		}
		userID, _ = res.LastInsertId()
	}

	// 2) project_members 行（actions 序列化成 JSON 数组）
	actionsJSON := "["
	for i, a := range actions {
		if i > 0 {
			actionsJSON += ","
		}
		actionsJSON += `"` + a + `"`
	}
	actionsJSON += "]"

	if _, err := db.Exec(`INSERT INTO project_members (
		project_id, user_id, subject_id, role_code, permission_actions,
		create_time, update_time, disable
	) VALUES (?, ?, 0, '本人', ?, ?, ?, 0)`,
		projectID, userID, actionsJSON, now, now); err != nil {
		t.Fatalf("insert project_members: %v", err)
	}
	return userID
}

// jsonReq 发起 JSON 请求并返回响应 body 解析后的 map
func jsonReq(t *testing.T, r *gin.Engine, method, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reqBody = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var data map[string]interface{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &data)
	}
	return w.Code, data
}

// jsonReqNoBody GET / DELETE 等
func jsonReqNoBody(t *testing.T, r *gin.Engine, method, path string) (int, map[string]interface{}) {
	return jsonReq(t, r, method, path, nil)
}

// uploadReq 发起 multipart/form-data 文件上传
func uploadReq(t *testing.T, r *gin.Engine, path, fieldName, filename string, content []byte, formExtras map[string]string) (int, map[string]interface{}) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)

	part, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	for k, v := range formExtras {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close()

	req := httptest.NewRequest("POST", path, body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var data map[string]interface{}
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &data)
	}
	return w.Code, data
}

// dataMap 把 resp.data 当 map 取出，简化测试断言
func dataMap(t *testing.T, resp map[string]interface{}) map[string]interface{} {
	t.Helper()
	d, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response.data is not a map: %+v", resp)
	}
	return d
}

// dataList 把 resp.data 当 slice 取出
func dataList(t *testing.T, resp map[string]interface{}) []interface{} {
	t.Helper()
	if resp["data"] == nil {
		return nil
	}
	d, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("response.data is not a list: %+v", resp)
	}
	return d
}

// successOk 断言 response.success == true
func successOk(t *testing.T, status int, resp map[string]interface{}) {
	t.Helper()
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%+v", status, resp)
		return
	}
	if v, _ := resp["success"].(bool); !v {
		t.Errorf("response.success != true; body=%+v", resp)
	}
}

// expectFailure 断言 response.success == false 或者 status 非 2xx
func expectFailure(t *testing.T, status int, resp map[string]interface{}) {
	t.Helper()
	if status >= 200 && status < 300 {
		if v, _ := resp["success"].(bool); v {
			t.Errorf("expected failure but got success: status=%d, body=%+v", status, resp)
		}
	}
}

// dummyFile 返回一个临时文件路径，写入指定内容
func dummyFile(t *testing.T, name string, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// itoa int64 转十进制字符串（避免引入 strconv 在多个 _test 文件里）
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}

// httpPost 直接做 POST JSON，返回 ResponseRecorder 以便取 status code
func httpPost(t *testing.T, r *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// httptestRecord 通用 HTTP 调用，返回 ResponseRecorder（取 status code 用）
func httptestRecord(t *testing.T, r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// stageIDByName 在 stages 列表里按 stage_code 反查 id
func stageIDByName(t *testing.T, stages []repository.FullStageInstance, code string) int64 {
	t.Helper()
	for _, s := range stages {
		if s.StageCode == code {
			return s.ID
		}
	}
	t.Fatalf("stage not found: %s", code)
	return 0
}

// seedTemplateAndProject 用真实仓储/服务搭建一个项目，HTTP 测试可以跨过 manage 拉模版直接用
func seedTemplateAndProject(t *testing.T, db *sqlx.DB) (project *struct {
	ID          int64
	ProjectCode string
}, stages []repository.FullStageInstance) {
	t.Helper()
	tplCode, tplVer := seedTestTemplateForHTTP(t, db)
	owner, custodian, security := seedTestSubjectsForHTTP(t, db)

	withProjectRoot(t, db)

	svc := repository.NewProjectInstantiationService(db)
	out, err := svc.Instantiate(repository.InstantiateInput{
		TemplateCode:       tplCode,
		TemplateVersion:    tplVer,
		ProjectName:        "HTTP 测试项目",
		ObjectShortCode:    "HT-T",
		SensitivityLevel:   "important",
		OwnerSubjectID:     owner,
		CustodianSubjectID: custodian,
		SecuritySubjectID:  security,
		Activate:           true,
		Members: []repository.MemberInput{
			{SubjectID: owner, RoleCode: "PM", PermissionActions: []string{"read", "write", "receive", "submit", "archive", "close"}},
		},
	})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	return &struct {
		ID          int64
		ProjectCode string
	}{ID: out.Project.ID, ProjectCode: out.Project.ProjectCode}, out.Stages
}

// seedTestTemplateForHTTP 复用 repository 包内的同名 fixture
func seedTestTemplateForHTTP(t *testing.T, db *sqlx.DB) (string, string) {
	t.Helper()
	// 用 SQL 直插一个最小模版 — 与 repository.seedTestTemplate 约定一致
	tplCode := "TPL-PRINT-BOOK"
	tplVer := "V2.1"
	now := "now"
	res, err := db.Exec(`INSERT INTO data_templates (template_code, template_name, template_version, status, project_sensitivity_level, cached_at, create_time, update_time, disable)
		VALUES (?, ?, ?, 'active', 'important', ?, ?, ?, 0)`,
		tplCode, "书目印刷", tplVer, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	tplID, _ := res.LastInsertId()

	stages := []struct {
		Code, Name, Type string
		Order            int
	}{
		{"MZ-SG", "收稿", "intake", 1},
		{"MZ-PB", "排版", "process", 2},
		{"MZ-SH", "审核", "process", 3},
	}
	stageIDs := map[string]int64{}
	for _, s := range stages {
		r, err := db.Exec(`INSERT INTO template_stages (template_id, stage_code, stage_name, stage_type, sort_order, cached_at, create_time, update_time, disable)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`, tplID, s.Code, s.Name, s.Type, s.Order, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		stageIDs[s.Code] = id
	}

	rules := []struct {
		Stage, Code, Name, State string
		Required                 int
		AllowedFileTypes         string
		Order                    int
	}{
		{"MZ-SG", "IN-001", "客户原稿", "input", 1, `["PDF"]`, 1},
		{"MZ-PB", "PRC-001", "排版临时文件", "process", 0, `["PSD"]`, 2},
		{"MZ-PB", "OUT-001", "排版完成稿", "output", 1, `["PDF"]`, 3},
		{"MZ-SH", "IN-001", "审核来稿", "input", 0, `["PDF"]`, 4},
	}
	for _, r := range rules {
		_, err := db.Exec(`INSERT INTO template_file_rules (template_stage_id, file_rule_code, file_name, data_state, required, allowed_file_types, sort_order, cached_at, create_time, update_time, disable)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			stageIDs[r.Stage], r.Code, r.Name, r.State, r.Required, r.AllowedFileTypes, r.Order, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	return tplCode, tplVer
}

// seedTestSubjectsForHTTP 写三主体
func seedTestSubjectsForHTTP(t *testing.T, db *sqlx.DB) (owner, custodian, security int64) {
	t.Helper()
	insert := func(code, name, typ string) int64 {
		now := "now"
		r, err := db.Exec(`INSERT INTO subjects (code, name, type, status, create_time, update_time, disable)
			VALUES (?, ?, ?, 'active', ?, ?, 0)`, code, name, typ, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	owner = insert("OWNER-1", "测试归属人", "person")
	custodian = insert("CUST-1", "测试保管部门", "department")
	security = insert("SEC-1", "测试安全责任组织", "organization")
	return
}
