package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestNextWarmAcceptsTurnWithCompatibleRuntime(t *testing.T) {
	turn := validWarmTurnFixture()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer ticket" ||
			request.Header.Get("X-Kodex-Runtime-Revision") != "system-assistant-core-v1" ||
			request.Header.Get("X-Kodex-Runtime-Revision-Digest") != strings.Repeat("f", 64) {
			http.Error(writer, "invalid binding", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(turn)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	warm := turn
	warm.Mode = runtimecontract.RunnerModeWarm
	warm.RunRef, warm.NodeRef, warm.TurnRef = "", "", ""
	warm.Attempt, warm.LeaseRef, warm.LeaseFence, warm.LeaseGeneration = 0, "", "", 0
	warm.InputDigest, warm.ExecutionBindingDigest, warm.MCPBindingDigest = "", "", ""
	warm.Task = ""
	warm.RuntimeRevisionRef = "system-assistant-core-v1"
	warm.RuntimeRevisionDigest = strings.Repeat("f", 64)

	got, available, err := client.NextWarm(context.Background(), warm)
	if err != nil {
		t.Fatalf("NextWarm() error = %v", err)
	}
	if !available || got.RunRef != turn.RunRef {
		t.Fatalf("NextWarm() = available %v, run %q", available, got.RunRef)
	}
}

func TestPostReportsOnlySafeHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "sensitive internal diagnostic", http.StatusConflict)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	err = client.post(context.Background(), validWarmTurnFixture(), "/complete", map[string]string{"result": "bounded"})
	if err == nil || err.Error() != "runtime callback rejected request with status 409" {
		t.Fatalf("post() error = %v", err)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatal("runtime callback response body escaped the provider boundary")
	}
}

func TestCompleteRejectsInvalidPayloadBeforeTransport(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	err = client.Complete(context.Background(), validWarmTurnFixture(), runtimecontract.RunnerCompletionRequest{})
	if err == nil || err.Error() != "validate runtime completion: runner completion is invalid" {
		t.Fatalf("Complete() error = %v", err)
	}
	if called {
		t.Fatal("invalid runtime completion reached the callback transport")
	}
}

func TestCompleteRetriesTransientCallbackWithoutChangingPayload(t *testing.T) {
	var attempts atomic.Int32
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		if attempts.Add(1) < 3 {
			http.Error(writer, "temporary owner failure", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond, time.Millisecond}}
	input := validWarmTurnFixture()
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: true, ResultSummary: "done"}
	if err := client.Complete(context.Background(), input, payload); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("Complete() attempts = %d, want 3", attempts.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 || !bytes.Equal(bodies[0], bodies[1]) || !bytes.Equal(bodies[1], bodies[2]) {
		t.Fatalf("terminal callback payload changed across retries: %q", bodies)
	}
}

func TestProgressDoesNotDuplicateTransientControlPlaneOutage(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(writer, "control plane restarting", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond, time.Millisecond}}
	err = client.Progress(context.Background(), validWarmTurnFixture(), "MODEL_REQUEST_RUNNING")
	if err == nil || err.Error() != "runtime callback rejected request with status 503" {
		t.Fatalf("Progress() error = %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("Progress() attempts = %d, want 1", attempts.Load())
	}
}

func TestDefaultCallbackRetryWindowCoversControlPlaneRestart(t *testing.T) {
	var window time.Duration
	for _, delay := range defaultCallbackRetryDelays {
		window += delay
	}
	if window < 7*time.Second {
		t.Fatalf("callback retry window = %s, want at least 7s", window)
	}
}

func TestRetryableCallbackStatusUsesClosedTransientSet(t *testing.T) {
	t.Parallel()
	for _, code := range []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if !retryableCallbackStatus(code) {
			t.Fatalf("status %d is not retryable", code)
		}
	}
	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
	} {
		if retryableCallbackStatus(code) {
			t.Fatalf("status %d is unexpectedly retryable", code)
		}
	}
}

func TestCompleteDoesNotRetryStateConflict(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(writer, "state conflict", http.StatusConflict)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond}}
	input := validWarmTurnFixture()
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: true, ResultSummary: "done"}
	err = client.Complete(context.Background(), input, payload)
	if err == nil || err.Error() != "runtime callback rejected request with status 409" {
		t.Fatalf("Complete() error = %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("Complete() attempts = %d, want 1", attempts.Load())
	}
}

