package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func mustTable(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	table, ok := asStringAnyMap(parent[key])
	if !ok {
		t.Fatalf("%s is not a table: %#v", key, parent[key])
	}
	return table
}
