package platform

import (
	"reflect"
	"testing"

	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAdmissionTerminalReceiptRequiresEveryOriginalImmutableCoordinate accepts an exact synthetic
// claim receipt and mutates all nineteen original expiry, risk and source coordinates to require
// rejection; it tests the pure predicate rather than receipt authentication or terminal SQL.
// TestAdmissionTerminalReceiptRequiresEveryOriginalImmutableCoordinate принимает точную
// синтетическую claim квитанцию и отдельно меняет все девятнадцать expiry, risk и source координат,
// требуя отказа; проверяется чистый predicate, а не подлинность квитанции либо terminal SQL.
func TestAdmissionTerminalReceiptRequiresEveryOriginalImmutableCoordinate(t *testing.T) {
	receipt := admissionClaimReceipt{Artifact: entity.ImageArtifact{Ref: "imgart_exact", Version: 2, BuildRef: "imgbld_exact", BuildAttempt: 1, RecipeGeneration: 1, ManifestDigest: "manifest", ImmutableBuildSHA256: "build", ProvenanceSHA256: "provenance", PolicyRevision: 1, PolicySHA256: "policy", SpecSHA256: "spec"}, Fence: 1, AuthorityGeneration: 3, AdmissionAttemptRef: "imgadm_exact", AdmissionAttempt: 1}
	input := repo.AdmissionTerminalInput{AdmissionExpiryInput: repo.AdmissionExpiryInput{ArtifactRef: receipt.Artifact.Ref, ExpectedVersion: 2, ExpectedFence: 1, ExpectedAuthorityGeneration: 3, ExpectedAdmissionAttemptRef: receipt.AdmissionAttemptRef, ExpectedAdmissionAttempt: 1, BuildRef: receipt.Artifact.BuildRef, ExpectedBuildAttempt: 1, RecipeGeneration: 1, ManifestDigest: "manifest", ImmutableBuildSHA256: "build", ProvenanceSHA256: "provenance", PolicyRevision: 1, PolicySHA256: "policy", SpecSHA256: "spec"}}
	if !matchesAdmissionTerminalReceipt(receipt, input) {
		t.Fatal("exact original receipt rejected")
	}
	fields := []string{"ArtifactRef", "ExpectedVersion", "ExpectedFence", "ExpectedAuthorityGeneration", "ExpectedAdmissionAttemptRef", "ExpectedAdmissionAttempt", "BuildRef", "ExpectedBuildAttempt", "RecipeGeneration", "ManifestDigest", "ImmutableBuildSHA256", "ProvenanceSHA256", "PolicyRevision", "PolicySHA256", "SpecSHA256", "RiskAcceptanceSHA256", "SourceAdmissionRevision", "SourceAdmissionReceiptSHA256", "SourceEvidenceManifestDigest"}
	for _, name := range fields {
		t.Run(name, func(t *testing.T) {
			changed := input
			field := reflect.ValueOf(&changed).Elem().FieldByName(name)
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
