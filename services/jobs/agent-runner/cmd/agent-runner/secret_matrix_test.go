package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type syntheticSecretFixture struct {
	class   string
	envName string
	value   string
}

func syntheticSecretMatrix() []syntheticSecretFixture {
	return []syntheticSecretFixture{
		{class: "OpenAI", envName: "OPENAI_API_KEY", value: "mc-sentinel-openai-7fd8a091"},
		{class: "GitHub", envName: "GH_TOKEN", value: "mc-sentinel-github-3af48d22"},
		{class: "Mattermost", envName: "MATTERCODEX_MATTERMOST_BOT_TOKEN", value: "mc-sentinel-mattermost-a4d10c77"},
		{class: "Kubernetes", envName: "KUBERNETES_BEARER_TOKEN", value: "mc-sentinel-kubernetes-cb2e8159"},
		{class: "PostgreSQL DSN", envName: "MATTERCODEX_DATABASE_DSN", value: "postgres://mc-sentinel-postgres-451aae90@127.0.0.1/disposable"},
		{class: "session token", envName: "MATTERCODEX_SESSION_TOKEN", value: "mc-sentinel-session-293efb61"},
		{class: "MCP token", envName: "MATTERCODEX_MCP_TOKEN", value: "mc-sentinel-mcp-f2ac340b"},
	}
}

func TestSyntheticSecretMatrixIsRedactedFromRunnerChannels(t *testing.T) {
	fixtures := syntheticSecretMatrix()
	for _, fixture := range fixtures {
		t.Setenv(fixture.envName, fixture.value)
	}
	t.Setenv("GITHUB_TOKEN", fixtures[1].value)
	t.Setenv("MATTERCODEX_GITHUB_TOKEN", fixtures[1].value)
	t.Setenv("MATTERCODEX_GITHUB_WEBHOOK_SECRET", fixtures[1].value)
	t.Setenv("MATTERCODEX_MATTERMOST_ADMIN_TOKEN", fixtures[2].value)
	t.Setenv("MATTERCODEX_MATTERMOST_SLASH_TOKEN", fixtures[2].value)
	t.Setenv("MATTERCODEX_MIGRATIONS_DATABASE_DSN", fixtures[4].value)
	t.Setenv("MATTERCODEX_RUNTIME_ENV_ALLOWLIST", "SYNTHETIC_RUNTIME_SECRET")
	t.Setenv("SYNTHETIC_RUNTIME_SECRET", "mc-sentinel-runtime-94fcbb2a")
	t.Setenv("MATTERCODEX_MCP_URL", "http://matter-codex.invalid/mcp/sessions/synthetic")
	t.Setenv("MATTERCODEX_CODEX_CONFIG_OVERLAY", "")

	redactor := newSecretRedactor(os.Environ())
	raw := secretFixtureAssignments(fixtures) + "\nSYNTHETIC_RUNTIME_SECRET=mc-sentinel-runtime-94fcbb2a"
	channels := map[string]string{
		"prompt":              redactor.Redact("Выполни диагностику.\n" + raw),
		"structured log":      redactor.Redact(`{"level":"error","message":` + string(mustJSON(t, raw)) + `}`),
		"error":               redactor.Redact("runtime failure: " + raw),
		"final":               redactor.Redact("final result: " + raw),
		"status":              redactor.Redact(string(mustJSON(t, sessionTurnStatusRequest{RunID: "synthetic", Phase: raw}))),
		"Mattermost payload":  redactor.Redact(string(mustJSON(t, map[string]any{"message": raw, "props": map[string]any{"matter_codex_event": "synthetic"}}))),
		"audit":               redactor.Redact(string(mustJSON(t, map[string]string{"event_type": "synthetic", "summary": raw}))),
		"artifact metadata":   redactor.Redact(string(mustJSON(t, map[string]string{"diagnostic": raw}))),
		"completion response": redactor.Redact(string(mustJSON(t, sessionTurnCompleteRequest{Status: "failed", FinalMessage: raw, ErrorMessage: raw}))),
		"encoded values":      redactor.Redact(encodedSecretFixtures(fixtures)),
	}

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := writeCodexConfig(configPath); err != nil {
		t.Fatalf("writeCodexConfig() error = %v", err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("чтение синтетической конфигурации: %v", err)
	}
	channels["Codex config"] = string(config)

	archiveRoot := t.TempDir()
	sessionDirectory := filepath.Join(archiveRoot, "sessions", "synthetic")
	if err := os.MkdirAll(sessionDirectory, 0o700); err != nil {
		t.Fatalf("создание synthetic session archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDirectory, "rollout.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatalf("запись synthetic session archive: %v", err)
	}
	archive, err := createCodexSessionArchive(archiveRoot)
	if err != nil {
		t.Fatalf("createCodexSessionArchive() error = %v", err)
	}
	restoredRoot := t.TempDir()
	if err := restoreCodexSessionArchive(archive, restoredRoot); err != nil {
		t.Fatalf("restoreCodexSessionArchive() error = %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(restoredRoot, "sessions", "synthetic", "rollout.jsonl"))
	if err != nil {
		t.Fatalf("чтение восстановленного synthetic archive: %v", err)
	}
	channels["session archive"] = string(restored)

	for channel, body := range channels {
		assertSyntheticSecretsAbsent(t, channel, body, fixtures)
		if strings.Contains(body, "mc-sentinel-runtime-94fcbb2a") {
			t.Fatalf("канал %s содержит значение runtime secret", channel)
		}
	}
}

func secretFixtureAssignments(fixtures []syntheticSecretFixture) string {
	lines := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		lines = append(lines, fixture.envName+"="+fixture.value)
	}
	return strings.Join(lines, "\n")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return body
}

func encodedSecretFixtures(fixtures []syntheticSecretFixture) string {
	values := make([]string, 0, len(fixtures)*3)
	for _, fixture := range fixtures {
		encoded, _ := json.Marshal(fixture.value)
		values = append(values,
			string(encoded[1:len(encoded)-1]),
			base64.StdEncoding.EncodeToString([]byte(fixture.value)),
			base64.RawStdEncoding.EncodeToString([]byte(fixture.value)),
		)
	}
	return strings.Join(values, "\n")
}

func assertSyntheticSecretsAbsent(t *testing.T, channel string, body string, fixtures []syntheticSecretFixture) {
	t.Helper()
	for _, fixture := range fixtures {
		encoded, _ := json.Marshal(fixture.value)
		representations := []string{
			fixture.value,
			string(encoded[1 : len(encoded)-1]),
			base64.StdEncoding.EncodeToString([]byte(fixture.value)),
			base64.RawStdEncoding.EncodeToString([]byte(fixture.value)),
		}
		for _, representation := range representations {
			if strings.Contains(body, representation) {
				t.Fatalf("канал %s содержит значение класса %s", channel, fixture.class)
			}
		}
	}
}
