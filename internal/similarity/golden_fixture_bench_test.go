package similarity

import (
	"context"
	"testing"
)

// 这个 benchmark 用合成 fixture 度量 BuildFamilies 端到端耗时，
// 重构每一步前后跑一次，对比 ns/op + allocs 量化收益。
//
// 用法：
//   go test ./internal/similarity/ -bench=BenchmarkBuildFamilies_GoldenFixture -benchmem -count=3 -run=^$
//
// 合成 fixture 不大，绝对耗时较低；但相对提升仍有指示意义。
// 真实 400 文件场景的基准走 TestGoldenSnapshot_RealDB（任务 #82）。
func BenchmarkBuildFamilies_GoldenFixture(b *testing.B) {
	dir := b.TempDir()
	inputs := buildSyntheticFixture(b, dir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fams, err := BuildFamilies(context.Background(), inputs, nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(fams) == 0 {
			b.Fatal("expected families")
		}
	}
}
