package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

// TestProviderOutboxPublicationContainer is an opt-in offline fixture for UID 10002 on disposable
// /workspace: WRITE prepares private/atomic files, PUBLISH shares them, REJECT_FOREIGN protects another UID.
// With no mode it returns without testing the container boundary; a passing direct call proves nothing.
// TestProviderOutboxPublicationContainer — opt-in offline fixture UID 10002 на disposable
// /workspace: WRITE готовит private/atomic files, PUBLISH выдаёт доступ, REJECT_FOREIGN защищает иной UID.
// Без mode она возвращается без проверки container boundary; успешный прямой вызов её не доказывает.
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
	if mode == "MANAGED_PRESERVE" {
		path := filepath.Join(outbox, "managed-report.md")
		var before, after unix.Stat_t
		body, err := os.ReadFile(path)
		if err != nil || unix.Stat(path, &before) != nil || before.Uid != 10001 || before.Gid != 29000 || before.Mode&0o7777 != 0o640 {
			t.Fatal("managed runner-owned fixture is invalid")
		}
		input := managedOutboxInputFixture()
		want := Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic managed result", Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}}
		got := publishCompletedProviderOutbox(t.Context(), input, want)
		next, readErr := os.ReadFile(path)
		if !reflect.DeepEqual(got, want) || unix.Stat(path, &after) != nil ||
			before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || before.Nlink != after.Nlink ||
			before.Uid != after.Uid || before.Gid != after.Gid || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
			readErr != nil || !bytes.Equal(body, next) {
			t.Fatal("provider modified or rejected the role-owned managed outbox")
		}
		return
	}
	if mode == "MANAGED_FOREIGN_WRITE" {
		if os.WriteFile(filepath.Join(outbox, "provider-owned.md"), []byte("foreign synthetic file"), 0o640) != nil {
			t.Fatal("create foreign managed artifact")
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
