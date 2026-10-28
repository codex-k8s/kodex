package archive

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"golang.org/x/sys/unix"
)

// TestWorkspaceWriterFrameClosedUnion preserves exact bounded fields while rejecting snapshot frames and invalid command combinations.
// TestWorkspaceWriterFrameClosedUnion сохраняет exact bounded fields и отклоняет snapshot frames/invalid command combinations.
func TestWorkspaceWriterFrameClosedUnion(t *testing.T) {
	base := workspaceWriterRequest{taskSHA256: [32]byte{1}, device: 42, inode: 99}
	for _, operation := range []byte{workspaceWriterHello, workspaceWriterDirectory, workspaceWriterFile, workspaceWriterSeal, workspaceWriterComplete, workspaceWriterFinish} {
		request := base
		request.operation = operation
		if operation == workspaceWriterDirectory || operation == workspaceWriterFile || operation == workspaceWriterSeal {
			request.path = strings.Repeat(strings.Repeat("я", 120)+"/", 16) + strings.Repeat("я", 120)
		}
		if operation == workspaceWriterFile {
			request.mode, request.size = 0o755, 123
		}
		raw, err := encodeWorkspaceWriterRequest(request)
		if err != nil {
			t.Fatal("valid command did not encode")
		}
		again, err := decodeWorkspaceWriterRequest(raw)
		if err != nil || again != request {
			t.Fatal("command changed task, inode, UTF-8 path or file parameters")
		}
		for _, bad := range [][]byte{raw[:66], append(append([]byte(nil), raw...), 0), append([]byte("KWR1"), raw[4:]...)} {
			if _, err := decodeWorkspaceWriterRequest(bad); err == nil {
				t.Fatal("alternate or malformed protocol accepted")
			}
		}
	}
	for _, invalid := range []workspaceWriterRequest{
		{operation: workspaceWriterHello},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: 99},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterHello, path: "file"},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterComplete, size: 1},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterFinish, mode: 0o600},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterDirectory, path: "../escape"},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterDirectory, path: "file", size: 1},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterFile, path: "/absolute"},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterFile, path: "file", size: -1},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterFile, path: "file", mode: 0o4755},
		{taskSHA256: base.taskSHA256, inode: base.inode, operation: workspaceWriterSeal, path: "file", size: 1},
	} {
		if _, err := encodeWorkspaceWriterRequest(invalid); err == nil {
			t.Fatal("invalid command acquired a wire representation")
		}
	}
}

// workspaceWriterPeerFixture starts the actual peer with the test UID and provides an explicit bounded join.
// Distinct fixed production UIDs still require the separate native container contour.
// workspaceWriterPeerFixture запускает actual peer с тестовым UID и возвращает explicit bounded join.
// Разные fixed production UID по-прежнему требуют отдельного native container contour.
func workspaceWriterPeerFixture(t *testing.T, fixture workspaceRestorePublicationFixture) (*workspaceFDWriter, func() error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	client, server := workspaceReaderConnections(t)
	identity := workspaceRestoreIdentity{actor: fixture.owner.UID, role: fixture.owner.UID, provider: fixture.owner.UID, group: fixture.owner.GID}
	completed := make(chan error, 1)
	go func() { completed <- serveWorkspaceWriterPeer(ctx, server, fixture.tree, fixture.task, identity) }()
	joined, result := false, error(nil)
	join := func() error {
		if !joined {
			select {
			case result = <-completed:
				joined = true
			case <-time.After(time.Second):
				t.Fatal("provider peer did not join")
			}
		}
		return result
	}
	t.Cleanup(func() { cancel(); _ = client.Close(); _ = server.Close(); _ = join() })
	writer, err := newWorkspaceFDWriterPeer(ctx, client, fixture.tree, fixture.task, identity)
	if err != nil {
		t.Fatal("actual writable peer handshake failed")
	}
	t.Cleanup(func() { _ = writer.Close() })
	return writer, join
}

