package platform

import (
	"reflect"
	"testing"

	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAdmissionTerminalReceiptRequiresEveryOriginalImmutableCoordinate(t *testing.T) {
	receipt := admissionClaimReceipt{Artifact: entity.ImageArtifact{Ref: "imgart_exact", Version: 2, BuildRef: "imgbld_exact", BuildAttempt: 1, RecipeGeneration: 1, ManifestDigest: "manifest", ImmutableBuildSHA256: "build", ProvenanceSHA256: "provenance", PolicyRevision: 1, PolicySHA256: "policy", SpecSHA256: "spec"}, Fence: 1, AuthorityGeneration: 3, AdmissionAttemptRef: "imgadm_exact", AdmissionAttempt: 1}
	input := repo.AdmissionTerminalInput{AdmissionExpiryInput: repo.AdmissionExpiryInput{ArtifactRef: receipt.Artifact.Ref, ExpectedVersion: 2, ExpectedFence: 1, ExpectedAuthorityGeneration: 3, ExpectedAdmissionAttemptRef: receipt.AdmissionAttemptRef, ExpectedAdmissionAttempt: 1, BuildRef: receipt.Artifact.BuildRef, ExpectedBuildAttempt: 1, RecipeGeneration: 1, ManifestDigest: "manifest", ImmutableBuildSHA256: "build", ProvenanceSHA256: "provenance", PolicyRevision: 1, PolicySHA256: "policy", SpecSHA256: "spec"}}
	if !matchesAdmissionTerminalReceipt(receipt, input) {
		t.Fatal("exact original receipt rejected")
	}
	fields := []string{"ArtifactRef", "ExpectedVersion", "ExpectedFence", "ExpectedAuthorityGeneration", "ExpectedAdmissionAttemptRef", "ExpectedAdmissionAttempt", "BuildRef", "ExpectedBuildAttempt", "RecipeGeneration", "ManifestDigest", "ImmutableBuildSHA256", "ProvenanceSHA256", "PolicyRevision", "PolicySHA256", "SpecSHA256"}
	for _, name := range fields {
		t.Run(name, func(t *testing.T) {
			changed := input
			field := reflect.ValueOf(&changed.AdmissionExpiryInput).Elem().FieldByName(name)
			if field.Kind() == reflect.String {
				field.SetString("foreign")
			} else {
				field.SetUint(field.Uint() + 1)
			}
			if matchesAdmissionTerminalReceipt(receipt, changed) {
				t.Fatal("mismatched immutable coordinate accepted")
			}
		})
	}
}
