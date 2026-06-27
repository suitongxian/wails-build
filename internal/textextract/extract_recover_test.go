package textextract

import (
	"testing"
	"time"
)

// panic in extractor goroutine 必须被 recover，函数返回空串，
// 测试进程不能崩溃。原 ExtractTextWithTimeout 实现没有 defer recover，
// 第三方 PDF 库（ledongthuc/pdf）一旦遇到损坏的 FlateDecode 流就会
// `panic: pred` 杀掉整个数可信终端进程。
func TestExtractTextWithRecover_RecoversFromExtractorPanic(t *testing.T) {
	panicker := func(path string) string {
		panic("synthetic: pred")
	}

	// 如果 recover 没生效，下面这行调用会让测试进程崩溃，
	// 整个 go test 拿到的就不是 FAIL 而是 panic exit。
	got := extractTextWithRecover("/fake/will-panic.pdf", panicker, 2*time.Second)
	if got != "" {
		t.Errorf("expected empty string after panic, got %q", got)
	}
}

// 即便 extractor panic 的是非 string 类型（任意 interface{} 值），
// recover 也得稳。
func TestExtractTextWithRecover_RecoversFromTypedPanic(t *testing.T) {
	panicker := func(path string) string {
		panic(struct{ Code int }{Code: 42})
	}

	got := extractTextWithRecover("/fake/typed-panic.pdf", panicker, 2*time.Second)
	if got != "" {
		t.Errorf("expected empty after typed panic, got %q", got)
	}
}

// 正常返回路径不受影响：extractor 正常返回啥就是啥。
func TestExtractTextWithRecover_PassesThroughNormalReturn(t *testing.T) {
	normal := func(path string) string {
		return "hello " + path
	}

	got := extractTextWithRecover("/fake/ok.txt", normal, 2*time.Second)
	if got != "hello /fake/ok.txt" {
		t.Errorf("expected pass-through, got %q", got)
	}
}

// 超时路径不受影响：extractor 慢于 timeout 时返回空，不阻塞调用方。
func TestExtractTextWithRecover_TimeoutReturnsEmpty(t *testing.T) {
	slow := func(path string) string {
		time.Sleep(500 * time.Millisecond)
		return "should-be-thrown-away"
	}

	start := time.Now()
	got := extractTextWithRecover("/fake/slow.pdf", slow, 50*time.Millisecond)
	elapsed := time.Since(start)

	if got != "" {
		t.Errorf("expected empty on timeout, got %q", got)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected return within ~50ms of timeout, took %v", elapsed)
	}
}
