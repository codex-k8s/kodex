package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// TestWorkspaceCanaryPublicationAndResetCoordinate runs eight bounded concurrent canary/publication/reset rounds on one disposable outbox and requires a final provenance publication to remain collectable under the cooperative directory lock.
// TestWorkspaceCanaryPublicationAndResetCoordinate выполняет восемь ограниченных concurrent canary/publication/reset раундов одного одноразового outbox и требует доступность итоговой provenance publication для сбора под согласующим directory lock.
func TestWorkspaceCanaryPublicationAndResetCoordinate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o770); err != nil {
		t.Fatal(err)
	}
	policy := runtimecontract.RuntimeWorkspacePolicyV1()
	provenance := workspace.ResultProvenance{Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: "rrev_fixture",
		RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 1,
		ExecutionBindingDigest: strings.Repeat("b", 64)}
	for range 8 {
		var workers sync.WaitGroup
		start := make(chan struct{})
		failures := make(chan error, 3)
		for _, operation := range []func() error{
			func() error { return workspace.RunCanary(t.Context(), root, policy) },
			func() error { return workspace.PublishResult(t.Context(), root, policy, provenance) },
			func() error { return resetWorkspaceDirectory(t.Context(), root, ".kodex/outbox") },
		} {
			workers.Go(func() { <-start; failures <- operation() })
		}
		close(start)
		workers.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal("coordinated workspace lifecycle failed")
			}
		}
	}
	if err := workspace.PublishResult(t.Context(), root, policy, provenance); err != nil {
		t.Fatal(err)
	}
	artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "synthetic result")
	if err != nil || len(artifacts) != 2 {
		t.Fatal("coordinated workspace publication is incomplete")
	}
}

