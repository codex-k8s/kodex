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

// configurationHomeFixture creates an independent temporary home and pinned synthetic authentication digest, leaving sandbox, volumes, overlays and secret projections explicit in each consuming test. It never reads live provider credentials.
// configurationHomeFixture создаёт независимый временный home и закреплённый дайджест synthetic authentication, оставляя sandbox, volumes, overlays и secret projections явными в каждом тесте. Живые provider-реквизиты не читает.
func configurationHomeFixture(t *testing.T) (model.Input, []byte) {
	t.Helper()
	workspace := t.TempDir()
	auth := []byte(`{"tokens":{"access_token":"test-only"}}`)
	digest := sha256.Sum256(auth)
	digestValue := hex.EncodeToString(digest[:])
	digestFile := filepath.Join(workspace, "auth.sha256")
	if err := os.WriteFile(digestFile, []byte(digestValue), 0o600); err != nil {
		t.Fatal(err)
	}
	return model.Input{WorkspaceRoot: workspace, CodexHome: filepath.Join(workspace, ".kodex", "state", "codex-home"),
		Model: "gpt-5", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "high",
		CodexApprovalPolicy: "never", CodexSandbox: "workspace-write",
		ProviderAuthSHA256File: digestFile, ProviderCredentialSHA256: digestValue}, auth
}

// TestConfigurationHomeFixturePinsIndependentSyntheticAuth checks separate paths and buffers plus exact digest-file binding, so extraction cannot silently share mutable authentication across test cases.
// TestConfigurationHomeFixturePinsIndependentSyntheticAuth проверяет отдельные paths/buffers и точную digest-file binding, не позволяя выносу незаметно разделить mutable authentication между тестами.
func TestConfigurationHomeFixturePinsIndependentSyntheticAuth(t *testing.T) {
	first, auth := configurationHomeFixture(t)
	second, otherAuth := configurationHomeFixture(t)
	digest := sha256.Sum256(auth)
	pin, err := os.ReadFile(first.ProviderAuthSHA256File)
	if err != nil || string(pin) != hex.EncodeToString(digest[:]) || string(pin) != first.ProviderCredentialSHA256 || first.WorkspaceRoot == second.WorkspaceRoot || first.CodexHome == second.CodexHome {
		t.Fatal("synthetic configuration fixture lost independent paths or authentication pin")
	}
	auth[0] = 'x'
	if otherAuth[0] != '{' {
		t.Fatal("synthetic configuration authentication buffers are shared")
	}
}

// TestProviderSandboxProbeUsesBoundedBubblewrapBoundary captures the proposed bwrap command without executing it, checks exact identity/root/environment arguments and safe failure classification; it does not prove a live kernel sandbox.
// TestProviderSandboxProbeUsesBoundedBubblewrapBoundary перехватывает предполагаемую bwrap-команду без запуска, сверяет точные identity/root/environment arguments и safe failure classification; не доказывает живую kernel sandbox.
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

