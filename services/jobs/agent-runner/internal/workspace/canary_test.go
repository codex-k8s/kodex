package workspace

import (
	"errors"
	"os"
	"path/filepath"
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

func TestWritableUsageStopsAtDirectoryAndByteBudgets(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a/b/c/d"), 0o700); err != nil {
		t.Fatal(err)
	}
	policy := testPolicy()
	policy.MaximumFileCount = 3
	if _, _, err := writableUsage(root, policy); DenialReason(err) != runtimecontract.RuntimeWorkspaceQuotaExceeded {
		t.Fatalf("directory budget error = %v", err)
	}
	file, err := os.Create(filepath.Join(root, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(runtimecontract.RuntimeWorkspaceWritableBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writableUsage(root, testPolicy()); DenialReason(err) != runtimecontract.RuntimeWorkspaceQuotaExceeded {
		t.Fatalf("byte budget error = %v", err)
	}
}
