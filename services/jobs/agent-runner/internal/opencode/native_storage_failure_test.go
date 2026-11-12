package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestNativeCheckpointStorageFailures confines a file-size limit to a child test process and
// qualifies real stopped export failure, invalid import rejection and recovery of the original transcript.
// It neither fills a filesystem nor changes limits of the parent test runner or host processes.
// TestNativeCheckpointStorageFailures ограничивает размер файла только в дочернем тестовом процессе
// и проверяет настоящий stopped export при отказе записи, запрет неверного import и сохранность transcript.
// Тест не заполняет файловую систему и не меняет лимиты родительского runner либо процессов хоста.
func TestNativeCheckpointStorageFailures(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	if os.Getenv("KODEX_TEST_NATIVE_STORAGE_CHILD") == "1" {
		qualifyNativeStorageFailures(t, binary)
		return
	}
	var before, after syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_FSIZE, &before) != nil {
		t.Fatal("read parent file-size limit")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/proc/self/exe", "-test.run=^TestNativeCheckpointStorageFailures$", "-test.timeout=55s", "-test.v")
	command.Env = append(offlineEnvironment(t.TempDir()), "TMPDIR="+t.TempDir(),
		"KODEX_TEST_OPENCODE_BINARY="+binary, "KODEX_TEST_NATIVE_STORAGE_CHILD=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native storage failure qualification failed: %v\n%s", err, output)
	}
	if syscall.Getrlimit(syscall.RLIMIT_FSIZE, &after) != nil || before != after {
		t.Fatal("parent file-size limit changed")
	}
	t.Logf("isolated native storage qualification: %s", output)
}