// TestWorkspaceWriterRestoresThroughVerifiedStreamAndPublisher proves real SCM_RIGHTS writes, explicit completion and peer join before publication.
// The fixture shares one unprivileged UID for both logical owners; it does not establish production UID separation.
// TestWorkspaceWriterRestoresThroughVerifiedStreamAndPublisher проверяет SCM_RIGHTS writes, explicit completion и peer join до publication.
// Fixture использует один непривилегированный UID для logical owners и не доказывает production UID separation.
func TestWorkspaceWriterRestoresThroughVerifiedStreamAndPublisher(t *testing.T) {
	encode := func(ctx context.Context, destination io.Writer, task model.Task) (string, int64, int64, int64, error) {
		encoder, err := newWorkspaceStreamEncoder(ctx, destination, task.Workspace.Policy)
		if err != nil || encoder.appendOwnedEntry("private", true, 0o700, 0, nil, workspaceStreamProviderOwner) != nil ||
			encoder.appendOwnedEntry("private/state", false, 0o600, 5, strings.NewReader("draft"), workspaceStreamProviderOwner) != nil ||
			encoder.appendOwnedEntry("readme.md", false, 0o644, 6, strings.NewReader("readme"), workspaceStreamRoleOwner) != nil {
			return "", 0, 0, 0, errWorkspaceStreamInvalid
		}
		return encoder.finish()
	}
	fixture := newWorkspaceRestorePublicationFixture(t, encode)
	writer, join := workspaceWriterPeerFixture(t, fixture)
	committed := false
	result, err := restoreWorkspaceStream(t.Context(), fixture.store, fixture.task, workspaceRestoreDestination{
		role: fixture.files, provider: writer, commit: func(ctx context.Context) error {
			if writer.Close() != nil || join() != nil || fixture.files.Close() != nil {
				return errWorkspaceObjectUnconfirmed
			}
			if fixture.publication.Commit(ctx, fixture.tree) != nil {
				return errWorkspaceObjectUnconfirmed
			}
			committed = true
			return nil
		},
	})
	if err != nil || !result.Success || !committed || fixture.store.puts != 2 || fixture.store.deletes != 0 {
		t.Fatal("writable peer failed to publish only the verified stream")
	}
	for name, content := range map[string]string{"private/state": "draft", "readme.md": "readme"} {
		raw, err := os.ReadFile(filepath.Join(fixture.root, "tree", name))
		if err != nil || string(raw) != content {
			t.Fatal("peer changed restored content")
		}
	}
	info, err := os.Stat(filepath.Join(fixture.root, "tree", "private"))
	if err != nil || info.Mode().Perm() != 0o700 || fixture.retry(t).Rejoin(t.Context()) != nil {
		t.Fatal("peer did not seal private permissions or preserve publication evidence")
	}
}

// TestWorkspaceWriterPartialFilesNeverFinish checks bounded writes, aborted transfer and cancellation with an actual joined peer.
// TestWorkspaceWriterPartialFilesNeverFinish проверяет bounded writes, aborted transfer и cancellation с actual joined peer.
func TestWorkspaceWriterPartialFilesNeverFinish(t *testing.T) {
	for _, failure := range []string{"short", "overrun", "pending-close", "unexpected-command"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newWorkspaceRestorePublicationFixture(t)
			writer, join := workspaceWriterPeerFixture(t, fixture)
			file, err := writer.File(t.Context(), "partial", 0o755, 2)
			if err != nil {
				t.Fatal("create transferred file")
			}
			switch failure {
			case "overrun":
				if n, err := file.Write([]byte("too")); err == nil || n != 0 {
					t.Fatal("unbounded transfer was acknowledged")
				}
			case "unexpected-command":
				if writer.Directory(t.Context(), "unexpected") == nil {
					t.Fatal("pending transfer allowed another command")
				}
			default:
				if n, err := file.Write([]byte("o")); err != nil || n != 1 {
					t.Fatal("prepare actual partial data")
				}
			}
			if failure != "pending-close" && file.Close() == nil || writer.Close() == nil || join() == nil {
				t.Fatal("partial or disconnected transfer completed successfully")
			}
			if writer.Close() == nil {
				t.Fatal("repeated close erased an unconfirmed provider result")
			}
			info, err := os.Stat(filepath.Join(fixture.root, workspaceRestoreStagingTree, "partial"))
			if err != nil || info.Mode().Perm() != 0o600 || fixture.publication.Rejoin(t.Context()) == nil {
				t.Fatal("partial evidence disappeared, was sealed or became executable work")
			}
		})
	}
}

