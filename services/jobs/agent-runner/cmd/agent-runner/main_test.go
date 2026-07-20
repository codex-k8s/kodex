package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestMain(testMain *testing.M) {
	if os.Getenv("MATTERCODEX_TEST_MCP_BOOTSTRAP_HELPER") == "1" {
		os.Exit(runMatterCodexMCPBootstrapShim())
	}
	os.Exit(testMain.Run())
}

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

func TestLatestCodexLimitsSummaryReadsAndMergesSessionSnapshots(t *testing.T) {
	codexHome := t.TempDir()
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "06", "25")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	body := strings.Join([]string{
		`{"timestamp":"2026-06-25T06:00:00Z","payload":{"type":"token_count","rate_limits":{"limitId":"codex","primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":1772881542},"secondary":{"usedPercent":55,"windowDurationMins":10080,"resetsAt":1773428970},"planType":"team"}}}`,
		`{"timestamp":"2026-06-25T05:59:00Z","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":46,"window_minutes":300,"resets_at":1772881542},"secondary":{"used_percent":84,"window_minutes":10080,"resets_at":1773428970},"plan_type":"team"}}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(sessionDir, "session.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}

	summary, err := latestCodexLimitsSummary(codexHome)
	if err != nil {
		t.Fatalf("latestCodexLimitsSummary() error = %v", err)
	}
	lines := strings.Split(summary, "\n")
	if len(lines) != 2 {
		t.Fatalf("summary = %q", summary)
	}
	if !strings.HasPrefix(lines[0], "🕔 5h ████░░░░  54%") {
		t.Fatalf("5h line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "📅 7d █░░░░░░░  16%") {
		t.Fatalf("7d line = %q", lines[1])
	}
}

func TestLatestCodexLimitsSummaryAllowsMissingSessions(t *testing.T) {
	summary, err := latestCodexLimitsSummary(t.TempDir())
	if err != nil {
		t.Fatalf("latestCodexLimitsSummary() error = %v", err)
	}
	if summary != "" {
		t.Fatalf("summary = %q, want empty", summary)
	}
}

func TestSessionAPIErrorRetryClassification(t *testing.T) {
	if !sessionAPIErrorRetriable(errors.New("connect: connection refused")) {
		t.Fatal("network error should be retriable")
	}
	if !sessionAPIErrorRetriable(sessionAPIStatusError{StatusCode: 502, Body: "bad gateway"}) {
		t.Fatal("5xx status should be retriable")
	}
	if sessionAPIErrorRetriable(sessionAPIStatusError{StatusCode: 401, Body: "unauthorized"}) {
		t.Fatal("4xx status should not be retriable")
	}
}

func TestCodexTransientCapacityFailureReadsStructuredEvent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	stderrPath := filepath.Join(dir, "stderr.log")
	body := strings.Join([]string{
		`{"type":"thread.started","thread_id":"session-1"}`,
		`{"type":"error","message":"Selected model is at capacity. Please try a different model."}`,
		`{"type":"turn.failed","message":"Selected model is at capacity. Please try a different model."}`,
	}, "\n")
	if err := os.WriteFile(eventsPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	if !codexTransientCapacityFailure(eventsPath, stderrPath, errors.New("exit status 1")) {
		t.Fatal("capacity failure was not classified as transient")
	}
}

func TestCodexTransientCapacityFailureRejectsUsageLimit(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	stderrPath := filepath.Join(dir, "stderr.log")
	if err := os.WriteFile(eventsPath, []byte(`{"type":"turn.failed","message":"You have reached your usage limit."}`), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	if codexTransientCapacityFailure(eventsPath, stderrPath, errors.New("exit status 1")) {
		t.Fatal("usage limit must not be retried as model capacity")
	}
}

func TestCodexProviderPolicyFailureReadsNestedStructuredEvent(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	stderrPath := filepath.Join(dir, "stderr.log")
	body := `{"type":"turn.failed","error":{"message":"Your request was flagged for possible cybersecurity risk. See Trusted Access for Cyber."}}`
	if err := os.WriteFile(eventsPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	if !codexProviderPolicyFailure(eventsPath, stderrPath, errors.New("exit status 1")) {
		t.Fatal("provider policy failure was not classified")
	}
}

func TestCodexProviderPolicyFailureRejectsAssistantDiscussion(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	stderrPath := filepath.Join(dir, "stderr.log")
	body := `{"type":"item.completed","item":{"text":"The cyber safety classifier is documented here."}}`
	if err := os.WriteFile(eventsPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		t.Fatalf("write stderr: %v", err)
	}

	if codexProviderPolicyFailure(eventsPath, stderrPath, errors.New("exit status 1")) {
		t.Fatal("assistant discussion must not be classified as a provider policy failure")
	}
}

func TestCodexCapacityRetryScheduleAndArtifacts(t *testing.T) {
	want := []time.Duration{time.Minute, 3 * time.Minute, 5 * time.Minute}
	if len(codexCapacityRetryDelays) != len(want) {
		t.Fatalf("retry delays = %#v", codexCapacityRetryDelays)
	}
	for index := range want {
		if codexCapacityRetryDelays[index] != want[index] {
			t.Fatalf("retry delay %d = %s, want %s", index, codexCapacityRetryDelays[index], want[index])
		}
	}
	if got := filepath.Base(sessionTurnEventsPath(42, 0)); got != "codex-events-42.jsonl" {
		t.Fatalf("initial events path = %q", got)
	}
	if got := filepath.Base(sessionTurnEventsPath(42, 2)); got != "codex-events-42-retry-2.jsonl" {
		t.Fatalf("retry events path = %q", got)
	}
	prompt := codexCapacityRetryPrompt(2, 3)
	for _, expected := range []string{"automatic retry 2/3", "Do not restart work", "existing locale"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("retry prompt misses %q: %q", expected, prompt)
		}
	}
}

func TestMatterCodexMCPBootstrapFailureBaselineRemainsFailedAfterDependencyRecovery(t *testing.T) {
	const (
		sessionKey   = "characteristic-session-51"
		sessionToken = "synthetic-session-token-not-logged"
		turnID       = int64(510077)
	)
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatalf("создание каталога артефактов: %v", err)
	}
	eventsPath := sessionTurnEventsPath(turnID, 0)
	stderrPath := sessionTurnStderrPath(turnID, 0)
	finalPath := filepath.Join(artifactsDir, fmt.Sprintf("session-turn-%d-final.md", turnID))
	for _, path := range []string{eventsPath, stderrPath, finalPath} {
		_ = os.Remove(path)
		path := path
		t.Cleanup(func() { _ = os.Remove(path) })
	}

	var dependencyAvailable atomic.Bool
	var mcpBootstrapRequests atomic.Int32
	claims := 0
	var completion sessionTurnCompleteRequest
	terminalState := ""
	mcpServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mcpBootstrapRequests.Add(1)
		if request.Method != http.MethodPost {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var envelope struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil || envelope.Method != "initialize" {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if !dependencyAvailable.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"synthetic-mattercodex","version":"1"}}}`))
	}))
	defer mcpServer.Close()
	completionObserved := make(chan struct{})
	dependencyRestored := make(chan struct{})
	go func() {
		<-completionObserved
		dependencyAvailable.Store(true)
		close(dependencyRestored)
	}()
	shimDirectory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(shimDirectory, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MATTERCODEX_MCP_URL", mcpServer.URL)
	t.Setenv("MATTERCODEX_MCP_TOKEN", "synthetic-mcp-bootstrap-token-not-logged")
	executionMarker := filepath.Join(t.TempDir(), "codex-executed")
	runner := &runner{sessionArchiveCreator: func(string, secretInventory) (string, error) { return "", nil }}
	t.Cleanup(runner.cleanupEphemeralRuntime)
	if err := runner.prepareEphemeralRuntime(); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexConfig(filepath.Join(runner.codexHome, "config.toml")); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+sessionToken {
			t.Error("session API получил неверную авторизацию")
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/internal/agent-sessions/" + sessionKey + "/turns/claim":
			claims++
			response.Header().Set("Content-Type", "application/json")
			if claims == 1 {
				_ = json.NewEncoder(response).Encode(sessionTurnClaimResponse{
					HasTurn: true, TurnID: turnID, RunID: "run-51-baseline", Prompt: "synthetic task",
				})
				return
			}
			_ = json.NewEncoder(response).Encode(sessionTurnClaimResponse{Exit: true})
		case "/internal/agent-sessions/" + sessionKey + "/turns/status":
			response.WriteHeader(http.StatusNoContent)
		case "/internal/agent-sessions/" + sessionKey + "/turns/complete":
			if err := json.NewDecoder(request.Body).Decode(&completion); err != nil {
				t.Errorf("декодирование завершения хода: %v", err)
				response.WriteHeader(http.StatusBadRequest)
				return
			}
			terminalState = completion.Status
			close(completionObserved)
			<-dependencyRestored
			response.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("неожиданный session API path: %s", request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.runSessionTurns(
		ctx, server.Client(), server.URL, sessionKey, sessionToken, "", t.TempDir(), []string{
			"MATTERCODEX_TEST_MCP_BOOTSTRAP_HELPER=1",
			"MATTERCODEX_TEST_MCP_ENDPOINT=" + mcpServer.URL,
			"MATTERCODEX_TEST_MCP_EXECUTION_MARKER=" + executionMarker,
		}, "",
	); err != nil {
		t.Fatalf("характеристический session loop: %v", err)
	}
	if completion.TurnID != turnID || completion.Status != "failed" || terminalState != "failed" || completion.ErrorMessage != "exit status 1" {
		t.Fatalf("baseline completion = %#v terminal_state=%q", completion, terminalState)
	}
	if _, err := os.Stat(executionMarker); err != nil || mcpBootstrapRequests.Load() != 1 || claims != 2 || !dependencyAvailable.Load() {
		t.Fatalf("baseline marker=%v mcp_requests=%d claims=%d dependency_available=%t", err, mcpBootstrapRequests.Load(), claims, dependencyAvailable.Load())
	}
	if info, err := os.Stat(eventsPath); err != nil || info.Size() != 0 {
		t.Fatalf("Codex events до MCP bootstrap: info=%v error=%v", info, err)
	}
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Fatalf("финальный файл неожиданно создан: %v", err)
	}
	if completion.FinalMessage == "" || completion.CodexSessionID != "" {
		t.Fatalf("baseline result потерял характеристические признаки: %#v", completion)
	}
	// Успех этого теста фиксирует только текущий долг #51: после восстановления
	// зависимости тот же ход остаётся failed и автоматически не продолжается.
}

func runMatterCodexMCPBootstrapShim() int {
	marker := os.Getenv("MATTERCODEX_TEST_MCP_EXECUTION_MARKER")
	if marker == "" {
		return 2
	}
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 2
	}
	_ = file.Close()
	config, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "config.toml"))
	endpoint := os.Getenv("MATTERCODEX_TEST_MCP_ENDPOINT")
	if err != nil || !strings.Contains(string(config), "required = true") || !strings.Contains(string(config), endpoint) {
		return 2
	}
	client := &http.Client{Timeout: time.Second}
	requestBody := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"codex-production-boundary-shim","version":"1"}}}`)
	request, err := http.NewRequest(http.MethodPost, endpoint, requestBody)
	if err != nil {
		return 2
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
	if err != nil || response.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintln(os.Stderr, "required MCP servers failed to initialize: mattercodex: handshaking with MCP server failed")
		return 1
	}
	_, _ = fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"unexpected-after-bootstrap"}`)
	return 0
}

