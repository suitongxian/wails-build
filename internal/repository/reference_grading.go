package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 参考文件「归类定级声明」：导入参考文件时，由导入者声明该文件的类别与敏感级别，
// 用 sidecar 落盘在参考桶目录里，供后续「一键归档」据此定级、落入九宫格对应分区。
//
// 不触碰用户原文件，只在工作空间参考目录内维护一份隐藏的定级清单。

// refGradeFileName 参考目录下的定级清单（隐藏 json，文件名→定级）。
const refGradeFileName = ".archive-grade.json"

// RefGrade 单个参考文件的归类定级。
type RefGrade struct {
	Category         string `json:"category"`          // internal/external/public
	SensitivityLevel string `json:"sensitivity_level"` // core/important/general
}

// NormalizeRefCategory 归一类别（接受中文或英文码）。
func NormalizeRefCategory(s string) string {
	switch strings.TrimSpace(s) {
	case "internal", "内部", "内部资料":
		return "internal"
	case "external", "外部", "外部资料":
		return "external"
	case "public", "公开", "公开资料":
		return "public"
	default:
		return ""
	}
}

// NormalizeSensitivity 归一敏感级别（接受中文或英文码）。
func NormalizeSensitivity(s string) string {
	switch strings.TrimSpace(s) {
	case "core", "core_secret", "核心", "核心级":
		return "core"
	case "important", "重要", "重要级":
		return "important"
	case "general", "一般", "一般级", "开放":
		return "general"
	default:
		return ""
	}
}

// DefaultLevelForCategory 类别 → 默认级别：内部资料默认重要级，外部/公开默认一般级。
func DefaultLevelForCategory(category string) string {
	switch NormalizeRefCategory(category) {
	case "internal":
		return "important"
	default: // external / public / 未声明
		return "general"
	}
}

// SensitivityToArchiveZone 文件级别 → 九宫格分区：核心=保密、重要=档案、一般=资料。
func SensitivityToArchiveZone(level string) string {
	switch NormalizeSensitivity(level) {
	case "core":
		return "保密"
	case "important":
		return "档案"
	default:
		return "资料"
	}
}

// RefGradeSidecarPath 参考目录下定级清单的完整路径。
func RefGradeSidecarPath(refDir string) string {
	return filepath.Join(refDir, refGradeFileName)
}

func readRefGradeMap(refDir string) map[string]RefGrade {
	m := map[string]RefGrade{}
	b, err := os.ReadFile(RefGradeSidecarPath(refDir))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]RefGrade{}
	}
	return m
}

// WriteRefGrade 记录某参考文件的类别+级别（按文件名键，幂等覆盖同名条目）。
// level 为空时按类别取默认级别。
func WriteRefGrade(refDir, fileName, category, level string) error {
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		return err
	}
	cat := NormalizeRefCategory(category)
	lvl := NormalizeSensitivity(level)
	if lvl == "" {
		lvl = DefaultLevelForCategory(cat)
	}
	m := readRefGradeMap(refDir)
	m[fileName] = RefGrade{Category: cat, SensitivityLevel: lvl}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(RefGradeSidecarPath(refDir), b, 0o644)
}

// ReadRefGrade 回读某参考文件的定级；无声明返回 ok=false。
func ReadRefGrade(refDir, fileName string) (RefGrade, bool) {
	g, ok := readRefGradeMap(refDir)[fileName]
	return g, ok
}
