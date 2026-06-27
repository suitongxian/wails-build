package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalFileStorageAdapter §7.8 本地文件系统适配器
//
// V1+V2 阶段 scan 上的所有文件操作都落到这里：项目根 root 下按
// {project_code}/stages/{stage_code}/{input|process|output}/ 组织。
//
// V3-6 把 V1 散落在 repository 包里的目录 / copy / checksum 逻辑
// 抽到这个适配器统一管理；repository 层后续依赖 Adapter 接口而非
// 具体实现，方便未来切换部门文件柜 / 对象存储等其他后端。
type LocalFileStorageAdapter struct {
	root string
}

// NewLocalFileStorageAdapter root 来自 SystemConfig.project_root。
// 空字符串时退到 ~/data-asset-projects。
func NewLocalFileStorageAdapter(root string) *LocalFileStorageAdapter {
	if strings.TrimSpace(root) == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, "data-asset-projects")
	}
	return &LocalFileStorageAdapter{root: root}
}

// Root 返回根目录绝对路径
func (a *LocalFileStorageAdapter) Root() string { return a.root }

// projectDir / stageDir / stateDir 内部路径解析（与 ProjectWorkspace 同语义）
func (a *LocalFileStorageAdapter) projectDir(projectCode string) string {
	return filepath.Join(a.root, projectCode)
}
func (a *LocalFileStorageAdapter) stageDir(projectCode, stageCode string) string {
	return filepath.Join(a.projectDir(projectCode), "stages", stageCode)
}
func (a *LocalFileStorageAdapter) stateDir(projectCode, stageCode, dataState string) string {
	return filepath.Join(a.stageDir(projectCode, stageCode), dataState)
}

// CreateProjectDirectory §7.8
func (a *LocalFileStorageAdapter) CreateProjectDirectory(projectCode string, stageCodes []string) error {
	if err := os.MkdirAll(a.root, 0o755); err != nil {
		return err
	}
	dirs := []string{
		a.projectDir(projectCode),
		filepath.Join(a.projectDir(projectCode), "metadata"),
		filepath.Join(a.projectDir(projectCode), "archive"),
	}
	for _, code := range stageCodes {
		dirs = append(dirs,
			a.stageDir(projectCode, code),
			a.stateDir(projectCode, code, "input"),
			a.stateDir(projectCode, code, "process"),
			a.stateDir(projectCode, code, "output"),
		)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// CreateStageDirectory §7.8
func (a *LocalFileStorageAdapter) CreateStageDirectory(projectCode, stageCode string) error {
	// 四个固定桶：工作依据(input)/参考文件(reference)/过程文件(process)/定稿(output)
	for _, st := range []string{"input", "reference", "process", "output"} {
		if err := os.MkdirAll(a.stateDir(projectCode, stageCode, st), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// SaveFile §7.8 流式写入，同时计算 SHA-256
func (a *LocalFileStorageAdapter) SaveFile(in SaveFileInput) (SaveFileResult, error) {
	dir := a.stateDir(in.ProjectCode, in.StageCode, in.DataState)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SaveFileResult{}, err
	}
	target := filepath.Join(dir, in.TargetFileName)

	f, err := os.Create(target)
	if err != nil {
		return SaveFileResult{}, err
	}
	defer f.Close()

	h := sha256.New()
	w := io.MultiWriter(f, h)
	n, err := io.Copy(w, in.Reader)
	if err != nil {
		return SaveFileResult{}, err
	}
	return SaveFileResult{
		StorageURI: target,
		Size:       n,
		Checksum:   strings.ToUpper(hex.EncodeToString(h.Sum(nil))),
	}, nil
}

// MoveFile §7.8
//
// 同盘内用 os.Rename；跨盘失败时降级为 copy + delete。
func (a *LocalFileStorageAdapter) MoveFile(currentURI, newProjectCode, newStageCode, newDataState, newFileName string) (string, error) {
	if _, err := os.Stat(currentURI); err != nil {
		return "", fmt.Errorf("源文件不存在: %w", err)
	}
	dir := a.stateDir(newProjectCode, newStageCode, newDataState)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	newURI := filepath.Join(dir, newFileName)
	if err := os.Rename(currentURI, newURI); err != nil {
		// 跨盘场景：copy + delete
		if err2 := copyFileBytes(currentURI, newURI); err2 != nil {
			return "", fmt.Errorf("rename failed (%v) and copy fallback failed: %w", err, err2)
		}
		if err2 := os.Remove(currentURI); err2 != nil {
			return "", fmt.Errorf("rename failed (%v) and remove after copy failed: %w", err, err2)
		}
	}
	return newURI, nil
}

// CopyAsInput §7.8
//
// 不做硬链接（防止上游修改污染下游）；直接 byte-copy 副本到下游环节 input 目录。
func (a *LocalFileStorageAdapter) CopyAsInput(sourceURI, targetProjectCode, targetStageCode, targetFileName string) (string, int64, string, error) {
	dir := a.stateDir(targetProjectCode, targetStageCode, "input")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, "", err
	}
	target := filepath.Join(dir, targetFileName)
	if err := copyFileBytes(sourceURI, target); err != nil {
		return "", 0, "", err
	}
	size, checksum, err := a.CalculateChecksum(target)
	if err != nil {
		return "", 0, "", err
	}
	return target, size, checksum, nil
}

// SealArchive §7.8
//
// V1 实现仅写 manifest.json + 计算 SHA-256，不打包整个项目目录。
// 后续如需 ZIP/TAR 打包，在此扩展不影响调用方。
func (a *LocalFileStorageAdapter) SealArchive(projectCode, manifestJSON string) (string, string, error) {
	archDir := filepath.Join(a.projectDir(projectCode), "archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		return "", "", err
	}
	path := filepath.Join(archDir, "manifest.json")
	if err := os.WriteFile(path, []byte(manifestJSON), 0o644); err != nil {
		return "", "", err
	}
	h := sha256.Sum256([]byte(manifestJSON))
	return path, strings.ToUpper(hex.EncodeToString(h[:])), nil
}

// DeleteFile §7.8 + §17.6 仅销账流程调用
//
// 适配器层只做"是否安全删除"判断；销账权限校验由 repository 层在调用前完成。
// 删除不存在的文件返回 nil（幂等）。
func (a *LocalFileStorageAdapter) DeleteFile(uri string) error {
	if uri == "" {
		return fmt.Errorf("uri 为空")
	}
	if _, err := os.Stat(uri); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return os.Remove(uri)
}

// CalculateChecksum §7.8 流式 SHA-256
func (a *LocalFileStorageAdapter) CalculateChecksum(uri string) (int64, string, error) {
	fi, err := os.Stat(uri)
	if err != nil {
		return 0, "", err
	}
	f, err := os.Open(uri)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return 0, "", err
	}
	return fi.Size(), strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}

// copyFileBytes 帮助函数：纯字节复制（不计算 checksum）
func copyFileBytes(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// 编译期保证 LocalFileStorageAdapter 实现 Adapter 接口
var _ Adapter = (*LocalFileStorageAdapter)(nil)
