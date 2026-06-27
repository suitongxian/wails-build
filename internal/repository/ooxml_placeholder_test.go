package repository

import (
	"archive/zip"
	"bytes"
	"testing"
)

// xlsx/docx 占位应是结构完整的 OOXML(zip) 包，含 [Content_Types].xml 与各自主 part，
// 这样本机 Excel/Word 打开不会报"文件已损坏"。
func TestOOXMLPlaceholders_ValidZip(t *testing.T) {
	cases := map[string]string{
		".xlsx": "xl/workbook.xml",
		".docx": "word/document.xml",
	}
	for ext, mainPart := range cases {
		data := placeholderContent(ext)
		if len(data) == 0 {
			t.Fatalf("%s 占位为空", ext)
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("%s 占位不是有效 zip: %v", ext, err)
		}
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		if !names["[Content_Types].xml"] {
			t.Fatalf("%s 缺 [Content_Types].xml", ext)
		}
		if !names[mainPart] {
			t.Fatalf("%s 缺主 part %s", ext, mainPart)
		}
	}

	// 确定性：同一二进制内多次构建字节一致（供跨进程 isPlaceholderFile 比对）
	if !bytes.Equal(buildMinimalXlsx(), buildMinimalXlsx()) {
		t.Fatal("xlsx 占位构建应确定（字节一致）")
	}
}
