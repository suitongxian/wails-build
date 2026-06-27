package repository

import "testing"

// V4-Q4 §3.6 九宫格存储基线 — 文档明列的 15 个 cell 全部覆盖
//
// 3 行（sensitivity_level） × 5 列（file_state） = 15 个存储位置中文 label。
func TestResolveStorageLabel_FullGrid(t *testing.T) {
	cases := []struct {
		level, state string
		want         string
	}{
		// 核心（涉密）行
		{SensCoreSecret, FileStatePersonalProcess, "个人核心文件保密夹"},
		{SensCoreSecret, FileStatePersonalFinal, "部门核心项目保密柜"},
		{SensCoreSecret, FileStateDeptStage, "部门核心项目保密柜"},
		{SensCoreSecret, FileStateDeptFinal, "单位核心要件保密室"},
		{SensCoreSecret, FileStateUnitRelease, "单位核心要件保密室"},

		// 重要（权威）行
		{SensImportant, FileStatePersonalProcess, "个人重要文件档案夹"},
		{SensImportant, FileStatePersonalFinal, "个人重要文件档案夹"},
		{SensImportant, FileStateDeptStage, "部门重要项目档案柜"},
		{SensImportant, FileStateDeptFinal, "部门重要项目档案柜"},
		{SensImportant, FileStateUnitRelease, "单位重要文件档案室"},

		// 一般（开放）行 — personal_final 升档为"个人重要档案夹"是文档 §3.6 明列
		{SensGeneral, FileStatePersonalProcess, "个人一般文件资料夹"},
		{SensGeneral, FileStatePersonalFinal, "个人重要文件档案夹"},
		{SensGeneral, FileStateDeptStage, "部门一般项目资料柜"},
		{SensGeneral, FileStateDeptFinal, "部门一般项目资料柜"},
		{SensGeneral, FileStateUnitRelease, "单位一般文本资料室"},
	}
	for _, c := range cases {
		got := ResolveStorageLabel(c.level, c.state)
		if got != c.want {
			t.Errorf("ResolveStorageLabel(%q, %q) = %q, want %q",
				c.level, c.state, got, c.want)
		}
	}
}

// V4-Q4 未知组合返回兜底
func TestResolveStorageLabel_UnknownCellFallback(t *testing.T) {
	got := ResolveStorageLabel("nonexistent_level", FileStatePersonalProcess)
	if got == "" {
		t.Error("未知组合不应返回空字符串")
	}
}
