package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

// 团队代理：承接注入 acceptor、组队注入 actor、query 透传，响应回传。
func TestHTTP_CentralizedProjects_TeamProxies(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	var acceptQuery, acceptBody, teamGetQuery, teamPostBody, stageTeamQuery, stageTeamBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "accept-project"):
			acceptQuery = req.URL.RawQuery
			b, _ := io.ReadAll(req.Body)
			acceptBody = string(b)
		case strings.Contains(req.URL.Path, "stage-team"):
			stageTeamQuery = req.URL.RawQuery
			if req.Method == "POST" {
				b, _ := io.ReadAll(req.Body)
				stageTeamBody = string(b)
			}
		case strings.Contains(req.URL.Path, "team"):
			teamGetQuery = req.URL.RawQuery
			if req.Method == "POST" {
				b, _ := io.ReadAll(req.Body)
				teamPostBody = string(b)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"username":"u1","display_name":"员工一"}]}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	// 承接：注入 acceptor=lead，透传 id
	status, resp := jsonReq(t, r, "POST", "/centralized-projects/accept-project?id=7", map[string]interface{}{})
	successOk(t, status, resp)
	if !strings.Contains(acceptQuery, "id=7") {
		t.Errorf("accept-project 应透传 id=7，实得 %q", acceptQuery)
	}
	if !strings.Contains(acceptBody, "\"acceptor\":\"lead\"") {
		t.Errorf("accept-project 应注入 acceptor=lead，实得 %s", acceptBody)
	}

	// 读项目团队：透传 application_id
	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/team?application_id=7")
	successOk(t, status, resp)
	if !strings.Contains(teamGetQuery, "application_id=7") {
		t.Errorf("team GET 应透传 application_id=7，实得 %q", teamGetQuery)
	}

	// 组建项目团队：注入 actor=lead，带 members
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/team?application_id=7", map[string]interface{}{
		"members": []map[string]interface{}{{"username": "u1", "display_name": "员工一"}},
	})
	successOk(t, status, resp)
	if !strings.Contains(teamPostBody, "\"actor\":\"lead\"") || !strings.Contains(teamPostBody, "u1") {
		t.Errorf("team POST 应注入 actor=lead 并带 members，实得 %s", teamPostBody)
	}

	// 组建环节团队：透传 application_id+stage_code，注入 actor
	status, resp = jsonReq(t, r, "POST", "/centralized-projects/stage-team?application_id=7&stage_code=STG-1", map[string]interface{}{
		"members": []map[string]interface{}{{"username": "u1"}},
	})
	successOk(t, status, resp)
	if !strings.Contains(stageTeamQuery, "application_id=7") || !strings.Contains(stageTeamQuery, "stage_code=STG-1") {
		t.Errorf("stage-team 应透传 application_id/stage_code，实得 %q", stageTeamQuery)
	}
	if !strings.Contains(stageTeamBody, "\"actor\":\"lead\"") {
		t.Errorf("stage-team POST 应注入 actor=lead，实得 %s", stageTeamBody)
	}
}
