package repository

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// NamingContext 命名规则渲染上下文
type NamingContext struct {
	ProjectCode      string
	ProjectName      string
	StageCode        string
	StageName        string
	LocalCode        string // file_rule_code，如 IN-001
	DisplayName      string // 文件规则名称（如"客户原稿"）
	VersionNo        string
	UserName         string
	OriginalFileName string
	Date             time.Time
	// 业务自定义变量（来自前端的 extra 字段，例如 {书名}）
	Extras map[string]string
}

// 中英文 → 标准化 key 的别名映射
//
// 模版里的命名规则可能用中文变量（如 {书名}、{日期}）也可能用英文（如 {date}）。
// 渲染时统一查表替换；未识别的变量保留原样，前端可在上传时手动改名。
var systemKeyAliases = map[string]string{
	// 项目
	"project_code": "project_code", "项目编码": "project_code",
	"project_name": "project_name", "项目名称": "project_name",
	// 环节
	"stage_code": "stage_code", "工作环节编码": "stage_code", "环节编码": "stage_code",
	"stage_name": "stage_name", "工作环节名称": "stage_name", "环节名称": "stage_name",
	// 文件
	"local_code": "local_code", "文件版本编码": "local_code", "规则编码": "local_code",
	"display_name": "display_name", "文件名称": "display_name",
	"version": "version", "版本": "version", "version_no": "version",
	// 时间
	"date":      "date",
	"日期":        "date",
	"time":      "time",
	"时间":        "time",
	"datetime":  "datetime",
	"timestamp": "timestamp",
	"original":  "original",
	"原始文件":      "original",
	"原文件名":      "original",
	"用户":        "user",
	"user":      "user",
	"username":  "user",
	"年月":        "yyyymm",
	"yyyymm":    "yyyymm",
}

// patternRe 匹配 {var}、{var:fmt} 等占位符
var patternRe = regexp.MustCompile(`\{([^{}]+)\}`)

// RenderNamingPattern 用上下文渲染命名模板
//
// 系统支持的变量见 systemKeyAliases；业务自定义变量（如 {书名}）从 Extras 取。
// 二者都未命中时，回退到 display_name 作为有意义的占位（避免 `原稿-{书名}.pdf` 这种丑陋文件名）。
// 渲染结果用作文件名的"基底"——不含后缀；后缀由调用方根据原始文件附加。
func RenderNamingPattern(pattern string, ctx NamingContext) string {
	if strings.TrimSpace(pattern) == "" {
		// 没配置 naming_pattern：fallback 为 "{display_name}-V{version}"
		pattern = "{display_name}-V{version}"
	}
	if ctx.Date.IsZero() {
		ctx.Date = time.Now()
	}

	out := patternRe.ReplaceAllStringFunc(pattern, func(raw string) string {
		// raw 形如 "{书名}"
		key := strings.Trim(raw, "{}")
		key = strings.TrimSpace(key)
		// 取 systemKeyAliases；找不到先看 Extras；最后回退到 display_name
		std, ok := systemKeyAliases[key]
		if !ok {
			if ctx.Extras != nil {
				if v, has := ctx.Extras[key]; has && v != "" {
					return v
				}
			}
			// V1 兜底：未识别的业务变量用 display_name 替代，
			// 比保留 `{书名}` 字面值更友好。V2 可加上传弹窗收集 extras。
			if ctx.DisplayName != "" {
				return ctx.DisplayName
			}
			return "" // 实在没有就置空（连同周围分隔符一起被 sanitize 折叠）
		}
		switch std {
		case "project_code":
			return ctx.ProjectCode
		case "project_name":
			return ctx.ProjectName
		case "stage_code":
			return ctx.StageCode
		case "stage_name":
			return ctx.StageName
		case "local_code":
			return ctx.LocalCode
		case "display_name":
			return ctx.DisplayName
		case "version":
			v := ctx.VersionNo
			v = strings.TrimPrefix(v, "V")
			v = strings.TrimPrefix(v, "v")
			if v == "" {
				v = "1.0"
			}
			return v
		case "date":
			return ctx.Date.Format("20060102")
		case "yyyymm":
			return ctx.Date.Format("200601")
		case "time":
			return ctx.Date.Format("150405")
		case "datetime":
			return ctx.Date.Format("20060102_150405")
		case "timestamp":
			return ctx.Date.Format("20060102150405")
		case "original":
			if ctx.OriginalFileName == "" {
				return ""
			}
			return strings.TrimSuffix(ctx.OriginalFileName, filepath.Ext(ctx.OriginalFileName))
		case "user":
			return ctx.UserName
		}
		return raw
	})

	// 文件名 sanitization：替换非法字符
	out = sanitizeFileName(out)
	return out
}

// sanitizeFileName 移除/替换文件系统禁用字符
//
// Windows 禁用：< > : " / \ | ? *
// Unix 禁用： /
// 控制字符 \x00-\x1F 也禁用
var fsBadChar = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func sanitizeFileName(s string) string {
	s = fsBadChar.ReplaceAllString(s, "_")
	s = strings.TrimSpace(s)
	// 折叠重复下划线、空格和分隔符（避免 "原稿--V1.0" 或 "原稿-_-V1.0" 这类残留）
	s = regexp.MustCompile(`[ _-]{2,}`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_ ")
	if s == "" {
		s = "untitled"
	}
	return s
}

// FileExtFromName 从原始文件名提取扩展名（小写、不含点）
func FileExtFromName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	return strings.TrimPrefix(ext, ".")
}

// IsAllowedFileType 校验扩展名是否在允许列表中
//
// allowed 为 JSON 数组字符串如 `["PDF","DOC"]`，比较大小写不敏感。
func IsAllowedFileType(ext, allowedJSON string) bool {
	ext = strings.ToUpper(strings.TrimPrefix(ext, "."))
	if ext == "" {
		return false
	}
	// allowedJSON 可能是 ["PDF","DOC"] 或 [\"PDF\",\"DOC\"]，尝试 JSON 解析
	var arr []string
	if err := jsonUnmarshal(allowedJSON, &arr); err != nil {
		return false
	}
	for _, a := range arr {
		if strings.EqualFold(strings.TrimSpace(a), ext) {
			return true
		}
	}
	return false
}
