package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
)

func TestCheckerReportsHealthyClusterVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"major":"1","minor":"31","gitVersion":"v1.31.0"}`))
	}))
	defer server.Close()
	secretValue := kubeconfigForServer(server.URL, "token-that-must-not-leak")
	checker := newTestChecker(t, fakeResolver{value: secretresolver.NewSecretValue([]byte(secretValue))})

	result, err := checker.CheckClusterConnectivity(context.Background(), checkTarget())
	if err != nil {
		t.Fatalf("CheckClusterConnectivity returned error: %v", err)
	}
	if result.Status != enum.ConnectivityCheckStatusSucceeded || result.HealthStatus != enum.ClusterHealthStatusHealthy {
		t.Fatalf("unexpected result: %+v", result)
	}
	var summary map[string]string
	if err := json.Unmarshal(result.SummaryJSON, &summary); err != nil {
		t.Fatalf("summary is not JSON: %v", err)
	}
	if summary["probe"] != "server_version" || summary["kubernetes_version"] != "v1.31.0" {
		t.Fatalf("unexpected summary: %v", summary)
	}
	if strings.Contains(string(result.SummaryJSON), "token-that-must-not-leak") {
		t.Fatalf("secret leaked into summary: %s", result.SummaryJSON)
	}
}

func TestCheckerClassifiesInvalidKubeconfigSafely(t *testing.T) {
	secretValue := "not-a-kubeconfig-token-that-must-not-leak"
	checker := newTestChecker(t, fakeResolver{value: secretresolver.NewSecretValue([]byte(secretValue))})

	result, err := checker.CheckClusterConnectivity(context.Background(), checkTarget())
	if err != nil {
		t.Fatalf("CheckClusterConnectivity returned error: %v", err)
	}
	if result.Status != enum.ConnectivityCheckStatusFailed || result.ErrorCode != "invalid_cluster_secret" {
		t.Fatalf("unexpected invalid kubeconfig result: %+v", result)
	}
	assertNoSecretLeak(t, secretValue, result)
}

func TestCheckerClassifiesMissingSecretSafely(t *testing.T) {
	checker := newTestChecker(t, fakeResolver{err: secretresolver.ErrSecretNotFound})

	result, err := checker.CheckClusterConnectivity(context.Background(), checkTarget())
	if err != nil {
		t.Fatalf("CheckClusterConnectivity returned error: %v", err)
	}
	if result.Status != enum.ConnectivityCheckStatusFailed || result.ErrorCode != "secret_not_found" {
		t.Fatalf("unexpected missing secret result: %+v", result)
	}
}

func TestCheckerClassifiesUnauthorizedAPIWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusUnauthorized)
	}))
	defer server.Close()
	secretValue := kubeconfigForServer(server.URL, "auth-token-that-must-not-leak")
	checker := newTestChecker(t, fakeResolver{value: secretresolver.NewSecretValue([]byte(secretValue))})

	result, err := checker.CheckClusterConnectivity(context.Background(), checkTarget())
	if err != nil {
		t.Fatalf("CheckClusterConnectivity returned error: %v", err)
	}
	if result.Status != enum.ConnectivityCheckStatusFailed || result.ErrorCode != "kubernetes_api_unreachable" {
		t.Fatalf("unexpected unauthorized API result: %+v", result)
	}
	assertNoSecretLeak(t, secretValue, result)
}

func TestCheckerClassifiesUnreachableAPIWithoutLeakingSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	secretValue := kubeconfigForServer(url, "closed-server-token-that-must-not-leak")
	checker := newTestChecker(t, fakeResolver{value: secretresolver.NewSecretValue([]byte(secretValue))})

	result, err := checker.CheckClusterConnectivity(context.Background(), checkTarget())
	if err != nil {
		t.Fatalf("CheckClusterConnectivity returned error: %v", err)
	}
	if result.Status != enum.ConnectivityCheckStatusFailed || result.ErrorCode != "kubernetes_api_unreachable" {
		t.Fatalf("unexpected unreachable API result: %+v", result)
	}
	assertNoSecretLeak(t, secretValue, result)
}

func newTestChecker(t *testing.T, resolver fakeResolver) *Checker {
	t.Helper()
	checker, err := NewChecker(resolver, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("NewChecker(): %v", err)
	}
	return checker
}

func checkTarget() fleetservice.ConnectivityCheckTarget {
	return fleetservice.ConnectivityCheckTarget{
		SecretStoreType: secretresolver.StoreTypeEnv,
		SecretStoreRef:  "KUBECONFIG_SECRET",
	}
}

func kubeconfigForServer(serverURL string, token string) string {
	return `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: ` + serverURL + `
users:
- name: test
  user:
    token: ` + token + `
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
`
}

func assertNoSecretLeak(t *testing.T, secretValue string, result fleetservice.ConnectivityCheckResult) {
	t.Helper()
	values := []string{result.ErrorCode, result.ErrorMessage, string(result.SummaryJSON)}
	for _, value := range values {
		if strings.Contains(value, secretValue) {
			t.Fatalf("secret leaked into result: %s", value)
		}
	}
}

type fakeResolver struct {
	value secretresolver.SecretValue
	err   error
}

func (r fakeResolver) Resolve(ctx context.Context, ref secretresolver.SecretRef) (secretresolver.SecretValue, error) {
	if err := ctx.Err(); err != nil {
		return secretresolver.SecretValue{}, err
	}
	if ref.StoreType == "" || ref.StoreRef == "" {
		return secretresolver.SecretValue{}, secretresolver.ErrInvalidRef
	}
	if r.err != nil {
		return secretresolver.SecretValue{}, r.err
	}
	if r.value.Len() == 0 {
		return secretresolver.SecretValue{}, errors.New("test resolver has no value")
	}
	return r.value, nil
}
