package repository

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateUsername 拒绝可能导致路径遍历或破坏文件系统的字符。
// 理论上从 manage 同步来的 Username 不会有这些，仅作兜底防御。
func ValidateUsername(username string) error {
	u := strings.TrimSpace(username)
	if u == "" {
		return fmt.Errorf("username is empty")
	}
	if strings.ContainsAny(u, "/\\\x00") {
		return fmt.Errorf("username %q contains forbidden character (/ \\ \\0)", username)
	}
	if u == "." || u == ".." || strings.HasPrefix(u, "..") {
		return fmt.Errorf("username %q is reserved or starts with '..'", username)
	}
	return nil
}

// ComputeDefaultWorkspace 返回 <homeDir>/<username>/workspace 形式的约定路径。
// 不做 IO，仅做拼接 + 安全校验，方便单测。
func ComputeDefaultWorkspace(homeDir, username string) (string, error) {
	if strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("home dir is empty")
	}
	if err := ValidateUsername(username); err != nil {
		return "", err
	}
	return filepath.Join(homeDir, strings.TrimSpace(username), "workspace"), nil
}
