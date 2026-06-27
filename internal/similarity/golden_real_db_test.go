package similarity

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"data-asset-scan-go/internal/repository"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// 真实数据 golden snapshot 测试。
//
// 必须通过环境变量启用，平时跑套件不会自动触发：
//   SIMILARITY_REAL_DB=/path/to/data.db                  # 必填：真实 sqlite 库路径
//   SIMILARITY_REAL_GOLDEN=/path/to/golden_real.json     # 选填：snapshot 文件输出路径，默认 testdata/golden_real.json
//
// 用法：
//   # 首次 capture（重构前跑这条，把当前算法的输出固化）
//   SIMILARITY_REAL_DB=$HOME/.local/share/data-asset-scan/db/data.db \
//   UPDATE_GOLDEN=1 \
//   go test ./internal/similarity/ -run TestGoldenSnapshot_RealDB -v -count=1
//
//   # 重构每一步之后 verify（应当 PASS，否则那一步引入了行为变化）
//   SIMILARITY_REAL_DB=$HOME/.local/share/data-asset-scan/db/data.db \
//   go test ./internal/similarity/ -run TestGoldenSnapshot_RealDB -v -count=1
//
// golden_real.json 默认不入 git（路径在 testdata/ 下且文件名不在 fixture 测试里），
// 由你本地管理。如需团队共享可手动 git add。
func TestGoldenSnapshot_RealDB(t *testing.T) {
	dbPath := os.Getenv("SIMILARITY_REAL_DB")
	if dbPath == "" {
		t.Skip("SIMILARITY_REAL_DB not set; skipping real-db golden snapshot test")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("SIMILARITY_REAL_DB=%s not accessible: %v", dbPath, err)
	}

	goldenPath := os.Getenv("SIMILARITY_REAL_GOLDEN")
	if goldenPath == "" {
		goldenPath = filepath.Join("testdata", "golden_real.json")
	}

	// 以只读方式打开真实 DB（绝不写）
	sqlDB, err := sql.Open("sqlite3", dbPath+"?mode=ro&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite3")

	// 把 DB 注入到 similarity 包供 cache 路径用
	prev := injectedDB
	SetDB(db)
	t.Cleanup(func() { SetDB(prev) })

	// 用 DBLoader 拉真实输入
	distRepo := repository.NewDataDistributingRepository(db, 100)
	loader := &DBLoader{Repo: distRepo}
	inputs, err := loader.LoadInputs()
	if err != nil {
		t.Fatalf("load inputs: %v", err)
	}
	if len(inputs) == 0 {
		t.Fatal("no inputs found in real DB; did you scan first?")
	}
	t.Logf("Loaded %d file inputs from %s", len(inputs), dbPath)

	fams, err := BuildFamilies(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("BuildFamilies: %v", err)
	}
	t.Logf("BuildFamilies produced %d families", len(fams))

	snap := FamiliesToSnapshot(fams)

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := SaveSnapshot(goldenPath, snap); err != nil {
			t.Fatalf("save golden: %v", err)
		}
		t.Logf("Captured real-db golden: %s (families=%d, members=%d)",
			goldenPath, len(snap.Families), countMembers(snap))
		return
	}

	want, err := LoadSnapshot(goldenPath)
	if err != nil {
		t.Fatalf("load golden (run with UPDATE_GOLDEN=1 first): %v", err)
	}
	if diff := CompareSnapshots(want, snap); diff != "" {
		// 真实数据 diff 可能很长，截前 N 行
		const maxLines = 50
		lines := splitLines(diff)
		display := diff
		if len(lines) > maxLines {
			display = joinLinesN(lines[:maxLines]) + fmt.Sprintf("\n... (%d more diff lines)", len(lines)-maxLines)
		}
		t.Fatalf("real-db golden diff:\n%s", display)
	}
	t.Logf("OK: real-db output matches golden (families=%d, members=%d)",
		len(snap.Families), countMembers(snap))
}

func countMembers(s FamilySnapshot) int {
	n := 0
	for _, f := range s.Families {
		n += len(f.Members)
	}
	return n
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func joinLinesN(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
