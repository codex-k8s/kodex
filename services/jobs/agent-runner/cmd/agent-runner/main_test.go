package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
	"k8s.io/client-go/tools/clientcmd"
)

func TestMain(testMain *testing.M) {
	if os.Getenv("MATTERCODEX_TEST_MCP_BOOTSTRAP_HELPER") == "1" {
		os.Exit(runMatterCodexMCPBootstrapShim())
	}
	if os.Getenv("MATTERCODEX_TEST_CREDENTIAL_ROTATION_HELPER") == "1" {
		os.Exit(runCredentialRotationHelper())
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

func TestCodexAuthCollectionWindow(t *testing.T) {
	if codexAuthCollectionWindow != 24*time.Hour {
		t.Fatalf("codexAuthCollectionWindow = %s, want 24h", codexAuthCollectionWindow)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitCodexAuthCollection(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitCodexAuthCollection() error = %v, want context canceled", err)
	}
}

func TestSameGitHubRepositoryRemote(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "same HTTPS remote", left: "https://github.com/codex-k8s/matter-codex.git\n", right: "https://github.com/codex-k8s/matter-codex.git", want: true},
		{name: "same SSH remote", left: "git@github.com:codex-k8s/matter-codex.git", right: "https://github.com/codex-k8s/matter-codex.git", want: true},
		{name: "same SSH URL remote", left: "ssh://git@github.com/codex-k8s/matter-codex.git", right: "https://github.com/codex-k8s/matter-codex.git", want: true},
		{name: "different repository", left: "https://github.com/codex-k8s/kodex.git", right: "https://github.com/codex-k8s/matter-codex.git"},
		{name: "missing remote", right: "https://github.com/codex-k8s/matter-codex.git"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameGitHubRepositoryRemote(test.left, test.right); got != test.want {
				t.Fatalf("sameGitHubRepositoryRemote(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
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

func TestMissingRolloutStartsReplacementCodexSessionInSameTurn(t *testing.T) {
	useTestWorkspace(t)

	const (
		sessionKey       = "missing-rollout-session"
		sessionToken     = "synthetic-missing-rollout-token"
		turnID           = int64(510079)
		missingSessionID = "019faebb-74bd-7023-b2fd-9e02c023b7e5"
		newSessionID     = "replacement-thread-id"
	)
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	promptCapture := filepath.Join(t.TempDir(), "replacement-prompt")
	invocations := []bool{}
	testRunner := &runner{
		sessionArchiveCreator: func(string, secretInventory) (string, error) {
			return "synthetic-archive", nil
		},
		commandContext: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			resume := false
			finalPath := ""
			for index, argument := range args {
				if argument == "resume" {
					resume = true
				}
				if argument == "--output-last-message" && index+1 < len(args) {
					finalPath = args[index+1]
				}
			}
			invocations = append(invocations, resume)
			mode := "success"
			if resume {
				mode = "missing"
			}
			return exec.CommandContext(
				ctx,
				os.Args[0],
				"-test.run=TestMissingRolloutCommandHelper",
				"--",
				mode,
				finalPath,
			)
		},
	}
	if err := testRunner.prepareEphemeralRuntime(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(testRunner.cleanupEphemeralRuntime)

	claims := 0
	var completion sessionTurnCompleteRequest
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
					HasTurn: true, TurnID: turnID, RunID: "run-missing-rollout",
					Prompt: "continue the original owner task", CodexSessionID: missingSessionID,
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
			response.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("неожиданный session API path: %s", request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := testRunner.runSessionTurns(
		ctx,
		server.Client(),
		server.URL,
		sessionKey,
		sessionToken,
		"synthetic-openai-account",
		t.TempDir(),
		[]string{
			"MATTERCODEX_TEST_MISSING_ROLLOUT_HELPER=1",
			"MATTERCODEX_TEST_MISSING_ROLLOUT_PROMPT=" + promptCapture,
			"MATTERCODEX_TEST_MISSING_ROLLOUT_SESSION=" + newSessionID,
		},
		missingSessionID,
	); err != nil {
		t.Fatalf("missing rollout recovery: %v", err)
	}

	if len(invocations) != 2 || !invocations[0] || invocations[1] {
		t.Fatalf("Codex invocations resume flags = %#v, want [true false]", invocations)
	}
	if completion.Status != "succeeded" || completion.CodexSessionID != newSessionID ||
		completion.FinalMessage != "replacement completed" ||
		completion.SessionArchiveGzipBase64 != "synthetic-archive" ||
		completion.Artifacts[sessionRecoveryKey] != "succeeded" {
		t.Fatalf("recovery completion = %#v", completion)
	}
	prompt, err := os.ReadFile(promptCapture)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"mattermost_get_thread",
		"same Mattermost thread, PVC, and workspace",
		"continue the original owner task",
	} {
		if !strings.Contains(string(prompt), expected) {
			t.Fatalf("recovery prompt misses %q: %s", expected, string(prompt))
		}
	}
	for _, path := range []string{
		sessionTurnStderrPath(turnID, 0),
		sessionTurnEventsPath(turnID, 1),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("recovery artifact %s: %v", filepath.Base(path), err)
		}
	}
}

func TestMissingRolloutCommandHelper(t *testing.T) {
	if os.Getenv("MATTERCODEX_TEST_MISSING_ROLLOUT_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) <= separator+2 {
		os.Exit(2)
	}
	mode := os.Args[separator+1]
	finalPath := os.Args[separator+2]
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	if mode == "missing" {
		_, _ = fmt.Fprintln(os.Stderr, "Error: thread/resume: thread/resume failed: no rollout found for thread id 019faebb-74bd-7023-b2fd-9e02c023b7e5 (code -32600)")
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("MATTERCODEX_TEST_MISSING_ROLLOUT_PROMPT"), prompt, 0o600); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(finalPath, []byte("replacement completed"), 0o600); err != nil {
		os.Exit(2)
	}
	_, _ = fmt.Fprintf(
		os.Stdout,
		"{\"type\":\"thread.started\",\"thread_id\":%q}\n",
		os.Getenv("MATTERCODEX_TEST_MISSING_ROLLOUT_SESSION"),
	)
	os.Exit(0)
}

func TestMatterCodexMCPBootstrapFailureBaselineRemainsFailedAfterDependencyRecovery(t *testing.T) {
	useTestWorkspace(t)

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
	useTestWorkspace(t)

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

func TestCredentialSourcesAreSnapshottedAndRotationCancelsWithoutPublication(t *testing.T) {
	for index, testCase := range []struct {
		name             string
		credentialEnv    string
		additionalSource bool
	}{
		{name: "explicit static credential", additionalSource: true},
		{name: "KUBECONFIG", credentialEnv: "KUBECONFIG"},
		{name: "scoped _FILE", credentialEnv: "SCOPED_CREDENTIAL_FILE"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "credential-source")
			sourceBody := "mc-rotation-before-71b8a920"
			if testCase.credentialEnv == "KUBECONFIG" {
				sourceBody = "apiVersion: v1\nkind: Config\nusers:\n- name: synthetic\n  user:\n    token: mc-rotation-before-71b8a920\n"
			}
			if err := os.WriteFile(source, []byte(sourceBody), 0o600); err != nil {
				t.Fatal(err)
			}
			started := filepath.Join(directory, "started")
			observation := filepath.Join(directory, "snapshot-observation")
			turnID := int64(780000 + index)
			persistentPaths := []string{
				sessionTurnEventsPath(turnID, 0),
				sessionTurnStderrPath(turnID, 0),
				filepath.Join(artifactsDir, fmt.Sprintf("rotation-final-%d.md", index)),
			}
			for _, path := range persistentPaths {
				_ = os.Remove(path)
				path := path
				t.Cleanup(func() { _ = os.Remove(path) })
			}
			extraEnv := []string{
				"MATTERCODEX_TEST_CREDENTIAL_ROTATION_HELPER=1",
				"MATTERCODEX_TEST_ROTATION_SOURCE=" + source,
				"MATTERCODEX_TEST_ROTATION_STARTED=" + started,
				"MATTERCODEX_TEST_ROTATION_OBSERVATION=" + observation,
				"MATTERCODEX_TEST_ROTATION_PATH_ENV=" + testCase.credentialEnv,
			}
			if testCase.credentialEnv != "" {
				extraEnv = append(extraEnv, testCase.credentialEnv+"="+source)
			}
			if testCase.credentialEnv == "SCOPED_CREDENTIAL_FILE" {
				extraEnv = append(extraEnv, "MATTERCODEX_RUNTIME_ENV_ALLOWLIST=SCOPED_CREDENTIAL_FILE")
			}
			runner := &runner{commandContext: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				finalPath := ""
				for argumentIndex, argument := range args {
					if argument == "--output-last-message" && argumentIndex+1 < len(args) {
						finalPath = args[argumentIndex+1]
					}
				}
				return exec.CommandContext(ctx, os.Args[0], "-test.run=^$", "--", finalPath)
			}}
			if testCase.additionalSource {
				runner.credentialFiles = []string{source}
			}
			t.Cleanup(runner.cleanupEphemeralRuntime)
			type result struct {
				err error
			}
			resultChannel := make(chan result, 1)
			workDir := t.TempDir()
			go func() {
				_, _, err := runner.runCodexSessionTurn(context.Background(), sessionTurnClaimResponse{
					TurnID: turnID, Prompt: "synthetic credential rotation boundary",
				}, "", fmt.Sprintf("rotation-final-%d.md", index), workDir, extraEnv, 0)
				resultChannel <- result{err: err}
			}()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("subprocess не достиг production start boundary")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := os.WriteFile(source, []byte("mc-rotation-after-bf26d104"), 0o600); err != nil {
				t.Fatal(err)
			}
			var runResult result
			select {
			case runResult = <-resultChannel:
			case <-time.After(5 * time.Second):
				t.Fatal("rotation не отменила subprocess")
			}
			var rotationErr credentialRotationError
			if !errors.As(runResult.err, &rotationErr) || !runner.safety.isUnsafe() {
				t.Fatalf("rotation result=%T unsafe=%t", runResult.err, runner.safety.isUnsafe())
			}
			if testCase.credentialEnv != "" {
				body, err := os.ReadFile(observation)
				if err != nil || string(body) != "snapshot-ok" {
					t.Fatalf("child не получил private snapshot path: observation=%q error=%v", string(body), err)
				}
			}
			for _, path := range persistentPaths {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("unsafe persistent output существует: %s", filepath.Base(path))
				}
			}
			if _, err := os.Stat(runner.rawArtifacts); !os.IsNotExist(err) {
				t.Fatalf("raw staging не удалён: %v", err)
			}
			if _, err := os.Stat(filepath.Join(runner.codexHome, "sessions")); !os.IsNotExist(err) {
				t.Fatalf("unsafe session source не удалён: %v", err)
			}
			archive, archiveErr := runner.createSessionArchive(runner.codexHome)
			if archive != "" || !errors.As(archiveErr, &rotationErr) {
				t.Fatalf("archive не запрещён после rotation: archive_set=%t error=%T", archive != "", archiveErr)
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests++
				response.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			for _, action := range []string{"turns/status", "turns/complete"} {
				err := runner.sessionJSONOnce(context.Background(), server.Client(), http.MethodPost, server.URL, "session", "token", action, sessionTurnStatusRequest{RunID: "run", Phase: "safe"}, nil)
				if !errors.As(err, &rotationErr) {
					t.Fatalf("%s не запрещён после rotation: %T", action, err)
				}
			}
			if requests != 0 {
				t.Fatalf("после rotation выполнены network requests: %d", requests)
			}
		})
	}
}

func TestProjectedServiceAccountRotationDoesNotCancelAndRedactsAllObservedValues(t *testing.T) {
	useTestWorkspace(t)
	const (
		oldCredential = "mc-service-account-before-834b97c0"
		newCredential = "mc-service-account-after--834b97c1"
		turnID        = int64(780100)
	)
	directory := t.TempDir()
	source := filepath.Join(directory, "projected-service-account-token")
	if err := os.WriteFile(source, []byte(oldCredential), 0o600); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(directory, "started")
	finalName := "service-account-rotation-final.md"
	persistentPaths := []string{
		sessionTurnEventsPath(turnID, 0),
		sessionTurnStderrPath(turnID, 0),
		filepath.Join(artifactsDir, finalName),
	}
	for _, path := range persistentPaths {
		_ = os.Remove(path)
		path := path
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	testRunner := &runner{
		credentialFiles:         []string{},
		rotatingCredentialFiles: []string{source},
		commandContext: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
			finalPath := ""
			for index, argument := range args {
				if argument == "--output-last-message" && index+1 < len(args) {
					finalPath = args[index+1]
				}
			}
			return exec.CommandContext(ctx, os.Args[0], "-test.run=^$", "--", finalPath)
		},
	}
	t.Cleanup(testRunner.cleanupEphemeralRuntime)
	resultChannel := make(chan error, 1)
	workDir := t.TempDir()
	go func() {
		_, _, err := testRunner.runCodexSessionTurn(context.Background(), sessionTurnClaimResponse{
			TurnID: turnID,
			Prompt: "synthetic projected service account rotation",
		}, "", finalName, workDir, []string{
			"MATTERCODEX_TEST_CREDENTIAL_ROTATION_HELPER=1",
			"MATTERCODEX_TEST_ROTATION_SOURCE=" + source,
			"MATTERCODEX_TEST_ROTATION_STARTED=" + started,
		}, 0)
		resultChannel <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subprocess не достиг production start boundary")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(source, []byte(newCredential), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-resultChannel:
		if err != nil {
			t.Fatalf("штатная ротация projected token отменила subprocess: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subprocess не завершился после штатной ротации")
	}
	if testRunner.safety.isUnsafe() {
		t.Fatal("штатная ротация projected token пометила turn небезопасным")
	}
	for _, credential := range []string{oldCredential, newCredential} {
		protected, err := testRunner.secrets.protect("credential=" + credential)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(protected, credential) || !strings.Contains(protected, redactedSecretValue) {
			t.Fatalf("observed projected token отсутствует в итоговом redaction inventory")
		}
	}
	for _, path := range persistentPaths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("санитизированный output %s отсутствует: %v", filepath.Base(path), err)
		}
		if strings.Contains(string(body), oldCredential) || strings.Contains(string(body), newCredential) {
			t.Fatalf("санитизированный output %s содержит projected token", filepath.Base(path))
		}
	}
}

func runCredentialRotationHelper() int {
	source := os.Getenv("MATTERCODEX_TEST_ROTATION_SOURCE")
	started := os.Getenv("MATTERCODEX_TEST_ROTATION_STARTED")
	observation := os.Getenv("MATTERCODEX_TEST_ROTATION_OBSERVATION")
	pathEnvironmentName := os.Getenv("MATTERCODEX_TEST_ROTATION_PATH_ENV")
	if source == "" || started == "" {
		return 2
	}
	if pathEnvironmentName != "" {
		snapshotPath := os.Getenv(pathEnvironmentName)
		info, err := os.Stat(snapshotPath)
		parentInfo, parentErr := os.Stat(filepath.Dir(snapshotPath))
		if snapshotPath == "" || snapshotPath == source || err != nil || parentErr != nil || info.Mode().Perm() != 0o600 || parentInfo.Mode().Perm() != 0o700 {
			return 2
		}
		if err := os.WriteFile(observation, []byte("snapshot-ok"), 0o600); err != nil {
			return 2
		}
	}
	if err := os.WriteFile(started, []byte("started"), 0o600); err != nil {
		return 2
	}
	time.Sleep(2 * time.Second)
	body, err := os.ReadFile(source)
	if err != nil {
		return 2
	}
	_, _ = os.Stdout.Write(body)
	if len(os.Args) > 0 {
		finalPath := os.Args[len(os.Args)-1]
		_ = os.WriteFile(finalPath, body, 0o600)
	}
	return 0
}

func TestStructuredKubeconfigSourcesAreBoundedSnapshottedAndRewritten(t *testing.T) {
	directory := t.TempDir()
	linked := map[string]string{
		"token":  "mc-kube-linked-token-1b0eb619",
		"key":    "mc-kube-linked-key-1b0eb620",
		"cert":   "mc-kube-linked-cert-1b0eb621",
		"ca":     "mc-kube-linked-ca-1b0eb622",
		"inline": "mc-kube-inline-token-1b0eb623",
		"server": "mc-kube-server-auth-1b0eb625",
		"proxy":  "mc-kube-proxy-pass-1b0eb626",
	}
	paths := map[string]string{}
	for _, name := range []string{"token", "key", "cert", "ca"} {
		value := linked[name]
		paths[name] = filepath.Join(directory, name)
		if err := os.WriteFile(paths[name], []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	firstKubeconfig := filepath.Join(directory, "kubeconfig-a")
	secondKubeconfig := filepath.Join(directory, "kubeconfig-b")
	firstBody := fmt.Sprintf("apiVersion: v1\nkind: Config\nusers:\n- name: first\n  user:\n    token: %s\n    tokenFile: %s\n    client-key-data: %s\nclusters:\n- name: first\n  cluster:\n    server: https://%s@kubernetes.invalid\n    proxy-url: https://proxy-user:%s@proxy.invalid\n    certificate-authority: %s\n", linked["inline"], filepath.Base(paths["token"]), base64.StdEncoding.EncodeToString([]byte("mc-kube-inline-key-1b0eb624")), linked["server"], linked["proxy"], filepath.Base(paths["ca"]))
	secondBody := fmt.Sprintf("apiVersion: v1\nkind: Config\nusers:\n- name: second\n  user:\n    client-key: %s\n    client-certificate: %s\nclusters:\n- name: second\n  cluster:\n    server: https://kubernetes.invalid\n", filepath.Base(paths["key"]), filepath.Base(paths["cert"]))
	for path, body := range map[string]string{firstKubeconfig: firstBody, secondKubeconfig: secondBody} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	testRunner := &runner{credentialFiles: []string{}}
	t.Cleanup(testRunner.cleanupEphemeralRuntime)
	environment := []string{"KUBECONFIG=" + strings.Join([]string{firstKubeconfig, secondKubeconfig}, string(os.PathListSeparator))}
	command, guard, err := testRunner.guardedCommand(context.Background(), environment, os.Args[0], "-test.run=^$")
	if err != nil {
		t.Fatalf("guardedCommand() error = %v", err)
	}
	defer guard.abort()
	rewrittenValue := environmentValue(command.Env, "KUBECONFIG")
	rewrittenKubeconfigs := filepath.SplitList(rewrittenValue)
	if len(rewrittenKubeconfigs) != 2 {
		t.Fatalf("rewritten KUBECONFIG entries = %d", len(rewrittenKubeconfigs))
	}
	observedReferences := map[string]string{}
	for _, path := range rewrittenKubeconfigs {
		if !strings.HasPrefix(path, guard.snapshotRoot+string(filepath.Separator)) || path == firstKubeconfig || path == secondKubeconfig {
			t.Fatalf("kubeconfig не переведён в private snapshot: %q", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		config, err := clientcmd.Load(body)
		if err != nil {
			t.Fatal(err)
		}
		for _, authInfo := range config.AuthInfos {
			for name, reference := range map[string]string{"token": authInfo.TokenFile, "key": authInfo.ClientKey, "cert": authInfo.ClientCertificate} {
				if reference != "" {
					observedReferences[name] = reference
				}
			}
		}
		for _, cluster := range config.Clusters {
			if cluster.CertificateAuthority != "" {
				observedReferences["ca"] = cluster.CertificateAuthority
			}
		}
	}
	for name, original := range paths {
		rewritten := observedReferences[name]
		if rewritten == "" || rewritten == original || !strings.HasPrefix(rewritten, guard.snapshotRoot+string(filepath.Separator)) {
			t.Fatalf("linked %s ref не переписан: %q", name, rewritten)
		}
		info, err := os.Stat(rewritten)
		body, readErr := os.ReadFile(rewritten)
		if err != nil || readErr != nil || info.Mode().Perm() != 0o600 || string(body) != linked[name] {
			t.Fatalf("linked %s snapshot не совпал: stat=%v read=%v", name, err, readErr)
		}
	}
	for _, secret := range []string{linked["inline"], linked["token"], linked["key"], linked["cert"], linked["server"], linked["proxy"], "mc-kube-inline-key-1b0eb624", base64.StdEncoding.EncodeToString([]byte("proxy-user:" + linked["proxy"]))} {
		protected, protectErr := testRunner.secrets.protect("transport: " + secret)
		if protectErr == nil && strings.Contains(protected, secret) {
			t.Fatal("фактическое kubeconfig credential value отсутствует в inventory")
		}
	}
}

func TestStructuredKubeconfigRejectsDynamicProvidersBeforeChild(t *testing.T) {
	for name, provider := range map[string]string{
		"exec":          "exec:\n      command: credential-plugin",
		"auth-provider": "auth-provider:\n      name: oidc",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kubeconfig")
			body := "apiVersion: v1\nkind: Config\nusers:\n- name: synthetic\n  user:\n    " + provider + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			testRunner := &runner{credentialFiles: []string{}}
			t.Cleanup(testRunner.cleanupEphemeralRuntime)
			_, _, err := testRunner.guardedCommand(context.Background(), []string{"KUBECONFIG=" + path}, os.Args[0], "-test.run=^$")
			var providerErr unsupportedCredentialProviderError
			if !errors.As(err, &providerErr) {
				t.Fatalf("dynamic provider error = %T", err)
			}
		})
	}
}

func TestKubeconfigLinkedFileEventCancelsRunningChild(t *testing.T) {
	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	kubeconfigPath := filepath.Join(directory, "kubeconfig")
	if err := os.WriteFile(tokenPath, []byte("mc-linked-before-5517e301"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "apiVersion: v1\nkind: Config\nusers:\n- name: synthetic\n  user:\n    tokenFile: token\n"
	if err := os.WriteFile(kubeconfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	testRunner := &runner{credentialFiles: []string{}}
	t.Cleanup(testRunner.cleanupEphemeralRuntime)
	cmd, guard, err := testRunner.guardedCommand(context.Background(), []string{
		"KUBECONFIG=" + kubeconfigPath,
		"MATTERCODEX_TEST_GUARD_SLEEP=1",
	}, os.Args[0], "-test.run=TestCredentialGuardSleepHelper")
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		_ = guard.finish(err)
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("mc-linked-after--5517e302"), 0o600); err != nil {
		t.Fatal(err)
	}
	runErr := guard.finish(cmd.Wait())
	var rotationErr credentialRotationError
	if !errors.As(runErr, &rotationErr) || !testRunner.safety.isUnsafe() {
		t.Fatalf("linked rotation error=%T unsafe=%t", runErr, testRunner.safety.isUnsafe())
	}
}

func TestCredentialGuardSleepHelper(t *testing.T) {
	if os.Getenv("MATTERCODEX_TEST_GUARD_SLEEP") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestRuntimeCodexAuthMutationCancelsChildAndBlocksPublication(t *testing.T) {
	const (
		oldCredential = "mc-runtime-auth-old-6e74d901"
		newCredential = "mc-runtime-auth-new-6e74d902"
		turnID        = int64(790201)
	)
	testRunner := &runner{credentialFiles: []string{}, commandContext: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		finalPath := ""
		for index, argument := range args {
			if argument == "--output-last-message" && index+1 < len(args) {
				finalPath = args[index+1]
			}
		}
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestRuntimeCodexAuthMutationHelper", "--", finalPath)
	}}
	if err := testRunner.prepareEphemeralRuntime(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(testRunner.cleanupEphemeralRuntime)
	if err := os.WriteFile(filepath.Join(testRunner.codexHome, "config.toml"), []byte("sandbox_mode = \"danger-full-access\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authBody, _ := json.Marshal(map[string]any{"tokens": map[string]string{"access_token": oldCredential}})
	if err := os.WriteFile(filepath.Join(testRunner.codexHome, "auth.json"), authBody, 0o600); err != nil {
		t.Fatal(err)
	}
	finalName := "runtime-auth-final.md"
	for _, path := range []string{sessionTurnEventsPath(turnID, 0), sessionTurnStderrPath(turnID, 0), filepath.Join(artifactsDir, finalName)} {
		_ = os.Remove(path)
		path := path
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	_, _, runErr := testRunner.runCodexSessionTurn(context.Background(), sessionTurnClaimResponse{TurnID: turnID, Prompt: "runtime auth mutation"}, "", finalName, t.TempDir(), []string{
		"MATTERCODEX_TEST_RUNTIME_AUTH_MUTATION=1",
		"MATTERCODEX_TEST_RUNTIME_AUTH_NEW=" + newCredential,
	}, 0)
	var rotationErr credentialRotationError
	if !errors.As(runErr, &rotationErr) || !testRunner.safety.isUnsafe() {
		t.Fatalf("runtime auth mutation error=%T unsafe=%t", runErr, testRunner.safety.isUnsafe())
	}
	for _, path := range []string{sessionTurnEventsPath(turnID, 0), sessionTurnStderrPath(turnID, 0), filepath.Join(artifactsDir, finalName)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unsafe runtime auth output опубликован: %s", filepath.Base(path))
		}
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	err := testRunner.sessionJSONOnce(context.Background(), server.Client(), http.MethodPost, server.URL, "session", "token", "turns/status", sessionTurnStatusRequest{RunID: "run", Phase: newCredential}, nil)
	if !errors.As(err, &rotationErr) || requests != 0 {
		t.Fatalf("runtime auth network boundary: error=%T requests=%d", err, requests)
	}
}

func TestRuntimeCodexAuthMutationHelper(t *testing.T) {
	if os.Getenv("MATTERCODEX_TEST_RUNTIME_AUTH_MUTATION") != "1" {
		return
	}
	credential := os.Getenv("MATTERCODEX_TEST_RUNTIME_AUTH_NEW")
	authBody, _ := json.Marshal(map[string]any{"tokens": map[string]string{"access_token": credential}})
	if err := os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json"), authBody, 0o600); err != nil {
		os.Exit(2)
	}
	_, _ = fmt.Fprintln(os.Stdout, credential)
	if len(os.Args) > 0 {
		_ = os.WriteFile(os.Args[len(os.Args)-1], []byte(credential), 0o600)
	}
	time.Sleep(30 * time.Second)
}

func TestCredentialEventGuardRejectsAllMutationShapesAndTransientMetadataRestore(t *testing.T) {
	for _, mutation := range []string{"write-restore-metadata", "create", "delete", "rename"} {
		t.Run(mutation, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "credential")
			original := "mc-event-before-90b7a610"
			if mutation != "create" {
				if err := os.WriteFile(source, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			testRunner := &runner{credentialFiles: []string{source}}
			t.Cleanup(testRunner.cleanupEphemeralRuntime)
			_, guard, err := testRunner.guardedCommand(context.Background(), nil, os.Args[0], "-test.run=^$")
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "write-restore-metadata":
				info, err := os.Stat(source)
				if err != nil {
					t.Fatal(err)
				}
				rotated := "mc-event-rotate-90b7a611"
				if len(rotated) != len(original) {
					t.Fatal("test fixture lengths differ")
				}
				if err := os.WriteFile(source, []byte(rotated), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(source, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(source, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(source, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "create":
				if err := os.WriteFile(source, []byte("mc-event-created-90b7a612"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
			case "rename":
				if err := os.Rename(source, filepath.Join(directory, "credential-replaced")); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for !testRunner.safety.isUnsafe() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			var rotationErr credentialRotationError
			if err := guard.start(); !errors.As(err, &rotationErr) || !testRunner.safety.isUnsafe() {
				t.Fatalf("mutation %s: start error=%T unsafe=%t", mutation, err, testRunner.safety.isUnsafe())
			}
			if err := guard.finish(nil); !errors.As(err, &rotationErr) {
				t.Fatalf("mutation %s: finish error=%T", mutation, err)
			}
		})
	}
}

type fakeCredentialEventWatcher struct {
	events    chan fsnotify.Event
	errors    chan error
	closeOnce sync.Once
	onAdd     func(string)
}

func newFakeCredentialEventWatcher() *fakeCredentialEventWatcher {
	return &fakeCredentialEventWatcher{events: make(chan fsnotify.Event, 64), errors: make(chan error, 8)}
}

func (watcher *fakeCredentialEventWatcher) Add(path string) error {
	if watcher.onAdd != nil {
		watcher.onAdd(path)
	}
	return nil
}

func (watcher *fakeCredentialEventWatcher) Close() error {
	watcher.closeOnce.Do(func() {
		close(watcher.events)
		close(watcher.errors)
	})
	return nil
}

func (watcher *fakeCredentialEventWatcher) Events() <-chan fsnotify.Event { return watcher.events }
func (watcher *fakeCredentialEventWatcher) Errors() <-chan error          { return watcher.errors }
func (watcher *fakeCredentialEventWatcher) TriggerSync(path string) error {
	watcher.events <- fsnotify.Event{Name: path, Op: fsnotify.Create}
	return nil
}

func TestCredentialEventGuardFailsClosedOnReadBeforeWatchAndWatcherErrors(t *testing.T) {
	t.Run("read-before-watch", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "credential")
		if err := os.WriteFile(source, []byte("mc-watch-before-2902f801"), 0o600); err != nil {
			t.Fatal(err)
		}
		watcher := newFakeCredentialEventWatcher()
		var once sync.Once
		watcher.onAdd = func(path string) {
			if path != filepath.Dir(source) {
				return
			}
			once.Do(func() {
				_ = os.WriteFile(source, []byte("mc-watch-during-2902f802"), 0o600)
				watcher.events <- fsnotify.Event{Name: source, Op: fsnotify.Write}
			})
		}
		testRunner := &runner{
			credentialFiles: []string{source},
			credentialWatcherFactory: func() (credentialEventWatcher, error) {
				return watcher, nil
			},
		}
		t.Cleanup(testRunner.cleanupEphemeralRuntime)
		_, guard, err := testRunner.guardedCommand(context.Background(), nil, os.Args[0], "-test.run=^$")
		if err == nil {
			err = guard.finish(guard.start())
		}
		var rotationErr credentialRotationError
		if !errors.As(err, &rotationErr) || !testRunner.safety.isUnsafe() {
			t.Fatalf("read-before-watch error=%T unsafe=%t", err, testRunner.safety.isUnsafe())
		}
	})

	for _, watcherError := range []error{fsnotify.ErrEventOverflow, errors.New("synthetic watcher failure")} {
		t.Run(watcherError.Error(), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "credential")
			if err := os.WriteFile(source, []byte("mc-watch-error-2902f803"), 0o600); err != nil {
				t.Fatal(err)
			}
			watcher := newFakeCredentialEventWatcher()
			testRunner := &runner{
				credentialFiles: []string{source},
				credentialWatcherFactory: func() (credentialEventWatcher, error) {
					return watcher, nil
				},
			}
			t.Cleanup(testRunner.cleanupEphemeralRuntime)
			_, guard, err := testRunner.guardedCommand(context.Background(), nil, os.Args[0], "-test.run=^$")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(testRunner.rawArtifacts, "raw"), []byte("unsafe"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(testRunner.codexHome, "sessions"), 0o700); err != nil {
				t.Fatal(err)
			}
			watcher.errors <- watcherError
			deadline := time.Now().Add(time.Second)
			for !testRunner.safety.isUnsafe() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			var rotationErr credentialRotationError
			if err := guard.finish(nil); !errors.As(err, &rotationErr) {
				t.Fatalf("watcher error result = %T", err)
			}
			if _, err := os.Stat(testRunner.rawArtifacts); !os.IsNotExist(err) {
				t.Fatalf("watcher error не удалил raw staging: %v", err)
			}
			if _, err := os.Stat(filepath.Join(testRunner.codexHome, "sessions")); !os.IsNotExist(err) {
				t.Fatalf("watcher error не удалил session staging: %v", err)
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests++
				response.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			err = testRunner.sessionJSONOnce(context.Background(), server.Client(), http.MethodPost, server.URL, "session", "token", "turns/status", sessionTurnStatusRequest{RunID: "run", Phase: "safe"}, nil)
			if !errors.As(err, &rotationErr) || requests != 0 {
				t.Fatalf("watcher error network boundary: error=%T requests=%d", err, requests)
			}
		})
	}
}

func TestCredentialCorpusServerOwnedBoundaries(t *testing.T) {
	sources := map[string]int64{}
	for index := 0; index < maxCredentialSources; index++ {
		if err := addCredentialSourceBudget(sources, fmt.Sprintf("/synthetic/source-%04d", index), 0); err != nil {
			t.Fatalf("source boundary %d rejected: %v", index, err)
		}
	}
	if err := addCredentialSourceBudget(sources, "/synthetic/source-over", 0); err == nil {
		t.Fatal("source count above boundary accepted")
	}
	for _, size := range []int64{maxCredentialSourceBytes - 1, maxCredentialSourceBytes, maxCredentialSourceBytes + 1} {
		corpus := map[string]int64{}
		err := addCredentialSourceBudget(corpus, "/synthetic/aggregate", size)
		if size <= maxCredentialSourceBytes && err != nil || size > maxCredentialSourceBytes && err == nil {
			t.Fatalf("aggregate source bytes %d: %v", size, err)
		}
	}
	for _, count := range []int{maxCredentialRepresentations - 1, maxCredentialRepresentations, maxCredentialRepresentations + 1} {
		budget := credentialRepresentationBudget{}
		var err error
		for index := 0; index < count; index++ {
			err = budget.add("x")
			if err != nil {
				break
			}
		}
		if count <= maxCredentialRepresentations && err != nil || count > maxCredentialRepresentations && err == nil {
			t.Fatalf("representation count %d: %v", count, err)
		}
	}
	for _, size := range []int64{maxCredentialRepresentationBytes - 1, maxCredentialRepresentationBytes, maxCredentialRepresentationBytes + 1} {
		budget := credentialRepresentationBudget{}
		err := budget.addSize(size)
		if size <= maxCredentialRepresentationBytes && err != nil || size > maxCredentialRepresentationBytes && err == nil {
			t.Fatalf("representation bytes %d: %v", size, err)
		}
	}
}

func TestKubeconfigSourceCountAcceptsBelowBoundaryAndRejects513(t *testing.T) {
	for _, count := range []int{16, maxKubeconfigSources, maxKubeconfigSources + 1} {
		t.Run(fmt.Sprintf("%d", count), func(t *testing.T) {
			directory := t.TempDir()
			paths := make([]string, 0, count)
			for index := 0; index < count; index++ {
				path := filepath.Join(directory, fmt.Sprintf("kubeconfig-%04d", index))
				if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			testRunner := &runner{credentialFiles: []string{}}
			t.Cleanup(testRunner.cleanupEphemeralRuntime)
			_, guard, err := testRunner.guardedCommand(context.Background(), []string{"KUBECONFIG=" + strings.Join(paths, string(os.PathListSeparator))}, os.Args[0], "-test.run=^$")
			if guard != nil {
				guard.abort()
			}
			var limitErr credentialCorpusLimitError
			if count <= maxKubeconfigSources && err != nil {
				t.Fatalf("KUBECONFIG sources at or below boundary rejected: %v", err)
			}
			if count > maxKubeconfigSources && !errors.As(err, &limitErr) {
				t.Fatalf("513 KUBECONFIG sources error = %T", err)
			}
		})
	}
}

func useTestWorkspace(t *testing.T) {
	t.Helper()
	oldWorkspaceDir := workspaceDir
	oldRepoDir := repoDir
	oldArtifactsDir := artifactsDir
	workspaceDir = t.TempDir()
	repoDir = filepath.Join(workspaceDir, "repo")
	artifactsDir = filepath.Join(workspaceDir, "artifacts")
	t.Cleanup(func() {
		workspaceDir = oldWorkspaceDir
		repoDir = oldRepoDir
		artifactsDir = oldArtifactsDir
	})
}

func mustTable(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	table, ok := asStringAnyMap(parent[key])
	if !ok {
		t.Fatalf("%s is not a table: %#v", key, parent[key])
	}
	return table
}
