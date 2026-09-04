package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func TestRuntimeRevisionDigestBindsEnvironmentImageAndTools(t *testing.T) {
	t.Parallel()
	image := runtimecontract.RuntimeEnvironmentImage{
		ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh", RecipeGeneration: 1,
		Reference: "registry.example/kodex/role@sha256:" + strings.Repeat("a", 64),
		Digest:    "sha256:" + strings.Repeat("a", 64),
	}
	tools := []runtimecontract.RuntimeEnvironmentTool{{
		Name: "GitHub CLI", Command: "gh", Description: "Работа с GitHub",
	}}
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools)
	if err != nil {
		t.Fatalf("compute baseline environment digest: %v", err)
	}
	input := runtimecontract.RunnerInput{RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionVersion: 1,
		EnvironmentImage: image, EnvironmentTools: tools, RuntimeEnvironmentDigest: environmentDigest}
	source := runtimecontract.RuntimeRevisionCredentialSource{SecretName: "provider-auth", SecretUID: "uid-1", SecretResourceVersion: "1"}
	baseline, err := runtimecontract.RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}

	image.Digest = "sha256:" + strings.Repeat("b", 64)
	image.Reference = "registry.example/kodex/role@" + image.Digest
	changedImageDigest, err := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools)
	if err != nil {
		t.Fatalf("compute changed image environment digest: %v", err)
	}
	input.EnvironmentImage = image
	input.RuntimeEnvironmentDigest = changedImageDigest
	if digest, _ := runtimecontract.RuntimeRevisionDigest(input, source); digest == baseline {
		t.Fatal("exact image change did not change RuntimeRevision digest")
	}

	image.Digest = "sha256:" + strings.Repeat("a", 64)
	image.Reference = "registry.example/kodex/role@" + image.Digest
	tools[0].UsageHint = "Используй только read-only команды"
	changedToolsDigest, err := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, tools)
	if err != nil {
		t.Fatalf("compute changed tools environment digest: %v", err)
	}
	input.EnvironmentImage = image
	input.EnvironmentTools = tools
	input.RuntimeEnvironmentDigest = changedToolsDigest
	if digest, _ := runtimecontract.RuntimeRevisionDigest(input, source); digest == baseline {
		t.Fatal("selected tools change did not change RuntimeRevision digest")
	}
}

func TestPromptUserCapabilitiesCannotExceedInitiatorAuthority(t *testing.T) {
	got := promptUserCapabilities("MEMBER", []string{"LAUNCH_RUNS"},
		[]string{"platform.run.launch", "platform.agent.manage"}, []string{"calendar.write"})
	if len(got) != 1 || got[0] != "platform.run.launch" {
		t.Fatalf("effective user capabilities = %#v", got)
	}
	owner := promptUserCapabilities("OWNER", nil,
		[]string{"platform.agent.manage"}, []string{"calendar.write"})
	if strings.Join(owner, ",") != "calendar.write,platform.agent.manage" {
		t.Fatalf("owner capabilities = %#v", owner)
	}
}

func TestRuntimeWorkspacePolicyIsBoundedAndServerOwned(t *testing.T) {
	policy := runtimeWorkspacePolicy()
	if policy.Revision != 1 || policy.Root != "/workspace" || policy.MaximumWritableBytes != 1<<30 ||
		policy.MaximumFileCount != 10_000 || len(policy.Digest) != 64 {
		t.Fatalf("workspace policy = %#v", policy)
	}
	if len(policy.Rules) != 4 || policy.Rules[0].Path != "/workspace/input" ||
		policy.Rules[0].Access != "READ_ONLY" || policy.Rules[3].Path != "/workspace" ||
		policy.Rules[3].Access != "WRITABLE" || policy.Rules[2].Path != "/workspace/.kodex/state/codex-home/auth.json" ||
		policy.Rules[2].Access != "READ_ONLY" {
		t.Fatalf("workspace path matrix = %#v", policy.Rules)
	}
	if strings.Join(policy.DenialReasons, ",") != "READ_ONLY,QUOTA_EXCEEDED,PATH_OUTSIDE_WORKSPACE,RUNTIME_IO_ERROR" {
		t.Fatalf("workspace denial reasons = %#v", policy.DenialReasons)
	}
}

