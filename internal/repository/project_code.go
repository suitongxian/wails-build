package repository

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// 项目编码格式：{object_short_code}-{YYYY}-{NNN}
// 示例：MC-NSXS-2024-001 / MOJ-NLF-2026-0001
//
// 流水号按 (object_short_code, year) 维度独立维护：同前缀同年分独立计数。
//
// 设计原则：
//   - 流水号默认 3 位补零（≥1000 自动扩展到 4 位）
//   - object_short_code 允许大写字母、数字、连字符；最大 32 字符
//   - 同前缀同年分原子查询 max+1，避免并发冲突（在事务内调用即可）

// projectCodeShortRe 校验 object_short_code 格式
var projectCodeShortRe = regexp.MustCompile(`^[A-Z0-9]+(-[A-Z0-9]+)*$`)

// ValidateObjectShortCode 校验项目编码前缀合法性（旧称"业务对象简码"，UI 已不再露出）
func ValidateObjectShortCode(code string) error {
	if code == "" {
		return fmt.Errorf("项目编码前缀不能为空")
	}
	if len(code) > 32 {
		return fmt.Errorf("项目编码前缀不能超过 32 字符")
	}
	if !projectCodeShortRe.MatchString(code) {
		return fmt.Errorf("项目编码前缀格式错误：仅允许大写字母、数字和连字符")
	}
	return nil
}

// GenerateProjectCode 在给定 db 上下文（事务或主连接）生成下一条项目编码
//
// 调用方应当在事务内使用，确保流水号生成与项目插入原子化。
func GenerateProjectCode(tx sqlx.Ext, objectShortCode string, refTime time.Time) (string, error) {
	if err := ValidateObjectShortCode(objectShortCode); err != nil {
		return "", err
	}
	year := refTime.Year()
	prefix := fmt.Sprintf("%s-%d-", objectShortCode, year)

	// 查询同前缀同年分的最大流水号
	row := tx.QueryRowx(`SELECT project_code FROM data_projects
		WHERE project_code LIKE ? AND disable = 0
		ORDER BY project_code DESC LIMIT 1`, prefix+"%")

	var lastCode string
	maxSeq := 0
	if err := row.Scan(&lastCode); err == nil {
		// 解析最后一段为流水号
		parts := strings.Split(lastCode, "-")
		if len(parts) >= 2 {
			seqPart := parts[len(parts)-1]
			var n int
			if _, e := fmt.Sscanf(seqPart, "%d", &n); e == nil {
				maxSeq = n
			}
		}
	}

	nextSeq := maxSeq + 1
	width := 3
	if nextSeq >= 1000 {
		width = 4
	}
	if nextSeq >= 10000 {
		width = 5
	}
	return fmt.Sprintf("%s%0*d", prefix, width, nextSeq), nil
}
