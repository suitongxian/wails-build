package httpd

import (
	"testing"
	"time"
)

// 集中立项本地目录命名：项目这一层用唯一立项编码(project_code)做文件夹名；
// 无 project_code 时回退 CPA-{manageAppID}。
func TestCentralizedDirCode_PrefersProjectCode(t *testing.T) {
	_, db, cleanup := setupTestServer(t)
	defer cleanup()

	// 1) 无本地记录 → 回退 CPA-{id}
	if got := centralizedDirCode(db, 100, ""); got != "CPA-100" {
		t.Fatalf("无记录应回退 CPA-100，实得 %s", got)
	}
	// 2) 显式传入 → 直接用
	if got := centralizedDirCode(db, 100, "XM-2026-0009"); got != "XM-2026-0009" {
		t.Fatalf("显式编码应优先，实得 %s", got)
	}
	// 3) 本地有该项目 → 目录名为「{项目名称}-{唯一编码}」（编码保证唯一，名称便于辨认）
	now := time.Now()
	if _, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, status, manage_remote_id, project_code, create_time, update_time)
		VALUES ('印刷项目','lead','accepted',100,'XM-2026-0100',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if got := centralizedDirCode(db, 100, ""); got != "印刷项目-XM-2026-0100" {
		t.Fatalf("应为「项目名称-编码」，实得 %s", got)
	}
	// 4) 显式传编码时也带上项目名前缀
	if got := centralizedDirCode(db, 100, "XM-2026-0100"); got != "印刷项目-XM-2026-0100" {
		t.Fatalf("显式编码也应带项目名前缀，实得 %s", got)
	}
	// 5) 项目名含非法路径字符 → 被清洗为下划线
	if _, err := db.Exec(`INSERT INTO centralized_project_applications
		(project_name, owner_name, status, manage_remote_id, project_code, create_time, update_time)
		VALUES ('A/B:C','lead','accepted',101,'XM-2026-0101',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if got := centralizedDirCode(db, 101, ""); got != "A_B_C-XM-2026-0101" {
		t.Fatalf("非法字符应被清洗，实得 %s", got)
	}
}
