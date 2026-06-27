package httpd

import (
	"errors"
	"strings"
	"testing"
)

// V1验证-1.50 SQL UNIQUE 约束错误映射成中文友好提示
func TestFriendlyError_UniqueConstraints(t *testing.T) {
	cases := []struct {
		raw  string
		ctx  string
		want string
	}{
		{"UNIQUE constraint failed: subjects.code", "创建主体失败", "该编码已被使用，请换一个唯一编码"},
		{"UNIQUE constraint failed: data_projects.project_code", "立项失败", "项目编码冲突，请稍后重试"},
		{"UNIQUE constraint failed: data_templates.template_code", "", "模版编码已存在"},
		{"UNIQUE constraint failed: asset_ledgers.ledger_code", "", "底账编号冲突，请稍后重试"},
		{"UNIQUE constraint failed: file_versions.file_version_code", "", "文件版本编码冲突，请稍后重试"},
		{"UNIQUE constraint failed: foo.bar", "", "存在重复数据，请检查唯一字段"},
	}
	for _, c := range cases {
		got := FriendlyError(errors.New(c.raw), c.ctx)
		if got != c.want {
			t.Errorf("FriendlyError(%q, %q) = %q, want %q", c.raw, c.ctx, got, c.want)
		}
	}
}

// V1验证-1.51 NOT NULL / CHECK / FK 约束
func TestFriendlyError_OtherConstraints(t *testing.T) {
	if got := FriendlyError(errors.New("NOT NULL constraint failed: subjects.code"), ""); !strings.Contains(got, "必填字段") {
		t.Errorf("NOT NULL: got %q", got)
	}
	if got := FriendlyError(errors.New("CHECK constraint failed: data_state"), ""); got != "字段值不在允许范围内（请检查类型/状态等枚举值）" {
		t.Errorf("CHECK: got %q", got)
	}
	if got := FriendlyError(errors.New("FOREIGN KEY constraint failed"), ""); got != "引用的关联记录不存在或已删除" {
		t.Errorf("FK: got %q", got)
	}
}

// V1验证-1.52 网络层错误
func TestFriendlyError_Network(t *testing.T) {
	cases := []struct {
		raw      string
		contains string
	}{
		{"dial tcp 127.0.0.1:3000: connect: connection refused", "无法连接到 manage 服务"},
		{"context deadline exceeded", "请求超时"},
		{"dial tcp: lookup foo.bar: no such host", "manage 地址解析失败"},
	}
	for _, c := range cases {
		got := FriendlyError(errors.New(c.raw), "")
		if !strings.Contains(got, c.contains) {
			t.Errorf("FriendlyError(%q) = %q, want contains %q", c.raw, got, c.contains)
		}
	}
}

// V1验证-1.53 已经是中文消息保持原样
func TestFriendlyError_PassthroughCJK(t *testing.T) {
	src := "立项必须至少有一个成员具备 close 权限（项目负责人）"
	if got := FriendlyError(errors.New(src), "立项失败"); got != src {
		t.Errorf("CJK should passthrough: got %q", got)
	}
}

// V1验证-1.54 nil error
func TestFriendlyError_Nil(t *testing.T) {
	if got := FriendlyError(nil, "x"); got != "" {
		t.Errorf("nil error should return empty string, got %q", got)
	}
}

// V1验证-1.55 上下文兜底
func TestFriendlyError_FallbackWithContext(t *testing.T) {
	got := FriendlyError(errors.New("some random db error"), "保存失败")
	if !strings.HasPrefix(got, "保存失败：") {
		t.Errorf("expected '保存失败：' prefix, got %q", got)
	}
}
