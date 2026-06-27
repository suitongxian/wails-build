package repository

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestStageFinalsUploadPull 验证上游上传定稿、下游拉取为工作依据：
// 用一个内存版 manage 模拟 stage-finals/upstream-finals/stage-final 三接口。
func TestStageFinalsUploadPull(t *testing.T) {
	db := openTestDB(t)
	root := t.TempDir()
	NewSystemConfigRepository(db).SetValue(KeyProjectRoot, root)
	ws := NewProjectWorkspace(root)

	// 内存 manage：存 (stage_code -> filename -> bytes)，并把 S2 的上游写死为 S1
	var mu sync.Mutex
	store := map[string]map[string][]byte{}
	idIndex := map[int64][2]string{} // id -> [stage, name]
	var seq int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/projects/stage-finals" && r.Method == "POST":
			_ = r.ParseMultipartForm(10 << 20)
			stage := r.FormValue("stage_code")
			name := r.FormValue("file_name")
			f, _, _ := r.FormFile("file")
			b, _ := io.ReadAll(f)
			mu.Lock()
			if store[stage] == nil {
				store[stage] = map[string][]byte{}
			}
			store[stage][name] = b
			seq++
			idIndex[seq] = [2]string{stage, name}
			mu.Unlock()
			w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case r.URL.Path == "/api/projects/upstream-finals" && r.Method == "GET":
			// 下游 S2 的上游=S1
			stage := r.URL.Query().Get("stage_code")
			if stage != "S2" {
				w.Write([]byte(`{"code":0,"data":{"upstream_stage_code":null,"files":[]}}`))
				return
			}
			mu.Lock()
			files := ""
			for id, sn := range idIndex {
				if sn[0] == "S1" {
					if files != "" {
						files += ","
					}
					files += fmt.Sprintf(`{"id":%d,"file_name":%q}`, id, sn[1])
				}
			}
			mu.Unlock()
			w.Write([]byte(fmt.Sprintf(`{"code":0,"data":{"upstream_stage_code":"S1","files":[%s]}}`, files)))
		case r.URL.Path == "/api/projects/stage-final" && r.Method == "GET":
			var id int64
			fmt.Sscanf(r.URL.Query().Get("id"), "%d", &id)
			mu.Lock()
			sn := idIndex[id]
			b := store[sn[0]][sn[1]]
			mu.Unlock()
			w.Write(b)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	// 上游 S1 产出一个定稿 + 一个空占位（空的不应上传）
	outS1 := ws.StageStateDir("P1", "S1", "output")
	os.MkdirAll(outS1, 0o755)
	os.WriteFile(filepath.Join(outS1, "起草定稿.pdf"), []byte("final-bytes"), 0o644)
	os.WriteFile(filepath.Join(outS1, "空占位.pdf"), []byte{}, 0o644)

	// 上传 S1 定稿
	uploaded, errs := UploadStageFinals(db, srv.Client(), srv.URL, "P1", "S1", "alice")
	if uploaded != 1 || len(errs) != 0 {
		t.Fatalf("应上传 1 个(空占位跳过)，实得 uploaded=%d errs=%v", uploaded, errs)
	}

	// 下游 S2 拉取上游(S1)定稿到 input/
	pulled, err := PullUpstreamFinals(db, srv.Client(), srv.URL, "P1", "S2")
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(pulled) != 1 || pulled[0] != "起草定稿.pdf" {
		t.Fatalf("应拉取到 起草定稿.pdf，实得 %v", pulled)
	}
	got, err := os.ReadFile(filepath.Join(ws.StageStateDir("P1", "S2", "input"), "起草定稿.pdf"))
	if err != nil || string(got) != "final-bytes" {
		t.Fatalf("input 应落地上游定稿内容: err=%v content=%q", err, string(got))
	}

	// 首环节 S1 无上游 → 拉取为空
	p0, _ := PullUpstreamFinals(db, srv.Client(), srv.URL, "P1", "S1")
	if len(p0) != 0 {
		t.Fatalf("首环节无上游，应拉取 0 个，实得 %v", p0)
	}
}