// TestWorkspaceWriterPeerRejectsForeignBindingAndCommandOrder proves kernel identity, exact task/root and explicit completion gates.
// TestWorkspaceWriterPeerRejectsForeignBindingAndCommandOrder проверяет kernel identity, exact task/root и explicit completion gates.
func TestWorkspaceWriterPeerRejectsForeignBindingAndCommandOrder(t *testing.T) {
	for _, failure := range []string{"wrong-peer", "wrong-task", "wrong-root", "no-hello", "repeat-hello", "request-rights", "no-file", "pending-finish", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newWorkspaceRestorePublicationFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			client, server := workspaceReaderConnections(t)
			identity := workspaceRestoreIdentity{actor: fixture.owner.UID, role: fixture.owner.UID, provider: fixture.owner.UID, group: fixture.owner.GID}
			if failure == "wrong-peer" {
				identity.role++
			}
			completed := make(chan error, 1)
			go func() { completed <- serveWorkspaceWriterPeer(ctx, server, fixture.tree, fixture.task, identity) }()
			digest, err := workspaceWriterTaskDigest(fixture.task)
			var root unix.Stat_t
			if err != nil || unix.Fstat(fixture.tree, &root) != nil || client.SetDeadline(time.Now().Add(2*time.Second)) != nil {
				t.Fatal("prepare peer binding")
			}
			request := workspaceWriterRequest{taskSHA256: digest, device: uint64(root.Dev), inode: root.Ino, operation: workspaceWriterHello}
			if failure == "wrong-task" {
				request.taskSHA256[0] ^= 1
			} else if failure == "wrong-root" {
				request.inode++
			} else if failure == "no-hello" {
				request.operation, request.path = workspaceWriterDirectory, "unexpected"
			}
			if failure == "request-rights" {
				raw, err := encodeWorkspaceWriterRequest(request)
				if err != nil {
					t.Fatal("encode transfer rejection fixture")
				}
				_, _, _ = client.WriteMsgUnix(raw, unix.UnixRights(fixture.tree), nil)
			} else {
				_ = sendWorkspaceWriterRequest(client, request)
			}
			if failure == "repeat-hello" || failure == "no-file" || failure == "pending-finish" || failure == "cancel" {
				if receiveWorkspaceReaderHello(client) != nil {
					t.Fatal("valid handshake failed before negative command")
				}
				switch failure {
				case "cancel":
					cancel()
				case "no-file":
					request.operation = workspaceWriterComplete
					_ = sendWorkspaceWriterRequest(client, request)
				case "pending-finish":
					request.operation, request.path, request.mode, request.size = workspaceWriterFile, "partial", 0o755, 2
					if sendWorkspaceWriterRequest(client, request) != nil {
						t.Fatal("request partial transfer")
					}
					fd, err := receiveWorkspaceWriterFile(client, fixture.tree, "partial", identity)
					if err != nil {
						t.Fatal("receive transferred file")
					}
					_, _ = fd.Write([]byte("ok"))
					_ = fd.Close()
					request.operation, request.path, request.mode, request.size = workspaceWriterFinish, "", 0, 0
					_ = sendWorkspaceWriterRequest(client, request)
				default:
					_ = sendWorkspaceWriterRequest(client, request)
				}
			}
			if receiveWorkspaceReaderHello(client) == nil {
				t.Error("invalid binding or command received success acknowledgement")
			}
			_ = client.Close()
			cancel()
			select {
			case err := <-completed:
				if err == nil {
					t.Error("invalid peer completed successfully")
				}
			case <-time.After(time.Second):
				t.Fatal("rejected peer did not join")
			}
			if fixture.publication.Rejoin(t.Context()) == nil {
				t.Fatal("rejected peer published executable data")
			}
		})
	}
}

