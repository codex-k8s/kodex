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
	"os"
	"os/exec"
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
			if protectErr == nil && strings.Contains(protected, representation) {
				t.Fatalf("file-only источник %s не защищён", source)
			}
		}
	}
	for _, representation := range []string{extraValue, base64.RawURLEncoding.EncodeToString([]byte(extraValue)), hex.EncodeToString([]byte(extraValue))} {
		protected, protectErr := inventory.protect("provider failure: " + representation)
		if protectErr == nil && strings.Contains(protected, representation) {
			t.Fatal("extraEnv источник не защищён")
		}
	}
}

func TestKubeconfigInventoryIgnoresShortSchemaFieldsButKeepsCredentialValues(t *testing.T) {
	const credential = "mc-kubeconfig-token-a53c90f1"
	path := filepath.Join(t.TempDir(), "kubeconfig")
	body := "apiVersion: v1\nkind: Config\nusers:\n- name: synthetic\n  user:\n    token: " + credential + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := buildSecretInventory([]string{"KUBECONFIG=" + path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inventory.validateForExecution(); err != nil {
		t.Fatalf("короткие несекретные поля kubeconfig отклонили inventory: %T", err)
	}
	protected, err := inventory.protect("provider failure: " + credential)
	if err != nil || strings.Contains(protected, credential) {
		t.Fatal("credential из kubeconfig не защищён")
	}
}

func TestShortSensitiveCredentialsFailClosedForEnvFileAndExtraEnvLengthsOneThroughSeven(t *testing.T) {
	for length := 1; length <= 7; length++ {
		for _, source := range []string{"env", "file", "extraEnv"} {
			t.Run(fmt.Sprintf("%s/%d", source, length), func(t *testing.T) {
				value := strings.Repeat("x", length)
				environment := []string{}
				explicitFiles := []string{}
				runnerEnvironment := []string{}
				testRunner := &runner{}
				switch source {
				case "env":
					environment = []string{"OPENAI_API_KEY=" + value}
					t.Setenv("OPENAI_API_KEY", value)
				case "file":
					path := filepath.Join(t.TempDir(), "credential")
					if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
						t.Fatal(err)
					}
					explicitFiles = []string{path}
					testRunner.credentialFiles = []string{path}
				case "extraEnv":
					environment = []string{"MATTERCODEX_MCP_TOKEN=" + value}
					runnerEnvironment = environment
				}
				inventory, err := buildSecretInventory(environment, explicitFiles)
				if err != nil {
					t.Fatal(err)
				}
				var shortErr unsupportedShortCredentialError
				if _, err := inventory.protect("безопасный диагностический текст"); !errors.As(err, &shortErr) {
					t.Fatalf("inventory не сохранил short credential: %T", err)
				}
				commandInvoked := false
				testRunner.commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
					commandInvoked = true
					return exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
				}
				t.Cleanup(testRunner.cleanupEphemeralRuntime)
				_, _, runErr := testRunner.runCodexSessionTurn(context.Background(), sessionTurnClaimResponse{
					TurnID: int64(770000 + length), Prompt: "synthetic short credential boundary",
				}, "", "short-final.md", t.TempDir(), runnerEnvironment, 0)
				if !errors.As(runErr, &shortErr) || commandInvoked {
					t.Fatalf("production boundary: error=%T command_invoked=%t", runErr, commandInvoked)
				}
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
					requests++
					response.WriteHeader(http.StatusNoContent)
				}))
				defer server.Close()
				transportRunner := &runner{secrets: inventory}
				err = transportRunner.sessionJSONOnce(context.Background(), server.Client(), http.MethodPost, server.URL, "session", "token", "turns/status", sessionTurnStatusRequest{RunID: "run", Phase: "safe"}, nil)
				if !errors.As(err, &shortErr) || requests != 0 {
					t.Fatalf("network boundary: error=%T requests=%d", err, requests)
				}
			})
		}
	}
}

