package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// TestDecodeStoredRuntimeEnvironmentProjectsSecretDescriptor round-trips a synthetic environment
// and full Secret descriptor through payload validation and stored projection, comparing exact
// runtime values. Missing source ref, namespace or revision must fail; this pure test reads no
// real Secret.
// TestDecodeStoredRuntimeEnvironmentProjectsSecretDescriptor проверяет круговой проход synthetic
// environment и полного Secret descriptor через payload validation и stored projection с точным
// сравнением runtime values. Отсутствие source ref, namespace либо revision должно давать отказ;
// реальный Secret не читается.
func TestDecodeStoredRuntimeEnvironmentProjectsSecretDescriptor(t *testing.T) {
	t.Parallel()
	values := []entity.RuntimeEnvironmentValue{{Name: "APP_TEST_MODE", Value: "local"}}
	descriptor := entity.RuntimeSecretDescriptor{
		Name: "SYNTHETIC_TEST_SECRET", SecretRef: "sec_abcdefgh", Namespace: "kodex-runtime", Revision: 2,
		SecretName: "runtime-secret-test-r2", SecretKey: "value", SecretUID: "test-uid",
		SecretResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64),
	}
	rawValues, rawSecrets, expectedValues, expectedSecrets, err := validateEnvironmentPayload(values, []entity.RuntimeSecretDescriptor{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	var actualValues []runtimecontract.RuntimeEnvironmentValue
	var actualSecrets []runtimecontract.RuntimeSecretProjection
	if err := decodeStoredRuntimeEnvironment(rawValues, rawSecrets, &actualValues, &actualSecrets); err != nil {
		t.Fatalf("decode stored environment: %v", err)
	}
	if !reflect.DeepEqual(actualValues, expectedValues) || !reflect.DeepEqual(actualSecrets, expectedSecrets) {
		t.Fatalf("stored environment projection changed: values=%v secrets=%v", actualValues, actualSecrets)
	}
	for name, mutate := range map[string]func(*entity.RuntimeSecretDescriptor){
		"missing source ref": func(item *entity.RuntimeSecretDescriptor) { item.SecretRef = "" },
		"missing namespace":  func(item *entity.RuntimeSecretDescriptor) { item.Namespace = "" },
		"missing revision":   func(item *entity.RuntimeSecretDescriptor) { item.Revision = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := descriptor
			mutate(&invalid)
			raw, err := json.Marshal([]entity.RuntimeSecretDescriptor{invalid})
			if err != nil {
				t.Fatal(err)
			}
			if err := decodeStoredRuntimeEnvironment(rawValues, raw, &actualValues, &actualSecrets); err == nil {
				t.Fatal("invalid stored Secret descriptor accepted")
			}
		})
	}
}

// TestRuntimeRevisionDigestBindsEnvironmentImageAndTools computes a baseline revision digest and
// proves that exact image digest/reference or selected tool usage-hint changes alter it after
// recomputing the environment digest. It checks content binding, not image provenance or
// execution.
// TestRuntimeRevisionDigestBindsEnvironmentImageAndTools вычисляет baseline revision digest и
// доказывает, что изменение exact image digest/reference либо selected tool usage hint меняет его
// после пересчёта environment digest. Проверяет content binding, а не image provenance либо
// execution.
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

// TestRuntimeExecutionProviderUsesClosedAdapterRegistry requires only the openai-codex definition
// key to resolve to the openai runtime adapter and rejects empty, adapter-name and unknown keys.
// No provider API is contacted.
// TestRuntimeExecutionProviderUsesClosedAdapterRegistry требует разрешения только definition key
// openai-codex в runtime adapter openai и отклоняет empty, adapter-name и unknown keys. Provider
// API не вызывается.
func TestRuntimeExecutionProviderUsesClosedAdapterRegistry(t *testing.T) {
	t.Parallel()
	provider, err := runtimeExecutionProvider("openai-codex")
	if err != nil || provider != "openai" {
		t.Fatalf("runtime provider = %q, err = %v", provider, err)
	}
	for _, invalid := range []string{"", "openai", "other-provider"} {
		if provider, err := runtimeExecutionProvider(invalid); err == nil || provider != "" {
			t.Fatalf("definition key %q produced provider %q, err = %v", invalid, provider, err)
		}
	}
}

