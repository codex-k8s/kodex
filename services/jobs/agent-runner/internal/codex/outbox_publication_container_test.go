package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

// Вызывается только отдельной offline container fixture без credentials.
func TestProviderOutboxPublicationContainer(t *testing.T) {
	mode := os.Getenv("KODEX_OUTBOX_PUBLICATION_TEST")
	if mode == "" {
		return
	}
	if os.Geteuid() != 10002 {
		t.Fatal("publication fixture identity is invalid")
	}
	root := "/workspace"
	outbox := filepath.Join(root, ".kodex/outbox")
	if mode == "WRITE" {
		if os.WriteFile(filepath.Join(outbox, "private.md"), []byte("private synthetic artifact"), 0o600) != nil {
			t.Fatal("write private artifact")
		}
		if os.WriteFile(filepath.Join(outbox, "atomic.md"), []byte("obsolete artifact"), 0o640) != nil {
			t.Fatal("write obsolete artifact")
		}
		temporary, err := os.CreateTemp(outbox, ".atomic-")
		if err != nil {
			t.Fatal("create private replacement")
		}
		if _, err := temporary.WriteString("atomic synthetic artifact"); err != nil || temporary.Close() != nil || os.Rename(temporary.Name(), filepath.Join(outbox, "atomic.md")) != nil {
			t.Fatal("replace private artifact")
		}
		if os.Chown(filepath.Join(outbox, "atomic.md"), -1, 10002) != nil {
			t.Fatal("prepare primary-group artifact")
		}
		return
	}
	input := model.Input{WorkspaceRoot: root, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1()}
	if mode == "PUBLISH" {
		if err := publishProviderOutbox(t.Context(), input); err != nil {
			t.Fatal("provider-owned private files failed publication")
		}
		return
	}
	if mode != "REJECT_FOREIGN" {
		t.Fatal("publication fixture mode is invalid")
	}
	var before, after unix.Stat_t
	path := filepath.Join(outbox, "foreign.md")
	if unix.Stat(path, &before) != nil || before.Uid != 10001 || before.Mode&0o7777 != 0o640 {
		t.Fatal("foreign fixture metadata is invalid")
	}
	if publishProviderOutbox(t.Context(), input) == nil || unix.Stat(path, &after) != nil || before != after {
		t.Fatal("publication accepted or changed a foreign-owned artifact")
	}
}
