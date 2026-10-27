package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

const (
	// workspaceWriterNativeTree is a disposable subpath mount of the exact unpublished generation, never a host worktree.
	// workspaceWriterNativeTree — disposable subpath mount точного unpublished generation, не host worktree.
	workspaceWriterNativeTree = "/workspace/restore-tree"
	// workspaceWriterNativeSocket is confined to the separate disposable shared IPC mount.
	// workspaceWriterNativeSocket ограничен отдельным disposable shared IPC mount.
	workspaceWriterNativeSocket = "/var/run/kodex/work-writer/writer.sock"
)

// workspaceWriterNativeCheckpoint reproduces identical immutable test work in both independently started UID consumers.
// Its memory object store is a fixture, not live object custody or an owner-issued restore grant.
// workspaceWriterNativeCheckpoint воспроизводит identical immutable test work в независимо запущенных UID consumers.
// Memory object store — fixture, не live object custody и не owner-issued restore grant.
func workspaceWriterNativeCheckpoint(t *testing.T) (model.Task, *workspaceObjectFixture) {
	t.Helper()
	task, _ := workspaceSnapshotFixture(t)
	encode := func(ctx context.Context, output io.Writer, task model.Task) (string, int64, int64, int64, error) {
		encoder, err := newWorkspaceStreamEncoder(ctx, output, task.Workspace.Policy)
		if err != nil || encoder.appendOwnedEntry("private", true, 0o700, 0, nil, workspaceStreamProviderOwner) != nil ||
			encoder.appendOwnedEntry("private/state", false, 0o600, 5, strings.NewReader("draft"), workspaceStreamProviderOwner) != nil ||
			encoder.appendOwnedEntry("role.txt", false, 0o600, 4, strings.NewReader("role"), workspaceStreamRoleOwner) != nil {
			return "", 0, 0, 0, errWorkspaceStreamInvalid
		}
		return encoder.finish()
	}
	store := &workspaceObjectFixture{Store: objectstoragetest.New()}
	result, err := snapshotWorkspace(t.Context(), store, task, encode)
	if err != nil {
		t.Fatal("prepare deterministic native checkpoint")
	}
	task = restoreTaskFromSnapshotFixture(t, task, result)
	task.Workspace.PVCUID = "pvc-fixture"
	return task, store
}

// TestWorkspaceWriterTwoUIDNative runs only in the explicit disposable container and checks real UID ownership and process joining.
// It does not admit a production Pod/PVC, contact live storage or change the owner lifecycle.
// TestWorkspaceWriterTwoUIDNative запускается только в explicit disposable container и проверяет actual UID ownership/process join.
// Он не допускает production Pod/PVC, не обращается к live storage и не меняет owner lifecycle.
func TestWorkspaceWriterTwoUIDNative(t *testing.T) {
	mode := os.Getenv("KODEX_WORKSPACE_WRITER_NATIVE")
	if mode == "" {
		t.Skip("requires explicit disposable writable two-UID fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if mode == "coordinator" {
		workspaceWriterNativeChildren(t, ctx)
		return
	}
	var capabilities [2]unix.CapUserData
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if unix.Capget(&header, &capabilities[0]) != nil || capabilities[0].Effective != 0 || capabilities[1].Effective != 0 ||
		capabilities[0].Permitted != 0 || capabilities[1].Permitted != 0 {
		t.Fatal("writable native consumer retained capabilities")
	}
	if mode == "prepare" {
		task, _ := workspaceWriterNativeCheckpoint(t)
		if os.Geteuid() != 10001 || PrepareWorkspaceRestore(ctx, task) != nil {
			t.Fatal("fixed UID native generation initializer failed")
		}
		return
	}
	root, err := unix.Open(workspaceWriterNativeTree, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open actual native restore tree")
	}
	defer unix.Close(root)
	if mode == "verify" {
		if os.Geteuid() != 10002 {
			t.Fatal("native provider verifier identity changed")
		}
		raw, err := os.ReadFile(workspaceWriterNativeTree + "/private/state")
		var file, directory unix.Stat_t
		if err != nil || string(raw) != "draft" || unix.Fstatat(root, "private/state", &file, unix.AT_SYMLINK_NOFOLLOW) != nil ||
			file.Uid != 10002 || file.Gid != 29000 || file.Mode&0o7777 != 0o600 ||
			unix.Fstatat(root, "private", &directory, unix.AT_SYMLINK_NOFOLLOW) != nil || directory.Uid != 10002 || directory.Mode&0o7777 != 0o700 {
			t.Fatal("provider content or original private ownership changed")
		}
		fd, err := unix.Openat(root, "role.txt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		if !errors.Is(err, unix.EACCES) {
			t.Fatal("provider read private role-owned content")
		}
		return
	}
	task, store := workspaceWriterNativeCheckpoint(t)
	identity := workspaceRestoreIdentity{actor: uint32(os.Geteuid()), role: 10001, provider: 10002, group: 29000}
	if mode == "provider" {
		if os.Geteuid() != 10002 {
			t.Fatal("native writer provider identity changed")
		}
		listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: workspaceWriterNativeSocket, Net: "unixpacket"})
		if err != nil {
			t.Fatal("listen for native writable peer")
		}
		defer listener.Close()
		if os.Chmod(workspaceWriterNativeSocket, 0o660) != nil {
			t.Fatal("seal native writable socket permissions")
		}
		stop := watchWorkspaceReaderCancel(ctx, listener)
		connection, err := listener.AcceptUnix()
		stop()
		if err != nil || serveWorkspaceWriterPeer(ctx, connection, root, task, identity) != nil {
			t.Fatal("fixed UID provider writable peer failed")
		}
		return
	}
	if mode != "main" || os.Geteuid() != 10001 {
		t.Fatal("native main writer identity changed")
	}
	workspaceWriterNativeMain(t, ctx, root, task, store, identity)
}

