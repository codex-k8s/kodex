package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

type cancelAfterCanaryWrite struct {
	context.Context
	root     string
	observed bool
}

func (ctx *cancelAfterCanaryWrite) Err() error {
	paths, _ := filepath.Glob(filepath.Join(ctx.root, ".kodex/outbox/.readiness-*/current.txt"))
	if len(paths) != 0 {
		ctx.observed = true
		return context.Canceled
	}
	return ctx.Context.Err()
}

func TestCanaryCancellationAfterWriteCleansTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterCanaryWrite{Context: t.Context(), root: root}
	if err := RunCanary(ctx, root, testPolicy()); err == nil || !ctx.observed {
		t.Fatal("cancellation did not interrupt the written canary")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".kodex/outbox"))
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled canary left files")
	}
}

func TestCanaryDoesNotCountImmutableContextAgainstWritableQuota(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{".kodex/outbox", "context/skills"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Create(filepath.Join(root, "context/skills/sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(runtimecontract.RuntimeWorkspaceWritableBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RunCanary(t.Context(), root, testPolicy()); err != nil {
		t.Fatalf("immutable context consumed writable quota: %v", err)
	}
}

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
	if err := RunCanary(t.Context(), root, testPolicy()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".kodex/outbox"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("canary cleanup: entries=%d err=%v", len(entries), err)
	}
}

func TestConcurrentCanariesDoNotInvalidateEachOther(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o770); err != nil {
		t.Fatal(err)
	}
	failures := make(chan error, 64)
	// Каждый раунд конечен: непрерывный захват lock одним worker не должен
	// превращать проверку filesystem race в тест превышения lock budget.
	for range 8 {
		var workers sync.WaitGroup
		start := make(chan struct{})
		for range 8 {
			workers.Go(func() {
				<-start
				if err := RunCanary(t.Context(), root, testPolicy()); err != nil {
					failures <- err
				}
			})
		}
		close(start)
		workers.Wait()
	}
	close(failures)
	for err := range failures {
		t.Errorf("concurrent canary rejected a healthy workspace: %s", DenialReason(err))
	}
	entries, err := os.ReadDir(filepath.Join(root, ".kodex/outbox"))
	if err != nil || len(entries) != 0 {
		t.Fatal("concurrent canary cleanup is incomplete")
	}
}

func TestWorkspaceWriteAcceptanceCreatesReplacesDeletesAndPublishesExactResult(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o770); err != nil {
		t.Fatal(err)
	}
	if err := RunCanary(t.Context(), root, testPolicy()); err != nil {
		t.Fatalf("positive create/read/replace/read/delete path failed: %v", err)
	}
	provenance := ResultProvenance{Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: "rrev_abcdefgh",
		RuntimeRevisionVersion: 7, RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3,
		ExecutionBindingDigest: strings.Repeat("b", 64)}
	if err := PublishResult(t.Context(), root, testPolicy(), provenance); err != nil {
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
			if err := PublishResult(t.Context(), root, testPolicy(), value); err == nil {
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
	err := RunCanary(t.Context(), root, testPolicy())
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
