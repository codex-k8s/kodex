package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestCodexShellEnvironmentAllowlistIncludesRuntimeEnv(t *testing.T) {
	t.Setenv("MATTERCODEX_RUNTIME_ENV_ALLOWLIST", "RADAR_AUTO_KUBECONFIG,invalid-name,STAGING_DB_URL,RADAR_AUTO_KUBECONFIG")

	values := codexShellEnvironmentAllowlist()
	joined := "," + strings.Join(values, ",") + ","

	for _, expected := range []string{"RADAR_AUTO_KUBECONFIG", "STAGING_DB_URL", "MATTERCODEX_GIT_ASKPASS"} {
		if !strings.Contains(joined, ","+expected+",") {
			t.Fatalf("allowlist missing %q: %#v", expected, values)
		}
	}
	if strings.Contains(joined, ",MATTERCODEX_MCP_TOKEN,") {
		t.Fatalf("allowlist exposes MCP bearer token to shell commands: %#v", values)
	}
	if strings.Contains(joined, ",invalid-name,") {
		t.Fatalf("allowlist contains invalid env name: %#v", values)
	}
	if strings.Count(joined, ",RADAR_AUTO_KUBECONFIG,") != 1 {
		t.Fatalf("allowlist contains duplicate runtime env: %#v", values)
	}
}

func TestDisableCodexConfigOverlayForAuthCheck(t *testing.T) {
	t.Setenv("MATTERCODEX_CODEX_CONFIG_OVERLAY", `sandbox_mode = "danger-full-access"`)

	if err := disableCodexConfigOverlayForAuthCheck(); err != nil {
		t.Fatalf("disable overlay: %v", err)
	}
	if value := strings.TrimSpace(os.Getenv("MATTERCODEX_CODEX_CONFIG_OVERLAY")); value != "" {
		t.Fatalf("overlay = %q, want empty", value)
	}
}

func TestWriteCodexConfigMergesRoleConfigWithoutDuplicateKeys(t *testing.T) {
	t.Setenv("MATTERCODEX_MCP_URL", "http://matter-codex-mcp")
	t.Setenv("MATTERCODEX_RUNTIME_ENV_ALLOWLIST", "RADAR_AUTO_KUBECONFIG")
	t.Setenv("MATTERCODEX_CODEX_CONFIG_OVERLAY", `
model = "gpt-5.5"
model_reasoning_effort = "xhigh"
approval_policy = "never"
sandbox_mode = "danger-full-access"
model_verbosity = "low"
web_search_request = true
personality = "pragmatic"
approvals_reviewer = "user"
network_access = true

[shell_environment_policy]
include_only = ["CUSTOM_ENV"]

[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.context7.env]
CONTEXT7_API_KEY = "test-context7-key"

[mcp_servers.openaiDeveloperDocs]
url = "https://developers.openai.com/mcp"
`)

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeCodexConfig(path); err != nil {
		t.Fatalf("writeCodexConfig() error = %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	config := map[string]any{}
	if _, err := toml.Decode(string(body), &config); err != nil {
		t.Fatalf("generated config is invalid TOML: %v\n%s", err, string(body))
	}

	if got := config["model"]; got != "gpt-5.5" {
		t.Fatalf("model = %#v", got)
	}
	if got := config["sandbox_mode"]; got != "danger-full-access" {
		t.Fatalf("sandbox_mode = %#v", got)
	}
	if got := config["web_search_request"]; got != true {
		t.Fatalf("web_search_request = %#v", got)
	}

	shellPolicy := mustTable(t, config, "shell_environment_policy")
	if got := shellPolicy["inherit"]; got != "all" {
		t.Fatalf("shell_environment_policy.inherit = %#v", got)
	}
	if got := shellPolicy["ignore_default_excludes"]; got != true {
		t.Fatalf("shell_environment_policy.ignore_default_excludes = %#v", got)
	}
	includeOnly, err := stringListValue(shellPolicy["include_only"])
	if err != nil {
		t.Fatalf("include_only: %v", err)
	}
	joined := "," + strings.Join(includeOnly, ",") + ","
	for _, expected := range []string{"CUSTOM_ENV", "GH_TOKEN", "MATTERCODEX_GIT_ASKPASS", "RADAR_AUTO_KUBECONFIG"} {
		if !strings.Contains(joined, ","+expected+",") {
			t.Fatalf("include_only missing %q: %#v", expected, includeOnly)
		}
	}
	if strings.Contains(joined, ",MATTERCODEX_MCP_TOKEN,") {
		t.Fatalf("include_only exposes MCP bearer token to shell commands: %#v", includeOnly)
	}

	mcpServers := mustTable(t, config, "mcp_servers")
	context7 := mustTable(t, mcpServers, "context7")
	context7Env := mustTable(t, context7, "env")
	if got := context7Env["CONTEXT7_API_KEY"]; got != "test-context7-key" {
		t.Fatalf("context7 env key = %#v", got)
	}
	if got := context7["startup_timeout_sec"]; got != int64(20) {
		t.Fatalf("context7 startup_timeout_sec = %#v", got)
	}
	mattercodex := mustTable(t, mcpServers, "mattercodex")
	if got := mattercodex["url"]; got != "http://matter-codex-mcp" {
		t.Fatalf("mattercodex url = %#v", got)
	}
	openaiDocs := mustTable(t, mcpServers, "openaiDeveloperDocs")
	if got := openaiDocs["url"]; got != "https://developers.openai.com/mcp" {
		t.Fatalf("openaiDeveloperDocs url = %#v", got)
	}
}

func mustTable(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	table, ok := asStringAnyMap(parent[key])
	if !ok {
		t.Fatalf("%s is not a table: %#v", key, parent[key])
	}
	return table
}
