package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestRunScopedSecretInventoryIncludesFileOnlyAndExtraEnvSources(t *testing.T) {
	directory := t.TempDir()
	fileOnly := map[string]string{
		"GitHub file":      "mc-file-github-7d2a5e901",
		"OpenAI auth.json": "mc-file-openai-9b71c4e02",
		"Kubernetes file":  "mc-file-kubernetes-f6a81d403",
	}
	githubPath := filepath.Join(directory, "github-token")
	authPath := filepath.Join(directory, "auth.json")
	kubernetesPath := filepath.Join(directory, "service-account-token")
	if err := os.WriteFile(githubPath, []byte(fileOnly["GitHub file"]), 0o600); err != nil {
		t.Fatal(err)
	}
	authBody, _ := json.Marshal(map[string]any{"tokens": map[string]string{"access_token": fileOnly["OpenAI auth.json"]}})
	if err := os.WriteFile(authPath, authBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubernetesPath, []byte(fileOnly["Kubernetes file"]), 0o600); err != nil {
		t.Fatal(err)
	}
	extraValue := "mc-extraenv-mcp-2e148cc04"
	inventory, err := buildSecretInventory(
		[]string{"MATTERCODEX_MCP_TOKEN=" + extraValue},
		[]string{githubPath, authPath, kubernetesPath},
	)
	if err != nil {
		t.Fatalf("buildSecretInventory() error = %v", err)
	}
	for source, value := range fileOnly {
		for _, representation := range []string{value, base64.RawURLEncoding.EncodeToString([]byte(value)), hex.EncodeToString([]byte(value))} {
			protected, protectErr := inventory.protect("provider failure: " + representation)
			if protectErr != nil || strings.Contains(protected, representation) {
				t.Fatalf("file-only источник %s не защищён", source)
			}
		}
	}
	for _, representation := range []string{extraValue, base64.RawURLEncoding.EncodeToString([]byte(extraValue)), hex.EncodeToString([]byte(extraValue))} {
		protected, protectErr := inventory.protect("provider failure: " + representation)
		if protectErr != nil || strings.Contains(protected, representation) {
			t.Fatal("extraEnv источник не защищён")
		}
	}
}

