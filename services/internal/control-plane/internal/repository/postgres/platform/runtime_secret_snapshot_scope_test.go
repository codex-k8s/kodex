package platform

import (
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRuntimeSecretTerminalSnapshotRejectsMissingOrMixedScope(t *testing.T) {
	for _, test := range []struct {
		name, scope, organization, project string
		valid                              bool
	}{
		{"organization", "ORGANIZATION", "org_example", "", true},
		{"project", "PROJECT", "org_example", "prj_example", true},
		{"missing scope", "", "org_example", "prj_example", false},
		{"unknown scope", "TENANT", "org_example", "", false},
		{"missing organization", "PROJECT", "", "prj_example", false},
		{"project without project", "PROJECT", "org_example", "", false},
		{"organization with project", "ORGANIZATION", "org_example", "prj_example", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(entity.RuntimeSecret{Ref: "sec_example", Namespace: "runtime", ScopeKind: test.scope, OrganizationRef: test.organization, ProjectRef: test.project})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = decodeRuntimeSecretSnapshot(raw); (err == nil) != test.valid {
				t.Fatalf("terminal snapshot owner tuple validation mismatch: %v", err)
			}
		})
	}
}

func TestRuntimeSecretDraftReceiptPinsFullCurrentOwnerTuple(t *testing.T) {
	current := entity.RuntimeSecretDraft{Ref: "draft_example", SecretRef: "sec_example", ScopeKind: "ORGANIZATION", OrganizationRef: "org_example", Generation: 3}
	valid := entity.RuntimeSecretDraftResult{Draft: current, Secret: &entity.RuntimeSecret{Ref: current.SecretRef, ScopeKind: current.ScopeKind, OrganizationRef: current.OrganizationRef}}
	if !validSecretDraftResultScope(current, valid) {
		t.Fatal("exact organization receipt rejected")
	}
	for _, mutate := range []func(*entity.RuntimeSecretDraftResult){
		func(r *entity.RuntimeSecretDraftResult) { r.Draft.ScopeKind = "" },
		func(r *entity.RuntimeSecretDraftResult) { r.Draft.OrganizationRef = "org_other" },
		func(r *entity.RuntimeSecretDraftResult) { r.Draft.ProjectRef = "prj_other" },
		func(r *entity.RuntimeSecretDraftResult) { r.Draft.SecretRef = "sec_other" },
		func(r *entity.RuntimeSecretDraftResult) { r.Draft.Ref = "draft_other" },
		func(r *entity.RuntimeSecretDraftResult) { r.Draft.Generation++ },
		func(r *entity.RuntimeSecretDraftResult) { r.Secret.OrganizationRef = "org_other" },
		func(r *entity.RuntimeSecretDraftResult) { r.Secret.Ref = "sec_other" },
		func(r *entity.RuntimeSecretDraftResult) {
			r.Secret.ScopeKind = "PROJECT"
			r.Secret.ProjectRef = "prj_other"
		},
	} {
		candidate := valid
		secret := *valid.Secret
		candidate.Secret = &secret
		mutate(&candidate)
		if validSecretDraftResultScope(current, candidate) {
			t.Fatal("changed owner tuple accepted as a completed receipt")
		}
	}
}
