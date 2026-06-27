package repository

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPdfPlaceholder PDF 占位非空且为有效 PDF 头；被识别为占位；真实 PDF 内容不算占位。
func TestPdfPlaceholder(t *testing.T) {
	pdfPH := placeholderContent(".pdf")
	if len(pdfPH) == 0 || !bytes.HasPrefix(pdfPH, []byte("%PDF-")) {
		t.Fatalf("PDF 占位应为非空且以 %%PDF- 开头")
	}
	if !strings.Contains(string(pdfPH), "%%EOF") {
		t.Fatalf("PDF 占位应包含 %%%%EOF 结尾")
	}
	dir := t.TempDir()

	// 写入 pdf 占位 → 识别为占位
	ph := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(ph, placeholderContent(".pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(ph)
	if fi.Size() == 0 {
		t.Fatal("pdf 占位不应为 0 字节")
	}
	if !isPlaceholderFile(ph, fi.Size()) {
		t.Fatal("pdf 占位应被识别为占位")
	}

	// 真实内容的 pdf → 不是占位
	real := filepath.Join(dir, "b.pdf")
	if err := os.WriteFile(real, []byte("%PDF-1.4 真实内容真实内容真实内容真实内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	rfi, _ := os.Stat(real)
	if isPlaceholderFile(real, rfi.Size()) {
		t.Fatal("真实 PDF 不应被当作占位")
	}

	// docx/xlsx 占位现为最小有效文件（非 0 字节、可正常打开），且被识别为占位
	for _, ext := range []string{".docx", ".xlsx"} {
		if len(placeholderContent(ext)) == 0 {
			t.Fatalf("%s 占位应为最小有效文件（非 0 字节）", ext)
		}
		f := filepath.Join(dir, "p"+ext)
		_ = os.WriteFile(f, placeholderContent(ext), 0o644)
		fi, _ := os.Stat(f)
		if !isPlaceholderFile(f, fi.Size()) {
			t.Fatalf("%s 占位应被识别为占位", ext)
		}
	}
	// 未登记类型（如 .txt）仍为 0 字节空占位
	if len(placeholderContent(".txt")) != 0 {
		t.Fatal(".txt 占位应为 0 字节")
	}
}
