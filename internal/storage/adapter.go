// Package storage 定义文件存储适配模块（§7.8 / §1.2 / §16）的统一接口。
//
// 文档要求：
//   - §1.2 "实体文件可以存储在本地文件系统、对象存储或外部文件系统中，
//     系统通过文件存储适配器进行对接"
//   - §7.8 列出 8 个方法（createProjectDirectory / createStageDirectory /
//     saveFile / moveFile / copyAsInput / sealArchive / deleteFile / calculateChecksum）
//   - §16 非功能要求 "可扩展性 = 文件存储应通过适配器扩展"
//   - §17.6 "文件实体删除必须通过生命周期销账流程"
//
// V3-6 MVP：仅实现本地适配器（LocalFileStorageAdapter）。后续可由
// DepartmentCabinet / UnitArchive / ObjectStorage 等其他实现替换；
// 具体后端形态在文档 §18.3 待确认。
package storage

import "io"

// Adapter §7.8 文件存储适配器统一接口
type Adapter interface {
	// CreateProjectDirectory 创建项目根目录树（含 metadata / archive / stages/*/* 三态子目录）
	// stageCodes 为该项目下所有工作环节编码
	CreateProjectDirectory(projectCode string, stageCodes []string) error

	// CreateStageDirectory 在已有项目下补建/修补单个环节的三态目录
	CreateStageDirectory(projectCode, stageCode string) error

	// SaveFile 把 src 流写到指定项目 + 环节 + 数据态下，返回 (storage_uri, size, checksum)。
	// targetFileName 已含扩展名；调用方负责文件名冲突避让。
	SaveFile(in SaveFileInput) (SaveFileResult, error)

	// MoveFile 移动文件到新位置（如安全策略 storage_tier 切换时使用）
	// 返回新的 storage_uri
	MoveFile(currentURI, newProjectCode, newStageCode, newDataState, newFileName string) (string, error)

	// CopyAsInput 把上游产出复制（或硬链接）到下游环节作为输入。返回 (新 storage_uri, size, checksum)。
	// 文档 §7.4 / §10.3 "输入文件 默认不可覆盖来源"——本方法保证下游用副本而非链接，
	// 即使源文件被修改也不影响下游的输入。
	CopyAsInput(sourceURI, targetProjectCode, targetStageCode, targetFileName string) (string, int64, string, error)

	// SealArchive 把整个项目目录打包（V1 仅生成 manifest.json，未做实际打包）
	// 返回归档清单路径 + SHA-256
	SealArchive(projectCode, manifestJSON string) (manifestPath string, sha256 string, err error)

	// DeleteFile 删除实体文件。仅允许从生命周期销账流程调用（业务侧通过权限/状态校验保证）。
	// 适配器层面不做调用者校验——文档 §17.6 由上层业务保证。
	DeleteFile(uri string) error

	// CalculateChecksum 计算文件 SHA-256（uppercase hex），返回 (size, checksum)
	CalculateChecksum(uri string) (size int64, checksum string, err error)
}

// SaveFileInput SaveFile 的入参
type SaveFileInput struct {
	Reader         io.Reader // 源数据流（调用方关闭）
	ProjectCode    string
	StageCode      string
	DataState      string // input / process / output
	TargetFileName string // 含扩展名，由命名规则渲染好
}

// SaveFileResult SaveFile 的结果
type SaveFileResult struct {
	StorageURI string
	Size       int64
	Checksum   string
}