func TestShortNonCredentialRuntimeEnvironmentValueIsAllowedAndRedacted(t *testing.T) {
	inventory, err := buildSecretInventory([]string{
		"MATTERCODEX_RUNTIME_ENV_ALLOWLIST=STAGING_SERVER_ROOT_USER",
		"STAGING_SERVER_ROOT_USER=root",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inventory.validateForExecution(); err != nil {
		t.Fatalf("короткий runtime username отклонил запуск: %T", err)
	}
	protected, err := inventory.protect("runtime user=root")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(protected, "root") {
		t.Fatalf("короткое runtime value не скрыто: %q", protected)
	}
}

func TestShortCredentialLikeRuntimeEnvironmentValueStillFailsClosed(t *testing.T) {
	inventory, err := buildSecretInventory([]string{
		"MATTERCODEX_RUNTIME_ENV_ALLOWLIST=STAGING_SERVER_ROOT_PASSWORD",
		"STAGING_SERVER_ROOT_PASSWORD=short",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var shortErr unsupportedShortCredentialError
	if err := inventory.validateForExecution(); !errors.As(err, &shortErr) {
		t.Fatalf("короткий runtime password не отклонён: %T", err)
	}
}

func TestSecretInventoryIndependentEncodingCorpusAndFragments(t *testing.T) {
	const secret = "mc/independent%encoding-8a2f6107"
	inventory, err := buildSecretInventory([]string{"OPENAI_API_KEY=" + secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded := []string{
		percentEncodeForTest(secret, true),
		percentEncodeForTest(secret, false),
		base64.RawURLEncoding.EncodeToString([]byte(secret)),
		hex.EncodeToString([]byte(secret)),
		strings.ToUpper(hex.EncodeToString([]byte(secret))),
	}
	for _, representation := range encoded {
		protected, protectErr := inventory.protect("ошибка поставщика: " + representation)
		if protectErr == nil && strings.Contains(protected, representation) {
			t.Fatal("независимое encoded-представление не защищено")
		}
	}
	for fragmentLength := 1; fragmentLength <= 7; fragmentLength++ {
		parts := make([]string, 0, len(secret)/fragmentLength+1)
		for offset := 0; offset < len(secret); offset += fragmentLength {
			end := min(offset+fragmentLength, len(secret))
			parts = append(parts, secret[offset:end])
		}
		fragmented := strings.Join(parts, "<split>")
		if _, err := inventory.protect(fragmented); err == nil {
			t.Fatalf("fragment length %d не завершился fail-closed", fragmentLength)
		} else {
			var fragmentErr unsafeSecretFragmentError
			if !errors.As(err, &fragmentErr) {
				t.Fatalf("fragment length %d error = %T", fragmentLength, err)
			}
		}
	}
}

func percentEncodeForTest(value string, uppercase bool) string {
	const upperDigits = "0123456789ABCDEF"
	const lowerDigits = "0123456789abcdef"
	digits := upperDigits
	if !uppercase {
		digits = lowerDigits
	}
	var encoded strings.Builder
	encoded.Grow(len(value) * 3)
	for _, item := range []byte(value) {
		encoded.WriteByte('%')
		encoded.WriteByte(digits[item>>4])
		encoded.WriteByte(digits[item&0x0f])
	}
	return encoded.String()
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
	for _, name := range []string{"sessions", "sessions/run"} {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "sessions/run/rollout.jsonl", Mode: 0o600, Size: maxSessionArchiveFileBytes + 1, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR,
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

func TestRestoreSessionArchiveRejectsExtendedHeadersOutsideCountedUSTARContract(t *testing.T) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:       "sessions/run/" + strings.Repeat("long-name-", 30),
		Mode:       0o600,
		Typeflag:   tar.TypeReg,
		Format:     tar.FormatPAX,
		PAXRecords: map[string]string{"comment": "unsupported extended header"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = tarWriter.Close()
	_ = gzipWriter.Close()
	err := restoreCodexSessionArchive(base64.StdEncoding.EncodeToString(compressed.Bytes()), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "формат записи") {
		t.Fatalf("extended header error = %v", err)
	}
}

func TestRestoreSessionArchiveRequiresDirectoryRootAndLeavesExistingTargetAtomic(t *testing.T) {
	t.Run("legacy setgid permissions are normalized", func(t *testing.T) {
		archive := encodeUSTARForTest(t, []testTarEntry{
			{header: tar.Header{Name: "sessions", Mode: 0o2755, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
			{header: tar.Header{Name: "sessions/run", Mode: 0o2755, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
			{header: tar.Header{Name: "sessions/run/rollout.jsonl", Mode: 0o644, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}, body: "legacy"},
		})
		root := t.TempDir()
		if err := restoreCodexSessionArchive(archive, root); err != nil {
			t.Fatalf("legacy archive restore error = %v", err)
		}
		for path, wantMode := range map[string]os.FileMode{
			filepath.Join(root, "sessions"):                         0o700,
			filepath.Join(root, "sessions", "run"):                  0o700,
			filepath.Join(root, "sessions", "run", "rollout.jsonl"): 0o600,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != wantMode {
				t.Fatalf("mode %s = %o, want %o", filepath.Base(path), info.Mode().Perm(), wantMode)
			}
		}
	})

	t.Run("regular-file root", func(t *testing.T) {
		archive := encodeUSTARForTest(t, []testTarEntry{{header: tar.Header{Name: "sessions", Mode: 0o600, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}}})
		root := t.TempDir()
		if err := restoreCodexSessionArchive(archive, root); err == nil || !strings.Contains(err.Error(), "directory root") {
			t.Fatalf("regular-file root error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "sessions")); !os.IsNotExist(err) {
			t.Fatalf("regular-file root изменил target: %v", err)
		}
	})

	t.Run("valid file then symlink", func(t *testing.T) {
		archive := encodeUSTARForTest(t, []testTarEntry{
			{header: tar.Header{Name: "sessions", Mode: 0o700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
			{header: tar.Header{Name: "sessions/run", Mode: 0o700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
			{header: tar.Header{Name: "sessions/run/rollout.jsonl", Mode: 0o600, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}, body: "accepted-before-error"},
			{header: tar.Header{Name: "sessions/run/z-link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "rollout.jsonl", Format: tar.FormatUSTAR}},
		})
		root := t.TempDir()
		target := filepath.Join(root, "sessions")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		existing := filepath.Join(target, "existing")
		if err := os.WriteFile(existing, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := restoreCodexSessionArchive(archive, root); err == nil {
			t.Fatal("symlink archive accepted")
		}
		body, err := os.ReadFile(existing)
		if err != nil || string(body) != "unchanged" {
			t.Fatalf("existing target изменён: body=%q error=%v", string(body), err)
		}
		if _, err := os.Stat(filepath.Join(target, "run", "rollout.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("partial target опубликован: %v", err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || entries[0].Name() != "sessions" {
			t.Fatalf("private staging не очищен: entries=%v error=%v", entries, err)
		}
	})

	t.Run("duplicate entry", func(t *testing.T) {
		archive := encodeUSTARForTest(t, []testTarEntry{
			{header: tar.Header{Name: "sessions", Mode: 0o700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
			{header: tar.Header{Name: "sessions/run", Mode: 0o700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
			{header: tar.Header{Name: "sessions/run", Mode: 0o700, Typeflag: tar.TypeDir, Format: tar.FormatUSTAR}},
		})
		if err := restoreCodexSessionArchive(archive, t.TempDir()); err == nil || !strings.Contains(err.Error(), "дубликат") {
			t.Fatalf("duplicate error = %v", err)
		}
	})

	t.Run("successful atomic replacement round-trip", func(t *testing.T) {
		sourceRoot := t.TempDir()
		if err := os.MkdirAll(filepath.Join(sourceRoot, "sessions", "run"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceRoot, "sessions", "run", "rollout.jsonl"), []byte("round-trip"), 0o600); err != nil {
			t.Fatal(err)
		}
		archive, err := createCodexSessionArchive(sourceRoot, secretInventory{})
		if err != nil {
			t.Fatal(err)
		}
		targetRoot := t.TempDir()
		if err := os.Mkdir(filepath.Join(targetRoot, "sessions"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetRoot, "sessions", "old"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := restoreCodexSessionArchive(archive, targetRoot); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(targetRoot, "sessions", "run", "rollout.jsonl"))
		if err != nil || string(body) != "round-trip" {
			t.Fatalf("round-trip body=%q error=%v", string(body), err)
		}
		if _, err := os.Stat(filepath.Join(targetRoot, "sessions", "old")); !os.IsNotExist(err) {
			t.Fatalf("old target остался после atomic exchange: %v", err)
		}
	})
}

type testTarEntry struct {
	header tar.Header
	body   string
}

func encodeUSTARForTest(t *testing.T, entries []testTarEntry) string {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := entry.header
		header.Size = int64(len(entry.body))
		if err := tarWriter.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := io.WriteString(tarWriter, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(compressed.Bytes())
}

func TestSessionArchiveDirectoryEntryBoundaryRoundTrip(t *testing.T) {
	t.Run("1024 entries with empty directories", func(t *testing.T) {
		root := t.TempDir()
		sessions := filepath.Join(root, "sessions")
		if err := os.MkdirAll(sessions, 0o700); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < maxSessionArchiveEntries-1; index++ {
			if err := os.Mkdir(filepath.Join(sessions, fmt.Sprintf("empty-%04d", index)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		archive, err := createCodexSessionArchive(root, secretInventory{})
		if err != nil {
			t.Fatalf("create exact entry boundary: %v", err)
		}
		restored := t.TempDir()
		if err := restoreCodexSessionArchive(archive, restored); err != nil {
			t.Fatalf("restore exact entry boundary: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(restored, "sessions"))
		if err != nil || len(entries) != maxSessionArchiveEntries-1 {
			t.Fatalf("restored empty directories=%d error=%v", len(entries), err)
		}
	})

	t.Run("1024 empty subdirectories exceed root-inclusive limit", func(t *testing.T) {
		root := t.TempDir()
		sessions := filepath.Join(root, "sessions")
		if err := os.MkdirAll(sessions, 0o700); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < maxSessionArchiveEntries; index++ {
			if err := os.Mkdir(filepath.Join(sessions, fmt.Sprintf("empty-%04d", index)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		_, err := createCodexSessionArchive(root, secretInventory{})
		var limitErr sessionArchiveLimitError
		if !errors.As(err, &limitErr) || limitErr.Limit != "количества записей" {
			t.Fatalf("over-bound entry error = %v", err)
		}
		if _, statErr := os.Stat(sessions); !os.IsNotExist(statErr) {
			t.Fatalf("unsafe source не удалён: %v", statErr)
		}
	})

	t.Run("mixed directories and files at exact boundary", func(t *testing.T) {
		root := t.TempDir()
		sessions := filepath.Join(root, "sessions")
		if err := os.MkdirAll(sessions, 0o700); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < maxSessionArchiveEntries-maxSessionArchiveFiles-1; index++ {
			if err := os.Mkdir(filepath.Join(sessions, fmt.Sprintf("directory-%04d", index)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		for index := 0; index < maxSessionArchiveFiles; index++ {
			if err := os.WriteFile(filepath.Join(sessions, fmt.Sprintf("file-%04d", index)), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		archive, err := createCodexSessionArchive(root, secretInventory{})
		if err != nil {
			t.Fatalf("create mixed boundary: %v", err)
		}
		restored := t.TempDir()
		if err := restoreCodexSessionArchive(archive, restored); err != nil {
			t.Fatalf("restore mixed boundary: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(restored, "sessions"))
		if err != nil || len(entries) != maxSessionArchiveEntries-1 {
			t.Fatalf("restored mixed entries=%d error=%v", len(entries), err)
		}
	})
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

func TestSessionTransportFailsClosedOnNormalizedAndMultiFragmentCorpus(t *testing.T) {
	const secret = "mc/transport%encoding-4197be20"
	inventory, err := buildSecretInventory([]string{"OPENAI_API_KEY=" + secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	representations := []string{
		hex.EncodeToString([]byte(secret)),
		strings.ToUpper(hex.EncodeToString([]byte(secret))),
		percentEncodeForTest(secret, true),
		percentEncodeForTest(secret, false),
	}
	for fragmentLength := 1; fragmentLength <= 7; fragmentLength++ {
		parts := make([]string, 0, len(secret)/fragmentLength+1)
		for offset := 0; offset < len(secret); offset += fragmentLength {
			parts = append(parts, secret[offset:min(offset+fragmentLength, len(secret))])
		}
		representations = append(representations, strings.Join(parts, "<split>"))
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	for index, representation := range representations {
		runner := &runner{secrets: inventory}
		err := runner.sessionJSONOnce(context.Background(), server.Client(), http.MethodPost, server.URL, "session", "token", "turns/complete", sessionTurnCompleteRequest{
			TurnID: int64(index + 1), RunID: "run", Status: "failed", ErrorMessage: representation,
		}, nil)
		var fragmentErr unsafeSecretFragmentError
		if !errors.As(err, &fragmentErr) {
			t.Fatalf("corpus item %d не завершился fail-closed: %T", index, err)
		}
	}
	if requests != 0 {
		t.Fatalf("normalized/fragment corpus пересёк network boundary: %d", requests)
	}

	runner := &runner{secrets: inventory}
	source := filepath.Join(t.TempDir(), "raw-staging")
	destination := filepath.Join(t.TempDir(), "published")
	if err := os.WriteFile(source, []byte(representations[2]), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runner.publishSanitizedFile(source, destination); err == nil {
		t.Fatal("unsafe normalized staging неожиданно опубликован")
	}
	for _, path := range []string{source, destination} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unsafe staging path не удалён: %s", filepath.Base(path))
		}
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