func TestProviderCredentialRefreshValidationRequiresExactImmutableSecretBinding(t *testing.T) {
	t.Parallel()
	valid := command.ProviderCredentialRefreshInput{
		LeaseRef: "lea_abcdefgh", Fence: "fnc_abcdefgh", Generation: 1,
		PreviousCredentialRevisionRef: "pcr_previous1", PreviousContentSHA256: strings.Repeat("a", 64),
		SecretName: "runtime-provider-refresh-1", SecretUID: "10000000-0000-4000-8000-000000000010",
		SecretResourceVersion: "42", ContentSHA256: strings.Repeat("b", 64),
	}
	if !validProviderCredentialRefresh(valid) {
		t.Fatalf("valid provider credential refresh was rejected: %#v", valid)
	}
	for name, mutate := range map[string]func(*command.ProviderCredentialRefreshInput){
		"uppercase digest": func(input *command.ProviderCredentialRefreshInput) { input.ContentSHA256 = strings.Repeat("B", 64) },
		"invalid uid":      func(input *command.ProviderCredentialRefreshInput) { input.SecretUID = "not-a-uuid" },
		"invalid secret":   func(input *command.ProviderCredentialRefreshInput) { input.SecretName = "Invalid_Secret" },
		"missing fence":    func(input *command.ProviderCredentialRefreshInput) { input.Fence = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := valid
			mutate(&input)
			if validProviderCredentialRefresh(input) {
				t.Fatalf("invalid provider credential refresh was accepted: %#v", input)
			}
		})
	}
}

func TestDirectRootWithoutProcessNode(t *testing.T) {
	t.Parallel()

	direct := map[string]any{"runID": "run-id", "rootRunID": "run-id"}
	if !directRootWithoutProcessNode(pgx.ErrNoRows, direct) {
		t.Fatal("direct root run must not require a separate root process node")
	}

	for name, test := range map[string]struct {
		err   error
		lease map[string]any
	}{
		"database failure": {err: errors.New("database unavailable"), lease: direct},
		"child run":        {err: pgx.ErrNoRows, lease: map[string]any{"runID": "child", "rootRunID": "root"}},
		"missing identity": {err: pgx.ErrNoRows, lease: map[string]any{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if directRootWithoutProcessNode(test.err, test.lease) {
				t.Fatal("non-direct lifecycle failure was accepted")
			}
		})
	}
}

func TestRuntimeSafeErrorCodeAcceptsMCPUnavailable(t *testing.T) {
	t.Parallel()

	if !runtimeSafeErrorCode("RUNTIME_MCP_UNAVAILABLE") {
		t.Fatal("RUNTIME_MCP_UNAVAILABLE was rejected by the runtime completion boundary")
	}
	if runtimeSafeErrorCode("RUNTIME_MCP_INTERNAL_DIAGNOSTIC") {
		t.Fatal("unknown runtime MCP failure code was accepted")
	}
}

func TestDecodeRunUsageValidatesStoredTurnBreakdown(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"total_tokens":120,"input_tokens":100,"cached_input_tokens":40,
		"cache_write_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,
		"model_context_window":200000,
		"turns":{"turn_abcdefgh":{"total_tokens":120,"input_tokens":100,"cached_input_tokens":40,
		"cache_write_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,
		"model_context_window":200000}}
	}`)
	usage, err := decodeRunUsage(raw)
	if err != nil {
		t.Fatalf("decode valid token usage: %v", err)
	}
	want := entity.TokenUsage{TotalTokens: 120, InputTokens: 100, CachedInputTokens: 40,
		CacheWriteInputTokens: 10, OutputTokens: 20, ReasoningOutputTokens: 5, ModelContextWindow: 200000}
	if usage != want {
		t.Fatalf("decoded usage = %#v, want %#v", usage, want)
	}

	for name, invalid := range map[string][]byte{
		"unknown field": []byte(`{"total_tokens":0,"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0,"unexpected":1}`),
		"invalid total": []byte(`{"total_tokens":2,"input_tokens":1,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0}`),
		"invalid turn":  []byte(`{"total_tokens":0,"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0,"turns":{"":{"total_tokens":0,"input_tokens":0,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0,"model_context_window":0}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeRunUsage(invalid); err == nil {
				t.Fatal("invalid stored token usage was accepted")
			}
		})
	}
}

func TestFilterIntegrationGrantsCannotBypassEffectiveCapabilities(t *testing.T) {
	t.Parallel()
	grants := []map[string]string{{"capabilityKey": "read"}, {"capabilityKey": "write"}}
	filtered := filterIntegrationGrants(grants, []string{"read"})
	if len(filtered) != 1 || filtered[0]["capabilityKey"] != "read" {
		t.Fatalf("filtered grants = %#v", filtered)
	}
}
