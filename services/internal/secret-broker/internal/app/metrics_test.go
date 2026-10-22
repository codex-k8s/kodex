package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
)

// TestMetricsSubsystemProducesValidPrometheusNames scrapes an in-memory HTTP recorder and checks
// build/readiness names use the intended subsystem without starting a listener.
// TestMetricsSubsystemProducesValidPrometheusNames получает metrics через in-memory HTTP recorder и
// проверяет build/readiness names нужного subsystem без запуска listener.
func TestMetricsSubsystemProducesValidPrometheusNames(t *testing.T) {
	t.Parallel()

	metrics := sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{
		"/secretbroker.v1.SecretBrokerService/CreateSecret": "create",
	})
	metrics.SetReady(true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/metrics", nil)
	metrics.PrometheusHandler().ServeHTTP(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("metrics status = %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "kodex_secret_broker_build_info") ||
		!strings.Contains(body, "kodex_secret_broker_readiness") {
		t.Fatalf("secret broker metrics are missing: %q", body)
	}
}

// TestSecretBrokerOwnerOperationsIncludeFailureAndRecovery checks the closed 20-entry owner registry and
// exact renewal/secret/projection/draft RPC mappings, not live authorization effectiveness.
// TestSecretBrokerOwnerOperationsIncludeFailureAndRecovery проверяет закрытый 20-entry owner registry и
// exact renewal/secret/projection/draft RPC mappings, не live authorization effectiveness.
func TestSecretBrokerOwnerOperationsIncludeFailureAndRecovery(t *testing.T) {
	t.Parallel()
	operations := secretBrokerOperations()
	if len(operations) != 20 ||
		operations["platform.provider-accounts.credential.renewal.authorize"] != controlplanev1.ProviderCredentialRenewalWorkService_AuthorizeProviderCredentialRenewal_FullMethodName ||
		operations["platform.provider-accounts.credential.renewal.validate"] != controlplanev1.ProviderCredentialRenewalWorkService_ValidateProviderCredentialRenewalObservation_FullMethodName ||
		operations["platform.runtime-secrets.operations.fail"] != controlplanev1.RuntimeSecretWorkService_FailRuntimeSecretOperation_FullMethodName ||
		operations["platform.runtime-secrets.operations.recover"] != controlplanev1.RuntimeSecretWorkService_ListRuntimeSecretRecoveryWork_FullMethodName ||
		operations["platform.runtime-secrets.materialization.recover"] != controlplanev1.RuntimeSecretWorkService_RecoverRuntimeSecretMaterialization_FullMethodName ||
		operations["platform.credential-projections.runtime.resolve"] != controlplanev1.RuntimeSecretWorkService_ResolveRuntimeCredentialProjection_FullMethodName ||
		operations["platform.credential-projections.runtime.validate"] != controlplanev1.RuntimeSecretWorkService_ValidateRuntimeCredentialProjection_FullMethodName ||
		operations["platform.credential-projections.stt.resolve"] != controlplanev1.RuntimeSecretWorkService_ResolveTranscriptionCredentialProjection_FullMethodName {
		t.Fatalf("secret broker owner operation registry is incomplete: %#v", operations)
	}
	for id, method := range map[string]string{
		"platform.runtime-secret-drafts.readiness.check":         controlplanev1.RuntimeSecretDraftWorkService_CheckRuntimeSecretDraftWorkReadiness_FullMethodName,
		"platform.runtime-secret-drafts.operations.consume":      controlplanev1.RuntimeSecretDraftWorkService_ConsumeRuntimeSecretDraftOperation_FullMethodName,
		"platform.runtime-secret-drafts.operations.complete":     controlplanev1.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftOperation_FullMethodName,
		"platform.runtime-secret-drafts.operations.fail":         controlplanev1.RuntimeSecretDraftWorkService_FailRuntimeSecretDraftOperation_FullMethodName,
		"platform.runtime-secret-drafts.operations.recover":      controlplanev1.RuntimeSecretDraftWorkService_ListRuntimeSecretDraftRecoveryWork_FullMethodName,
		"platform.runtime-secret-drafts.materialization.recover": controlplanev1.RuntimeSecretDraftWorkService_RecoverRuntimeSecretDraftMaterialization_FullMethodName,
		"platform.runtime-secret-drafts.cleanup.complete":        controlplanev1.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftCleanup_FullMethodName,
	} {
		if operations[id] != method {
			t.Fatal("secret draft owner operation registry is incomplete")
		}
	}
}