// workspaceWriterNativeMain restores through the real writable peer and waits for its coordinator-confirmed process exit before publishing.
// workspaceWriterNativeMain восстанавливает через actual writable peer и ждёт coordinator-confirmed process exit до publication.
func workspaceWriterNativeMain(t *testing.T, ctx context.Context, root int, task model.Task, store *workspaceObjectFixture, identity workspaceRestoreIdentity) {
	t.Helper()
	subpath, err := task.Workspace.Target.GenerationSubPath()
	if err != nil {
		t.Fatal("resolve native selected generation")
	}
	parent, err := unix.Open("/restore/"+path.Dir(subpath), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open private native generation")
	}
	defer unix.Close(parent)
	owner := securefile.OwnedRecordIdentity{UID: 10001, GID: 29000}
	publication, err := newWorkspaceRestorePublication(ctx, parent, task, "pvc-fixture", owner)
	if err != nil {
		t.Fatal("open native publisher")
	}
	defer publication.Close()
	var connection *net.UnixConn
	for ctx.Err() == nil {
		connection, err = net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: workspaceWriterNativeSocket, Net: "unixpacket"})
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err != nil || connection == nil {
		t.Fatal("connect native writer provider")
	}
	writer, err := newWorkspaceFDWriterPeer(ctx, connection, root, task, identity)
	if err != nil {
		t.Fatal("native authenticated writer handshake failed")
	}
	defer writer.Close()
	files, err := newWorkspaceRestoreFilesystem(ctx, root, task.Workspace.Policy, identity)
	if err != nil {
		t.Fatal("open role-owned native writer")
	}
	defer files.Close()
	joined := os.NewFile(3, "native-provider-process-join")
	defer joined.Close()
	result, err := restoreWorkspaceStream(ctx, store, task, workspaceRestoreDestination{role: files, provider: writer,
		commit: func(ctx context.Context) error {
			if writer.Close() != nil || files.Close() != nil {
				return errWorkspaceObjectUnconfirmed
			}
			var result [1]byte
			if _, err := io.ReadFull(joined, result[:]); err != nil || result[0] != 1 {
				return errWorkspaceObjectUnconfirmed
			}
			return publication.Commit(ctx, root)
		}})
	if err != nil || !result.Success || publication.Rejoin(ctx) != nil || store.puts != 2 || store.deletes != 0 {
		t.Fatal("native stream did not publish after provider process join")
	}
	var file unix.Stat_t
	raw, err := os.ReadFile(workspaceWriterNativeTree + "/role.txt")
	if err != nil || string(raw) != "role" || unix.Fstatat(root, "role.txt", &file, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		file.Uid != 10001 || file.Gid != 29000 || file.Mode&0o7777 != 0o600 {
		t.Fatal("native role file ownership or content changed")
	}
	fd, err := unix.Openat(root, "private/state", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if fd >= 0 {
		_ = unix.Close(fd)
	}
	if !errors.Is(err, unix.EACCES) {
		t.Fatal("main writer read provider-private content")
	}
}

// workspaceWriterNativeChildren starts capability-free consumers and signals the main only after Wait proves exact provider process exit.
// Its root coordinator possesses only identity-switch fixture capabilities, not data-read or filesystem override capabilities.
// workspaceWriterNativeChildren запускает capability-free consumers и уведомляет main только после Wait exact provider process.
// Root coordinator имеет только identity-switch fixture capabilities без data-read/filesystem override capabilities.
func workspaceWriterNativeChildren(t *testing.T, ctx context.Context) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Fatal("native writer coordinator must switch isolated child identities")
	}
	command := func(mode string, uid uint32) *exec.Cmd {
		child := exec.CommandContext(ctx, "/proc/self/exe", "-test.run=^TestWorkspaceWriterTwoUIDNative$", "-test.timeout=25s", "-test.v")
		child.Env = []string{"KODEX_WORKSPACE_WRITER_NATIVE=" + mode, "GOMAXPROCS=1", "GOMEMLIMIT=16MiB", "TMPDIR=/tmp"}
		child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: 29000}}
		child.WaitDelay = time.Second
		return child
	}
	readJoin, writeJoin, err := os.Pipe()
	if err != nil {
		t.Fatal("create native provider process-join channel")
	}
	defer readJoin.Close()
	defer writeJoin.Close()
	provider, main := command("provider", 10002), command("main", 10001)
	main.ExtraFiles = []*os.File{readJoin}
	var providerOutput, mainOutput bytes.Buffer
	provider.Stdout, provider.Stderr = &providerOutput, &providerOutput
	main.Stdout, main.Stderr = &mainOutput, &mainOutput
	if provider.Start() != nil {
		t.Fatal("start native writer provider")
	}
	if main.Start() != nil {
		_ = provider.Process.Kill()
		_ = provider.Wait()
		t.Fatal("start native writer main")
	}
	providerErr := provider.Wait()
	marker := byte(1)
	if providerErr != nil {
		marker = 0
	}
	_, joinErr := writeJoin.Write([]byte{marker})
	_ = writeJoin.Close()
	mainErr := main.Wait()
	if providerErr != nil || mainErr != nil || joinErr != nil {
		t.Logf("main: %s", mainOutput.Bytes())
		t.Logf("provider: %s", providerOutput.Bytes())
		t.Fatal("native writable consumers did not complete")
	}
	verification, err := command("verify", 10002).CombinedOutput()
	if err != nil {
		t.Logf("verify: %s", verification)
		t.Fatal("native provider readback failed")
	}
}
