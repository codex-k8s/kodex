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
	current.setResolverReady(true)
	current.setProcess(processReady)
	if ready, _ := current.Ready(); !ready {
		t.Fatal("ACTIVE policy and validated resolver must be ready")
	}
	request := httptest.NewRequest(http.MethodGet, "/policy", nil)
	response := httptest.NewRecorder()
	newPolicyHandler(current).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"policyState":"ACTIVE"`) ||
		!strings.Contains(response.Body.String(), `"resolverState":"VALIDATED"`) || !strings.Contains(response.Body.String(), active.Digest()) {
		t.Fatalf("unexpected safe readback: %d %s", response.Code, response.Body.String())
	}
	current.setProcess(processDraining)
	if ready, _ := current.Ready(); ready {
		t.Fatal("DRAINING state must become not-ready before shutdown")
	}
}

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

func TestConfigUsesOneTypedParseAndEnforcesCanonicalDigest(t *testing.T) {
	values := map[string]string{
		"EGRESS_GATEWAY_POLICY_FILE":              "/var/run/config/kodex/egress-gateway/policy.json",
		"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION": "2026-08-07.1",
		"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST":   strings.Repeat("a", 64),
		"EGRESS_GATEWAY_CONNECT_LISTEN":           ":8080",
		"EGRESS_GATEWAY_STT_CONNECT_LISTEN":       ":8081",
		"EGRESS_GATEWAY_TECHNICAL_LISTEN":         ":9090",
		"EGRESS_GATEWAY_RESOLV_CONF":              "/etc/resolv.conf",
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
			STTConnectAddress: "127.0.0.1:0",
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

func TestShutdownBudgetMatchesDeploymentContract(t *testing.T) {
	if MinimumTerminationGrace != 45*time.Second {
		t.Fatalf("unexpected minimum termination grace: %s", MinimumTerminationGrace)
	}
}

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
