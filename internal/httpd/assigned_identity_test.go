package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// Bug2：王司长在「环节分工」看到空白。根因是身份匹配——/assigned 用 currentOperator 作 owner_name 查 manage。
// 若识别不到登录用户（空/system），原来静默返回空（与"被指派项目为空"无法区分）。
// 修复：识别不到时明确报错，提示重新登录。
func TestHTTP_AssignedCentralized_UnresolvedUser_Errors(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, "http://manage.invalid.local")
	// 不设活跃用户 → currentOperator = "system"

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/assigned")
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	if resp["success"] == true {
		t.Fatalf("身份未识别应返回 success:false，实得 %v", resp)
	}
	if e, _ := resp["error"].(string); !strings.Contains(e, "登录") {
		t.Fatalf("错误应提示重新登录，实得 %q", e)
	}
}

// 中文登录名 bug：项目负责人账号是中文时，/assigned 把 owner_name 以原始 UTF-8 字节塞进 URL，
// manage(node/h3) 解析为乱码，匹配不到 → 空列表。修复：owner_name 必须标准百分号编码(ASCII)，
// manage 解码后能还原成中文并匹配到项目。
func TestHTTP_AssignedCentralized_ChineseOwnerName_ProperlyEncoded(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	const owner = "张主任"
	withActiveUser(t, db, owner)

	var rawURIs []string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rawURIs = append(rawURIs, req.RequestURI) // 未解码的原始请求行
		w.Header().Set("Content-Type", "application/json")
		// 仅当 owner_name 正确解码为中文时才"命中"返回一条
		if req.URL.Query().Get("owner_name") == owner {
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":1,"project_name":"中文负责人项目","owner_name":"张主任"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/assigned")
	successOk(t, status, resp)

	// 1) 原始请求行必须是纯 ASCII（中文已百分号编码），否则 node 端会解析成乱码
	for _, u := range rawURIs {
		for i := 0; i < len(u); i++ {
			if u[i] > 127 {
				t.Fatalf("请求 URI 含未编码的非 ASCII 字节，中文未正确编码：%q", u)
			}
		}
		if !strings.Contains(u, "%E5") { // “张”的 UTF-8 首字节 E5 的百分号编码
			t.Fatalf("owner_name 中文未百分号编码，URI=%q", u)
		}
	}
	// 2) manage 解码后命中，返回了被指派的项目（不是空列表）
	data, _ := resp["data"].([]interface{})
	if len(data) == 0 {
		t.Fatalf("中文负责人应能查到被指派的项目，实得空列表")
	}
}

// 识别到登录用户时，/assigned 应在响应里回带解析出的 username，便于前端显示"当前识别登录名"，
// 让"按这个名查到 0 条"一眼可见（定位身份不匹配）。
func TestHTTP_AssignedCentralized_EchoesResolvedUsername(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "wang")

	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// 任何 list 查询都返回空集合（模拟 manage 上没有 owner_name=wang 的项目）
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/assigned")
	successOk(t, status, resp)
	if resp["username"] != "wang" {
		t.Fatalf("响应应回带 username=wang，实得 %v", resp["username"])
	}
}
