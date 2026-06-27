package httpd

import "testing"

func TestImmediateUpstreamStage(t *testing.T) {
	all := []string{"SG", "PB", "SC"}
	cases := map[string]string{"SG": "", "PB": "SG", "SC": "PB", "NOPE": ""}
	for stage, want := range cases {
		if got := immediateUpstreamStage(all, stage); got != want {
			t.Errorf("immediateUpstreamStage(%q)=%q，期望 %q", stage, got, want)
		}
	}
	if got := immediateUpstreamStage(nil, "X"); got != "" {
		t.Errorf("空列表应返回空，实得 %q", got)
	}
}

func TestCentralizedProjectCode(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()
	_, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, project_code, owner_name, submitted_by, status, sync_status, sensitivity_level, project_scope, output_custody_scope, output_custody_note, manage_remote_id, create_time, update_time, disable)
		VALUES ('P','XM-2026-0001','wang','zhang','approved','synced','general','unit','unit','',42,'now','now',0)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := centralizedProjectCode(db, 42); got != "XM-2026-0001" {
		t.Fatalf("应返回裸 project_code=XM-2026-0001，实得 %q", got)
	}
	if got := centralizedProjectCode(db, 999); got != "" {
		t.Fatalf("不存在应返回空，实得 %q", got)
	}
}
