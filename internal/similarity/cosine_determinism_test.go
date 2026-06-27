package similarity

import "testing"

// 历史 bug：cosine / vecNorm 通过 map 迭代做浮点累加，map order 随机 →
// 同一对输入跑多次会拿到 bit-不同的分数（虽然 %.10f 看着一样）。
// 这个测试 pin 住「sorted summation」修复，防回归。
func TestTfidfCosine_DeterministicAcrossRuns(t *testing.T) {
	const a = "客户合同正本：甲方为示例公司，乙方为另一示例公司。本合同自双方签字之日起生效。"
	const b = "客户合同正本：甲方为示例公司，乙方为某某公司。合同自双方签字之日起生效，有效期一年。"

	first := tfidfCosine(a, b)
	for i := 0; i < 200; i++ {
		got := tfidfCosine(a, b)
		if got != first {
			t.Fatalf("tfidfCosine non-deterministic: iter %d got %v, first %v (bit diff)",
				i, got, first)
		}
	}
}

func TestCosine_DeterministicAcrossRuns(t *testing.T) {
	// 构造一个有足够 key 数量的 map 让 Go 的 map 随机化效应明显
	makeVec := func() map[string]float64 {
		v := make(map[string]float64)
		for i := 0; i < 50; i++ {
			v[string(rune('a'+i%26))+string(rune('a'+(i*7)%26))] = float64(i+1) / 13.0
		}
		return v
	}
	a := makeVec()
	b := makeVec()
	first := cosine(a, b)
	for i := 0; i < 200; i++ {
		if got := cosine(a, b); got != first {
			t.Fatalf("cosine non-deterministic: iter %d got %v, first %v", i, got, first)
		}
	}
}