// TestWorkspaceWriterReceiverRejectsExtraCapabilities validates actual ancillary descriptor ownership and access modes.
// TestWorkspaceWriterReceiverRejectsExtraCapabilities проверяет actual ancillary descriptor ownership и access modes.
func TestWorkspaceWriterReceiverRejectsExtraCapabilities(t *testing.T) {
	for _, failure := range []string{"read-only", "read-write", "directory", "extra-fd", "wrong-inode", "wrong-owner", "not-empty"} {
		t.Run(failure, func(t *testing.T) {
			root, files, rootFD := workspaceRestoreFilesystemFixture(t, t.Context())
			client, server := workspaceReaderConnections(t)
			if os.WriteFile(filepath.Join(root, "file"), nil, 0o600) != nil {
				t.Fatal("prepare descriptor receiver fixture")
			}
			flags := unix.O_WRONLY
			switch failure {
			case "read-only":
				flags = unix.O_RDONLY
			case "read-write":
				flags = unix.O_RDWR
			case "not-empty":
				if os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0o600) != nil {
					t.Fatal("prepare nonempty inode")
				}
			case "wrong-inode":
				if os.WriteFile(filepath.Join(root, "other"), nil, 0o600) != nil {
					t.Fatal("prepare unrelated inode")
				}
			}
			name := "file"
			if failure == "directory" {
				name, flags = ".", unix.O_RDONLY|unix.O_DIRECTORY
			} else if failure == "wrong-inode" {
				name = "other"
			}
			fd, err := unix.Openat(rootFD, name, flags|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal("open supplied descriptor")
			}
			defer unix.Close(fd)
			rights := unix.UnixRights(fd)
			if failure == "extra-fd" {
				rights = unix.UnixRights(fd, fd)
			}
			if _, _, err := server.WriteMsgUnix([]byte{1}, rights, nil); err != nil {
				t.Fatal("send rejected descriptor fixture")
			}
			identity := files.identity
			if failure == "wrong-owner" {
				identity.provider++
			}
			if file, err := receiveWorkspaceWriterFile(client, rootFD, "file", identity); err == nil || file != nil {
				t.Fatal("unexpected read/write/owner/inode capability accepted")
			}
		})
	}
}

// TestWorkspaceWriterTaskModeRejectsReadOnlyWork prevents a reconciliation or snapshot task from obtaining a writer handshake digest.
// TestWorkspaceWriterTaskModeRejectsReadOnlyWork запрещает reconciliation/snapshot task получать writer handshake digest.
func TestWorkspaceWriterTaskModeRejectsReadOnlyWork(t *testing.T) {
	fixture := newWorkspaceRestorePublicationFixture(t)
	for _, mode := range []string{"snapshot", "reconcile"} {
		t.Run(mode, func(t *testing.T) {
			task := fixture.task
			body := *task.Workspace
			task.Workspace = &body
			if mode == "snapshot" {
				task.Kind = "SNAPSHOT"
			} else {
				task.Attempt, body.ReconcileOnly = 2, true
			}
			if _, err := workspaceWriterTaskDigest(task); err == nil {
				t.Fatal("read-only task acquired writable protocol binding")
			}
		})
	}
}

// TestWorkspaceWriterJoinRequiresCleanEOF rejects post-FINISH data and unexpected capabilities before publication may proceed.
// TestWorkspaceWriterJoinRequiresCleanEOF отклоняет post-FINISH data и unexpected capabilities до разрешения publication.
func TestWorkspaceWriterJoinRequiresCleanEOF(t *testing.T) {
	for _, boundary := range []string{"eof", "trailing", "rights", "closed"} {
		t.Run(boundary, func(t *testing.T) {
			client, server := workspaceReaderConnections(t)
			switch boundary {
			case "trailing":
				if _, _, err := server.WriteMsgUnix([]byte{2}, nil, nil); err != nil {
					t.Fatal("send unexpected trailing frame")
				}
			case "rights":
				file, err := os.CreateTemp(t.TempDir(), "unexpected-capability")
				if err != nil {
					t.Fatal("prepare unexpected descriptor")
				}
				defer file.Close()
				if _, _, err := server.WriteMsgUnix([]byte{1}, unix.UnixRights(int(file.Fd())), nil); err != nil {
					t.Fatal("send unexpected descriptor")
				}
			case "closed":
				if client.Close() != nil {
					t.Fatal("close receiver before peer join")
				}
			}
			if server.Close() != nil || (receiveWorkspaceWriterJoin(client) == nil) != (boundary == "eof") {
				t.Fatal("join confused a data frame, capability or local closure with clean EOF")
			}
		})
	}
}
