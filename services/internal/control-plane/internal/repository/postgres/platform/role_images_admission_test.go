package platform

import (
	"strings"
	"testing"
)

// TestRoleImageAdmissionPolicyRotationQueries inspects embedded SQL for stale-policy
// terminalization, claim-token cleanup, pending/current-policy/latest-build selection and owner
// expiry instead of lease reclaim; static clauses do not execute or prove concurrent SQL behavior.
// TestRoleImageAdmissionPolicyRotationQueries проверяет embedded SQL на завершение старой policy,
// очистку claim tokens, выбор pending/current-policy/latest-build и owner expiry вместо повторного
// claim; проверка clauses не исполняет SQL и не доказывает конкурентное поведение.
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
		"build.stage = 'completed'",
		"recipe.version = artifact.recipe_version",
		"latest.created_at desc",
	} {
		if !strings.Contains(claim, required) {
			t.Fatalf("admission claim selector does not enforce %q", required)
		}
	}
	if strings.Contains(claim, "admission_claim_expires_at") {
		t.Fatal("expired admission claim can be reclaimed instead of terminalized")
	}
	for _, required := range []string{"admission_claim_expires_at <= clock_timestamp()", "admission_state = 'failed'", "admission_claim_token_sha256 = null"} {
		if !strings.Contains(strings.ToLower(queryRoleImagesExpireAdmissions), required) {
			t.Fatalf("owner expiry does not enforce %q", required)
		}
	}
}

// TestRoleImageSupplyWorkAvailabilityIsReadOnlyAndUsesClaimEligibility checks embedded
// availability SQL contains policy, recipe and expiry eligibility while forbidding locks and write
// clauses; it verifies query shape without connecting to PostgreSQL.
// TestRoleImageSupplyWorkAvailabilityIsReadOnlyAndUsesClaimEligibility проверяет наличие policy,
// recipe и expiry eligibility в embedded availability SQL при запрете locks и write clauses;
// проверяется форма запроса без соединения с PostgreSQL.
func TestRoleImageSupplyWorkAvailabilityIsReadOnlyAndUsesClaimEligibility(t *testing.T) {
	query := strings.ToLower(queryRoleImagesGetSupplyWorkAvailability)
	for _, required := range []string{
		"artifact.policy_revision = @policy_revision",
		"artifact.policy_sha256 = @policy_sha256",
		"artifact.admission_state = 'pending'",
		"artifact.admission_claim_expires_at <= clock_timestamp()",
		"artifact.admission_state in ('pending', 'claimed')",
		"artifact.policy_revision <> @policy_revision",
		"artifact.policy_sha256 <> @policy_sha256",
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