func TestRecordNativeToolCallUsesExecutionScopedBoundedPayload(t *testing.T) {
	var captured runtimecontract.RunnerNativeToolCallRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/executions/lease_abcdefgh/native-tool-call" ||
			request.Header.Get("Authorization") != "Bearer ticket" {
			http.NotFound(writer, request)
			return
		}
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&captured) != nil {
			http.Error(writer, "invalid", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	input := validWarmTurnFixture()
	call := runtimecontract.NativeToolCall{CallID: "call-shell", Kind: runtimecontract.NativeToolKindShell,
		State: runtimecontract.NativeToolStateSucceeded, DurationMS: 25, SafeResult: runtimecontract.NativeToolResultCompleted,
		SafeParameters: map[string]any{"action_count": 1, "action_kinds": []string{"READ"}, "cwd_scope": "WORKSPACE", "exit_code": "ZERO", "source": "AGENT"}}
	if err := client.RecordNativeToolCall(context.Background(), input, call); err != nil {
		t.Fatalf("RecordNativeToolCall() error = %v", err)
	}
	if captured.RuntimeRevisionDigest != input.RuntimeRevisionDigest || captured.CallID != call.CallID || captured.Kind != call.Kind {
		t.Fatalf("captured payload = %#v", captured)
	}
}

func TestRecordNativeToolCallRetriesTransientCallback(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(writer, "control plane restarting", http.StatusGatewayTimeout)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond}}
	call := runtimecontract.NativeToolCall{CallID: "call-shell", Kind: runtimecontract.NativeToolKindShell,
		State: runtimecontract.NativeToolStateSucceeded, SafeResult: runtimecontract.NativeToolResultCompleted,
		SafeParameters: map[string]any{"action_count": 1, "action_kinds": []string{"READ"}, "cwd_scope": "WORKSPACE", "exit_code": "ZERO", "source": "AGENT"}}
	if err := client.RecordNativeToolCall(context.Background(), validWarmTurnFixture(), call); err != nil {
		t.Fatalf("RecordNativeToolCall() error = %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("RecordNativeToolCall() attempts = %d, want 2", attempts.Load())
	}
}

func TestProgressDoesNotRetryClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(writer, "invalid progress", http.StatusBadRequest)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket", retryDelays: []time.Duration{time.Millisecond}}
	err = client.Progress(context.Background(), validWarmTurnFixture(), "MODEL_REQUEST_RUNNING")
	if err == nil || err.Error() != "runtime callback rejected request with status 400" {
		t.Fatalf("Progress() error = %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("Progress() attempts = %d, want 1", attempts.Load())
	}
}

func TestRecordNativeToolCallRejectsUnsafeParametersBeforeTransport(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	call := runtimecontract.NativeToolCall{CallID: "call-shell", Kind: runtimecontract.NativeToolKindShell,
		State: runtimecontract.NativeToolStateSucceeded, SafeResult: runtimecontract.NativeToolResultCompleted,
		SafeParameters: map[string]any{"command": "print secret"}}
	if err := client.RecordNativeToolCall(context.Background(), validWarmTurnFixture(), call); err == nil {
		t.Fatal("unsafe native tool projection was accepted")
	}
	if called {
		t.Fatal("unsafe native tool projection reached transport")
	}
}

