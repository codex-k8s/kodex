package app

import (
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
)

// TestContinuationHasBoundedIssuerDiagnosticName checks the continuation RPC has its exact issuer diagnostic label while the verifier profile and a foreign method normalize to unknown. It exercises only local label mapping, not RPC authorization or continuation issuance.
// TestContinuationHasBoundedIssuerDiagnosticName проверяет точную диагностическую метку RPC продолжения для issuer, тогда как профиль verifier и чужой метод дают unknown. Проверяется только локальное сопоставление меток, а не полномочия RPC или выдача продолжения.
func TestContinuationHasBoundedIssuerDiagnosticName(t *testing.T) {
	method := api.AuthorizationIssuerService_IssueContinuationAuthorizationContext_FullMethodName
	if normalizedMethod(ModeIssuer, method) != "issue_continuation_authorization_context" {
		t.Fatal("continuation operation is not registered for issuer diagnostics")
	}
	if normalizedMethod(ModeVerifier, method) != "unknown" || normalizedMethod(ModeIssuer, "/foreign.Service/Call") != "unknown" {
		t.Fatal("diagnostic method registry widened unexpectedly")
	}
}
