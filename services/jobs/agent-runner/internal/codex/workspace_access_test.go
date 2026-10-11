package codex

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

func workspaceAccessFixture(t *testing.T) (string, *os.File, uint32, uint32) {
	t.Helper()
	path := t.TempDir()
	if unix.Chmod(path, 0o2770) != nil {
		t.Fatal("prepare synthetic working directory")
	}
	directory, err := os.Open(path)
	if err != nil {
		t.Fatal("open synthetic working directory")
	}
	t.Cleanup(func() { directory.Close() })
	return path, directory, uint32(os.Geteuid()), uint32(os.Getegid())
}

func TestProviderWorkspaceAccessNormalizesDirectoriesNotPrivateFiles(t *testing.T) {
	path, directory, uid, group := workspaceAccessFixture(t)
	private := filepath.Join(path, "private", "nested")
	if os.MkdirAll(private, 0o700) != nil || os.WriteFile(filepath.Join(private, "private.txt"), []byte("synthetic sentinel"), 0o600) != nil {
		t.Fatal("prepare private working tree")
	}
	before, _ := os.Stat(filepath.Join(private, "private.txt"))
	if err := normalizeWorkspaceAccess(t.Context(), directory, runtimecontract.RuntimeWorkspacePolicyV1(), uid, uid, group); err != nil {
		t.Fatal("writer directory normalization failed")
	}
	for _, path := range []string{filepath.Dir(private), private} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o750 {
			t.Fatal("private directory not group-traversable")
		}
	}
	after, _ := os.Stat(filepath.Join(private, "private.txt"))
	if before == nil || after == nil || !os.SameFile(before, after) || after.Mode().Perm() != 0o600 || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		t.Fatal("normalization changed private file")
	}
}

func TestProviderWorkspaceAccessPreservesRunnerMaterializedInput(t *testing.T) {
	path, directory, uid, group := workspaceAccessFixture(t)
	input := filepath.Join(path, "input", "aset", "files")
	if os.MkdirAll(input, 0o750) != nil || os.WriteFile(filepath.Join(input, "source.txt"), []byte("pinned input"), 0o640) != nil {
		t.Fatal("prepare runner input")
	}
	before, _ := os.Stat(input)
	if normalizeWorkspaceAccess(t.Context(), directory, runtimecontract.RuntimeWorkspacePolicyV1(), uid, uid+1, group) != nil {
		t.Fatal("healthy runner input rejected")
	}
	after, _ := os.Stat(input)
	if before.Mode() != after.Mode() || before.ModTime() != after.ModTime() {
		t.Fatal("runner-owned input directory changed")
	}
}

func TestProviderWorkspaceAccessRejectsUnsafeTree(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "foreign owner", "runner private directory", "root mode", "root group", "count", "bytes", "cancelled", "invalid policy"} {
		t.Run(kind, func(t *testing.T) {
			path, directory, uid, group := workspaceAccessFixture(t)
			policy := runtimecontract.RuntimeWorkspacePolicyV1()
			providerUID, runnerUID := uid, uid
			ctx := t.Context()
			file := filepath.Join(path, "file")
			if os.WriteFile(file, []byte("private sentinel"), 0o600) != nil {
				t.Fatal("prepare synthetic file")
			}
			switch kind {
			case "symlink":
				if os.Symlink(file, filepath.Join(path, "link")) != nil {
					t.Fatal("prepare symlink")
				}
			case "hardlink":
				if os.Link(file, filepath.Join(path, "link")) != nil {
					t.Fatal("prepare hardlink")
				}
			case "fifo":
				if unix.Mkfifo(filepath.Join(path, "fifo"), 0o600) != nil {
					t.Fatal("prepare FIFO")
				}
			case "foreign owner":
				providerUID, runnerUID = uid+1, uid+2
			case "runner private directory":
				providerUID = uid + 1
				if os.Mkdir(filepath.Join(path, "private"), 0o700) != nil {
					t.Fatal("prepare private runner directory")
				}
			case "root mode":
				if unix.Chmod(path, 0o770) != nil {
					t.Fatal("prepare root mode")
				}
			case "root group":
				group++
			case "count":
				policy, _ = runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 4096, MaxFiles: 1})
			case "bytes":
				policy, _ = runtimecontract.RuntimeWorkspacePolicyWithLimits(&runtimecontract.RuntimeWorkspaceLimits{MaxBytes: 1, MaxFiles: 10})
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "invalid policy":
				policy.Digest = strings.Repeat("0", 64)
			}
			err := normalizeWorkspaceAccess(ctx, directory, policy, runnerUID, providerUID, group)
			if !errors.Is(err, errProviderWorkspaceAccess) || strings.Contains(err.Error(), "sentinel") || strings.Contains(err.Error(), path) {
				t.Fatal("unsafe tree did not fail closed without disclosure")
			}
		})
	}
}