// TestPrepareHomeDeniesShellReadOfProviderState materializes a temporary TOML profile and checks explicit native-routing/local-memory flags, exact read/deny rules and required MCP settings without granting broad home access or persisting a real credential.
// TestPrepareHomeDeniesShellReadOfProviderState материализует временный TOML profile и проверяет явные native-routing/local-memory flags, точные read/deny rules и required MCP settings без широкого home access и сохранения реальных реквизитов.
func TestPrepareHomeDeniesShellReadOfProviderState(t *testing.T) {
	setRuntimeTransportFixture(t)
	input, auth := configurationHomeFixture(t)
	workspace, home := input.WorkspaceRoot, input.CodexHome
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
		profile.Filesystem[filepath.Join(workspace, "input")] != "read" ||
		profile.Filesystem[filepath.Join(workspace, "knowledge")] != "read" ||
		profile.Filesystem[runtimecontract.RuntimeContextRoot] != "read" ||
		profile.Filesystem[filepath.Join(workspace, ".kodex/outbox")] != "read" ||
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

// TestPublishedVolumePermissionsAreExactAndRespectSandbox checks read/write rules for exactly the pinned published volumes and rejects foreign, reserved, unpinned, changed or duplicate volume policies without broadening protected filesystem paths.
// TestPublishedVolumePermissionsAreExactAndRespectSandbox проверяет read/write rules ровно pinned published volumes и отклоняет foreign, reserved, unpinned, changed и duplicate volume policies без расширения protected filesystem paths.
func TestPublishedVolumePermissionsAreExactAndRespectSandbox(t *testing.T) {
	setRuntimeTransportFixture(t)
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	policy.Volumes = []runtimecontract.RuntimeVolume{
		{Name: "build-cache", Kind: runtimecontract.RuntimeVolumeEphemeralDisk, SizeMiB: 4096},
		{Name: "scratch", Kind: runtimecontract.RuntimeVolumeEphemeralMemory, SizeMiB: 128},
	}
	policy, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	for sandbox, access := range map[string]string{"workspace-write": "write", "read-only": "read"} {
		t.Run(sandbox, func(t *testing.T) {
			input, auth := configurationHomeFixture(t)
			workspace := input.WorkspaceRoot
			input.Model, input.CodexSandbox, input.EnvironmentPolicy = "fixture-model", sandbox, policy
			if err := PrepareHomeWithAuth(input, "http://127.0.0.1:12345/mcp", auth); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(input.CodexHome, "config.toml"))
			var config runtimeConfig
			if err != nil {
				t.Fatal(err)
			}
			if _, err := toml.Decode(string(raw), &config); err != nil {
				t.Fatal(err)
			}
			paths := config.Permissions[config.DefaultPermissions].Filesystem
			for _, volume := range policy.Volumes {
				if paths[volume.MountPath] != access {
					t.Fatal("published volume permission was lost")
				}
			}
			for _, path := range []string{"/workspace", "/workspace/.kodex", "/workspace/.kodex/volumes", "/workspace/.kodex/volumes/undeclared", input.CodexHome} {
				if paths[path] != "" {
					t.Fatal("volume permission broadened the protected filesystem boundary")
				}
			}
			if paths[filepath.Join(input.CodexHome, "auth.json")] != "deny" || paths[filepath.Join(workspace, "input")] != "read" {
				t.Fatal("volume permission replaced a protected rule")
			}
		})
	}
	for name, mutate := range map[string]func(*runtimecontract.RuntimeEnvironmentPolicy){
		"foreign mount": func(p *runtimecontract.RuntimeEnvironmentPolicy) { p.Volumes[0].MountPath = "/run/secrets" },
		"reserved name": func(p *runtimecontract.RuntimeEnvironmentPolicy) { p.Volumes[0].Name = "workspace" },
		"changed quota": func(p *runtimecontract.RuntimeEnvironmentPolicy) { p.Volumes[0].SizeMiB++ },
		"missing pin":   func(p *runtimecontract.RuntimeEnvironmentPolicy) { p.VolumesDigest = "" },
		"duplicate":     func(p *runtimecontract.RuntimeEnvironmentPolicy) { p.Volumes = append(p.Volumes, p.Volumes[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := policy
			candidate.Volumes = append([]runtimecontract.RuntimeVolume(nil), policy.Volumes...)
			mutate(&candidate)
			if _, err := runtimeVolumePermissions(model.Input{EnvironmentPolicy: candidate, CodexSandbox: "workspace-write"}); !errors.Is(err, ErrRuntimeProfile) {
				t.Fatal("unbound or invalid volume reached the permission profile")
			}
		})
	}
}

// TestPrepareHomePreservesPinnedSandboxBoundary tests both approved CLI bases, fixed native-routing settings and the joint workspace-write/artifact-capability requirement for outbox writes, rejecting unknown and full-access bases.
// TestPrepareHomePreservesPinnedSandboxBoundary проверяет обе approved CLI bases, фиксированные native-routing settings и совместное требование workspace-write/artifact capability для outbox writes, отклоняя unknown и full-access bases.
func TestPrepareHomePreservesPinnedSandboxBoundary(t *testing.T) {
	setRuntimeTransportFixture(t)
	for sandbox, expected := range map[string]string{"read-only": ":read-only", "workspace-write": ":workspace"} {
		t.Run(sandbox, func(t *testing.T) {
			input, auth := configurationHomeFixture(t)
			workspace, home := input.WorkspaceRoot, input.CodexHome
			input.CodexSandbox = sandbox
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
			input.Capabilities = []string{runtimecontract.ArtifactCapability}
			if err := PrepareHomeWithAuth(input, "http://127.0.0.1:12345/mcp", auth); err != nil {
				t.Fatal(err)
			}
			raw, err = os.ReadFile(filepath.Join(home, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := toml.Decode(string(raw), &config); err != nil {
				t.Fatal(err)
			}
			outboxAccess := "read"
			if sandbox == "workspace-write" {
				outboxAccess = "write"
			}
			if config.Permissions[config.DefaultPermissions].Filesystem[filepath.Join(workspace, ".kodex/outbox")] != outboxAccess {
				t.Fatal("outbox write did not require both sandbox write and artifact authority")
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

// TestPrepareHomeMaterializesOnlyBoundEnvironment uses an inert secret sentinel to verify nonsecret values are persisted, approved secret/transport names are filtered in, and their values or private MCP token are not embedded in generated TOML.
// TestPrepareHomeMaterializesOnlyBoundEnvironment использует неактивный secret sentinel для проверки сохранения nonsecret values, включения approved secret/transport names в filters и отсутствия их values или private MCP token в TOML.
func TestPrepareHomeMaterializesOnlyBoundEnvironment(t *testing.T) {
	setRuntimeTransportFixture(t)
	t.Setenv("CRM_TOKEN", "synthetic-secret-must-not-persist")
	input, auth := configurationHomeFixture(t)
	home := input.CodexHome
	input.ConfigOverlay = "model_reasoning_effort = \"high\"\n\n[history]\npersistence = \"none\"\n"
	input.EnvironmentValues = []runtimecontract.RuntimeEnvironmentValue{{Name: "APP_MODE", Value: "review"}}
	input.SecretProjections = []runtimecontract.RuntimeSecretProjection{{Name: "CRM_TOKEN", SecretName: "runtime-crm-v1", SecretKey: "token",
		SecretUID: "7fe2f86e-4bb9-4325-a983-a389367c1cbf", SecretResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64)}}
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

// TestPrepareHomeMaterializesPinnedHostedSearchWithoutEgressChanges checks declared hosted-search modes survive TOML generation without enabling sandbox egress, leaves absent selection unset and rejects an unknown mode.
// TestPrepareHomeMaterializesPinnedHostedSearchWithoutEgressChanges проверяет сохранение объявленных hosted-search modes в TOML без включения sandbox egress, отсутствие выдуманного default при пустом выборе и отказ unknown mode.
func TestPrepareHomeMaterializesPinnedHostedSearchWithoutEgressChanges(t *testing.T) {
	setRuntimeTransportFixture(t)
	for _, mode := range []string{"", "disabled", "cached", "indexed", "live", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			input, auth := configurationHomeFixture(t)
			input.Model = "fixture-model"
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

// TestValidateProviderAuthenticationFailsClosed checks synthetic subscription/API-key snapshots accept only matching digests and supported authentication shapes, rejecting malformed content, mismatches and unsupported modes before provider use.
// TestValidateProviderAuthenticationFailsClosed проверяет принятие synthetic subscription/API-key snapshots лишь с соответствующими digests и поддерживаемой auth shape, отклоняя malformed content, mismatch и unsupported mode до провайдера.
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

// TestValidateRuntimeSelectionRejectsInvalidModelReasoningToolAndMCP accepts the pinned baseline and future server-issued model/effort, then rejects wrong provider, empty model, overlay mismatch/unknown/MCP keys and missing tools without executing them.
// TestValidateRuntimeSelectionRejectsInvalidModelReasoningToolAndMCP принимает pinned baseline и будущие server-issued model/effort, затем отклоняет wrong provider, empty model, overlay mismatch/unknown/MCP keys и отсутствующие tools без их запуска.
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

// TestRuntimeSelectionAcceptsAllSevenAPICapabilities exercises the seven historical synthetic model/effort rows without contacting a provider; it checks local acceptance, not current external model availability or entitlement.
// TestRuntimeSelectionAcceptsAllSevenAPICapabilities проверяет семь исторических synthetic model/effort rows без провайдера; это local acceptance, не актуальная внешняя доступность моделей или entitlement.
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
