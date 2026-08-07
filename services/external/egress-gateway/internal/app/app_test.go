package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
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
		"EGRESS_GATEWAY_POLICY_FILE":              "/var/run/config/mattercodex/egress-gateway/policy.json",
		"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION": "2026-08-07.1",
		"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST":   strings.Repeat("a", 64),
		"EGRESS_GATEWAY_CONNECT_LISTEN":           ":8080",
		"EGRESS_GATEWAY_TECHNICAL_LISTEN":         ":9090",
		"EGRESS_GATEWAY_RESOLV_CONF":              "/etc/resolv.conf",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	if _, err := loadConfig(); err != nil {
		t.Fatal(err)
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

func loadRepositoryPolicy(t *testing.T) *policy.Active {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..")
	value, err := os.ReadFile(filepath.Join(root, "deploy", "k8s", "base", "egress-gateway", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.Load(value, "2026-08-07.1", "5c71fefd60e624d6891e857442302c2b119f21b76b474d3c34f1c6df330f62ae")
	if err != nil {
		t.Fatal(err)
	}
	return active
}