func TestProviderWorkspaceAccessRejectsReplacedDirectory(t *testing.T) {
	path, directory, uid, group := workspaceAccessFixture(t)
	if os.Mkdir(filepath.Join(path, "child"), 0o700) != nil {
		t.Fatal("prepare child")
	}
	child, err := os.Open(filepath.Join(path, "child"))
	if err != nil {
		t.Fatal("open child")
	}
	defer child.Close()
	var before unix.Stat_t
	if unix.Fstat(int(child.Fd()), &before) != nil || os.Rename(filepath.Join(path, "child"), filepath.Join(path, "old")) != nil || os.Mkdir(filepath.Join(path, "child"), 0o700) != nil {
		t.Fatal("replace child")
	}
	usage := workspaceAccessUsage{entries: 2}
	if !errors.Is(normalizeWorkspaceAccessChild(t.Context(), directory, child, "child", before, runtimecontract.RuntimeWorkspacePolicyV1(), uid, uid, group, &usage), errProviderWorkspaceAccess) {
		t.Fatal("replaced pathname accepted")
	}
	info, _ := os.Stat(filepath.Join(path, "child"))
	if info.Mode().Perm() != 0o700 {
		t.Fatal("replacement inode modified")
	}
}

// Только отдельный root fixture executable; production никогда не меняет UID.
func TestProviderWorkspaceAccessKernel(t *testing.T) {
	if child := os.Getenv("KODEX_WORKSPACE_ACCESS_KERNEL_CHILD"); child != "" {
		workspaceAccessKernelChild(t, child)
		return
	}
	if os.Getenv("KODEX_WORKSPACE_ACCESS_KERNEL_TEST") != "1" {
		t.Skip("disposable root fixture entrypoint only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("fixture root identity required")
	}
	parent := t.TempDir()
	if os.Chmod(parent, 0o711) != nil {
		t.Fatal("prepare confined fixture parent")
	}
	root := filepath.Join(parent, "root")
	for _, relative := range []string{"", ".kodex", ".kodex/outbox", "work", "work/input", "work/input/aset", "work/input/aset/files"} {
		path := filepath.Join(root, relative)
		if os.Mkdir(path, 0o700) != nil || os.Chown(path, 10001, 29000) != nil || unix.Chmod(path, 0o2770) != nil {
			t.Fatal("prepare runner-owned fixture directory")
		}
	}
	private := filepath.Join(root, "work/private")
	if os.Mkdir(private, 0o700) != nil || os.Chown(private, 10002, 10002) != nil || unix.Chmod(private, 0o700) != nil {
		t.Fatal("prepare private provider fixture directory")
	}
	file := filepath.Join(private, "private.txt")
	input := filepath.Join(root, "work/input/aset/files/source.txt")
	if os.WriteFile(file, []byte("private synthetic sentinel"), 0o600) != nil || os.Chown(file, 10002, 10002) != nil ||
		os.WriteFile(input, []byte("immutable source copy"), 0o640) != nil || os.Chown(input, 10001, 29000) != nil {
		t.Fatal("prepare private fixture files")
	}
	for _, relative := range []string{"input", "input/aset", "input/aset/files"} {
		path := filepath.Join(root, relative)
		if os.Mkdir(path, 0o700) != nil || os.Chown(path, 10001, 29000) != nil || os.Chmod(path, 0o550) != nil {
			t.Fatal("prepare read-only original fixture directory")
		}
	}
	original := filepath.Join(root, "input/aset/files/original.txt")
	if os.WriteFile(original, []byte("immutable source copy"), 0o440) != nil || os.Chown(original, 10001, 29000) != nil {
		t.Fatal("prepare read-only original fixture")
	}
	type snapshot struct {
		stat unix.Stat_t
		sum  [sha256.Size]byte
	}
	before := map[string]snapshot{}
	for _, path := range []string{file, input, original} {
		var stat unix.Stat_t
		body, err := os.ReadFile(path)
		if err != nil || unix.Lstat(path, &stat) != nil {
			t.Fatal("read confined fixture oracle")
		}
		before[path] = snapshot{stat: stat, sum: sha256.Sum256(body)}
	}
	// Копия только собственного test ELF, а не обход private artifact mode.
	source, err := os.Open("/proc/self/exe")
	if err != nil {
		t.Fatal("open fixture executable")
	}
	defer source.Close()
	executable, err := os.OpenFile(filepath.Join(parent, "fixture.test"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o500)
	if err != nil {
		t.Fatal("create fixture executable")
	}
	if _, err := io.Copy(executable, source); err != nil || executable.Chmod(0o555) != nil || executable.Sync() != nil || executable.Close() != nil {
		t.Fatal("seal fixture executable")
	}
	executable, err = os.Open(filepath.Join(parent, "fixture.test"))
	if err != nil {
		t.Fatal("open sealed fixture executable")
	}
	defer executable.Close()
	parentFD, err := os.Open(parent)
	if err != nil {
		t.Fatal("open fixture parent")
	}
	defer parentFD.Close()
	for _, stage := range []struct {
		mode string
		uid  uint32
	}{{"BEFORE", 10001}, {"NORMALIZE", 10002}, {"AFTER", 10001}, {"PRIVATE_FILE", 10001}} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		command := exec.CommandContext(ctx, "/proc/self/fd/4", "-test.run=^TestProviderWorkspaceAccessKernel$", "-test.timeout=4s")
		command.Env = []string{"LANG=C", "KODEX_WORKSPACE_ACCESS_KERNEL_CHILD=" + stage.mode}
		command.ExtraFiles = []*os.File{parentFD, executable}
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: stage.uid, Gid: stage.uid, Groups: []uint32{29000}}}
		err := command.Run()
		cancel()
		if err != nil {
			t.Fatal("closed dual-UID fixture stage failed: " + stage.mode)
		}
	}
	var stat unix.Stat_t
	if unix.Stat(private, &stat) != nil || stat.Uid != 10002 || stat.Gid != 29000 || stat.Mode&0o7777 != 0o750 ||
		unix.Stat(file, &stat) != nil || stat.Uid != 10002 || stat.Gid != 10002 || stat.Mode&0o7777 != 0o600 {
		t.Fatal("kernel fixture permission result mismatch")
	}
	for path, snapshot := range before {
		var after unix.Stat_t
		body, err := os.ReadFile(path)
		if err != nil || unix.Lstat(path, &after) != nil || !sameWorkspaceAccessStat(snapshot.stat, after) || sha256.Sum256(body) != snapshot.sum {
			t.Fatal("private file, input copy or original changed")
		}
	}
}

