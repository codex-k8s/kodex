package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// Этот процесс запускается отдельной archive kernel-fixture с UID native writer.
// Он не обращается к Codex, Kubernetes либо постоянным каталогам платформы.
func TestRestoredRolloutCaptureFixture(t *testing.T) {
	mode := os.Getenv("KODEX_TEST_RESTORE_MODE")
	if mode == "" {
		return
	}
	workspace := os.Getenv("KODEX_TEST_RESTORE_WORKSPACE")
	if (mode != "WRONG_RESTORE_OWNER" && mode != "NATIVE_WRITER_RESTORE_OWNER") || os.Geteuid() != 10002 ||
		filepath.Dir(workspace) != os.TempDir() || !strings.HasPrefix(filepath.Base(workspace), "kodex-session-restore-") {
		t.Fatal("isolated runner fixture binding is invalid")
	}
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000001.jsonl"
	path := filepath.Join(workspace, relative)
	input := model.Input{WorkspaceRoot: workspace, CodexHome: filepath.Join(workspace, ".kodex/state/codex-home")}
	if mode == "WRONG_RESTORE_OWNER" {
		if writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0); err == nil {
			writer.Close()
			t.Fatal("native writer received append access to a foreign-owned restored source")
		} else if !errors.Is(err, syscall.EACCES) {
			t.Fatal("foreign-owned restored source failed at an unexpected append boundary")
		}
		_, _, _, _, err := captureRollout(input, path)
		if err == nil || err.Error() != "verify Codex app-server rollout" {
			t.Fatal("foreign-owned restore did not fail at the canonical capture permission boundary")
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal("shared group cannot read the restored source")
		}
		defer file.Close()
		if !errors.Is(file.Chown(-1, 29000), syscall.EPERM) || !errors.Is(file.Chmod(0o640), syscall.EPERM) {
			t.Fatal("foreign-owned restore did not reproduce kernel EPERM without capabilities")
		}
		return
	}
	// Моделируем новый durable history frame от фактического app-server UID,
	// а не только возможность прочитать ранее восстановленный архив.
	const newHistory = "{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n"
	writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal("native writer cannot append to its restored source")
	}
	written, writeErr := writer.WriteString(newHistory)
	syncErr := writer.Sync()
	closeErr := writer.Close()
	if writeErr != nil || written != len(newHistory) || syncErr != nil || closeErr != nil {
		t.Fatal("new native history was not durably appended")
	}
	_, capturedRelative, digest, size, err := captureRollout(input, path)
	body := []byte("{\"type\":\"session_meta\"}\n" + newHistory)
	hash := sha256.Sum256(body)
	if err != nil || capturedRelative != relative || digest != hex.EncodeToString(hash[:]) || size != int64(len(body)) {
		t.Fatal("native-writer-owned restore failed exact next history capture")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("capture changed the restricted source mode")
	}
	owner := info.Sys().(*syscall.Stat_t)
	if owner.Uid != 10002 || owner.Gid != 29000 {
		t.Fatal("capture changed the canonical restored source owner")
	}
}
