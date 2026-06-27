package repository

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
)

// 占位文件需为「能被本机程序正常打开」的最小有效文件：0 字节的 .pdf/.xlsx 会被阅读器/Excel 当作
// 损坏文件。故对这些格式填入最小有效内容；纯文本类(.txt/.csv 等)及未列出的类型仍用 0 字节空占位。
// placeholderByExt：扩展名 → 占位内容（非空格式才登记，登记的同时用于 isPlaceholderFile 反向识别）。
var placeholderByExt = map[string][]byte{
	".pdf":  buildMinimalPDF(),
	".xlsx": buildMinimalXlsx(),
	".docx": buildMinimalDocx(),
}

func buildMinimalPDF() []byte {
	stream := "BT /F1 16 Tf 72 720 Td (Placeholder - to be filled) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	// 二进制标记注释：让阅读器把文件按二进制处理（缺这一行某些阅读器会判损坏）。
	buf.Write([]byte{'%', 0xE2, 0xE3, 0xCF, 0xD3, '\n'})
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body))
	}
	xref := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objs)+1))
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref))
	return buf.Bytes()
}

// buildOOXML 把若干 part 打成一个 OOXML(zip) 包。固定不设修改时间→输出确定（跨进程可字节比对，
// 供 isPlaceholderFile 识别）。
func buildOOXML(parts []struct{ name, body string }) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		// 不设 Modified（保持零值）→ 不写入时间扩展字段，输出确定。
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate})
		if err == nil {
			_, _ = w.Write([]byte(p.body))
		}
	}
	_ = zw.Close()
	return buf.Bytes()
}

// buildMinimalXlsx 一个最小有效 .xlsx（空白工作表 Sheet1）。
func buildMinimalXlsx() []byte {
	return buildOOXML([]struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
			`</Relationships>`},
		{"xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`},
	})
}

// buildMinimalDocx 一个最小有效 .docx（含"待填写"一行）。
func buildMinimalDocx() []byte {
	return buildOOXML([]struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			`<w:p><w:r><w:t>待填写</w:t></w:r></w:p></w:body></w:document>`},
	})
}

// placeholderContent 占位文件初始内容：.pdf/.xlsx/.docx 用最小有效文件（可正常打开），其余类型为 0 字节空文件。
func placeholderContent(ext string) []byte {
	if b, ok := placeholderByExt[strings.ToLower(ext)]; ok {
		return b
	}
	return []byte{}
}

// IsPlaceholderFile 导出版，供其它包（如 httpd 工作台文件清单）判断是否为未填写的占位文件。
func IsPlaceholderFile(path string, size int64) bool { return isPlaceholderFile(path, size) }

// isPlaceholderFile 判断是否仍是"未填写的占位文件"：0 字节，或内容恰为对应格式的占位模板。
// size 由调用方传入；仅当大小与该格式占位一致时才读文件比对，开销可控。
func isPlaceholderFile(path string, size int64) bool {
	if size == 0 {
		return true
	}
	ph, ok := placeholderByExt[strings.ToLower(filepath.Ext(path))]
	if ok && size == int64(len(ph)) {
		if b, err := os.ReadFile(path); err == nil && bytes.Equal(b, ph) {
			return true
		}
	}
	return false
}

// 开始工作时按模版「文档标识」规则预建空占位文件（2026-06-01）：
// 让用户清楚本环节该产出/需要哪些文件、放哪，双击占位填内容即可（原地保存不跑偏）。
//
// 三态全建：input(工作依据) + process(过程文件) + output(定稿) 均按 allowed_file_types 首个扩展名
// 建 0 字节空占位，各落到自己的桶目录（input/、process/、output/）。（2026-06-16 起 output 也预建。）
// 空占位(0 字节)不会被自动归档（AutoArchiveStage 跳过 0 字节文件，避免空文件挂账/MD5 撞车）；
// 用户填入内容(非空)后才会被归档。幂等：已存在的文件不覆盖（不会盖掉上游交付/已填的真实文件）。

// firstFileExt 取"允许文件类型"的第一个，规整成带点小写后缀。
// 兼容两种存法：JSON 数组串 `["docx","pdf"]`（模版创作落库的格式）与逗号分隔 `PDF,DOCX`。
// 否则会出现形如 `.["docx"]` 的脏后缀。
func firstFileExt(allowed string) string {
	// 去掉 JSON 数组的方括号与引号，统一成逗号分隔再取首个
	cleaned := strings.NewReplacer("[", "", "]", "", "\"", "", "'", "", " ", "").Replace(allowed)
	for _, t := range strings.Split(cleaned, ",") {
		t = strings.TrimSpace(strings.ToLower(t))
		t = strings.TrimPrefix(t, ".")
		if t != "" {
			return "." + t
		}
	}
	return "" // 未指定类型 → 无后缀
}