func workspaceAccessKernelChild(t *testing.T, mode string) {
	root := "/proc/self/fd/3/root"
	if mode == "NORMALIZE" {
		if os.Geteuid() != 10002 {
			t.Fatal("wrong provider identity")
		}
		directory, err := os.Open(filepath.Join(root, "work"))
		if err != nil {
			t.Fatal("open exact fixture work")
		}
		defer directory.Close()
		if normalizeWorkspaceAccess(t.Context(), directory, runtimecontract.RuntimeWorkspacePolicyV1(), 10001, 10002, 29000) != nil {
			t.Fatal("provider directory normalization failed")
		}
		return
	}
	if os.Geteuid() != 10001 {
		t.Fatal("wrong runner identity")
	}
	switch mode {
	case "BEFORE":
		if err := workspacepolicy.RunCanary(t.Context(), root, runtimecontract.RuntimeWorkspacePolicyV1()); err == nil || workspacepolicy.DenialReason(err) != runtimecontract.RuntimeWorkspaceReadOnly {
			t.Fatal("private directory did not reproduce quota traversal denial")
		}
	case "AFTER":
		if workspacepolicy.RunCanary(t.Context(), root, runtimecontract.RuntimeWorkspacePolicyV1()) != nil {
			t.Fatal("runner metadata canary rejected normalized work")
		}
	case "PRIVATE_FILE":
		if _, err := os.ReadFile(filepath.Join(root, "work/private/private.txt")); !errors.Is(err, syscall.EACCES) {
			t.Fatal("private provider file became readable")
		}
	default:
		t.Fatal("unknown fixture stage")
	}
}
