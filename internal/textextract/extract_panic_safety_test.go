package textextract

import (
	"os"
	"path/filepath"
	"testing"
)

// ExtractText 必须对任何输入（包括看起来像但实际损坏的格式 magic）都不 panic，
// 防止第三方解析库的内部 panic 杀进程。
//
// 注意：精确复现 ledongthuc/pdf 的 "panic: pred"（损坏的 FlateDecode + PNG predictor）
// 需要构造合规 PDF 头 + 故意损坏的压缩流，篇幅大且依赖库的内部实现细节。
// 这里用"看起来像但实际乱码"的 magic 字节做轻量回归——目的是 exercise 各路 format
// dispatch 分支，确保任何路径的 panic 都被 ExtractText 顶层 defer recover 接住。
//
// 关键 invariant：任何输入都不能让测试进程崩溃。任何 panic 透传出来都会让 go test
// 拿到 panic exit code 而非 FAIL，这本身就是 assertion。
func TestExtractText_NeverPanicsOnGarbageInput(t *testing.T) {
	tmp := t.TempDir()

	cases := []struct {
		name    string
		ext     string
		content []byte
	}{
		{"fake_pdf_with_garbage_after_header", "pdf",
			[]byte("%PDF-1.4\n%\xff\xff\xff\xff\nstream\n\xde\xad\xbe\xef\xff\xfe\xfd\xfc\xfbendstream\n")},
		{"fake_zip_docx", "docx",
			[]byte("PK\x03\x04not-actually-a-valid-zip-payload\xff\x00\xff\x00")},
		{"fake_rtf_truncated", "rtf",
			[]byte("{\\rtf1\\ansi\\deff0{\\fonttbl{\\f0 ")},
		{"fake_ole_doc", "doc",
			[]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1 garbage that looks like OLE but is not")},
		{"empty_pdf", "pdf", []byte{}},
		{"truncated_pdf_header", "pdf", []byte("%PDF-1")},
		{"html_with_invalid_charset", "html",
			[]byte("<html><head><meta charset=\"\xff\xfe-unknown\"></head><body>x</body></html>")},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := filepath.Join(tmp, tc.name+"."+tc.ext)
			if err := os.WriteFile(f, tc.content, 0644); err != nil {
				t.Fatal(err)
			}
			// 调用一定要在 t.Run 里：即便它 panic 也不会逃出去杀 test binary，
			// testing 框架会把 t.Run 内的 panic 转成 FAIL。
			// 但我们要的是更强保证：ExtractText 自己内部 recover 后返回 ""。
			got := ExtractText(f)
			// 不强求返回 ""——某些垃圾输入恰好能产出非空字符串（如 RTF/HTML 文本片段），
			// 关键是没 panic 出来。
			_ = got
		})
	}
}

// 不存在的路径必须正常返回 ""（保留既有行为）
func TestExtractText_NonexistentPathReturnsEmpty(t *testing.T) {
	got := ExtractText("/this/path/definitely/does/not/exist.pdf")
	if got != "" {
		t.Errorf("expected empty for nonexistent path, got %q", got)
	}
}
