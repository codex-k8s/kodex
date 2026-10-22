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

// TestNextWarmAcceptsTurnWithCompatibleRuntime serves one synthetic turn and checks warm revision/ticket headers, proving compatible turns remain available despite different turn-specific bindings.
// TestNextWarmAcceptsTurnWithCompatibleRuntime выдаёт один синтетический ход и проверяет warm-ревизию/ticket в заголовках: совместимый ход доступен при отличающихся привязках конкретного исполнения.
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

// TestPostReportsOnlySafeHTTPStatus returns a private conflict body and requires the callback error to contain only the fixed safe HTTP-status diagnostic.
// TestPostReportsOnlySafeHTTPStatus возвращает приватное тело конфликта и требует от callback только фиксированную безопасную диагностику HTTP-статуса.
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

// TestCompleteRejectsInvalidPayloadBeforeTransport rejects an empty completion locally and asserts that the synthetic callback endpoint was never invoked.
// TestCompleteRejectsInvalidPayloadBeforeTransport локально отклоняет пустое завершение и проверяет отсутствие вызова синтетического callback endpoint.
func TestCompleteRejectsInvalidPayloadBeforeTransport(t *testing.T) {
	called := false
	client := callbackFixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	err := client.Complete(context.Background(), validWarmTurnFixture(), runtimecontract.RunnerCompletionRequest{})
	if err == nil || err.Error() != "validate runtime completion: runner completion is invalid" {
		t.Fatalf("Complete() error = %v", err)
	}
	if called {
		t.Fatal("invalid runtime completion reached the callback transport")
	}
}

// TestCompleteRetriesTransientCallbackWithoutChangingPayload makes two 503 replies precede success and verifies exactly three requests with byte-identical completion bodies.
// TestCompleteRetriesTransientCallbackWithoutChangingPayload выдаёт два ответа 503 перед успехом и проверяет ровно три запроса с побайтно одинаковым телом завершения.
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
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: true, ResultSummary: "done"}
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

// TestProgressDoesNotDuplicateTransientControlPlaneOutage configures retries but requires ordinary progress to make only one request when the control plane returns 503.
// TestProgressDoesNotDuplicateTransientControlPlaneOutage задаёт повторы, но требует от обычного progress только один запрос при ответе control plane 503.
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

// TestDefaultCallbackRetryWindowCoversControlPlaneRestart checks that scheduled backoff alone totals at least seven seconds; it does not restart a control plane or measure request timeouts.
// TestDefaultCallbackRetryWindowCoversControlPlaneRestart проверяет сумму задержек не менее семи секунд; не перезапускает control plane и не измеряет сроки запросов.
func TestDefaultCallbackRetryWindowCoversControlPlaneRestart(t *testing.T) {
	var window time.Duration
	for _, delay := range defaultCallbackRetryDelays {
		window += delay
	}
	if window < 7*time.Second {
		t.Fatalf("callback retry window = %s, want at least 7s", window)
	}
}

// TestRetryableCallbackStatusUsesClosedTransientSet checks the six allowed transient statuses and representative client/authority/conflict failures that must not retry.
// TestRetryableCallbackStatusUsesClosedTransientSet проверяет шесть разрешённых временных статусов и представительные клиентские/authority/конфликтные отказы без повторов.
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

// TestCompleteDoesNotRetryStateConflict returns 409 and requires exactly one terminal callback despite a configured retry schedule.
// TestCompleteDoesNotRetryStateConflict возвращает 409 и требует ровно один конечный callback при заданном расписании повторов.
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
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: true, ResultSummary: "done"}
	err = client.Complete(context.Background(), input, payload)
	if err == nil || err.Error() != "runtime callback rejected request with status 409" {
		t.Fatalf("Complete() error = %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("Complete() attempts = %d, want 1", attempts.Load())
	}
}

