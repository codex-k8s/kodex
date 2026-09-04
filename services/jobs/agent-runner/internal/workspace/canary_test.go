package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func testPolicy() runtimecontract.RuntimeWorkspacePolicy {
	return runtimecontract.RuntimeWorkspacePolicyV1()
}

func TestRunCanaryExercisesAtomicWritablePathAndCleansUp(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{".kodex/outbox", "input", "knowledge"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o770); err != nil {
			t.Fatal(err)
		}
	}
	if err := RunCanary(root, testPolicy()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".kodex/outbox"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("canary cleanup: entries=%d err=%v", len(entries), err)
	}
}

func TestWorkspaceWriteAcceptanceCreatesReplacesDeletesAndPublishesExactResult(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o770); err != nil {
		t.Fatal(err)
	}
	if err := RunCanary(root, testPolicy()); err != nil {
		t.Fatalf("positive create/read/replace/read/delete path failed: %v", err)
	}
	provenance := ResultProvenance{Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: "rrev_abcdefgh",
		RuntimeRevisionVersion: 7, RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3,
		ExecutionBindingDigest: strings.Repeat("b", 64)}
	if err := PublishResult(root, testPolicy(), provenance); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".kodex/outbox/workspace-write-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var actual ResultProvenance
	if json.Unmarshal(raw, &actual) != nil || actual != provenance {
		t.Fatalf("published provenance = %#v, want %#v", actual, provenance)
	}
	if _, err := os.Stat(filepath.Join(root, ".kodex/outbox/.workspace-write-result.next")); !os.IsNotExist(err) {
		t.Fatalf("temporary result survived atomic replace: %v", err)
	}
}

func TestPublishResultRejectsInvalidOrIncompleteProvenance(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o770); err != nil {
		t.Fatal(err)
	}
	valid := ResultProvenance{Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: "rrev_abcdefgh",
		RuntimeRevisionVersion: 7, RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3,
		ExecutionBindingDigest: strings.Repeat("b", 64)}
	for name, mutate := range map[string]func(*ResultProvenance){
		"revision":          func(value *ResultProvenance) { value.RuntimeRevisionDigest = strings.Repeat("z", 64) },
		"attempt":           func(value *ResultProvenance) { value.Attempt = 0 },
		"execution binding": func(value *ResultProvenance) { value.ExecutionBindingDigest = "caller" },
	} {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if err := PublishResult(root, testPolicy(), value); err == nil {
				t.Fatal("invalid result provenance was accepted")
			}
		})
	}
}

func TestRunCanaryRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex"), 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".kodex/outbox")); err != nil {
		t.Fatal(err)
	}
	err := RunCanary(root, testPolicy())
	if DenialReason(err) != runtimecontract.RuntimeWorkspacePathOutsideWorkspace {
		t.Fatalf("reason=%q err=%v", DenialReason(err), err)
	}
}

func TestWorkspaceDenialsAreExactAndDoNotContainPaths(t *testing.T) {
	policy := testPolicy()
	if access, reason := policy.AccessForPath("/workspace/input/file"); access != runtimecontract.RuntimeWorkspaceReadOnly || reason != "" {
		t.Fatalf("read-only policy=(%q,%q)", access, reason)
	}
	if _, reason := policy.AccessForPath("/workspace/../foreign"); reason != runtimecontract.RuntimeWorkspacePathOutsideWorkspace {
		t.Fatalf("traversal reason=%q", reason)
	}
	for _, protected := range []string{"/workspace/input/file", "/workspace/knowledge/memory.md", "/workspace/.kodex/state/codex-home/auth.json"} {
		if access, reason := policy.AccessForPath(protected); access != runtimecontract.RuntimeWorkspaceReadOnly || reason != "" {
			t.Fatalf("protected path %q policy=(%q,%q)", protected, access, reason)
		}
	}
	for _, escaped := range []string{"/foreign/workspace/result", "../foreign", "/workspace/out/../../credential"} {
		if _, reason := policy.AccessForPath(escaped); reason != runtimecontract.RuntimeWorkspacePathOutsideWorkspace {
			t.Fatalf("escape path %q reason=%q", escaped, reason)
		}
	}
	if withinQuota(policy.MaximumWritableBytes, 0, 1, policy) || withinQuota(0, policy.MaximumFileCount, 1, policy) {
		t.Fatal("quota overflow accepted")
	}
	for cause, reason := range map[error]string{syscall.EROFS: runtimecontract.RuntimeWorkspaceReadOnly, syscall.ENOSPC: runtimecontract.RuntimeWorkspaceQuotaExceeded, syscall.ELOOP: runtimecontract.RuntimeWorkspacePathOutsideWorkspace, errors.New("io"): runtimecontract.RuntimeWorkspaceIOError} {
		err := classify(cause)
		if DenialReason(err) != reason || filepath.IsAbs(err.Error()) {
			t.Errorf("classify(%v)=%q err=%q", cause, DenialReason(err), err)
		}
	}
}
