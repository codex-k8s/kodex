package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
)

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

func TestSecretBrokerOwnerOperationsIncludeFailureAndRecovery(t *testing.T) {
	t.Parallel()
	operations := secretBrokerOperations()
	if len(operations) != 10 ||
		operations["platform.runtime-secrets.operations.fail"] != controlplanev1.RuntimeSecretWorkService_FailRuntimeSecretOperation_FullMethodName ||
		operations["platform.runtime-secrets.operations.recover"] != controlplanev1.RuntimeSecretWorkService_ListRuntimeSecretRecoveryWork_FullMethodName ||
		operations["platform.runtime-secrets.materialization.recover"] != controlplanev1.RuntimeSecretWorkService_RecoverRuntimeSecretMaterialization_FullMethodName ||
		operations["platform.credential-projections.runtime.resolve"] != controlplanev1.RuntimeSecretWorkService_ResolveRuntimeCredentialProjection_FullMethodName ||
		operations["platform.credential-projections.runtime.validate"] != controlplanev1.RuntimeSecretWorkService_ValidateRuntimeCredentialProjection_FullMethodName ||
		operations["platform.credential-projections.stt.resolve"] != controlplanev1.RuntimeSecretWorkService_ResolveTranscriptionCredentialProjection_FullMethodName {
		t.Fatalf("secret broker owner operation registry is incomplete: %#v", operations)
	}
}
