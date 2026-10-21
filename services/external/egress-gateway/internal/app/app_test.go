package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// TestStateAndSharedTechnicalServerPublishEffectiveReadinessAndReadback checks the active lifecycle and public view.
// TestStateAndSharedTechnicalServerPublishEffectiveReadinessAndReadback проверяет цикл активной политики и публичное состояние.
func TestStateAndSharedTechnicalServerPublishEffectiveReadinessAndReadback(t *testing.T) {
	active := loadRepositoryPolicy(t)
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{})
	business, err := internalobservability.New(metrics.Register)
	if err != nil {
		t.Fatal(err)
	}
	current := newState(active, readiness, metrics, business)
	if ready, _ := current.Ready(); ready {
		t.Fatal("BOOTING state must not be ready")
	}
	current.setResolverConfigured()
	current.setProcess(processReady)
	if ready, _ := current.Ready(); !ready {
		t.Fatal("ACTIVE policy and configured resolver must be ready")
	}
	request := httptest.NewRequest(http.MethodGet, "/policy", nil)
	response := httptest.NewRecorder()
	newPolicyHandler(current).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"policyState":"ACTIVE"`) ||
		!strings.Contains(response.Body.String(), `"resolverState":"CONFIGURED"`) || !strings.Contains(response.Body.String(), active.Digest()) {
		t.Fatalf("unexpected safe readback: %d %s", response.Code, response.Body.String())
	}
	current.setProcess(processDraining)
	if ready, _ := current.Ready(); ready {
		t.Fatal("DRAINING state must become not-ready before shutdown")
	}
}

// TestInvalidPolicyUsesSharedReadinessAndSafeReadback keeps invalid policy details out of readback.
// TestInvalidPolicyUsesSharedReadinessAndSafeReadback не выдаёт детали недопустимой политики.
func TestInvalidPolicyUsesSharedReadinessAndSafeReadback(t *testing.T) {
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{})
	business, err := internalobservability.New(metrics.Register)
	if err != nil {
		t.Fatal(err)
	}
	current := newInvalidPolicyState(readiness, metrics, business)
	if ready, _ := current.Ready(); ready {
		t.Fatal("invalid policy must stay not ready")
	}
	response := httptest.NewRecorder()
	newPolicyHandler(current).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/policy", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"policyState":"INVALID"`) ||
		!strings.Contains(response.Body.String(), `"revision":""`) || !strings.Contains(response.Body.String(), `"digest":""`) {
		t.Fatalf("unexpected invalid policy readback: %s", response.Body.String())
	}
}

// TestConfigUsesOneTypedParseAndEnforcesCanonicalDigest checks port bindings and digest syntax.
// TestConfigUsesOneTypedParseAndEnforcesCanonicalDigest проверяет порты и формат дайджеста.
func TestConfigUsesOneTypedParseAndEnforcesCanonicalDigest(t *testing.T) {
	values := map[string]string{
		"EGRESS_GATEWAY_POLICY_FILE":                       "/var/run/config/kodex/egress-gateway/policy.json",
		"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION":          "2026-08-07.1",
		"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST":            strings.Repeat("a", 64),
		"EGRESS_GATEWAY_CONNECT_LISTEN":                    ":8080",
		"EGRESS_GATEWAY_STT_CONNECT_LISTEN":                ":8081",
		"EGRESS_GATEWAY_MAIL_CONNECT_LISTEN":               ":8082",
		"EGRESS_GATEWAY_MAIL_POLICY_FILE":                  "/var/run/config/kodex/egress-gateway-mail/policy.json",
		"EGRESS_GATEWAY_MAIL_POLICY_DIGEST":                strings.Repeat("b", 64),
		"EGRESS_GATEWAY_INTEGRATION_CONNECT_LISTEN":        ":8083",
		"EGRESS_GATEWAY_INTEGRATION_POLICY_FILE":           "/var/run/config/kodex/egress-gateway-integration/integration-policy.json",
		"EGRESS_GATEWAY_INTEGRATION_POLICY_DIGEST":         strings.Repeat("c", 64),
		"EGRESS_GATEWAY_RUNTIME_CONNECT_LISTEN":            ":8084",
		"EGRESS_GATEWAY_RUNTIME_SIGNING_KEY_FILE":          "/var/run/secrets/kodex/egress-gateway/runtime-signing/key",
		"EGRESS_GATEWAY_RUNTIME_PROXY_CA_CERTIFICATE_FILE": "/var/run/secrets/kodex/egress-gateway/runtime-proxy-ca/tls.crt",
		"EGRESS_GATEWAY_RUNTIME_PROXY_CA_PRIVATE_KEY_FILE": "/var/run/secrets/kodex/egress-gateway/runtime-proxy-ca/tls.key",
		"EGRESS_GATEWAY_TECHNICAL_LISTEN":                  ":9090",
		"EGRESS_GATEWAY_RESOLV_CONF":                       "/etc/resolv.conf",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	config, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.ConnectAddress, config.STTConnectAddress = config.STTConnectAddress, config.ConnectAddress
	if err := config.validate(); err == nil {
		t.Fatal("listener configuration must not exchange CNI-bound profiles")
	}
	t.Setenv("EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST", strings.Repeat("A", 64))
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected uppercase digest to fail canonical validation")
	}
}

