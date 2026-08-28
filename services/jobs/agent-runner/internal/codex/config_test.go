package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestPrepareHomeDeniesShellReadOfProviderState(t *testing.T) {
	workspace := t.TempDir()
	home := filepath.Join(workspace, ".kodex", "state", "codex-home")
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	digestFile := filepath.Join(workspace, "auth.sha256")
	if err := os.WriteFile(digestFile, []byte(hex.EncodeToString(digest[:])), 0o600); err != nil {
		t.Fatal(err)
	}
	input := model.Input{WorkspaceRoot: workspace, CodexHome: home, Model: "gpt-5",
		CodexApprovalPolicy: "never", CodexSandbox: "workspace-write",
		ProviderAuthSHA256File: digestFile, ProviderCredentialSHA256: hex.EncodeToString(digest[:])}
	if err := PrepareHomeWithAuth(input, "http://127.0.0.1:12345/mcp", auth); err != nil {
		t.Fatalf("PrepareHomeWithAuth() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var config runtimeConfig
	metadata, err := toml.Decode(string(raw), &config)
	profile := config.Permissions[config.DefaultPermissions]
	if err != nil || len(metadata.Undecoded()) != 0 || profile.Extends != ":workspace" ||
		profile.Filesystem[filepath.Join(home, "auth.json")] != "deny" || profile.Filesystem[home] != "" ||
		profile.Filesystem["/proc"] != "deny" ||
		profile.Filesystem["/run/secrets"] != "deny" ||
		profile.Filesystem["/var/run/secrets"] != "" ||
		config.MCPServers["kodex"].BearerTokenEnvVar != "KODEX_MCP_PROXY_TOKEN" {
		t.Fatalf("provider permission boundary is incomplete: %#v", config)
	}
	for path := range profile.Filesystem {
		if filepath.IsAbs(path) && strings.Contains(path, "*") {
			t.Fatalf("absolute deny path must not require a pre-sandbox glob scan: %q", path)
		}
	}
}

func TestPrepareHomePreservesPinnedSandboxBoundary(t *testing.T) {
	for sandbox, expected := range map[string]string{"read-only": ":read-only", "workspace-write": ":workspace"} {
		t.Run(sandbox, func(t *testing.T) {
			workspace := t.TempDir()
			home := filepath.Join(workspace, ".kodex", "state", "codex-home")
			auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
			digest := sha256.Sum256(auth)
			digestFile := filepath.Join(workspace, "auth.sha256")
			if err := os.WriteFile(digestFile, []byte(hex.EncodeToString(digest[:])), 0o600); err != nil {
				t.Fatal(err)
			}
			input := model.Input{WorkspaceRoot: workspace, CodexHome: home, Model: "gpt-5",
				CodexApprovalPolicy: "never", CodexSandbox: sandbox,
				ProviderAuthSHA256File: digestFile, ProviderCredentialSHA256: hex.EncodeToString(digest[:])}
			if err := PrepareHomeWithAuth(input, "http://127.0.0.1:12345/mcp", auth); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			var config runtimeConfig
			if _, err := toml.Decode(string(raw), &config); err != nil || config.Permissions[config.DefaultPermissions].Extends != expected {
				t.Fatalf("sandbox %s expanded to %#v: %v", sandbox, config.Permissions, err)
			}
		})
	}
	if _, err := codexPermissionBase("danger-full-access"); err == nil {
		t.Fatal("danger-full-access was accepted")
	}
	if _, err := codexPermissionBase("unknown"); err == nil {
		t.Fatal("unknown sandbox was accepted")
	}
}

func TestPrepareHomeMaterializesOnlyBoundEnvironment(t *testing.T) {
	workspace := t.TempDir()
	home := filepath.Join(workspace, ".kodex", "state", "codex-home")
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	digestFile := filepath.Join(workspace, "auth.sha256")
	if err := os.WriteFile(digestFile, []byte(hex.EncodeToString(digest[:])), 0o600); err != nil {
		t.Fatal(err)
	}
	input := model.Input{WorkspaceRoot: workspace, CodexHome: home, Model: "gpt-5",
		CodexApprovalPolicy: "never", CodexSandbox: "workspace-write",
		ProviderAuthSHA256File: digestFile, ProviderCredentialSHA256: hex.EncodeToString(digest[:]),
		ConfigOverlay:     "model_reasoning_effort = \"high\"\n\n[history]\npersistence = \"none\"\n",
		EnvironmentValues: []runtimecontract.RuntimeEnvironmentValue{{Name: "APP_MODE", Value: "review"}},
		SecretProjections: []runtimecontract.RuntimeSecretProjection{{Name: "CRM_TOKEN", SecretName: "runtime-crm-v1", SecretKey: "token",
			SecretUID: "7fe2f86e-4bb9-4325-a983-a389367c1cbf", SecretResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64)}}}
	if err := PrepareHomeWithAuth(input, "http://127.0.0.1:12345/mcp", auth); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config runtimeConfig
	if _, err := toml.Decode(string(raw), &config); err != nil {
		t.Fatal(err)
	}
	if config.ModelReasoningEffort != "high" || config.History.Persistence != "none" ||
		config.ShellEnvironmentPolicy.Set["APP_MODE"] != "review" ||
		config.ShellEnvironmentPolicy.Set["CRM_TOKEN"] != "" ||
		config.ShellEnvironmentPolicy.Filters["CRM_TOKEN"] != "include" ||
		config.ShellEnvironmentPolicy.Filters["KODEX_MCP_PROXY_TOKEN"] != "" {
		t.Fatalf("unexpected effective config: %#v", config)
	}
}

func TestValidateProviderAuthenticationFailsClosed(t *testing.T) {
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	if err := validateProviderAuthenticationPayload(auth, hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("validateProviderAuthenticationPayload() error = %v", err)
	}
	if err := validateProviderAuthenticationPayload(auth, "invalid"); !errors.Is(err, ErrProviderAuthentication) {
		t.Fatalf("digest mismatch error = %v", err)
	}
	if err := validateProviderAuthenticationPayload([]byte("not-json"), hex.EncodeToString(digest[:])); !errors.Is(err, ErrProviderAuthentication) {
		t.Fatalf("malformed snapshot error = %v", err)
	}
}
