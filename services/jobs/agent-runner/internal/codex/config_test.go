package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
)

func TestPrepareHomeDeniesShellReadOfProviderState(t *testing.T) {
	workspace := t.TempDir()
	home := filepath.Join(workspace, ".matter-codex", "state", "codex-home")
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	input := model.Input{WorkspaceRoot: workspace, CodexHome: home, CodexModel: "gpt-5",
		CodexApprovalPolicy: "never", CodexSandbox: "workspace-write",
		CredentialFiles: model.CredentialFiles{CodexAuthSHA256: hex.EncodeToString(digest[:])}}
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
		profile.Filesystem[home] != "deny" || profile.Filesystem[filepath.Join(home, "**")] != "deny" ||
		profile.Filesystem["/proc/**"] != "deny" ||
		config.MCPServers["mattercodex"].BearerTokenEnvVar != "MATTERCODEX_MCP_PROXY_TOKEN" {
		t.Fatalf("provider permission boundary is incomplete: %#v", config)
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
