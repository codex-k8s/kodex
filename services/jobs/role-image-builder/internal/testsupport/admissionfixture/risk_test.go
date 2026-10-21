package admissionfixture

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestRiskAcceptanceCopiesOnlyTheOriginalBindingFields varies the report inputs
// and checks every copied binding plus the derived source manifest. ProjectRef is
// deliberately not copied: this helper preserves the original organization fixture,
// rather than extending it into a new project-scope validation scenario.
// TestRiskAcceptanceCopiesOnlyTheOriginalBindingFields меняет входной отчёт
// и проверяет все переносимые привязки и производный исходный манифест. ProjectRef
// намеренно не копируется: helper сохраняет прежнюю organization-fixture, а не
// расширяет её новым сценарием проверки проектной области.
func TestRiskAcceptanceCopiesOnlyTheOriginalBindingFields(t *testing.T) {
	for _, suffix := range []string{"first", "second"} {
		binding := runtimecontract.ImageVulnerabilityReport{
			ScopeKind: "ORGANIZATION", OrganizationRef: "organization-" + suffix,
			ProjectRef: "unused-project", ArtifactRef: "artifact-" + suffix, ImageDigest: "image-" + suffix,
			RecipeRef: "recipe-" + suffix, RecipeVersion: 11, RecipeGeneration: 13,
			BuildRef: "build-" + suffix, BuildVersion: 17, BuildAttempt: 19,
			PolicyRevision: 23, PolicySHA256: "policy-" + suffix,
		}
		got := RiskAcceptance(binding, "report-"+suffix, "projection-"+suffix, "receipt-"+suffix)
		if got.ScopeKind != binding.ScopeKind || got.OrganizationRef != binding.OrganizationRef ||
			got.ProjectRef != "" || got.ArtifactRef != binding.ArtifactRef || got.ImageDigest != binding.ImageDigest ||
			got.RecipeRef != binding.RecipeRef || got.RecipeVersion != binding.RecipeVersion || got.RecipeGeneration != binding.RecipeGeneration ||
			got.BuildRef != binding.BuildRef || got.BuildVersion != binding.BuildVersion || got.BuildAttempt != binding.BuildAttempt ||
			got.PolicyRevision != binding.PolicyRevision || got.PolicySHA256 != binding.PolicySHA256 {
			t.Fatal("synthetic risk binding changed")
		}
		if got.ReportSHA256 != "report-"+suffix || got.ProjectionSHA256 != "projection-"+suffix ||
			got.SourceAdmissionReceiptSHA256 != "receipt-"+suffix || got.SourceEvidenceManifestDigest != "sha256:receipt-"+suffix {
			t.Fatal("synthetic report or source receipt binding changed")
		}
	}
}

// TestRiskAcceptanceKeepsSyntheticMetadataAndDoesNotValidate checks the fixed fake
// decision metadata and proves that empty inputs are not repaired or treated as a
// validated decision; caller-side canonicalization and validation remain mandatory.
// TestRiskAcceptanceKeepsSyntheticMetadataAndDoesNotValidate проверяет фиксированные
// вымышленные метаданные и отсутствие исправления либо допуска пустых данных;
// канонизация и проверка по-прежнему выполняются вызывающим кодом.
func TestRiskAcceptanceKeepsSyntheticMetadataAndDoesNotValidate(t *testing.T) {
	got := RiskAcceptance(runtimecontract.ImageVulnerabilityReport{}, "", "", "")
	if got.Schema != runtimecontract.ImageRiskAcceptanceSchema || got.DecisionRef != "imgrisk_abcdefgh" ||
		got.DecisionVersion != 1 || got.Action != "ACCEPT_RISK" || got.SourceAdmissionRevision != 1 ||
		got.Reason != "Риск принят для точного образа" || got.DecidedByActorRef != "act_abcdefgh" || got.DecidedAt != "2026-10-05T05:00:00Z" {
		t.Fatal("synthetic decision metadata changed")
	}
	if _, err := runtimecontract.CanonicalImageRiskAcceptance(got); err == nil {
		t.Fatal("incomplete synthetic binding unexpectedly became valid")
	}
}