func TestKilledCodexSubprocessLeavesOnlySanitizedPersistentOutputs(t *testing.T) {
	const (
		secret = "mc-killed-child-secret-51de7802"
		turnID = int64(510078)
	)
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		sessionTurnEventsPath(turnID, 0),
		sessionTurnStderrPath(turnID, 0),
		filepath.Join(artifactsDir, fmt.Sprintf("session-turn-%d-final.md", turnID)),
	}
	for _, path := range paths {
		_ = os.Remove(path)
		path := path
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	runner := &runner{commandContext: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		finalPath := ""
		for index, argument := range args {
			if argument == "--output-last-message" && index+1 < len(args) {
				finalPath = args[index+1]
			}
		}
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestKilledCodexSubprocessHelper", "--", finalPath)
	}}
	t.Cleanup(runner.cleanupEphemeralRuntime)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _, runErr := runner.runCodexSessionTurn(ctx, sessionTurnClaimResponse{
		TurnID: turnID, Prompt: "synthetic kill test",
	}, "", fmt.Sprintf("session-turn-%d-final.md", turnID), t.TempDir(), []string{
		"MATTERCODEX_TEST_KILLED_HELPER=1",
		"OPENAI_API_KEY=" + secret,
	}, 0)
	if runErr == nil {
		t.Fatal("убитый subprocess неожиданно завершился успешно")
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("санитизированный persistent output %s отсутствует: %v", filepath.Base(path), err)
		}
		if strings.Contains(string(body), secret) {
			t.Fatalf("persistent output %s содержит raw secret", filepath.Base(path))
		}
	}
	entries, err := os.ReadDir(runner.rawArtifacts)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("raw staging не очищен после kill: %d файлов", len(entries))
	}
	rollout := filepath.Join(runner.codexHome, "sessions", "synthetic", "rollout.jsonl")
	body, err := os.ReadFile(rollout)
	if err != nil {
		t.Fatalf("санитизированный source rollout отсутствует: %v", err)
	}
	if strings.Contains(string(body), secret) || !strings.Contains(string(body), redactedSecretValue) {
		t.Fatal("source rollout после kill не санитизирован")
	}
}

func TestKilledCodexSubprocessHelper(t *testing.T) {
	if os.Getenv("MATTERCODEX_TEST_KILLED_HELPER") != "1" {
		return
	}
	secret := os.Getenv("OPENAI_API_KEY")
	finalPath := os.Args[len(os.Args)-1]
	if err := os.MkdirAll(filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "synthetic"), 0o700); err != nil {
		os.Exit(2)
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "synthetic", "rollout.jsonl"), []byte(secret), 0o600)
	_ = os.WriteFile(finalPath, []byte(secret), 0o600)
	_, _ = fmt.Fprintln(os.Stdout, secret)
	_, _ = fmt.Fprintln(os.Stderr, secret)
	time.Sleep(30 * time.Second)
}

func mustTable(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	table, ok := asStringAnyMap(parent[key])
	if !ok {
		t.Fatalf("%s is not a table: %#v", key, parent[key])
	}
	return table
}
