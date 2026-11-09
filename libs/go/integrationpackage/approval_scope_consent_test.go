package integrationpackage

import "testing"

// TestApprovalScopeConsentIsExplicitAndBounded preserves default root lineage and requires a complete human choice before a reusable consent can span roots.
// TestApprovalScopeConsentIsExplicitAndBounded сохраняет default root lineage и требует полного выбора человека перед межзапусковым reusable consent.
func TestApprovalScopeConsentIsExplicitAndBounded(t *testing.T) {
	legacy := Capability{}
	selected, err := legacy.ResolveApprovalScopeConsent(nil)
	if err != nil || selected != (ApprovalScopeConsent{DurationSeconds: 86400, MaxEffects: 100, Lifetime: "ROOT_RUN"}) {
		t.Fatal("historical consent defaults changed")
	}
	if _, err := legacy.ResolveApprovalScopeConsent(&ApprovalScopeConsent{86400, 100, "INITIATOR_RESOURCE"}); err == nil {
		t.Fatal("historical definition acquired cross-run consent authority")
	}
	capability := Capability{ApprovalScope: &ApprovalScopeContract{Name: "merge request", RequiredInputPaths: []string{"/project_id", "/merge_request_iid"},
		Limits: &ApprovalScopeLimits{604800, 10000}, AllowedLifetimes: []string{"ROOT_RUN", "INITIATOR_RESOURCE"}}}
	selected, err = capability.ResolveApprovalScopeConsent(nil)
	if err != nil || selected.Lifetime != "ROOT_RUN" || selected.DurationSeconds != 86400 || selected.MaxEffects != 100 {
		t.Fatal("larger policy bounds silently broadened default consent")
	}
	for _, requested := range []ApprovalScopeConsent{{86400, 1, "ROOT_RUN"}, {86400, 100, "INITIATOR_RESOURCE"}, {604800, 10000, "INITIATOR_RESOURCE"}, {}, {0, 1, "ROOT_RUN"}, {1, 0, "ROOT_RUN"}, {604801, 1, "ROOT_RUN"}, {1, 10001, "ROOT_RUN"}, {1, 1, ""}, {1, 1, "UNKNOWN"}} {
		got, err := capability.ResolveApprovalScopeConsent(&requested)
		want := requested.DurationSeconds >= 1 && requested.DurationSeconds <= 604800 && requested.MaxEffects >= 1 && requested.MaxEffects <= 10000 && (requested.Lifetime == "ROOT_RUN" || requested.Lifetime == "INITIATOR_RESOURCE")
		if (err == nil) != want || want && got != requested {
			t.Fatal("human consent bounds or lifetime changed")
		}
	}
	capability.ApprovalScope.Limits = &ApprovalScopeLimits{3600, 3}
	selected, err = capability.ResolveApprovalScopeConsent(nil)
	if err != nil || selected.DurationSeconds != 3600 || selected.MaxEffects != 3 {
		t.Fatal("default consent exceeded stricter tool policy")
	}
}

// TestApprovalScopeLifetimesRejectExpansion ensures managed packages cannot introduce cross-run reuse against a ROOT_RUN-only baseline or return aliased mutable choices.
// TestApprovalScopeLifetimesRejectExpansion запрещает managed packages вводить cross-run reuse при ROOT_RUN-only baseline либо возвращать изменяемые общие choices.
func TestApprovalScopeLifetimesRejectExpansion(t *testing.T) {
	baseline := Capability{ApprovalScope: &ApprovalScopeContract{Name: "resource", RequiredInputPaths: []string{}}}
	changed := Capability{ApprovalScope: &ApprovalScopeContract{Name: "resource", RequiredInputPaths: []string{}, AllowedLifetimes: []string{"ROOT_RUN", "INITIATOR_RESOURCE"}}}
	if narrowApprovalScopeContract(changed, baseline) || !narrowApprovalScopeContract(baseline, changed) {
		t.Fatal("managed lifetime narrowing changed")
	}
	choices, err := changed.ApprovalScopeLifetimes()
	if err != nil {
		t.Fatal(err)
	}
	choices[0] = "MUTATED"
	fresh, _ := changed.ApprovalScopeLifetimes()
	if fresh[0] != "ROOT_RUN" {
		t.Fatal("lifetime choices share mutable state")
	}
	for _, choices := range [][]string{{}, {"UNKNOWN"}, {"ROOT_RUN", "ROOT_RUN"}, {"ROOT_RUN", "INITIATOR_RESOURCE", "ROOT_RUN"}} {
		changed.ApprovalScope.AllowedLifetimes = choices
		if _, err := changed.ApprovalScopeLifetimes(); err == nil {
			t.Fatal("invalid lifetime policy admitted")
		}
	}
}
