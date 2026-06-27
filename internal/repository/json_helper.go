package repository

import "encoding/json"

// jsonUnmarshal 内部 JSON 解析包装（避免裸 import 在多文件中重复）
func jsonUnmarshal(s string, v interface{}) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}