// TestRecordNativeToolCallUsesExecutionScopedBoundedPayload checks the POST lease route, ticket and typed native-tool projection, retaining revision, call identifier and tool kind.
// TestRecordNativeToolCallUsesExecutionScopedBoundedPayload проверяет POST-маршрут lease, ticket и типизированную проекцию инструмента с сохранением ревизии, идентификатора вызова и вида.
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
	call := runtimecontract.NativeToolCall{CallID: "call-shell", Revision: 2, Kind: runtimecontract.NativeToolKindShell,
		State: runtimecontract.NativeToolStateSucceeded, DurationMS: 25, SafeResult: runtimecontract.NativeToolResultCompleted,
		SafeParameters: map[string]any{"action_count": 1, "action_kinds": []string{"READ"}, "cwd_scope": "WORKSPACE", "exit_code": "ZERO", "source": "AGENT"}}
	if err := client.RecordNativeToolCall(context.Background(), input, call); err != nil {
		t.Fatalf("RecordNativeToolCall() error = %v", err)
	}
	if captured.RuntimeRevisionDigest != input.RuntimeRevisionDigest || captured.CallID != call.CallID || captured.Kind != call.Kind {
		t.Fatalf("captured payload = %#v", captured)
	}
}

// TestRecordNativeToolCallRetriesTransientCallback returns 504 once and requires a second request to deliver the same bounded native-tool event successfully.
// TestRecordNativeToolCallRetriesTransientCallback однократно возвращает 504 и требует второй запрос для успешной доставки того же ограниченного события инструмента.
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
	call := runtimecontract.NativeToolCall{CallID: "call-shell", Revision: 2, Kind: runtimecontract.NativeToolKindShell,
		State: runtimecontract.NativeToolStateSucceeded, SafeResult: runtimecontract.NativeToolResultCompleted,
		SafeParameters: map[string]any{"action_count": 1, "action_kinds": []string{"READ"}, "cwd_scope": "WORKSPACE", "exit_code": "ZERO", "source": "AGENT"}}
	if err := client.RecordNativeToolCall(context.Background(), validWarmTurnFixture(), call); err != nil {
		t.Fatalf("RecordNativeToolCall() error = %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("RecordNativeToolCall() attempts = %d, want 2", attempts.Load())
	}
}

// TestProgressDoesNotRetryClientError returns 400 and requires one progress attempt with a safe status error even when retry delays are configured.
// TestProgressDoesNotRetryClientError возвращает 400 и требует одну попытку progress с безопасной ошибкой статуса даже при заданных задержках.
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

// TestRecordNativeToolCallRejectsUnsafeParametersBeforeTransport supplies forbidden raw command parameters and verifies rejection before any callback request.
// TestRecordNativeToolCallRejectsUnsafeParametersBeforeTransport передаёт запрещённые сырые параметры команды и проверяет отказ до любого callback-запроса.
func TestRecordNativeToolCallRejectsUnsafeParametersBeforeTransport(t *testing.T) {
	called := false
	client := callbackFixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	call := runtimecontract.NativeToolCall{CallID: "call-shell", Revision: 2, Kind: runtimecontract.NativeToolKindShell,
		State: runtimecontract.NativeToolStateSucceeded, SafeResult: runtimecontract.NativeToolResultCompleted,
		SafeParameters: map[string]any{"command": "print secret"}}
	if err := client.RecordNativeToolCall(context.Background(), validWarmTurnFixture(), call); err == nil {
		t.Fatal("unsafe native tool projection was accepted")
	}
	if called {
		t.Fatal("unsafe native tool projection reached transport")
	}
}

// TestCommitProviderCredentialRefreshUsesExecutionScopedCallback checks the exact lease route/ticket and typed revision, prior credential identity/hash and synthetic rotated authentication bytes.
// TestCommitProviderCredentialRefreshUsesExecutionScopedCallback проверяет точный маршрут lease/ticket, типизированную ревизию, прежние identity/хеш реквизитов и синтетические байты новой авторизации.
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

