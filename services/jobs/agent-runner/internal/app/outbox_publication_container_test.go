package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

func TestRunnerOutboxPublicationContainer(t *testing.T) {
	mode := os.Getenv("KODEX_OUTBOX_PUBLICATION_TEST")
	if mode == "" {
		return
	}
	if os.Geteuid() != 10001 || mode != "UNPUBLISHED" && mode != "COLLECT" {
		t.Fatal("collector fixture binding is invalid")
	}
	root := "/workspace"
	for _, name := range []string{"private.md", "atomic.md"} {
		path := filepath.Join(root, ".kodex/outbox", name)
		if mode == "UNPUBLISHED" {
			file, err := os.Open(path)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, syscall.EACCES) {
				t.Fatal("private provider artifact did not reproduce kernel EACCES for runner")
			}
		} else {
			var stat unix.Stat_t
			if unix.Stat(path, &stat) != nil || stat.Uid != 10002 || stat.Gid != 29000 || stat.Mode&0o7777 != 0o640 {
				t.Fatal("published artifact does not retain dual-UID ABI")
			}
		}
	}
	artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "synthetic completion")
	if mode == "UNPUBLISHED" {
		if err == nil {
			t.Fatal("unpublished private artifacts were collected")
		}
		return
	}
	if err != nil || len(artifacts) != 3 {
		t.Fatal("published artifacts did not reach the canonical collector")
	}
	want := map[string]string{"result.md": "synthetic completion", "private.md": "private synthetic artifact", "atomic.md": "atomic synthetic artifact"}
	for _, artifact := range artifacts {
		body, ok := want[artifact.FileName]
		digest := sha256.Sum256([]byte(body))
		if !ok || string(artifact.Content) != body || artifact.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatal("published artifact bytes or digest changed")
		}
	}
}
