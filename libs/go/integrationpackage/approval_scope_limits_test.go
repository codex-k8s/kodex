package integrationpackage

import (
	"math"
	"testing"
)

// TestApprovalScopeLimitsBounds prevents invalid or overflowing limits before SQL and retains defaults for exact historical package snapshots.
// TestApprovalScopeLimitsBounds запрещает неверные либо переполняющие limits до SQL и сохраняет defaults для exact historical package snapshots.
func TestApprovalScopeLimitsBounds(t *testing.T) {
	legacy, err := (Capability{}).ResolvedApprovalScopeLimits()
	if err != nil || legacy.DurationSeconds != 86400 || legacy.MaxEffects != 100 {
		t.Fatal("historical approval limits changed")
	}
	for _, limits := range []ApprovalScopeLimits{{3600, 3}, {1, 1}, {604800, 10000}, {0, 100}, {-1, 1}, {604801, 1}, {1, 0}, {1, -1}, {1, 10001}, {math.MaxInt64, 1}, {1, math.MaxInt64}} {
		capability := Capability{ApprovalScope: &ApprovalScopeContract{Name: "resource", RequiredInputPaths: []string{}, Limits: &limits}}
		got, err := capability.ResolvedApprovalScopeLimits()
		want := limits.DurationSeconds >= 1 && limits.DurationSeconds <= 604800 && limits.MaxEffects >= 1 && limits.MaxEffects <= 10000
		if (err == nil) != want || want && got != limits {
			t.Fatalf("approval limits admission mismatch for %+v", limits)
		}
	}
}

// TestManagedApprovalScopeLimitsNarrowing permits shorter consent and a smaller effect budget while rejecting identity removal or authority expansion.
// TestManagedApprovalScopeLimitsNarrowing допускает более короткое согласование и меньший бюджет effects, отклоняя удаление identity либо расширение полномочий.
func TestManagedApprovalScopeLimitsNarrowing(t *testing.T) {
	baseline := Capability{ApprovalScope: &ApprovalScopeContract{Name: "merge request", RequiredInputPaths: []string{"/project_path", "/merge_request_iid"}}}
	candidate := baseline
	candidate.ApprovalScope = &ApprovalScopeContract{Name: "merge request", RequiredInputPaths: []string{"/project_path", "/merge_request_iid"}, Limits: &ApprovalScopeLimits{DurationSeconds: 3600, MaxEffects: 3}}
	if !narrowApprovalScopeContract(candidate, baseline) {
		t.Fatal("lower approval budget was not configurable")
	}
	for _, limits := range []ApprovalScopeLimits{{86401, 3}, {3600, 101}, {0, 1}} {
		candidate.ApprovalScope.Limits = &limits
		if narrowApprovalScopeContract(candidate, baseline) {
			t.Fatal("managed revision expanded approval authority")
		}
	}
	candidate.ApprovalScope.Limits = &ApprovalScopeLimits{DurationSeconds: 3600, MaxEffects: 3}
	candidate.ApprovalScope.RequiredInputPaths = []string{"/project_path"}
	if narrowApprovalScopeContract(candidate, baseline) || narrowApprovalScopeContract(Capability{}, baseline) || narrowApprovalScopeContract(baseline, Capability{}) {
		t.Fatal("managed revision removed or invented resource identity")
	}
}