// TestClamavModeRequiresOnlyExactSingleDestinationListener rejects unregistered modes.
// TestClamavModeRequiresOnlyExactSingleDestinationListener отклоняет незарегистрированные режимы.
func TestClamavModeRequiresOnlyExactSingleDestinationListener(t *testing.T) {
	for key, value := range map[string]string{
		"EGRESS_GATEWAY_MODE":                     "clamav",
		"EGRESS_GATEWAY_POLICY_FILE":              "/var/run/config/kodex/clamav-egress/policy.json",
		"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION": "2026-09-27.1",
		"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST":   strings.Repeat("a", 64),
		"EGRESS_GATEWAY_CONNECT_LISTEN":           ":8080",
		"EGRESS_GATEWAY_TECHNICAL_LISTEN":         ":9090",
		"EGRESS_GATEWAY_RESOLV_CONF":              "/etc/resolv.conf",
	} {
		t.Setenv(key, value)
	}
	config, err := loadConfig()
	if err != nil || config.Mode != "clamav" {
		t.Fatalf("single destination mode rejected: %v", err)
	}
	t.Setenv("EGRESS_GATEWAY_MODE", "unregistered")
	if _, err := loadConfig(); err == nil {
		t.Fatal("unregistered egress mode must be rejected")
	}
}

// TestInvalidPolicyRuntimeCancelsAndJoinsWithoutConnectListener checks bounded shutdown on degraded startup.
// TestInvalidPolicyRuntimeCancelsAndJoinsWithoutConnectListener проверяет остановку при деградации запуска.
func TestInvalidPolicyRuntimeCancelsAndJoinsWithoutConnectListener(t *testing.T) {
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{})
	business, err := internalobservability.New(metrics.Register)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- runTechnicalOnly(lifecycle, context.Background(), Config{
			TechnicalAddress: "127.0.0.1:0", ConnectAddress: "127.0.0.1:0",
			STTConnectAddress:         "127.0.0.1:0",
			MailConnectAddress:        "127.0.0.1:0",
			IntegrationConnectAddress: "127.0.0.1:0",
		}, newInvalidPolicyState(readiness, metrics, business), metrics, business)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("invalid-policy technical runtime did not cancel and join")
	}
}

// TestShutdownBudgetMatchesDeploymentContract pins the grace period used by deployment.
// TestShutdownBudgetMatchesDeploymentContract закрепляет запас времени установки.
func TestShutdownBudgetMatchesDeploymentContract(t *testing.T) {
	if MinimumTerminationGrace != 45*time.Second {
		t.Fatalf("unexpected minimum termination grace: %s", MinimumTerminationGrace)
	}
}

// TestReadinessWorkerResultDistinguishesShutdownFromFailure separates cancellation and worker failure.
// TestReadinessWorkerResultDistinguishesShutdownFromFailure различает отмену и сбой worker.
func TestReadinessWorkerResultDistinguishesShutdownFromFailure(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	cancel()
	if err := readinessWorkerResult(lifecycle, fmt.Errorf("wait workers: %w", context.Canceled)); err != nil {
		t.Fatalf("normal lifecycle cancellation must succeed: %v", err)
	}

	workerFailure := errors.New("readiness refresh failed")
	err := readinessWorkerResult(context.Background(), workerFailure)
	if !errors.Is(err, workerFailure) {
		t.Fatalf("unexpected worker failure must remain fail-closed: %v", err)
	}
	if err := readinessWorkerResult(context.Background(), context.Canceled); err == nil {
		t.Fatal("worker cancellation without lifecycle shutdown must fail")
	}
}

// loadRepositoryPolicy loads the checked-in policy fixture for app lifecycle tests.
// loadRepositoryPolicy загружает закреплённую policy-fixture для проверок жизненного цикла.
func loadRepositoryPolicy(t *testing.T) *policy.Active {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..")
	value, err := os.ReadFile(filepath.Join(root, "deploy", "k8s", "base", "egress-gateway", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.Load(value, "2026-09-05.1", "8529a00f3e8923e59d1776ee64d1965ee1e8f891daa17b94927c816248d6f218")
	if err != nil {
		t.Fatal(err)
	}
	return active
}