// TestRuntimeWorkspacePolicyIsBoundedAndServerOwned checks the fixed workspace root, revision,
// quotas, denial-reason order and exact projection against the shared versioned policy. Restoring
// all path rules must validate and preserve its digest; filesystem enforcement is outside this
// pure regression.
// TestRuntimeWorkspacePolicyIsBoundedAndServerOwned проверяет fixed workspace root, revision,
// quotas, порядок denial reasons и точную проекцию shared versioned policy. Восстановление всех
// path rules должно пройти validation и сохранить digest; filesystem enforcement не входит в pure
// regression.
func TestRuntimeWorkspacePolicyIsBoundedAndServerOwned(t *testing.T) {
	policy := runtimeWorkspacePolicy()
	if policy.Revision != 1 || policy.Root != "/workspace" || policy.MaximumWritableBytes != 1<<30 ||
		policy.MaximumFileCount != 10_000 || len(policy.Digest) != 64 {
		t.Fatalf("workspace policy = %#v", policy)
	}
	shared := runtimecontract.RuntimeWorkspacePolicyV1()
	if policy.Digest != shared.Digest || len(policy.Rules) != len(shared.Rules) {
		t.Fatal("producer workspace policy differs from shared runtime contract")
	}
	restored := runtimecontract.RuntimeWorkspacePolicy{Revision: policy.Revision, Root: policy.Root, Digest: policy.Digest,
		MaximumWritableBytes: policy.MaximumWritableBytes, MaximumFileCount: policy.MaximumFileCount, DenialReasons: policy.DenialReasons}
	for _, rule := range policy.Rules {
		restored.Rules = append(restored.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.Path, Access: rule.Access})
	}
	if !reflect.DeepEqual(restored, shared) || restored.Validate() != nil {
		t.Fatal("producer projection changed shared rule order or digest")
	}
	if strings.Join(policy.DenialReasons, ",") != "READ_ONLY,QUOTA_EXCEEDED,PATH_OUTSIDE_WORKSPACE,RUNTIME_IO_ERROR" {
		t.Fatalf("workspace denial reasons = %#v", policy.DenialReasons)
	}
}

// TestProviderCredentialRefreshValidationRequiresExactImmutableSecretBinding accepts one synthetic
// fenced refresh descriptor and rejects uppercase digest, noncanonical Secret UUID/name and
// missing fence mutations. This is shape validation only; it grants no lease authority or live
// Secret access.
// TestProviderCredentialRefreshValidationRequiresExactImmutableSecretBinding принимает один
// synthetic fenced refresh descriptor и отклоняет uppercase digest, некорректные Secret UUID/name
// и missing fence. Проверяет только shape; не выдаёт lease authority или live Secret access.
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

// TestDirectRootWithoutProcessNode allows only ErrNoRows with equal nonempty run/root IDs to
// represent a direct root without a separate process node. Backend failure, child lineage and
// missing identity must remain failures; no database is used.
// TestDirectRootWithoutProcessNode допускает только ErrNoRows с равными непустыми run/root IDs как
// direct root без отдельного process node. Backend failure, child lineage и missing identity
// остаются отказами; БД не используется.
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

// TestRuntimeSafeErrorCodeAcceptsMCPUnavailable checks that the closed completion-code classifier
// accepts RUNTIME_MCP_UNAVAILABLE while rejecting an internal diagnostic lookalike. It does not
// test an actual MCP outage.
// TestRuntimeSafeErrorCodeAcceptsMCPUnavailable проверяет, что closed completion-code classifier
// принимает RUNTIME_MCP_UNAVAILABLE и отклоняет похожий internal diagnostic. Реальный MCP outage
// не проверяется.
func TestRuntimeSafeErrorCodeAcceptsMCPUnavailable(t *testing.T) {
	t.Parallel()

	if !runtimeSafeErrorCode("RUNTIME_MCP_UNAVAILABLE") {
		t.Fatal("RUNTIME_MCP_UNAVAILABLE was rejected by the runtime completion boundary")
	}
	if runtimeSafeErrorCode("RUNTIME_MCP_INTERNAL_DIAGNOSTIC") {
		t.Fatal("unknown runtime MCP failure code was accepted")
	}
}

