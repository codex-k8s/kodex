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
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// Детерминированный процесс агента выполняет настоящий файловый сценарий без
// обращения к платной модели. Отказы проверяются отдельно от успешной записи.
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

func TestWorkspaceSubprocessWriteAndCompletionProvenance(t *testing.T) {
	root := workspaceProcessFixture(t)
	runWorkspaceProcess(t, root, "positive")
	input := model.Input{WorkspaceRoot: root, Capabilities: []string{runtimecontract.ArtifactCapability},
		RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionVersion: 7,
		RuntimeRevisionDigest: strings.Repeat("a", 64), Attempt: 3, ExecutionBindingDigest: strings.Repeat("b", 64)}
	provenance := workspace.ResultProvenance{Schema: "kodex.workspace-write-result.v1",
		RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, ExecutionBindingDigest: input.ExecutionBindingDigest}
	if err := workspace.PublishResult(root, runtimecontract.RuntimeWorkspacePolicyV1(), provenance); err != nil {
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

func TestWorkspaceCanaryWithNonRootProcess(t *testing.T) {
	root := workspaceProcessFixture(t)
	runWorkspaceProcess(t, root, "canary")
}

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
	if err := resetWorkspaceDirectory(root, ".kodex/outbox"); err != nil {
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

func TestWorkspaceSubprocessRejectsProtectedWrites(t *testing.T) {
	for _, mode := range []string{"immutable", "credential", "foreign", "symlink", "traversal"} {
		t.Run(mode, func(t *testing.T) {
			root := workspaceProcessFixture(t)
			runWorkspaceProcess(t, root, mode)
		})
	}
}

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
