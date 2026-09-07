package app

import (
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
)

func TestContinuationHasBoundedIssuerDiagnosticName(t *testing.T) {
	method := api.AuthorizationIssuerService_IssueContinuationAuthorizationContext_FullMethodName
	if normalizedMethod(ModeIssuer, method) != "issue_continuation_authorization_context" {
		t.Fatal("continuation operation is not registered for issuer diagnostics")
	}
	if normalizedMethod(ModeVerifier, method) != "unknown" || normalizedMethod(ModeIssuer, "/foreign.Service/Call") != "unknown" {
		t.Fatal("diagnostic method registry widened unexpectedly")
	}
}