func TestSecretInventoryIndependentEncodingCorpusAndFragments(t *testing.T) {
	const secret = "mc-independent-encoding-8a2f6107"
	inventory, err := buildSecretInventory([]string{"OPENAI_API_KEY=" + secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded := []string{
		url.QueryEscape(secret),
		base64.RawURLEncoding.EncodeToString([]byte(secret)),
		hex.EncodeToString([]byte(secret)),
	}
	for _, representation := range encoded {
		protected, protectErr := inventory.protect("ошибка поставщика: " + representation)
		if protectErr != nil || strings.Contains(protected, representation) {
			t.Fatal("независимое encoded-представление не защищено")
		}
	}
	middle := len(secret) / 2
	fragmented := secret[:middle] + "<разрыв>" + secret[middle:]
	if _, err := inventory.protect(fragmented); err == nil {
		t.Fatal("fragment-представление не завершилось fail-closed")
	} else {
		var fragmentErr unsafeSecretFragmentError
		if !errors.As(err, &fragmentErr) {
			t.Fatalf("fragment error = %T, want unsafeSecretFragmentError", err)
		}
	}
}

func TestSessionArchiveSanitizesSourceAndRejectsLimitsBeforeAllocation(t *testing.T) {
	const secret = "mc-session-source-6a9df183"
	inventory, err := buildSecretInventory([]string{"MATTERCODEX_SESSION_TOKEN=" + secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions", "run")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sessionDir, "rollout.jsonl")
	if err := os.WriteFile(source, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := createCodexSessionArchive(root, inventory); err != nil {
		t.Fatalf("createCodexSessionArchive() error = %v", err)
	}
	sanitized, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sanitized), secret) || !strings.Contains(string(sanitized), redactedSecretValue) {
		t.Fatal("исходный rollout не был атомарно санитизирован")
	}

	oversizeRoot := t.TempDir()
	oversizeDir := filepath.Join(oversizeRoot, "sessions", "run")
	if err := os.MkdirAll(oversizeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oversizePath := filepath.Join(oversizeDir, "rollout.jsonl")
	file, err := os.OpenFile(oversizePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxSessionArchiveFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	_, err = createCodexSessionArchive(oversizeRoot, inventory)
	var limitErr sessionArchiveLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("oversize error = %v, want sessionArchiveLimitError", err)
	}
	if _, statErr := os.Stat(filepath.Join(oversizeRoot, "sessions")); !os.IsNotExist(statErr) {
		t.Fatalf("oversize source tree не удалён: %v", statErr)
	}
}

func TestSessionArchiveAcceptsExactPerFileBoundary(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions", "run")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	boundary := bytes.Repeat([]byte("x"), int(maxSessionArchiveFileBytes))
	if err := os.WriteFile(filepath.Join(sessionDir, "rollout.jsonl"), boundary, 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := createCodexSessionArchive(root, secretInventory{})
	if err != nil {
		t.Fatalf("exact boundary archive error = %v", err)
	}
	if strings.TrimSpace(archive) == "" {
		t.Fatal("exact boundary archive пуст")
	}
}

func TestSessionArchiveRejectsFileCountAndTotalLimitsBeforeUnboundedRead(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		root := t.TempDir()
		sessionDir := filepath.Join(root, "sessions", "run")
		if err := os.MkdirAll(sessionDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for index := 0; index <= maxSessionArchiveFiles; index++ {
			path := filepath.Join(sessionDir, fmt.Sprintf("event-%04d.jsonl", index))
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, err := createCodexSessionArchive(root, secretInventory{})
		var limitErr sessionArchiveLimitError
		if !errors.As(err, &limitErr) || limitErr.Limit != "количества файлов" {
			t.Fatalf("file count error = %v", err)
		}
	})

	t.Run("total bytes", func(t *testing.T) {
		root := t.TempDir()
		sessionDir := filepath.Join(root, "sessions", "run")
		if err := os.MkdirAll(sessionDir, 0o700); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < 5; index++ {
			path := filepath.Join(sessionDir, fmt.Sprintf("event-%02d.jsonl", index))
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(maxSessionArchiveFileBytes); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			_ = file.Close()
		}
		_, err := createCodexSessionArchive(root, secretInventory{})
		var limitErr sessionArchiveLimitError
		if !errors.As(err, &limitErr) || limitErr.Limit != "общего размера" {
			t.Fatalf("total size error = %v", err)
		}
	})
}

func TestRestoreSessionArchiveRejectsOversizeHeader(t *testing.T) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "sessions/run/rollout.jsonl", Mode: 0o600, Size: maxSessionArchiveFileBytes + 1, Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	err := restoreCodexSessionArchive(base64.StdEncoding.EncodeToString(compressed.Bytes()), t.TempDir())
	var limitErr sessionArchiveLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("restore oversize error = %v, want sessionArchiveLimitError", err)
	}
}

func TestSessionTransportProtectsRawJSONAndBase64ValuesBeforeBotService(t *testing.T) {
	fixtures := syntheticSecretMatrix()
	environment := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		environment = append(environment, fixture.envName+"="+fixture.value)
	}
	inventory, err := buildSecretInventory(environment, nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, 0, len(fixtures)*3)
	for _, fixture := range fixtures {
		jsonValue, _ := json.Marshal(fixture.value)
		parts = append(parts,
			fixture.value,
			string(jsonValue[1:len(jsonValue)-1]),
			base64.StdEncoding.EncodeToString([]byte(fixture.value)),
		)
	}
	rawFailure := "provider failure: " + strings.Join(parts, " | ")
	received := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		received <- string(body)
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runner := &runner{secrets: inventory}
	client := server.Client()
	if err := runner.sessionJSONOnce(context.Background(), client, http.MethodPost, server.URL, "session", "token", "turns/status", sessionTurnStatusRequest{
		RunID: "run", Phase: rawFailure,
	}, nil); err != nil {
		t.Fatalf("status transport error = %v", err)
	}
	if err := runner.sessionJSONOnce(context.Background(), client, http.MethodPost, server.URL, "session", "token", "turns/complete", sessionTurnCompleteRequest{
		TurnID: 1, RunID: "run", Status: "failed", ErrorMessage: rawFailure, FinalMessage: rawFailure,
		Artifacts: map[string]string{"diagnostic": rawFailure},
	}, nil); err != nil {
		t.Fatalf("completion transport error = %v", err)
	}
	for index := 0; index < 2; index++ {
		body := <-received
		assertSyntheticSecretsAbsent(t, "session transport", body, fixtures)
	}
}

func TestSessionTransportFailsClosedBeforeNetworkOnSecretFragments(t *testing.T) {
	const secret = "mc-fragment-transport-8a10f7b2"
	inventory, err := buildSecretInventory([]string{"OPENAI_API_KEY=" + secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	middle := len(secret) / 2
	fragmented := secret[:middle] + "<split>" + secret[middle:]
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runner := &runner{secrets: inventory}
	err = runner.sessionJSONOnce(context.Background(), server.Client(), http.MethodPost, server.URL, "session", "token", "turns/status", sessionTurnStatusRequest{
		RunID: "run", Phase: fragmented,
	}, nil)
	var fragmentErr unsafeSecretFragmentError
	if !errors.As(err, &fragmentErr) || requests != 0 {
		t.Fatalf("fragment transport error=%v requests=%d", err, requests)
	}
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
	archive, err := createCodexSessionArchive(archiveRoot, redactor.inventory)
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
