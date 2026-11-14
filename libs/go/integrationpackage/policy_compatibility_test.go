package integrationpackage

import (
	"encoding/json"
	"testing"
)

// legacyPolicyPackage preserves a whole predecessor for historical policy tests, without narrowing a successor by removing fields.
// legacyPolicyPackage сохраняет полный predecessor для historical policy tests без удаления полей successor.
func legacyPolicyPackage(t *testing.T, key, version string) Package {
	t.Helper()
	raw, err := policyCompatibilitySnapshots.ReadFile("compatibility/" + key + "-" + version + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPolicySuccessorsPreserveHistoricalAuthority checks every closed predecessor, wrong digest/version, complete managed narrowing and unchanged defaults against all configurable successors.
// TestPolicySuccessorsPreserveHistoricalAuthority проверяет все closed predecessors, wrong digest/version, полное managed narrowing и неизменные defaults всех successors.
func TestPolicySuccessorsPreserveHistoricalAuthority(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for key, registry := range historicalPolicyRevisions {
		t.Run(key, func(t *testing.T) {
			current := shipped[key]
			previousPackages := trustedHistoricalPolicyPackages(current)
			if len(previousPackages) != len(registry.versions) {
				t.Fatal("historical registry unavailable")
			}
			for _, previous := range previousPackages {
				resolved, ok := ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest)
				if !ok || resolved.Digest != previous.Digest || ValidateExecutableRevision(previous, current) != nil {
					t.Fatal("historical authority rejected")
				}
				if _, ok := ResolveShippedRevision(current, previous.Metadata.Version, current.Digest); ok {
					t.Fatal("mixed pins accepted")
				}
				previous.Metadata.Origin = OriginUI
				previous.Metadata.Version = "99.0.0"
				raw, _ := json.Marshal(previous)
				managed, err := Parse(raw)
				if err != nil || ValidateExecutableRevision(managed, current) != nil {
					t.Fatal("complete historical managed baseline rejected", err)
				}
			}
			latestPrevious := previousPackages[len(previousPackages)-1]
			for _, cap := range current.Spec.Capabilities {
				old, exists := latestPrevious.Capability(cap.Key)
				if key != "github" && exists && cap.ApprovalPolicy != old.ApprovalPolicy {
					t.Fatal("default policy broadened")
				}
				if key == "github" {
					if cap.ResourceScope.Kind != "GITHUB_SCOPE" || cap.Risk == "READ" && (!cap.AllowsApprovalPolicy("NONE") || cap.ApprovalPolicy != "NONE") || cap.Risk != "READ" && cap.AllowsApprovalPolicy("NONE") {
						t.Fatal("GitHub successor widened explicit scope or policy")
					}
					continue
				}
				if cap.Key == "github.repository.bundle.publish" {
					if cap.AllowsApprovalPolicy("NONE") {
						t.Fatal("native bundle bypass")
					}
					continue
				}
				if cap.ApprovalScope == nil {
					t.Fatal("per-tool approval identity missing", cap.Key)
				}
				for _, policy := range []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"} {
					if !cap.AllowsApprovalPolicy(policy) {
						t.Fatal("declared configurable policy missing", cap.Key)
					}
				}
			}
		})
	}
}
