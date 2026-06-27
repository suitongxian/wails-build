package httpd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 工作组代理：involved 注入 username=operator；work-group 透传 application_id；响应回传。
func TestHTTP_CentralizedProjects_WorkGroupProxies(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	var involvedUser, wgQuery string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "involved"):
			involvedUser = req.URL.Query().Get("username")
		case strings.Contains(req.URL.Path, "work-group"):
			wgQuery = req.URL.RawQuery
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"lead":{"username":"lead"}}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/involved")
	successOk(t, status, resp)
	if involvedUser != "lead" {
		t.Errorf("involved 应注入 username=lead，实得 %q", involvedUser)
	}

	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/work-group?application_id=7")
	successOk(t, status, resp)
	if !strings.Contains(wgQuery, "application_id=7") {
		t.Errorf("work-group 应透传 application_id=7，实得 %q", wgQuery)
	}
}