// TestWorkspaceAgentProcess is an inert helper without an explicit mode; controlled test children perform real synthetic create/read/atomic-replace/delete or protected-write denial inside the prepared mount view. It uses no model or actual agent session, and the canary mode requires non-root execution.
// TestWorkspaceAgentProcess — неактивный helper без явного mode; управляемые тестовые children выполняют настоящий synthetic create/read/atomic-replace/delete либо protected-write denial в подготовленном mount view. Модель и настоящая agent session не используются; canary mode требует non-root.
func TestWorkspaceAgentProcess(t *testing.T) {
	mode := os.Getenv("KODEX_WORKSPACE_TEST_PROCESS")
	if mode == "" {
		return
	}
	if mode == "canary" {
		if os.Geteuid() == 0 {
			t.Fatal("workspace canary fixture must run as non-root")
		}
		if err := workspace.RunCanary(t.Context(), "/workspace", runtimecontract.RuntimeWorkspacePolicyV1()); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir("/workspace/.kodex/outbox")
		if err != nil || len(entries) != 0 {
			t.Fatal("non-root canary did not clean temporary files")
		}
		return
	}
	if mode != "positive" {
		path := map[string]string{
			"immutable":  "/workspace/input/immutable.txt",
			"credential": "/workspace/.kodex/state/codex-home/auth.json",
			"foreign":    "/foreign/result.txt",
			"symlink":    "/workspace/escape/result.txt",
			"traversal":  "/workspace/../foreign/result.txt",
		}[mode]
		if path == "" {
			t.Fatal("unknown test process mode")
		}
		if err := os.WriteFile(path, []byte("forbidden"), 0o600); !errors.Is(err, unix.EROFS) && !errors.Is(err, os.ErrPermission) {
			t.Fatalf("protected write did not fail closed: %v", err)
		}
		return
	}
	if err := os.MkdirAll("/workspace/work/nested", 0o700); err != nil {
		t.Fatal(err)
	}
	path := "/workspace/work/nested/current.txt"
	if err := os.WriteFile(path, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "initial" {
		t.Fatalf("initial read failed: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".next", []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".next", path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || os.SameFile(before, after) {
		t.Fatal("atomic replacement did not replace the inode")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "replacement" {
		t.Fatalf("replacement read failed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("deleted file is still present")
	}
	if err := os.WriteFile("/workspace/.kodex/outbox/agent-result.txt", []byte("create/read/atomic-replace/read/delete completed"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestWorkspaceSubprocessWriteAndCompletionProvenance runs synthetic filesystem CRUD, publishes exact revision/attempt provenance and checks the three-artifact completion satisfies the canonical consumer contract without a provider run.
// TestWorkspaceSubprocessWriteAndCompletionProvenance выполняет synthetic filesystem CRUD, публикует точные revision/attempt provenance и проверяет three-artifact completion по каноническому consumer contract без провайдера.
func TestWorkspaceSubprocessWriteAndCompletionProvenance(t *testing.T) {
	root := workspaceProcessFixture(t)
	runWorkspaceProcess(t, root, "positive")
	input := model.Input{WorkspaceRoot: root, Capabilities: []string{runtimecontract.ArtifactCapability},
		RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionVersion: 7,
		RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3, ExecutionBindingDigest: strings.Repeat("b", 64)}
	provenance := workspace.ResultProvenance{Schema: "kodex.workspace-write-result.v1",
		RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, ExecutionBindingDigest: input.ExecutionBindingDigest}
	if err := workspace.PublishResult(t.Context(), root, runtimecontract.RuntimeWorkspacePolicyV1(), provenance); err != nil {
		t.Fatal(err)
	}
	artifacts, err := completionArtifacts(input, "Completed.")
	if err != nil || len(artifacts) != 3 {
		t.Fatalf("completion artifacts: count=%d error=%v", len(artifacts), err)
	}
	found := false
	for _, artifact := range artifacts {
		if artifact.FileName == "workspace-write-result.json" {
			var actual workspace.ResultProvenance
			if json.Unmarshal(artifact.Content, &actual) != nil || actual != provenance {
				t.Fatal("completion lost exact revision/attempt provenance")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("completion omitted provenance")
	}
	completion := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		Attempt: input.Attempt, Success: true, ResultSummary: "Completed.", Artifacts: artifacts}
	if err := completion.Validate(); err != nil {
		t.Fatalf("published completion violates the consumer contract: %v", err)
	}
}

// TestWorkspaceCanaryWithNonRootProcess invokes the canary helper in the prepared non-root mount fixture and relies on it to prove write readiness and complete temporary cleanup.
// TestWorkspaceCanaryWithNonRootProcess вызывает canary helper в подготовленной non-root mount fixture для проверки write readiness и полной временной очистки.
func TestWorkspaceCanaryWithNonRootProcess(t *testing.T) {
	root := workspaceProcessFixture(t)
	runWorkspaceProcess(t, root, "canary")
}

// TestNextAttemptClearsOutboxWithoutFollowingForeignSymlink adds a previous-attempt symlink to a separate temporary sentinel tree and requires reset to empty the outbox without modifying the target.
// TestNextAttemptClearsOutboxWithoutFollowingForeignSymlink добавляет previous-attempt symlink на отдельное временное sentinel-дерево и требует пустой outbox после reset без изменения target.
func TestNextAttemptClearsOutboxWithoutFollowingForeignSymlink(t *testing.T) {
	root := workspaceProcessFixture(t)
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "sentinel"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	outbox := filepath.Join(root, ".kodex/outbox")
	if err := os.Symlink(foreign, filepath.Join(outbox, "previous-attempt")); err != nil {
		t.Fatal(err)
	}
	if err := resetWorkspaceDirectory(t.Context(), root, ".kodex/outbox"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(outbox)
	if err != nil || len(entries) != 0 {
		t.Fatal("previous attempt survived outbox reset")
	}
	if raw, err := os.ReadFile(filepath.Join(foreign, "sentinel")); err != nil || string(raw) != "preserve" {
		t.Fatal("outbox reset followed a foreign symlink")
	}
}

// TestCompletionKeepsProvenanceAtArtifactLimit exceeds the synthetic outbox file count and requires a sixteen-artifact result with workspace provenance prioritized immediately after result.md.
// TestCompletionKeepsProvenanceAtArtifactLimit превышает число synthetic outbox files и требует sixteen-artifact result с workspace provenance сразу после result.md.
func TestCompletionKeepsProvenanceAtArtifactLimit(t *testing.T) {
	root := workspaceProcessFixture(t)
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(filepath.Join(root, ".kodex/outbox", "a"+strconv.Itoa(i)+".txt"), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".kodex/outbox/workspace-write-result.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "done")
	if err != nil || len(artifacts) != 16 || artifacts[1].FileName != "workspace-write-result.json" {
		t.Fatalf("bounded completion lost provenance: count=%d error=%v", len(artifacts), err)
	}
}

// TestWorkspaceSubprocessRejectsProtectedWrites checks immutable input, credential marker, foreign mount, symlink and traversal writes fail in controlled child mount fixtures without contacting a model.
// TestWorkspaceSubprocessRejectsProtectedWrites проверяет отказы записи immutable input, credential marker, foreign mount, symlink и traversal в управляемых child mount fixtures без модели.
func TestWorkspaceSubprocessRejectsProtectedWrites(t *testing.T) {
	for _, mode := range []string{"immutable", "credential", "foreign", "symlink", "traversal"} {
		t.Run(mode, func(t *testing.T) {
			root := workspaceProcessFixture(t)
			runWorkspaceProcess(t, root, mode)
		})
	}
}

// workspaceProcessFixture creates only a temporary fixed workspace tree and inert auth marker for mount tests; the marker is not a live credential and the returned root remains owned by test cleanup.
// workspaceProcessFixture создаёт лишь временное фиксированное workspace-дерево и неактивный auth marker для mount tests; marker не живые реквизиты, возвращённый root принадлежит test cleanup.
func workspaceProcessFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, relative := range []string{"input", ".kodex/state/codex-home", ".kodex/outbox", "foreign"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".kodex/state/codex-home/auth.json"), []byte("fixture-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// runWorkspaceProcess runs the current test binary under bounded bwrap user/mount isolation with a read-only system root, protected input/auth/foreign bindings and an explicit synthetic mode. It reports child failure without invoking the production runner or asserting network-namespace isolation that these arguments do not request.
// runWorkspaceProcess запускает текущий тестовый бинарь в ограниченной bwrap user/mount isolation с read-only system root, защищёнными input/auth/foreign bindings и явным synthetic mode. Сообщает child failure без production runner и без заявления network-namespace isolation, не запрошенной этими аргументами.
func runWorkspaceProcess(t *testing.T, root, mode string) {
	t.Helper()
	if mode == "symlink" {
		if err := os.Symlink("/foreign", filepath.Join(root, "escape")); err != nil {
			t.Fatal(err)
		}
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("bwrap is required for the workspace subprocess acceptance test")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bwrap, "--unshare-user", "--uid", strconv.Itoa(os.Geteuid()),
		"--gid", strconv.Itoa(os.Getegid()), "--tmpfs", "/", "--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64", "--ro-bind", executable, "/agent-test",
		"--bind", root, "/workspace",
		"--ro-bind", filepath.Join(root, "input"), "/workspace/input",
		"--ro-bind", filepath.Join(root, ".kodex/state/codex-home/auth.json"), "/workspace/.kodex/state/codex-home/auth.json",
		"--ro-bind", filepath.Join(root, "foreign"), "/foreign", "--chdir", "/workspace",
		"--remount-ro", "/", "/agent-test", "-test.run=^TestWorkspaceAgentProcess$")
	command.Env = []string{"KODEX_WORKSPACE_TEST_PROCESS=" + mode, "PATH=/usr/bin:/bin"}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("workspace process %s: %v\n%s", mode, err, output)
	}
}

// TestCollectArtifactsDoesNotBlockOnFIFO places a synthetic FIFO in a temporary outbox and requires immediate collection of only result.md rather than blocking for a pipe writer.
// TestCollectArtifactsDoesNotBlockOnFIFO помещает synthetic FIFO во временный outbox и требует сбор лишь result.md без ожидания pipe writer.
func TestCollectArtifactsDoesNotBlockOnFIFO(t *testing.T) {
	root := workspaceProcessFixture(t)
	if err := unix.Mkfifo(filepath.Join(root, ".kodex/outbox/pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "done")
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("FIFO collection: count=%d error=%v", len(artifacts), err)
	}
}
