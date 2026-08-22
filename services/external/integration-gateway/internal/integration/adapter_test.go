package integration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmptyConnectionCatalogKeepsAdapterConstructible(t *testing.T) {
	t.Parallel()
	adapter, err := New(Config{CredentialDirectory: "/var/run/secrets/mattercodex/integration-connections", ProxyURL: "http://egress-gateway.mattercodex-system.svc.cluster.local:8080", Timeout: 10 * time.Second})
	if err != nil || adapter == nil {
		t.Fatalf("New() error = %v", err)
	}
}

func TestConfiguredBaseURLRequiresDeploymentAllowlist(t *testing.T) {
	t.Parallel()
	adapter, err := New(Config{CredentialDirectory: "/run/credentials", ProxyURL: "http://egress-gateway.mattercodex-system.svc.cluster.local:8080", AllowedHosts: "chat.example.test", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.configuredBaseURL(Request{Configuration: map[string]any{"base_url": "https://forged.example.test"}}, "base_url"); err == nil {
		t.Fatal("configuredBaseURL() accepted a host outside deployment allowlist")
	}
}

func TestReadCredentialAllowsProjectedSymlinkInsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	versioned := filepath.Join(root, "..data")
	if err := os.Mkdir(versioned, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versioned, "token"), []byte("secret-value"), 0o640); err != nil {
		t.Fatal(err)
	}
	binding := filepath.Join(root, "github-main")
	if err := os.Mkdir(binding, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..data", "token"), filepath.Join(binding, "token")); err != nil {
		t.Fatal(err)
	}
	adapter := &Adapter{credentialDirectory: root}
	value, err := adapter.readCredential("github-main", "token")
	if err != nil || string(value) != "secret-value" {
		t.Fatalf("readCredential() = %q, %v", value, err)
	}
}

func TestOutcomeExposesOnlySafeCode(t *testing.T) {
	t.Parallel()
	success, code := Outcome(errors.New("raw provider response with secret"))
	if success || code != "INTEGRATION_UNAVAILABLE" {
		t.Fatalf("Outcome() = %v, %q", success, code)
	}
}

func TestRepositoryPathRejectsTraversal(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"../secret", "/absolute", "folder//file", "folder/./file"} {
		if safeRepositoryPath(value) {
			t.Fatalf("safeRepositoryPath(%q) = true", value)
		}
	}
}

func TestGitHubProjectionDropsProviderLinks(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"type":"file","name":"report.txt","path":"reports/report.txt","sha":"safe-digest","size":7,"encoding":"base64","content":"cmVwb3J0","download_url":"https://token-bearing.example.test/raw"}`)
	projected, err := projectGitHubContent(raw, "reports/report.txt")
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid([]byte(projected)) == false || contains(projected, "download_url") || contains(projected, "token-bearing") {
		t.Fatalf("provider-only field escaped projection: %s", projected)
	}
}

func TestKubernetesProjectionDropsSpecAndSensitiveMetadata(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"sales-worker","namespace":"sales","generation":4,"annotations":{"secret.example/token":"must-not-escape"}},"spec":{"template":{"spec":{"containers":[{"env":[{"name":"TOKEN","value":"must-not-escape"}]}]}}},"status":{"observedGeneration":4,"replicas":2,"readyReplicas":2,"availableReplicas":2,"conditions":[{"type":"Available","status":"True","reason":"MinimumReplicasAvailable","message":"unbounded provider message"}]}}`)
	projected, err := projectKubernetesWorkload(raw, "sales", "sales-worker")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"must-not-escape", "annotations", "spec", "message"} {
		if contains(projected, forbidden) {
			t.Fatalf("provider-only field %q escaped projection: %s", forbidden, projected)
		}
	}
}

func contains(value, fragment string) bool {
	return strings.Contains(value, fragment)
}
