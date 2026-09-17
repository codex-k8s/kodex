package platform

import (
	"strings"
	"testing"
)

func TestRoleImageAdmissionPolicyRotationQueries(t *testing.T) {
	reject := strings.ToLower(queryRoleImagesRejectStaleAdmissionCandidates)
	for _, required := range []string{
		"admission_state in ('pending', 'claimed')",
		"policy_revision <> @policy_revision",
		"policy_sha256 <> @policy_sha256",
		"admission_state = 'rejected'",
		"admission_claim_token_sha256 = null",
		"admission_claim_expires_at = null",
		"promotion_state = 'rejected'",
		"promotion_claim_token_sha256 = null",
		"promotion_authorization_token_sha256 = null",
		"request.state in ('queued', 'promoting')",
	} {
		if !strings.Contains(reject, required) {
			t.Fatalf("stale admission terminalization does not enforce %q", required)
		}
	}
	if strings.Contains(reject, "admission_verdict = 'rejected'") {
		t.Fatal("policy rotation synthesized a scanner admission verdict")
	}

	claim := strings.ToLower(queryRoleImagesClaimAdmissionCandidate)
	for _, required := range []string{
		"policy_revision = @policy_revision",
		"policy_sha256 = @policy_sha256",
		"admission_state = 'pending'",
		"admission_claim_expires_at <= clock_timestamp()",
	} {
		if !strings.Contains(claim, required) {
			t.Fatalf("admission claim selector does not enforce %q", required)
		}
	}
}

func TestRoleImageSupplyWorkAvailabilityIsReadOnlyAndUsesClaimEligibility(t *testing.T) {
	query := strings.ToLower(queryRoleImagesGetSupplyWorkAvailability)
	for _, required := range []string{
		"artifact.policy_revision = @policy_revision",
		"artifact.policy_sha256 = @policy_sha256",
		"artifact.admission_state = 'pending'",
		"artifact.admission_claim_expires_at <= clock_timestamp()",
		"request.state = 'queued' and artifact.promotion_state = 'pending'",
		"artifact.promotion_claim_expires_at <= clock_timestamp()",
		"artifact.promotion_authorization_expires_at <= clock_timestamp()",
		"recipe.version = artifact.recipe_version",
		"recipe.generation = artifact.recipe_generation",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("supply work availability does not enforce %q", required)
		}
	}
	for _, forbidden := range []string{"for update", "insert ", "update ", "delete "} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("supply work availability is not read-only: %q", forbidden)
		}
	}
}
