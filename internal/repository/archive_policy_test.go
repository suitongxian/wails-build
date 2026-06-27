package repository

import "testing"

func TestDecideArchiveTargetForState_PersonalNeverSyncs(t *testing.T) {
	got := DecideArchiveTargetForState(PersonalImportantProjectCode, SensImportant, FileStatePersonalFinal)
	if got.Action != ArchiveActionNoSync {
		t.Fatalf("personal files should not sync to manage, got %+v", got)
	}
}

func TestDecideArchiveTargetForState_DepartmentFinalTargets(t *testing.T) {
	cases := []struct {
		name  string
		level string
		want  string
	}{
		{"general dept final", SensGeneral, StorageTierDepartmentCabinet},
		{"important dept final", SensImportant, StorageTierDepartmentCabinet},
		{"core dept final", SensCoreSecret, StorageTierSecureRoom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideArchiveTargetForState("FORMAL-PROJECT", tc.level, FileStateDeptFinal)
			if got.Action != ArchiveActionSync || got.TargetTier != tc.want {
				t.Fatalf("unexpected decision: got %+v want target %s", got, tc.want)
			}
		})
	}
}

func TestDecideArchiveTargetForState_UnitReleaseTargets(t *testing.T) {
	got := DecideArchiveTargetForState("FORMAL-PROJECT", SensImportant, FileStateUnitRelease)
	if got.Action != ArchiveActionSync || got.TargetTier != StorageTierUnitArchive {
		t.Fatalf("important unit release should target unit_archive, got %+v", got)
	}

	got = DecideArchiveTargetForState("FORMAL-PROJECT", SensCoreSecret, FileStateUnitRelease)
	if got.Action != ArchiveActionSync || got.TargetTier != StorageTierSecureRoom {
		t.Fatalf("core unit release should target secure_room, got %+v", got)
	}
}
