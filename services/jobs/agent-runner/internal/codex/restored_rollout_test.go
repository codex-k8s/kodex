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

// Этот процесс запускается отдельной archive kernel-fixture с UID runner.
// Он не обращается к Codex, Kubernetes либо постоянным каталогам платформы.
func TestRestoredRolloutCaptureFixture(t *testing.T) {
	mode := os.Getenv("KODEX_TEST_RESTORE_MODE")
	if mode == "" {
		return
	}
	workspace := os.Getenv("KODEX_TEST_RESTORE_WORKSPACE")
	if (mode != "OLD_RESTORE_OWNER" && mode != "RUNNER_RESTORE_OWNER") || os.Geteuid() != 10001 ||
		filepath.Dir(workspace) != os.TempDir() || !strings.HasPrefix(filepath.Base(workspace), "kodex-session-restore-") {
		t.Fatal("isolated runner fixture binding is invalid")
	}
	relative := ".kodex/state/codex-home/sessions/2026/08/28/rollout-00000000-0000-4000-8000-000000000001.jsonl"
	path := filepath.Join(workspace, relative)
	input := model.Input{WorkspaceRoot: workspace, CodexHome: filepath.Join(workspace, ".kodex/state/codex-home")}
	_, capturedRelative, digest, size, err := captureRollout(input, path)
	if mode == "OLD_RESTORE_OWNER" {
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
	body := []byte("{\"type\":\"session_meta\"}\n")
	hash := sha256.Sum256(body)
	if err != nil || capturedRelative != relative || digest != hex.EncodeToString(hash[:]) || size != int64(len(body)) {
		t.Fatal("runner-owned restore failed exact next capture")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal("capture changed the restricted source mode")
	}
	owner := info.Sys().(*syscall.Stat_t)
	if owner.Uid != 10001 || owner.Gid != 29000 {
		t.Fatal("capture changed the canonical restored source owner")
	}
}