func TestCommitProviderCredentialRefreshUsesExecutionScopedCallback(t *testing.T) {
	var captured runtimecontract.RunnerProviderCredentialRefreshRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/executions/lease_abcdefgh/provider-credential-refresh" ||
			request.Header.Get("Authorization") != "Bearer ticket" {
			http.NotFound(writer, request)
			return
		}
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&captured) != nil {
			http.Error(writer, "invalid", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	input := validWarmTurnFixture()
	payload := runtimecontract.RunnerProviderCredentialRefreshRequest{
		RuntimeRevisionDigest:         input.RuntimeRevisionDigest,
		PreviousCredentialRevisionRef: input.ProviderCredentialRef,
		PreviousContentSHA256:         input.ProviderCredentialSHA256,
		Authentication:                []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"rotated"}}`),
	}
	if err := client.CommitProviderCredentialRefresh(context.Background(), input, payload); err != nil {
		t.Fatalf("CommitProviderCredentialRefresh() error = %v", err)
	}
	if captured.RuntimeRevisionDigest != payload.RuntimeRevisionDigest ||
		captured.PreviousCredentialRevisionRef != payload.PreviousCredentialRevisionRef ||
		captured.PreviousContentSHA256 != payload.PreviousContentSHA256 ||
		string(captured.Authentication) != string(payload.Authentication) {
		t.Fatal("captured payload metadata does not match request")
	}
}

func TestCommitProviderCredentialRefreshRejectsInvalidPayloadBeforeTransport(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{http: server.Client(), base: base, token: "ticket"}
	if err := client.CommitProviderCredentialRefresh(context.Background(), validWarmTurnFixture(), runtimecontract.RunnerProviderCredentialRefreshRequest{}); err == nil {
		t.Fatal("invalid provider credential refresh was accepted")
	}
	if called {
		t.Fatal("invalid provider credential refresh reached transport")
	}
}

func validWarmTurnFixture() runtimecontract.RunnerInput {
	imageDigest := "sha256:" + strings.Repeat("a", 64)
	image := runtimecontract.RuntimeEnvironmentImage{ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh",
		RecipeGeneration: 1, Reference: "registry.example/roles@" + imageDigest, Digest: imageDigest}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	environmentDigest, _ := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, nil, policy)
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV6, Mode: runtimecontract.RunnerModeTurn,
		OrganizationRef:  "org_abcdefgh",
		WorkloadInstance: "runtime-controller-1", RunRef: "run_abcdefgh", NodeRef: "node_abcdefgh",
		ProjectRef: "prj_abcdefgh", SessionRef: "session_abcdefgh", TurnRef: "turn_abcdefgh", AgentRef: "agent_abcdefgh",
		Attempt: 1, LeaseRef: "lease_abcdefgh", LeaseFence: "fence-1", LeaseGeneration: 1,
		InputDigest:        strings.Repeat("0", 64),
		RuntimeRevisionRef: "revision_abcdefgh", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("b", 64), ImageReference: "registry.example/roles@" + imageDigest,
		ImageManifestDigest: imageDigest, EnvironmentImage: image, RoleRuntimeContractRevision: 1,
		RoleRuntimeContractSHA256: strings.Repeat("d", 64), RoleDefinitionRef: "roledef_abcdefgh",
		RuntimeProfileRef: "profile_abcdefgh", RuntimeProfileRevision: "profile-revision-1",
		InstructionRef: "instr_abcdefgh", InstructionDigest: strings.Repeat("5", 64),
		PromptTemplateRef: "prompt_abcdefgh", PromptTemplateDigest: strings.Repeat("6", 64),
		PromptMaterializationDigest: strings.Repeat("7", 64), SystemAssistant: true,
		Instructions: "Complete the bounded task.", Task: "Prepare the customer response.",
		Provider: "openai", Model: "codex", ProviderAccountRef: "pacc_abcdefgh",
		ProviderCredentialRef: "pcr_abcdefgh", ProviderCredentialRevision: 1,
		ProviderCredentialSHA256: strings.Repeat("e", 64),
		RuntimeConfigRef:         "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1,
		ConfigOverlayDigest:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 1,
		RuntimeEnvironmentDigest: environmentDigest,
		EnvironmentPolicy:        policy, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1(),
		EffectiveKubernetesAccess: access,
		EnvironmentBindingRef:     "aenv_abcdefgh", EnvironmentBindingVersion: 1, EnvironmentBindingDigest: strings.Repeat("3", 64),
		CodexSandbox:        "read-only",
		CodexApprovalPolicy: "never", CallbackURL: "https://10.0.0.10:8444",
		CallbackTLS: runtimecontract.RuntimeTLSBinding{
			ServerName:      "runtime-controller-callback.kodex-system.svc.cluster.local",
			CAFile:          "/var/run/config/kodex/runtime/callback/ca.crt",
			CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt",
			PrivateKeyFile:  "/var/run/secrets/kodex/runtime/callback-client/tls.key",
		},
		ExecutionTicketFile:    "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:       "/run/secrets/kodex/runtime/provider/auth.json",
		ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot:          "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
	input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
	return input
}
