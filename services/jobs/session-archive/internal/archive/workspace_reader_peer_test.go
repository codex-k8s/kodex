package archive

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// workspaceReaderPeerFixture starts one actual bounded peer using synthetic same-UID test ownership.
// It joins the server on cleanup; it is not the required production two-UID/native acceptance.
// workspaceReaderPeerFixture запускает actual bounded peer с synthetic same-UID test ownership.
// Cleanup завершает server; это не обязательная production two-UID/native acceptance.
func workspaceReaderPeerFixture(t *testing.T, mainUID, sourceUID uint32) (*net.UnixConn, workspaceReaderRequest, unix.Stat_t, context.CancelFunc, <-chan error) {
	t.Helper()
	root, rootFD := workspaceTreeFixture(t)
	if err := os.WriteFile(filepath.Join(root, "private"), []byte("source600"), 0o600); err != nil {
		t.Fatal(err)
	}
	var source, rootStat unix.Stat_t
	if unix.Fstatat(rootFD, "private", &source, unix.AT_SYMLINK_NOFOLLOW) != nil || unix.Fstat(rootFD, &rootStat) != nil {
		t.Fatal("stat isolated reader fixture")
	}
	client, server := workspaceReaderConnections(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)
	request := workspaceReaderRequest{taskSHA256: [32]byte{1}, device: uint64(rootStat.Dev), inode: rootStat.Ino}
	terminal := make(chan error, 1)
	go func() {
		terminal <- serveWorkspaceReaderPeer(ctx, server, rootFD, request.taskSHA256, mainUID, sourceUID, uint32(os.Getgid()), 4)
	}()
	t.Cleanup(func() {
		cancel()
		_ = client.Close()
		_ = server.Close()
		select {
		case <-terminal:
		case <-time.After(2 * time.Second):
			t.Error("owned reader did not join after cancellation")
		}
	})
	return client, request, source, cancel, terminal
}

// TestWorkspaceReaderPeerRequiresHandshakeAndExactTaskRoot exercises the actual HELLO and FD consumers.
// A successful peer can read only the already observed owned inode after the complete binding handshake.
// TestWorkspaceReaderPeerRequiresHandshakeAndExactTaskRoot проверяет actual HELLO и FD consumers.
// Успешный peer читает только observed owned inode после complete binding handshake.
func TestWorkspaceReaderPeerRequiresHandshakeAndExactTaskRoot(t *testing.T) {
	uid := uint32(os.Geteuid())
	client, request, expected, _, _ := workspaceReaderPeerFixture(t, uid, uid)
	if err := workspaceReaderHello(client, request, uid); err != nil {
		t.Fatal("exact task/root handshake rejected")
	}
	request.path = "private"
	file, err := openWorkspaceReaderSource(client, request, expected, uid, uid, uint32(os.Getgid()))
	if err != nil {
		t.Fatal("exact regular source descriptor rejected")
	}
	raw, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(raw) != "source600" {
		t.Fatal("source descriptor changed private bytes")
	}
}

// TestWorkspaceReaderPeerRejectsScopePeerOwnerAndCancelledSession blocks every mismatched source context.
// All helpers join through fixture cleanup, including a receiver cancelled while waiting for another request.
// TestWorkspaceReaderPeerRejectsScopePeerOwnerAndCancelledSession отклоняет mismatched source context.
// Fixture cleanup завершает helpers, включая receiver при отмене ожидания следующего request.
func TestWorkspaceReaderPeerRejectsScopePeerOwnerAndCancelledSession(t *testing.T) {
	uid := uint32(os.Geteuid())
	for _, name := range []string{"task", "root", "peer", "server_peer", "before_hello", "owner", "cancel", "path"} {
		t.Run(name, func(t *testing.T) {
			mainUID, sourceUID := uid, uid
			if name == "peer" {
				mainUID++
			}
			if name == "owner" {
				sourceUID++
			}
			client, request, expected, cancel, _ := workspaceReaderPeerFixture(t, mainUID, sourceUID)
			if name == "task" {
				request.taskSHA256[0]++
			}
			if name == "root" {
				request.inode++
			}
			if name == "before_hello" {
				request.path = "private"
				file, err := openWorkspaceReaderSource(client, request, expected, uid, uid, uint32(os.Getgid()))
				if file != nil {
					_ = file.Close()
				}
				if err == nil || file != nil {
					t.Fatal("descriptor granted before task/root handshake")
				}
				return
			}
			expectedPeer := uid
			if name == "server_peer" {
				expectedPeer++
			}
			err := workspaceReaderHello(client, request, expectedPeer)
			if name == "task" || name == "root" || name == "peer" || name == "server_peer" {
				if err == nil {
					t.Fatal("mismatched peer/task/root passed the handshake")
				}
				return
			}
			if err != nil {
				t.Fatal("isolated valid handshake rejected")
			}
			request.path = "private"
			if name == "cancel" {
				cancel()
			}
			if name == "path" {
				request.path = "../escape"
			}
			file, err := openWorkspaceReaderSource(client, request, expected, uid, uid, uint32(os.Getgid()))
			if err == nil || file != nil {
				if file != nil {
					_ = file.Close()
				}
				t.Fatal("rejected scope or ownership yielded a descriptor")
			}
		})
	}
}
