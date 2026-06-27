package scanner

import (
	"testing"
	"time"
)

// 验证 progressReportInterval 是 500ms（合同性测试，防止有人手抖改成大值
// 让进度条卡死，或小值让性能回退）
func TestProgressReportInterval_Is500ms(t *testing.T) {
	if progressReportInterval != 500*time.Millisecond {
		t.Errorf("progressReportInterval = %v, want 500ms", progressReportInterval)
	}
}
