package httpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"data-asset-scan-go/internal/repository"
)

func TestHTTP_CentralizedProjects_P2Proxies(t *testing.T) {
	r, db, cleanup := setupTestServer(t)
	defer cleanup()
	withActiveUser(t, db, "lead")

	var stageTasksQuery, assignBody, stageUnreadAssignee, markStagesBody string
	manage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "stage-tasks"):
			stageTasksQuery = req.URL.RawQuery
		case strings.Contains(req.URL.Path, "assign-tasks"):
			b, _ := io.ReadAll(req.Body)
			assignBody = string(b)
		case strings.Contains(req.URL.Path, "stage-unread-count"):
			stageUnreadAssignee = req.URL.Query().Get("assignee")
		case strings.Contains(req.URL.Path, "mark-stages-seen"):
			b, _ := io.ReadAll(req.Body)
			markStagesBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"count":2,"updated":1}}`))
	}))
	defer manage.Close()
	repository.NewSystemConfigRepository(db).SetValue(repository.KeyManageEndpoint, manage.URL)

	status, resp := jsonReqNoBody(t, r, "GET", "/centralized-projects/stage-tasks?application_id=1&stage_code=STG-1")
	successOk(t, status, resp)
	if !strings.Contains(stageTasksQuery, "application_id=1") || !strings.Contains(stageTasksQuery, "stage_code=STG-1") {
		t.Errorf("stage-tasks 应透传 application_id/stage_code，实得 %q", stageTasksQuery)
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/assign-tasks?application_id=1&stage_code=STG-1", map[string]interface{}{
		"assignments": []map[string]interface{}{{"task_code": "TK-1", "task_name": "录入", "assignee_username": "u1"}},
	})
	successOk(t, status, resp)
	if !strings.Contains(assignBody, "\"actor\":\"lead\"") || !strings.Contains(assignBody, "TK-1") {
		t.Errorf("assign-tasks 应注入 actor=lead 并带 assignments，实得 %s", assignBody)
	}

	status, resp = jsonReqNoBody(t, r, "GET", "/centralized-projects/stage-unread-count")
	successOk(t, status, resp)
	if stageUnreadAssignee != "lead" {
		t.Errorf("stage-unread-count 应以 assignee=lead 转发，实得 %q", stageUnreadAssignee)
	}

	status, resp = jsonReq(t, r, "POST", "/centralized-projects/mark-stages-seen", map[string]interface{}{})
	successOk(t, status, resp)
	if !strings.Contains(markStagesBody, "\"assignee\":\"lead\"") {
		t.Errorf("mark-stages-seen 应在 body 注入 assignee=lead，实得 %s", markStagesBody)
	}
}
