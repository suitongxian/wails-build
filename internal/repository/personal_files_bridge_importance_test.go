package repository

import (
	"testing"
)

func TestSyncImportanceFromProjectCode(t *testing.T) {
	cases := []struct {
		code string
		want int
	}{
		{PersonalCoreProjectCode, 1},
		{PersonalImportantProjectCode, 2},
		{PersonalGeneralProjectCode, 3},
		{"BIZ-NON-PERSONAL", 0},
		{"", 0},
	}
	for _, c := range cases {
		got := SyncImportanceFromProjectCode(c.code)
		if got != c.want {
			t.Errorf("SyncImportanceFromProjectCode(%q) = %d, want %d", c.code, got, c.want)
		}
	}
}
