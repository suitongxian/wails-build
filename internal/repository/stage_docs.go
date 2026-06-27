package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

// 在线编辑过程文档（2026-06-01，演示用）：
// 用户在 app 内编辑文档，保存路径完全由 模版(projectCode/stageCode)+文档标识 决定，
// 用户无需关心存到哪个目录——这就是 ScaffoldStageFiles 预建占位「双击填内容」的在线版。
//
// 安全约束：
//   - 仅在本环节 process/ 目录内读写；文件名只取 basename 防目录穿越。
//   - 写的是用户自己创作的过程草稿（与占位同源），不是扫描文件，不违「严禁删改扫描文件」铁律。
//   - 跨平台：filepath.Join + os.*，目录不存在则按需创建。

// StageDoc process/ 下的一个文档（供在线编辑列表，含空占位）。
type StageDoc struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Empty bool   `json:"empty"`
}

// safeDocName 取 basename 并拒绝空 / 含路径分隔的名字，防目录穿越（跨平台）。
func safeDocName(name string) (string, error) {
	base := filepath.Base(name)
	if name == "" || base == "." || base == ".." || base != name {
		return "", fmt.Errorf("非法文件名: %q", name)
	}
	return base, nil
}

func stageProcessDir(db *sqlx.DB, templateCode, stageCode string) string {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	return NewProjectWorkspace(root).StageStateDir(templateCode, stageCode, "process")
}

// ListStageProcessDocs 列出 process/ 下全部文件（含空占位，供在线编辑挑选）。
func ListStageProcessDocs(db *sqlx.DB, templateCode, stageCode string) ([]StageDoc, error) {
	dir := stageProcessDir(db, templateCode, stageCode)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []StageDoc{}, nil // 目录不存在 → 空列表（尚未开始工作）
	}
	docs := []StageDoc{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		docs = append(docs, StageDoc{Name: e.Name(), Size: fi.Size(), Empty: isPlaceholderFile(p, fi.Size())})
	}
	return docs, nil
}

// ListStageInputDocs 列出 input/（工作依据：上游交付来的文件）下全部文件，只读展示。
func ListStageInputDocs(db *sqlx.DB, templateCode, stageCode string) ([]StageDoc, error) {
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	dir := NewProjectWorkspace(root).StageStateDir(templateCode, stageCode, "input")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []StageDoc{}, nil
	}
	docs := []StageDoc{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		docs = append(docs, StageDoc{Name: e.Name(), Size: fi.Size(), Empty: isPlaceholderFile(p, fi.Size())})
	}
	return docs, nil
}

// ReadStageInputDoc 读取某「工作依据」(input) 文件内容（文本，只读查看）。
func ReadStageInputDoc(db *sqlx.DB, templateCode, stageCode, name string) (string, error) {
	base, err := safeDocName(name)
	if err != nil {
		return "", err
	}
	root := NewSystemConfigRepository(db).GetEffectiveProjectRoot()
	dir := NewProjectWorkspace(root).StageStateDir(templateCode, stageCode, "input")
	data, err := os.ReadFile(filepath.Join(dir, base))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取工作依据失败: %w", err)
	}
	return string(data), nil
}

// ReadStageProcessDoc 读取某过程文档内容（文本）。不存在 → 空内容（新建态）。
func ReadStageProcessDoc(db *sqlx.DB, templateCode, stageCode, name string) (string, error) {
	base, err := safeDocName(name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(stageProcessDir(db, templateCode, stageCode), base)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取文档失败: %w", err)
	}
	return string(data), nil
}

// WriteStageProcessDoc 把内容写入某过程文档（自动按模版目录保存；目录不存在则建）。返回落地路径。
func WriteStageProcessDoc(db *sqlx.DB, templateCode, stageCode, name, content string) (string, error) {
	base, err := safeDocName(name)
	if err != nil {
		return "", err
	}
	dir := stageProcessDir(db, templateCode, stageCode)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("建目录失败 %s: %w", dir, err)
	}
	path := filepath.Join(dir, base)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("保存文档失败 %s: %w", path, err)
	}
	return path, nil
}
