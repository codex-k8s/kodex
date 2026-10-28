package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// workspacePeerSocketFixture keeps real Unix paths short and explicitly sets the shared group under the current test identity.
// workspacePeerSocketFixture сохраняет короткие actual Unix paths и явно задаёт shared group текущей test identity.
func workspacePeerSocketFixture(t *testing.T) (string, uint32, uint32) {
	t.Helper()
	directory, err := os.MkdirTemp("", "kps-")
	if err != nil {
		t.Fatal("prepare private socket fixture")
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if os.Chown(directory, -1, os.Getegid()) != nil || unix.Chmod(directory, 0o2770) != nil {
		t.Fatal("prepare shared socket fixture")
	}
	return directory, uint32(os.Geteuid()), uint32(os.Getegid())
}

// TestWorkspacePeerSocketPublishesExactEndpointAndCleansOwnInode exercises actual listen/dial/accept for each independent protocol endpoint.
// TestWorkspacePeerSocketPublishesExactEndpointAndCleansOwnInode проверяет actual listen/dial/accept обоих независимых protocol endpoints.
func TestWorkspacePeerSocketPublishesExactEndpointAndCleansOwnInode(t *testing.T) {
	for _, writable := range []bool{false, true} {
		directory, uid, gid := workspacePeerSocketFixture(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		listener, err := newWorkspacePeerListener(ctx, directory, writable, uid, uid, gid)
		if err != nil {
			t.Fatal("publish real task-private socket")
		}
		t.Cleanup(func() { _ = listener.Close() })
		accepted := make(chan error, 1)
		go func() {
			connection, err := listener.Accept(ctx)
			if err == nil {
				err = connection.Close()
			}
			accepted <- err
		}()
		client, err := dialWorkspacePeer(ctx, directory, writable, uid, uid, gid)
		if err != nil {
			cancel()
			t.Fatal("dial exact task-private socket")
		}
		_ = client.Close()
		select {
		case err := <-accepted:
			if err != nil {
				t.Fatal("accept exact peer")
			}
		case <-ctx.Done():
			t.Fatal("bounded accept did not join")
		}
		if listener.Close() != nil || listener.Close() != nil {
			t.Fatal("close owned IPC listener")
		}
		for _, name := range []string{"reader.sock", "writer.sock", ".reader-preparing.sock", ".writer-preparing.sock"} {
			if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				t.Fatal("owned IPC endpoint remained after clean join")
			}
		}
	}
}

// TestWorkspacePeerSocketPreservesExistingAndReplacedEntries forbids replacement, permission repair and unrelated unlink on failure.
// TestWorkspacePeerSocketPreservesExistingAndReplacedEntries запрещает replacement, permission repair и unrelated unlink при отказе.
func TestWorkspacePeerSocketPreservesExistingAndReplacedEntries(t *testing.T) {
	for _, fault := range []string{"existing", "preparing", "replacement", "parent-mode", "parent-owner", "parent-group", "provider-identity"} {
		t.Run(fault, func(t *testing.T) {
			directory, uid, gid := workspacePeerSocketFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			observed := directory
			role, provider, group := uid, uid, gid
			switch fault {
			case "existing", "preparing", "replacement":
				name := "writer.sock"
				if fault == "preparing" {
					name = ".writer-preparing.sock"
				}
				observed = filepath.Join(directory, name)
				if fault == "replacement" {
					listener, err := newWorkspacePeerListener(ctx, directory, true, role, provider, group)
					if err != nil || os.Rename(observed, filepath.Join(directory, "retained.sock")) != nil {
						t.Fatal("prepare replaced endpoint")
					}
					if os.WriteFile(observed, []byte("retain"), 0o600) != nil || listener.Close() == nil || listener.Close() == nil {
						t.Fatal("replaced socket was removed or failed close turned successful")
					}
					if raw, err := os.ReadFile(observed); err != nil || string(raw) != "retain" {
						t.Fatal("unrelated replacement was damaged")
					}
					return
				}
				if os.WriteFile(observed, []byte("retain"), 0o600) != nil {
					t.Fatal("prepare occupied socket name")
				}
			case "parent-mode":
				if os.Chmod(directory, 0o777) != nil {
					t.Fatal("prepare unsafe parent mode")
				}
			case "parent-owner":
				role++
			case "parent-group":
				group++
			case "provider-identity":
				provider++
			}
			before, err := os.Lstat(observed)
			if err != nil {
				t.Fatal("observe rejected socket state")
			}
			listener, err := newWorkspacePeerListener(ctx, directory, true, role, provider, group)
			if err == nil || listener != nil || strings.Contains(err.Error(), directory) {
				t.Fatal("unsafe socket creation was accepted or disclosed a path")
			}
			after, err := os.Lstat(observed)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Fatal("rejected socket state was repaired or replaced")
			}
		})
	}
}

// TestWorkspacePeerSocketDialRejectsUnsafeEndpoint closes mode, alias and foreign owner boundaries before any protocol frame.
// TestWorkspacePeerSocketDialRejectsUnsafeEndpoint проверяет mode, alias и foreign owner boundaries до protocol frame.
func TestWorkspacePeerSocketDialRejectsUnsafeEndpoint(t *testing.T) {
	for _, fault := range []string{"mode", "symlink", "owner", "group", "other-protocol"} {
		t.Run(fault, func(t *testing.T) {
			directory, uid, gid := workspacePeerSocketFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
			defer cancel()
			listener, err := newWorkspacePeerListener(ctx, directory, true, uid, uid, gid)
			if err != nil {
				t.Fatal("prepare socket for rejected dial")
			}
			t.Cleanup(func() { _ = listener.Close() })
			writable := true
			peer, group := uid, gid
			switch fault {
			case "mode":
				if os.Chmod(filepath.Join(directory, "writer.sock"), 0o666) != nil {
					t.Fatal("prepare socket mode drift")
				}
			case "symlink":
				if os.Rename(filepath.Join(directory, "writer.sock"), filepath.Join(directory, "retained.sock")) != nil ||
					os.Symlink("retained.sock", filepath.Join(directory, "writer.sock")) != nil {
					t.Fatal("prepare linked socket")
				}
			case "owner":
				peer++
			case "group":
				group++
			case "other-protocol":
				writable = false
			}
			if connection, err := dialWorkspacePeer(ctx, directory, writable, uid, peer, group); err == nil || connection != nil {
				t.Fatal("unsafe or alternate protocol socket accepted")
			}
		})
	}
}
