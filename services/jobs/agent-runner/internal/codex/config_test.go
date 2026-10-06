package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestProviderSandboxProbeUsesBoundedBubblewrapBoundary(t *testing.T) {
	previousEUID := os.Geteuid()
	called := false
	err := verifyProviderSandbox(context.Background(), func(command *exec.Cmd) error {
		called = true
		expected := []string{"/usr/bin/bwrap", "--unshare-user", "--uid", strconv.Itoa(previousEUID),
			"--gid", strconv.Itoa(previousEUID), "--ro-bind", "/", "/", "/usr/bin/true"}
		if !slices.Equal(command.Args, expected) || !slices.Equal(command.Env, []string{"PATH=/usr/local/bin:/usr/bin:/bin"}) {
			t.Fatalf("unexpected provider sandbox probe: args=%q env=%q", command.Args, command.Env)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("verifyProviderSandbox() error = %v, called = %t", err, called)
	}
	if err := verifyProviderSandbox(context.Background(), func(*exec.Cmd) error { return errors.New("denied") }); err == nil || err.Error() != "Codex provider sandbox is unavailable" {
		t.Fatalf("provider sandbox probe failure was not classified safely: %v", err)
	}
}

func TestPrepareHomeDeniesShellReadOfProviderState(t *testing.T) {
	setRuntimeTransportFixture(t)
	workspace := t.TempDir()
	home := filepath.Join(workspace, ".kodex", "state", "codex-home")
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	digestFile := filepath.Join(workspace, "auth.sha256")
	if err := os.WriteFile(digestFile, []byte(hex.EncodeToString(digest[:])), 0o600); err != nil {
		t.Fatal(err)
	}
	input := model.Input{WorkspaceRoot: workspace, CodexHome: home, Model: "gpt-5", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high",
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
	if !metadata.IsDefined("features", "code_mode_host") || config.Features.CodeModeHost ||
		!metadata.IsDefined("features", "code_mode", "enabled") || config.Features.CodeMode.Enabled ||
		!metadata.IsDefined("features", "code_mode_only") || config.Features.CodeModeOnly {
		t.Fatal("native tools are not explicitly routed away from the disabled code-mode host")
	}
	if !metadata.IsDefined("features", "memories") || !metadata.IsDefined("memories", "generate_memories") ||
		!metadata.IsDefined("memories", "use_memories") || config.Features.Memories || config.Memories.GenerateMemories || config.Memories.UseMemories {
		t.Fatal("provider local memory is not explicitly disabled")
	}
	if err != nil || len(metadata.Undecoded()) != 0 || profile.Extends != ":workspace" ||
		profile.Filesystem[filepath.Join(home, "auth.json")] != "deny" || profile.Filesystem[home] != "" ||
		profile.Filesystem["/proc"] != "deny" ||
		profile.Filesystem["/run/secrets"] != "deny" ||
		profile.Filesystem["/var/run/secrets"] != "" ||
		config.MCPServers["kodex"].BearerTokenEnvVar != "KODEX_MCP_PROXY_TOKEN" ||
		config.MCPServers["kodex"].DefaultToolsApprovalMode != "approve" ||
		config.MCPServers["kodex"].ToolTimeoutSeconds != runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds ||
		!slices.Equal(config.Features.CodeMode.DirectOnlyToolNamespaces, []string{"functions", "web", "mcp__kodex"}) {
		t.Fatalf("provider permission boundary is incomplete: %#v", config)
	}
	for path := range profile.Filesystem {
		if filepath.IsAbs(path) && strings.Contains(path, "*") {
			t.Fatalf("absolute deny path must not require a pre-sandbox glob scan: %q", path)
		}
	}
}

func TestPrepareHomePreservesPinnedSandboxBoundary(t *testing.T) {
	setRuntimeTransportFixture(t)
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
			input := model.Input{WorkspaceRoot: workspace, CodexHome: home, Model: "gpt-5", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high",
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
			if _, err := toml.Decode(string(raw), &config); err != nil || config.Permissions[config.DefaultPermissions].Extends != expected ||
				config.ApprovalPolicy != input.CodexApprovalPolicy || config.Features.CodeModeHost ||
				config.Features.CodeModeOnly || config.Features.CodeMode.Enabled ||
				!slices.Equal(config.Features.CodeMode.DirectOnlyToolNamespaces, []string{"functions", "web", "mcp__kodex"}) {
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
	setRuntimeTransportFixture(t)
	t.Setenv("CRM_TOKEN", "synthetic-secret-must-not-persist")
	workspace := t.TempDir()
	home := filepath.Join(workspace, ".kodex", "state", "codex-home")
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	digestFile := filepath.Join(workspace, "auth.sha256")
	if err := os.WriteFile(digestFile, []byte(hex.EncodeToString(digest[:])), 0o600); err != nil {
		t.Fatal(err)
	}
	input := model.Input{WorkspaceRoot: workspace, CodexHome: home, Model: "gpt-5", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high",
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
		len(config.ShellEnvironmentPolicy.Filters) != 4+len(runtimeTransportEnvironmentNames) ||
		config.ShellEnvironmentPolicy.Filters["CRM_TOKEN"] != "include" ||
		config.ShellEnvironmentPolicy.Filters["APP_MODE"] != "include" ||
		config.ShellEnvironmentPolicy.Filters["KODEX_MCP_PROXY_TOKEN"] != "" {
		t.Fatalf("unexpected effective config: %#v", config)
	}
	for _, name := range runtimeTransportEnvironmentNames {
		if config.ShellEnvironmentPolicy.Filters[name] != "include" || config.ShellEnvironmentPolicy.Set[name] != "" {
			t.Fatalf("transport variable %s is missing or persisted", name)
		}
	}
	if strings.Contains(string(raw), "synthetic-secret-must-not-persist") || strings.Contains(string(raw), "fixture.signature") ||
		strings.Contains(string(raw), "include_only") {
		t.Fatal("configuration persisted credentials or legacy environment filters")
	}
}

func TestPrepareHomeMaterializesPinnedHostedSearchWithoutEgressChanges(t *testing.T) {
	setRuntimeTransportFixture(t)
	for _, mode := range []string{"", "disabled", "cached", "indexed", "live", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			workspace := t.TempDir()
			auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
			digest := sha256.Sum256(auth)
			digestFile := filepath.Join(workspace, "auth.sha256")
			if err := os.WriteFile(digestFile, []byte(hex.EncodeToString(digest[:])), 0o600); err != nil {
				t.Fatal(err)
			}
			input := model.Input{WorkspaceRoot: workspace, CodexHome: filepath.Join(workspace, ".kodex", "state", "codex-home"), Model: "fixture-model", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high", CodexApprovalPolicy: "never", CodexSandbox: "workspace-write", ProviderAuthSHA256File: digestFile, ProviderCredentialSHA256: hex.EncodeToString(digest[:])}
			input.EnvironmentPolicy.Network.WebAccess.Mode = runtimecontract.RuntimeWebAccessNone
			if mode != "" {
				input.ConfigOverlay = "web_search = \"" + mode + "\"\n"
			}
			err := PrepareHomeWithAuth(input, "http://127.0.0.1:12345/mcp", auth)
			if mode == "unknown" {
				if err == nil {
					t.Fatal("unsupported mode reached writer")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(input.CodexHome, "config.toml"))
			var config runtimeConfig
			metadata, decodeErr := toml.Decode(string(raw), &config)
			if err != nil || decodeErr != nil || config.WebSearchMode != mode || metadata.IsDefined("web_search") != (mode != "") || config.DefaultPermissions != "kodex-runtime" || config.Permissions[config.DefaultPermissions].Network.Enabled {
				t.Fatal("mode was lost/defaulted or expanded sandbox egress")
			}
		})
	}
}

func TestValidateProviderAuthenticationFailsClosed(t *testing.T) {
	auth := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	if err := validateProviderAuthenticationPayload(auth, hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("validateProviderAuthenticationPayload() error = %v", err)
	}
	if err := validateProviderAuthenticationPayload(auth, "invalid"); !errors.Is(err, ErrProviderAuthentication) {
		t.Fatalf("digest mismatch error = %v", err)
	}
	apiKeyAuth := []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"test-only"}`)
	apiKeyDigest := sha256.Sum256(apiKeyAuth)
	if err := validateProviderAuthenticationPayload(apiKeyAuth, hex.EncodeToString(apiKeyDigest[:])); err != nil {
		t.Fatalf("API key snapshot error = %v", err)
	}
	if err := validateProviderAuthenticationPayload([]byte("not-json"), hex.EncodeToString(digest[:])); !errors.Is(err, ErrProviderAuthentication) {
		t.Fatalf("malformed snapshot error = %v", err)
	}
	unsupported := []byte(`{"auth_mode":"local-development","access_token":"not-configured"}`)
	unsupportedDigest := sha256.Sum256(unsupported)
	if err := validateProviderAuthenticationPayload(unsupported, hex.EncodeToString(unsupportedDigest[:])); !errors.Is(err, ErrProviderAuthentication) {
		t.Fatalf("unsupported authentication mode error = %v", err)
	}
}

func TestValidateRuntimeSelectionRejectsInvalidModelReasoningToolAndMCP(t *testing.T) {
	valid := model.Input{ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high", Provider: "openai", Model: "gpt-5.4", ConfigOverlay: "model_reasoning_effort = \"high\"\n",
		EnvironmentTools: []runtimecontract.RuntimeEnvironmentTool{{Name: "Shell", Command: "sh", Description: "Shell"}}}
	if err := validateRuntimeSelection(valid); err != nil {
		t.Fatalf("valid runtime selection rejected: %v", err)
	}
	future := valid
	future.Model, future.ConfigOverlay, future.EffectiveReasoningEffort = "future-account-model", "", "custom-effort"
	if validateRuntimeSelection(future) != nil {
		t.Fatal("server-issued model and effort were replaced by a local catalog")
	}
	for name, mutate := range map[string]func(*model.Input){
		"provider":        func(input *model.Input) { input.Provider = "foreign" },
		"model":           func(input *model.Input) { input.Model = "" },
		"model reasoning": func(input *model.Input) { input.ConfigOverlay = "model_reasoning_effort = \"max\"\n" },
		"TOML key":        func(input *model.Input) { input.ConfigOverlay = "unknown = true\n" },
		"MCP configuration": func(input *model.Input) {
			input.ConfigOverlay = "[mcp_servers.foreign]\nurl = \"https://example.invalid\"\n"
		},
		"tool": func(input *model.Input) { input.EnvironmentTools[0].Command = "kodex-tool-that-does-not-exist" },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			input.EnvironmentTools = append([]runtimecontract.RuntimeEnvironmentTool(nil), valid.EnvironmentTools...)
			mutate(&input)
			if err := validateRuntimeSelection(input); !errors.Is(err, ErrRuntimeProfile) {
				t.Fatalf("unknown runtime selection error = %v", err)
			}
		})
	}
}

func TestRuntimeSelectionAcceptsAllSevenAPICapabilities(t *testing.T) {
	for modelID, efforts := range map[string][]string{
		"gpt-6-astra":   {"low", "medium", "high", "xhigh", "max"},
		"gpt-5.6-sol":   {"none", "low", "medium", "high", "xhigh", "max"},
		"gpt-5.6-terra": {"none", "low", "medium", "high", "xhigh", "max"},
		"gpt-5.6-luna":  {"none", "low", "medium", "high", "xhigh", "max"},
		"gpt-5.5":       {"none", "low", "medium", "high", "xhigh"},
		"gpt-5.4":       {"none", "low", "medium", "high", "xhigh"},
		"gpt-5.4-mini":  {"none", "low", "medium", "high", "xhigh"},
	} {
		for _, effort := range efforts {
			t.Run(modelID+"/"+effort, func(t *testing.T) {
				input := model.Input{Provider: "openai", Model: modelID, ReasoningMode: runtimecontract.ReasoningSupported,
					EffectiveReasoningEffort: effort, ConfigOverlay: "model_reasoning_effort = \"" + effort + "\"\n"}
				if err := validateRuntimeSelection(input); err != nil {
					t.Fatalf("documented API effort rejected: %v", err)
				}
			})
		}
	}
}
