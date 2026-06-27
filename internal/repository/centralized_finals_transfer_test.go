package repository

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// CPA 跨机文件交接：上传端必须用「本机目录名」读本地定稿、用「项目级唯一键(project_code)」作 manage 键，
// 二者不同——避免多个 CPA 项目共用 template_code 导致定稿串档。
func TestUploadCentralizedStageFinals_UsesProjectKeyNotLocalDir(t *testing.T) {
	root := t.TempDir()
	db := openTestDB(t)
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)

	localDir := "二季度核算-XM-2026-0001" // 本机目录名（含项目名）
	manageKey := "XM-2026-0001"          // 项目级唯一键

	// 任务级定稿：stages/SG/T1/output/凭证.pdf
	out := ws.TaskStateDir(localDir, "SG", "T1", "output")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "凭证.pdf"), []byte("FINAL-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	type got struct{ tpl, stage, name string }
	var recv []got
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/projects/stage-finals" && r.Method == "POST" {
			_ = r.ParseMultipartForm(1 << 20)
			recv = append(recv, got{r.FormValue("template_code"), r.FormValue("stage_code"), r.FormValue("file_name")})
			w.Write([]byte(`{"code":0,"message":"ok"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	uploaded, errs := UploadCentralizedStageFinals(db, srv.Client(), srv.URL, localDir, manageKey, "SG", "zhang")
	if uploaded != 1 || len(errs) > 0 {
		t.Fatalf("应上传 1 个定稿，实得 uploaded=%d errs=%v", uploaded, errs)
	}
	if len(recv) != 1 || recv[0].tpl != manageKey || recv[0].stage != "SG" || recv[0].name != "凭证.pdf" {
		t.Fatalf("manage 应收到 template_code=项目键(%s)、stage=SG、凭证.pdf，实得 %+v", manageKey, recv)
	}
}

// 拉取端：按 (项目键, 上游环节) 从 manage 下载定稿，落到下游环节本机 input/。
func TestPullCentralizedStageFinals_DownloadsIntoInput(t *testing.T) {
	root := t.TempDir()
	db := openTestDB(t)
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)

	localDir := "二季度核算-XM-2026-0001"
	manageKey := "XM-2026-0001"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects/stage-finals-list":
			if r.URL.Query().Get("template_code") == manageKey && r.URL.Query().Get("stage_code") == "SG" {
				w.Write([]byte(`{"code":0,"data":[{"id":11,"file_name":"凭证.pdf"}]}`))
				return
			}
			w.Write([]byte(`{"code":0,"data":[]}`))
		case "/api/projects/stage-final":
			if r.URL.Query().Get("id") == "11" {
				_, _ = io.WriteString(w, "FINAL-DATA")
				return
			}
			w.WriteHeader(404)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	pulled, err := PullCentralizedStageFinals(db, srv.Client(), srv.URL, manageKey, "SG", localDir, "PB")
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(pulled) != 1 || pulled[0] != "凭证.pdf" {
		t.Fatalf("应拉取 1 个文件，实得 %v", pulled)
	}
	dst := filepath.Join(ws.StageStateDir(localDir, "PB", "input"), "凭证.pdf")
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "FINAL-DATA" {
		t.Fatalf("上游定稿应落到下游 PB 环节 input/，实得 err=%v content=%q", err, string(b))
	}
}

// 防串档回归：用项目键作 manage 键，两个不同项目即使 stage_code 相同也不会互相看到定稿。
func TestUploadCentralizedStageFinals_DistinctProjectsNoCollision(t *testing.T) {
	root := t.TempDir()
	db := openTestDB(t)
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)
	out := ws.TaskStateDir("projA-XM-1", "SG", "T1", "output")
	_ = os.MkdirAll(out, 0o755)
	_ = os.WriteFile(filepath.Join(out, "a.pdf"), []byte("A"), 0o644)

	keys := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		keys[fmt.Sprintf("%s|%s", r.FormValue("template_code"), r.FormValue("stage_code"))] = true
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	UploadCentralizedStageFinals(db, srv.Client(), srv.URL, "projA-XM-1", "XM-0001", "SG", "u")
	if !keys["XM-0001|SG"] {
		t.Fatalf("应按项目键 XM-0001 存储，实得键 %v", keys)
	}
}