// ScaffoldStageProcessDocsForProject 立项/环节启动时，按环节下【每个文件任务】的文档标识，
// 在各任务目录预建空占位（集中立项 CPA 虚拟项目用）。逐个文件任务委托 ScaffoldTaskDocsForProject，
// 故同样三态全建：input + process + output（各落对应桶目录）。返回新建文件路径。幂等：已存在不覆盖。
func ScaffoldStageProcessDocsForProject(db *sqlx.DB, templateCode, projectCode, stageCode string) ([]string, error) {
	var tplID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return nil, fmt.Errorf("模版不存在: %s: %w", templateCode, err)
	}
	var stageID int64
	if err := db.Get(&stageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tplID, stageCode); err != nil {
		return nil, fmt.Errorf("环节不存在: %s: %w", stageCode, err)
	}
	// 环节下所有文件任务，逐个建任务目录 + 预建该任务的过程占位。
	var taskCodes []string
	if err := db.Select(&taskCodes, `
		SELECT task_code FROM template_tasks
		WHERE template_stage_id = ? AND disable = 0 ORDER BY sort_order, id`, stageID); err != nil {
		return nil, fmt.Errorf("读取文件任务失败: %w", err)
	}
	var created []string
	for _, tc := range taskCodes {
		paths, err := ScaffoldTaskDocsForProject(db, templateCode, projectCode, stageCode, tc)
		if err != nil {
			return created, err
		}
		created = append(created, paths...)
	}
	return created, nil
}

// ScaffoldTaskDocsForProject 按 templateCode 下某「工作环节-文件任务」的过程文档标识，
// 在 projectCode(CPA 虚拟项目) 的该环节 process 目录预建空占位文件。幂等：已存在不覆盖。
func ScaffoldTaskDocsForProject(db *sqlx.DB, templateCode, projectCode, stageCode, taskCode string) ([]string, error) {
	var tplID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return nil, fmt.Errorf("本地模版缺失: %s: %w", templateCode, err)
	}
	var stageID int64
	if err := db.Get(&stageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tplID, stageCode); err != nil {
		return nil, fmt.Errorf("环节不存在: %s: %w", stageCode, err)
	}
	var taskID int64
	if err := db.Get(&taskID, `SELECT id FROM template_tasks WHERE template_stage_id = ? AND task_code = ? AND disable = 0`, stageID, taskCode); err != nil {
		return nil, fmt.Errorf("文件任务不存在: %s: %w", taskCode, err)
	}
	type rule struct {
		FileName  string `db:"file_name"`
		DataState string `db:"data_state"`
		Allowed   string `db:"allowed_file_types"`
	}
	var rules []rule
	// 三态全建：工作依据(input) + 过程文件(process) + 定稿(output) 都预建空占位。
	if err := db.Select(&rules, `
		SELECT file_name, data_state, allowed_file_types
		FROM template_file_rules
		WHERE template_task_id = ? AND disable = 0 AND data_state IN ('input','process','output')`, taskID); err != nil {
		return nil, fmt.Errorf("读取文件任务文档标识失败: %w", err)
	}
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)
	// 五层落盘：建该文件任务的三态目录，占位各落到 stages/{stage}/{task}/{input|process}/。
	if _, err := ws.CreateTaskDir(projectCode, stageCode, taskCode); err != nil {
		return nil, fmt.Errorf("建文件任务目录失败: %w", err)
	}
	var created []string
	for _, r := range rules {
		if strings.TrimSpace(r.FileName) == "" {
			continue
		}
		dir := ws.TaskStateDir(projectCode, stageCode, taskCode, r.DataState) // 按数据态落到 input/ process/ output/
		ext := firstFileExt(r.Allowed)
		path := filepath.Join(dir, sanitizeFileName(r.FileName)+ext)
		if _, err := os.Stat(path); err == nil {
			continue // 幂等（不覆盖已存在的真实文件/已填占位）
		}
		if err := os.WriteFile(path, placeholderContent(ext), 0o644); err != nil {
			return created, fmt.Errorf("建占位文件失败 %s: %w", path, err)
		}
		created = append(created, path)
	}
	return created, nil
}

// ScaffoldStageFiles 为某环节按文档标识规则预建 input/process/output 三态空占位文件，返回新建的文件路径。
func ScaffoldStageFiles(db *sqlx.DB, templateCode, stageCode string) ([]string, error) {
	// 解析模版/环节 id
	var tplID int64
	if err := db.Get(&tplID, `SELECT id FROM data_templates WHERE template_code = ? AND disable = 0`, templateCode); err != nil {
		return nil, fmt.Errorf("模版不存在: %s: %w", templateCode, err)
	}
	var stageID int64
	if err := db.Get(&stageID, `SELECT id FROM template_stages WHERE template_id = ? AND stage_code = ? AND disable = 0`, tplID, stageCode); err != nil {
		return nil, fmt.Errorf("环节不存在: %s: %w", stageCode, err)
	}

	type rule struct {
		FileName  string `db:"file_name"`
		DataState string `db:"data_state"`
		Allowed   string `db:"allowed_file_types"`
	}
	var rules []rule
	if err := db.Select(&rules, `
		SELECT file_name, data_state, allowed_file_types
		FROM template_file_rules
		WHERE template_stage_id = ? AND disable = 0
		  AND data_state IN ('input','process','output')`, stageID); err != nil {
		return nil, fmt.Errorf("读取文档标识规则失败: %w", err)
	}

	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	ws := NewProjectWorkspace(root)

	var created []string
	for _, r := range rules {
		if strings.TrimSpace(r.FileName) == "" {
			continue
		}
		ext := firstFileExt(r.Allowed)
		fname := sanitizeFileName(r.FileName) + ext
		dir := ws.StageStateDir(templateCode, stageCode, r.DataState)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return created, fmt.Errorf("建目录失败 %s: %w", dir, err)
		}
		path := filepath.Join(dir, fname)
		if _, err := os.Stat(path); err == nil {
			continue // 已存在（用户可能已填）→ 不覆盖，幂等
		}
		if err := os.WriteFile(path, placeholderContent(ext), 0o644); err != nil {
			return created, fmt.Errorf("建占位文件失败 %s: %w", path, err)
		}
		created = append(created, path)
	}
	return created, nil
}
