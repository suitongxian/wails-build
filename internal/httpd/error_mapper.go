package httpd

import "strings"

// FriendlyError 把后端原始 error 映射成用户友好中文提示
//
// 优先匹配特定 SQL 约束错误；其余原样返回。
// 如果上下文已有领域错误（"模版未找到" / "敏感等级" 等中文消息），保持不变。
func FriendlyError(err error, ctx string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)

	// SQLite UNIQUE 约束
	if strings.Contains(low, "unique constraint failed") {
		switch {
		case strings.Contains(low, "subjects.code"):
			return "该编码已被使用，请换一个唯一编码"
		case strings.Contains(low, "data_projects.project_code"):
			return "项目编码冲突，请稍后重试"
		case strings.Contains(low, "data_templates.template_code"):
			return "模版编码已存在"
		case strings.Contains(low, "asset_ledgers.ledger_code"):
			return "底账编号冲突，请稍后重试"
		case strings.Contains(low, "file_versions.file_version_code"):
			return "文件版本编码冲突，请稍后重试"
		default:
			return "存在重复数据，请检查唯一字段"
		}
	}

	// SQLite NOT NULL 约束
	if strings.Contains(low, "not null constraint failed") {
		// 提取出 .column 名
		idx := strings.Index(low, "failed: ")
		if idx > 0 {
			col := strings.TrimSpace(msg[idx+len("failed: "):])
			return "必填字段缺失：" + col
		}
		return "必填字段缺失"
	}

	// SQLite CHECK 约束
	if strings.Contains(low, "check constraint failed") {
		return "字段值不在允许范围内（请检查类型/状态等枚举值）"
	}

	// SQLite FOREIGN KEY
	if strings.Contains(low, "foreign key constraint failed") {
		return "引用的关联记录不存在或已删除"
	}

	// 常见底层网络/连接
	if strings.Contains(low, "connection refused") || strings.Contains(low, "no route to host") {
		return "无法连接到 manage 服务，请检查地址是否正确、服务是否已启动"
	}
	if strings.Contains(low, "timeout") || strings.Contains(low, "deadline exceeded") {
		return "请求超时，请检查网络或重试"
	}
	if strings.Contains(low, "no such host") {
		return "manage 地址解析失败，请检查 host 名"
	}

	// 已经是中文 / 业务层错误，直接透出（保留原始信息）
	if containsCJK(msg) {
		return msg
	}

	// 兜底：英文原文 + 上下文提示
	if ctx != "" {
		return ctx + "：" + msg
	}
	return msg
}

// containsCJK 简单判断字符串是否含中日韩字符
func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}
