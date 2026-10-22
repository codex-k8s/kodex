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

// cancelAfterCanaryWrite wraps the test context and marks cancellation only once a nonce-owned current file exists, targeting cleanup after an actual write.
// cancelAfterCanaryWrite оборачивает тестовый контекст и отмечает отмену лишь после появления current-файла nonce-каталога для проверки очистки после реальной записи.
type cancelAfterCanaryWrite struct {
	context.Context
	root     string
	observed bool
}

// Err observes the synthetic readiness file and returns cancellation after it appears; otherwise it forwards the underlying context error.
// Err наблюдает синтетический файл готовности и возвращает отмену после его появления; иначе передаёт ошибку исходного контекста.
func (ctx *cancelAfterCanaryWrite) Err() error {
	paths, _ := filepath.Glob(filepath.Join(ctx.root, ".kodex/outbox/.readiness-*/current.txt"))
	if len(paths) != 0 {
		ctx.observed = true
		return context.Canceled
	}
	return ctx.Context.Err()
}

// TestCanaryCancellationAfterWriteCleansTemporaryFiles injects cancellation after file creation and requires an error plus an empty outbox after cleanup.
// TestCanaryCancellationAfterWriteCleansTemporaryFiles вводит отмену после создания файла и требует ошибку и пустой outbox после очистки.
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

// TestCanaryDoesNotCountImmutableContextAgainstWritableQuota places an oversized sparse file in protected context and verifies it does not consume the writable-workspace quota.
// TestCanaryDoesNotCountImmutableContextAgainstWritableQuota помещает чрезмерный sparse-файл в защищённый context и проверяет отсутствие его учёта в writable-квоте workspace.
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

// TestCanaryDoesNotTraverseSeparateSessionVolumeForWorkspaceQuota builds oversized, permission-restricted synthetic provider state and requires readiness without traversing that separate quota domain.
// TestCanaryDoesNotTraverseSeparateSessionVolumeForWorkspaceQuota создаёт чрезмерный синтетический provider state с ограниченными правами и требует готовность без обхода этой отдельной области квоты.
func TestCanaryDoesNotTraverseSeparateSessionVolumeForWorkspaceQuota(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, ".kodex/state/codex-home/private")
	for _, path := range []string{".kodex/outbox", "input", "knowledge", ".kodex/state/codex-home/private"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Create(filepath.Join(private, "provider-state"))
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
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(private, 0o700) })
	if err := RunCanary(t.Context(), root, testPolicy()); err != nil {
		t.Fatalf("separate session volume blocked workspace readiness: %v", err)
	}
}

// testPolicy returns a fresh canonical workspace policy for synthetic filesystem tests without changing runtime configuration.
// testPolicy возвращает новую каноническую workspace policy для синтетических файловых тестов без изменения runtime-конфигурации.
func testPolicy() runtimecontract.RuntimeWorkspacePolicy {
	return runtimecontract.RuntimeWorkspacePolicyV1()
}

// TestCanaryEnforcesConfiguredLowerWorkspaceQuota accepts an empty workspace under a reduced limit, then requires the quota reason after writing one byte above it.
// TestCanaryEnforcesConfiguredLowerWorkspaceQuota принимает пустой workspace с уменьшенным лимитом и требует причину quota после записи на один байт сверх него.
func TestCanaryEnforcesConfiguredLowerWorkspaceQuota(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	policy, err := runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 10})
	if err != nil || RunCanary(t.Context(), root, policy) != nil {
		t.Fatal("bounded writable workspace not ready")
	}
	if err := os.WriteFile(filepath.Join(root, "result"), make([]byte, 4097), 0o600); err != nil {
		t.Fatal(err)
	}
	err = RunCanary(t.Context(), root, policy)
	var denial *Denial
	if !errors.As(err, &denial) || denial.Reason != runtimecontract.RuntimeWorkspaceQuotaExceeded {
		t.Fatal("configured lower quota was not enforced")
	}
}

// TestRunCanaryExercisesAtomicWritablePathAndCleansUp runs the actual synthetic create/read/replace/delete probe and requires no remaining outbox entries.
// TestRunCanaryExercisesAtomicWritablePathAndCleansUp выполняет реальную синтетическую пробу создания/чтения/замены/удаления и требует отсутствия остаточных entries outbox.
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

// TestConcurrentCanariesDoNotInvalidateEachOther runs eight bounded rounds of eight synchronized probes against one workspace and requires all successes and complete cleanup.
// TestConcurrentCanariesDoNotInvalidateEachOther выполняет восемь ограниченных раундов по восемь синхронных проб одного workspace и требует все успехи и полную очистку.
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

// TestWorkspaceWriteAcceptanceCreatesReplacesDeletesAndPublishesExactResult runs the probe, publishes synthetic provenance and checks exact decoded fields with no surviving temporary result; it is local unit evidence, not platform acceptance.
// TestWorkspaceWriteAcceptanceCreatesReplacesDeletesAndPublishesExactResult выполняет пробу, публикует синтетическую provenance и сверяет точные поля без остаточного временного результата; это локальный unit, не приёмка платформы.
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

// TestPublishResultRejectsInvalidOrIncompleteProvenance independently corrupts revision digest, attempt and execution digest and requires publication to reject each case.
// TestPublishResultRejectsInvalidOrIncompleteProvenance отдельно повреждает дайджест ревизии, попытку и дайджест исполнения и требует отказ публикации в каждом случае.
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

// TestRunCanaryRejectsSymlinkEscape points the outbox outside the synthetic root through a symlink and requires the path-outside-workspace reason.
// TestRunCanaryRejectsSymlinkEscape направляет outbox за синтетический root через symlink и требует причину выхода из workspace.
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

// TestWorkspaceDenialsAreExactAndDoNotContainPaths checks protected and escaped policy paths, byte/file quota boundaries and representative errno classifications without exposing absolute paths.
// TestWorkspaceDenialsAreExactAndDoNotContainPaths проверяет защищённые и выходящие из policy пути, byte/file квоты и представительные errno-классификации без раскрытия абсолютных путей.
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

// TestWritableUsageStopsAtDirectoryAndByteBudgets exceeds the entry budget with nested directories and the byte budget with a sparse file, requiring quota failures in both walks.
// TestWritableUsageStopsAtDirectoryAndByteBudgets превышает бюджет entries вложенными каталогами и byte-бюджет sparse-файлом, требуя quota-отказов обоих обходов.
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
