package repository

import (
	"testing"
)

func TestSystemConfig_ClaimFamilyDefaults(t *testing.T) {
	db := openTestDB(t)
	repo := NewSystemConfigRepository(db)

	policy := repo.GetValue(KeyClaimFamilyDefaultPolicy)
	if policy != ClaimFamilyPolicySameContentOnly {
		t.Errorf("default policy = %q, want %q", policy, ClaimFamilyPolicySameContentOnly)
	}

	skip := repo.GetValue(KeyClaimFamilySkipDialog)
	if skip != "false" {
		t.Errorf("default skip = %q, want \"false\"", skip)
	}
}
