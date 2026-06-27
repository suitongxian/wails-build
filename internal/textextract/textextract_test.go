package textextract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractText_PlainText(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(p, []byte("hello world\nsecond line"), 0644); err != nil {
		t.Fatal(err)
	}
	got := ExtractText(p)
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected 'hello world' in extracted text, got %q", got)
	}
}

func TestExtractText_NonExistent(t *testing.T) {
	got := ExtractText("/no/such/path.txt")
	if got != "" {
		t.Errorf("expected empty for non-existent, got %q", got)
	}
}

func TestExtractText_UnknownBinary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "binary.bin")
	if err := os.WriteFile(p, []byte{0x00, 0x01, 0x02}, 0644); err != nil {
		t.Fatal(err)
	}
	got := ExtractText(p)
	// 二进制格式不被识别 → 不报错即可（输出可空可非空，不严格断言）
	_ = got
}

func TestExtractTextWithTimeout_Fast(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "small.txt")
	os.WriteFile(p, []byte("quick content"), 0644)
	got := ExtractTextWithTimeout(p, 2*time.Second)
	if !strings.Contains(got, "quick") {
		t.Errorf("expected 'quick' in got %q", got)
	}
}