// TestCompleteRejectsMismatchedAttemptBeforeTransport changes only the attempt of an otherwise valid completion and verifies rejection without contacting the endpoint.
// TestCompleteRejectsMismatchedAttemptBeforeTransport меняет только попытку корректного в остальном завершения и проверяет отказ без обращения к endpoint.
func TestCompleteRejectsMismatchedAttemptBeforeTransport(t *testing.T) {
	called := false
	client := callbackFixtureClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { called = true }))
	input := validWarmTurnFixture()
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt + 1, Success: true, ResultSummary: "done"}
	if err := client.Complete(context.Background(), input, payload); err == nil {
		t.Fatal("completion with a foreign attempt was accepted")
	}
	if called {
		t.Fatal("foreign attempt reached transport")
	}
}

// TestCommitProviderCredentialRefreshRejectsInvalidPayloadBeforeTransport sends an empty rotation request and requires validation to fail before the synthetic server is called.
// TestCommitProviderCredentialRefreshRejectsInvalidPayloadBeforeTransport передаёт пустую ротацию и требует отказ проверки до обращения к синтетическому серверу.
func TestCommitProviderCredentialRefreshRejectsInvalidPayloadBeforeTransport(t *testing.T) {
	called := false
	client := callbackFixtureClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	if err := client.CommitProviderCredentialRefresh(context.Background(), validWarmTurnFixture(), runtimecontract.RunnerProviderCredentialRefreshRequest{}); err == nil {
		t.Fatal("invalid provider credential refresh was accepted")
	}
	if called {
		t.Fatal("invalid provider credential refresh reached transport")
	}
}

// callbackFixtureClient starts an isolated loopback HTTP fixture with a synthetic ticket, registers server cleanup even on fatal failure and leaves each test's handler and assertions explicit.
// callbackFixtureClient запускает изолированную HTTP-фикстуру на loopback с синтетическим ticket, регистрирует закрытие сервера даже при fatal-отказе и оставляет handler и проверки каждого теста явными.
func callbackFixtureClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{http: server.Client(), base: base, token: "ticket"}
}

// validWarmTurnFixture builds a fresh synthetic system-assistant turn with pinned environment and execution digests. Paths and credential hashes are inert metadata; no files, credentials, workloads or provider calls are created.
// validWarmTurnFixture создаёт новый синтетический ход системного помощника с закреплёнными окружением и дайджестами исполнения. Пути и хеши реквизитов — неактивные данные; файлы, реквизиты, нагрузки и вызовы провайдера не создаются.
func validWarmTurnFixture() runtimecontract.RunnerInput {
	imageDigest := "sha256:" + strings.Repeat("a", 64)
	image := runtimecontract.RuntimeEnvironmentImage{ArtifactRef: "imgart_abcdefgh", RecipeRef: "imgrec_abcdefgh",
		RecipeGeneration: 1, Reference: "registry.example/roles@" + imageDigest, Digest: imageDigest}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	access, _ := runtimecontract.RuntimeKubernetesAccessForExecution(policy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	environmentDigest, _ := runtimecontract.RuntimeEnvironmentDigest(nil, nil, image, nil, policy)
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV9, Mode: runtimecontract.RunnerModeTurn,
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
		PromptMaterializationDigest: strings.Repeat("7", 64), AssistantScope: runtimecontract.AssistantScopeSystem,
		Instructions: "Complete the bounded task.", Task: "Prepare the customer response.",
		Provider: "openai", Model: "codex", ProviderAccountRef: "pacc_abcdefgh",
		ProviderCredentialRef: "pcr_abcdefgh", ProviderCredentialRevision: 1,
		ProviderCredentialSHA256: strings.Repeat("e", 64),
		RuntimeConfigRef:         "rconf_abcdefgh", RuntimeConfigVersion: 1, RuntimeConfigDigest: strings.Repeat("1", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 1, ProviderPolicyDigest: strings.Repeat("2", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 1, ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "medium",
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
	input.InputDigest, _ = runtimecontract.RuntimeBoundedInputDigest(input.BoundedInput)
	input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
	return input
}