// TestRuntimeSafeErrorCodeAcceptsArtifactFailureOnly requires the public artifact-invalid
// completion code and rejects its internal diagnostic lookalike. The assertion covers the closed
// code boundary, not object scanning or storage.
// TestRuntimeSafeErrorCodeAcceptsArtifactFailureOnly требует public artifact-invalid completion
// code и отклоняет похожий internal diagnostic. Assertion проверяет closed code boundary, а не
// object scanning либо storage.
func TestRuntimeSafeErrorCodeAcceptsArtifactFailureOnly(t *testing.T) {
	t.Parallel()
	if !runtimeSafeErrorCode("RUNTIME_ARTIFACT_INVALID") || runtimeSafeErrorCode("RUNTIME_ARTIFACT_INTERNAL_DIAGNOSTIC") {
		t.Fatal("artifact completion failure code boundary is invalid")
	}
}

// TestAssistantTerminalTurnDoesNotChangeConversationTitles inspects the shipped terminal
// conversation UPDATE for exact session/version/timestamp semantics and absence of title or
// second-argument writes. This SQL-shape assertion does not execute a terminal transaction.
// TestAssistantTerminalTurnDoesNotChangeConversationTitles проверяет shipped terminal conversation
// UPDATE на exact session/version/timestamp semantics и отсутствие title либо second-argument
// writes. SQL-shape assertion не выполняет terminal transaction.
func TestAssistantTerminalTurnDoesNotChangeConversationTitles(t *testing.T) {
	t.Parallel()
	query := queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt
	if strings.Contains(query, "title") || strings.Contains(query, "$2") {
		t.Fatal("terminal result can overwrite a conversation title")
	}
	if !strings.Contains(query, "version = version + 1") || !strings.Contains(query, "updated_at = clock_timestamp()") || !strings.Contains(query, "WHERE session_id = $1::uuid") {
		t.Fatal("terminal conversation update lost its exact session or version semantics")
	}
}

// TestDecodeRunUsageValidatesStoredTurnBreakdown decodes stored aggregate and per-turn token usage
// into the exact expected totals, rejecting unknown fields, inconsistent totals and invalid turn
// keys. It checks strict stored JSON handling without provider billing or live metering.
// TestDecodeRunUsageValidatesStoredTurnBreakdown декодирует stored aggregate и per-turn token
// usage в точные expected totals, отклоняя unknown fields, несогласованные totals и invalid turn
// keys. Проверяет strict stored JSON handling без provider billing либо live metering.
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

// TestFilterIntegrationGrantsCannotBypassEffectiveCapabilities filters mixed read/write grants by
// the supplied effective capability set and requires only the read grant to remain. It is a local
// projection regression, not proof of actor or grant authority.
// TestFilterIntegrationGrantsCannotBypassEffectiveCapabilities фильтрует смешанные read/write
// grants по переданному effective capability set и требует сохранения только read grant. Это local
// projection regression, а не доказательство actor либо grant authority.
func TestFilterIntegrationGrantsCannotBypassEffectiveCapabilities(t *testing.T) {
	t.Parallel()
	grants := []map[string]string{{"capabilityKey": "read", "operation": "mattermost.post.read"}, {"capabilityKey": "write", "operation": "mattermost.post.send"}}
	filtered := filterIntegrationGrants(grants, []string{"read"})
	if len(filtered) != 1 || filtered[0]["capabilityKey"] != "read" {
		t.Fatalf("filtered grants = %#v", filtered)
	}
}

// TestRuntimeIntegrationGrantsExcludeSystemSubscriptions requires both callable and
// capability-filtered projections to retain an ordinary read while excluding inbound,
// gate-decision and empty operations sharing its capability. Broker subscriptions and actual
// runtime grants are not created.
// TestRuntimeIntegrationGrantsExcludeSystemSubscriptions требует от callable и capability-filtered
// projections сохранить обычный read и исключить inbound, gate-decision и empty operations с той
// же capability. Broker subscriptions и реальные runtime grants не создаются.
func TestRuntimeIntegrationGrantsExcludeSystemSubscriptions(t *testing.T) {
	t.Parallel()
	grants := []map[string]string{
		{"capabilityKey": "read", "operation": "mattermost.post.read"},
		{"capabilityKey": "read", "operation": "mattermost.inbound"},
		{"capabilityKey": "read", "operation": "mattermost.gate_decisions"},
		{"capabilityKey": "read", "operation": ""},
	}
	for _, filtered := range [][]map[string]string{callableIntegrationGrants(grants), filterIntegrationGrants(grants, []string{"read"})} {
		if len(filtered) != 1 || filtered[0]["operation"] != "mattermost.post.read" {
			t.Fatalf("system subscription reached runtime capabilities: %#v", filtered)
		}
	}
}