// qualifyNativeStorageFailures uses an actual native user message without invoking any model,
// seals its official export, exercises closed failures, then restores the same messages in a new HOME.
// qualifyNativeStorageFailures сохраняет настоящее native user message без вызова модели,
// запечатывает официальный export, проверяет закрытые отказы и восстанавливает те же сообщения в новом HOME.
func qualifyNativeStorageFailures(t *testing.T, binary string) {
	t.Helper()
	server := startNativeServer(t, binary, t.TempDir())
	session, err := server.client.CreateSession(t.Context(), "Synthetic storage failure qualification")
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.client.SendPrompt(t.Context(), session.ID, Prompt{
		MessageID: "msg_61234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, NoReply: true,
		Parts: []TextPart{{Type: "text", Text: strings.Repeat("SYNTHETIC_ORIGINAL_TRANSCRIPT\n", 1024)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.stop(t)
	root := t.TempDir()
	sealed := filepath.Join(root, "sealed.json")
	checkpoint, err := server.server.Export(t.Context(), session.ID, sealed)
	if err != nil || checkpoint.Messages != 1 || checkpoint.SizeBytes <= 4096 {
		t.Fatal("real native export did not establish the storage fixture")
	}
	original, err := os.ReadFile(sealed)
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{sealed, filepath.Join(root, "missing-parent", "export.json")} {
		if result, err := server.server.Export(t.Context(), session.ID, destination); !errors.Is(err, ErrCheckpoint) || result != (Checkpoint{}) {
			t.Fatal("unavailable or occupied export destination became an accepted checkpoint")
		}
	}
	limited := filepath.Join(root, "limited.json")
	qualifyLimitedNativeExport(t, server.server, session.ID, limited)
	if _, err := os.Lstat(limited); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed export left a partial checkpoint")
	}
	if current, err := os.ReadFile(sealed); err != nil || !bytes.Equal(current, original) {
		t.Fatal("failed export changed the confirmed checkpoint")
	}
	verified := filepath.Join(root, "verified.json")
	after, err := server.server.Export(t.Context(), session.ID, verified)
	if err != nil || after.SHA256 != checkpoint.SHA256 {
		t.Fatal("failed export damaged native state or changed its original history")
	}
	for _, mode := range []string{"missing", "truncated", "foreign-session", "version", "digest"} {
		t.Run(mode, func(t *testing.T) {
			home := emptyNativeRestoreHome(t)
			source, expected := sealed, checkpoint.SHA256
			identity := session.ID
			switch mode {
			case "missing":
				source = filepath.Join(root, "missing.json")
			case "foreign-session":
				identity = "ses_foreign"
			case "digest":
				expected = strings.Repeat("0", 64)
			default:
				raw := bytes.TrimSpace(original)
				raw = raw[:len(raw)-1]
				if mode == "version" {
					var value map[string]any
					if json.Unmarshal(original, &value) != nil {
						t.Fatal("inspect synthetic official export")
					}
					value["info"].(map[string]any)["version"] = "unqualified"
					raw, err = json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
				}
				source = filepath.Join(t.TempDir(), "invalid.json")
				if os.WriteFile(source, raw, 0o600) != nil {
					t.Fatal("prepare invalid native checkpoint")
				}
				digest := sha256.Sum256(raw)
				expected = hex.EncodeToString(digest[:])
			}
			if !errors.Is(restore(t.Context(), binary, home, source, identity, expected), ErrCheckpoint) {
				t.Fatal("invalid native checkpoint was silently restored")
			}
			entries, err := os.ReadDir(filepath.Join(home, "data"))
			if err != nil || len(entries) != 0 {
				t.Fatal("invalid checkpoint initialized an empty replacement session")
			}
		})
	}
	home := emptyNativeRestoreHome(t)
	if err := restore(t.Context(), binary, home, sealed, session.ID, checkpoint.SHA256); err != nil {
		t.Fatal("confirmed native checkpoint did not recover after storage refusal")
	}
	if !errors.Is(restore(t.Context(), binary, home, sealed, session.ID, checkpoint.SHA256), ErrCheckpoint) {
		t.Fatal("initialized native HOME was silently merged")
	}
	recovered := startNativeServer(t, binary, home)
	messages, err := recovered.client.Messages(t.Context(), session.ID, 10, "")
	if err != nil || len(messages) != 1 {
		t.Fatal("original native messages disappeared after storage refusal")
	}
	recovered.stop(t)
	restoredPath := filepath.Join(root, "restored.json")
	if _, err := recovered.server.Export(t.Context(), session.ID, restoredPath); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(restoredPath)
	var originalRecord, restoredRecord map[string]any
	if err != nil || json.Unmarshal(original, &originalRecord) != nil || json.Unmarshal(restored, &restoredRecord) != nil ||
		!reflect.DeepEqual(originalRecord["messages"], restoredRecord["messages"]) {
		t.Fatal("official recovery changed original native message values")
	}
	t.Logf("native storage: exportBytes=%d limitBytes=4096 retainedMessages=1 invalidRestores=5 modelRequests=0", checkpoint.SizeBytes)
}

// qualifyLimitedNativeExport restores the child's previous soft limit even when an assertion fails;
// the real OpenCode CLI must return no accepted checkpoint under the enforced file-size budget.
// qualifyLimitedNativeExport возвращает прежний soft limit дочернего процесса даже при ошибке проверки;
// настоящий OpenCode CLI не должен выдавать принятый checkpoint при превышении размера файла.
func qualifyLimitedNativeExport(t *testing.T, server *Server, sessionID, destination string) {
	t.Helper()
	var previous syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous) != nil || previous.Cur <= 4096 {
		t.Fatal("file-size limit fixture is unavailable")
	}
	limited := previous
	limited.Cur = 4096
	if syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited) != nil {
		t.Fatal("set child file-size limit")
	}
	defer func() {
		if syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous) != nil {
			t.Error("restore child file-size limit")
		}
	}()
	if result, err := server.Export(t.Context(), sessionID, destination); !errors.Is(err, ErrCheckpoint) || result != (Checkpoint{}) {
		t.Fatal("limited official export became an accepted checkpoint")
	}
}

// emptyNativeRestoreHome creates private empty XDG roots for the production import guard.
// emptyNativeRestoreHome создаёт приватные пустые XDG roots для production import guard.
func emptyNativeRestoreHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if os.Chmod(home, 0o700) != nil {
		t.Fatal("prepare native HOME")
	}
	for _, name := range []string{"config", "cache", "data", "state"} {
		if os.Mkdir(filepath.Join(home, name), 0o700) != nil {
			t.Fatal("prepare native XDG root")
		}
	}
	return home
}
