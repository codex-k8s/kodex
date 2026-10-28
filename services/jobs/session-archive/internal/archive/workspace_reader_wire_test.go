package archive

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestWorkspaceReaderFramePreserves4096UTF8Path checks actual private wire bytes without JSON escaping expansion.
// Unknown versions, excess bytes and traversals are rejected before a source descriptor can be opened.
// TestWorkspaceReaderFramePreserves4096UTF8Path проверяет private wire bytes без JSON escaping expansion.
// Unknown versions, excess bytes и traversal отклоняются до source descriptor open.
func TestWorkspaceReaderFramePreserves4096UTF8Path(t *testing.T) {
	value := workspaceReaderRequest{taskSHA256: [32]byte{1}, device: 42, inode: 99,
		path: strings.Repeat(strings.Repeat("я", 120)+"/", 16) + strings.Repeat("я", 120)}
	raw, err := encodeWorkspaceReaderRequest(value)
	if err != nil || len(raw) != workspaceReaderHeaderBytes+4096 {
		t.Fatal("exact UTF-8 path exceeded the closed binary frame")
	}
	again, err := decodeWorkspaceReaderRequest(raw)
	if err != nil || again != value {
		t.Fatal("private wire changed checkpoint or UTF-8 path bytes")
	}
	wrongVersion := append([]byte(nil), raw...)
	wrongVersion[0] = 'X'
	for _, invalid := range [][]byte{wrongVersion, append(append([]byte(nil), raw...), 0), raw[:workspaceReaderHeaderBytes], raw[:len(raw)-1]} {
		if _, err := decodeWorkspaceReaderRequest(invalid); err == nil {
			t.Fatal("malformed private frame accepted")
		}
	}
	for _, path := range []string{"../escape", "/absolute", "a/../escape", "a\x00file", value.path + "x"} {
		value.path = path
		if _, err := encodeWorkspaceReaderRequest(value); err == nil {
			t.Fatal("unsafe source path accepted")
		}
	}
}

// workspaceReaderConnections creates an actual Unixpacket pair and joins its bounded accept helper.
// No fixture UID is treated as a production peer; production factories retain their fixed10001/10002 IDs.
// workspaceReaderConnections создаёт actual Unixpacket pair и завершает bounded accept helper.
// Fixture UID не является production peer; production factories сохраняют fixed10001/10002 IDs.
func workspaceReaderConnections(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	directory, err := os.MkdirTemp("", "kwr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	address := &net.UnixAddr{Name: filepath.Join(directory, "reader.sock"), Net: "unixpacket"}
	listener, err := net.ListenUnix("unixpacket", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan *net.UnixConn, 1)
	errors := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			errors <- err
			return
		}
		accepted <- connection
	}()
	client, err := net.DialUnix("unixpacket", nil, address)
	if err != nil {
		_ = listener.Close()
		select {
		case late := <-accepted:
			_ = late.Close()
		case <-errors:
		case <-time.After(2 * time.Second):
			t.Error("Unixpacket accept did not join after dial failure")
		}
		t.Fatal(err)
	}
	var server *net.UnixConn
	select {
	case server = <-accepted:
	case err := <-errors:
		_ = client.Close()
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		_ = client.Close()
		_ = listener.Close()
		select {
		case late := <-accepted:
			_ = late.Close()
		case <-errors:
		}
		t.Fatal("Unixpacket accept exceeded fixture budget")
	}
	deadline := time.Now().Add(2 * time.Second)
	if client.SetDeadline(deadline) != nil || server.SetDeadline(deadline) != nil {
		_ = client.Close()
		_ = server.Close()
		t.Fatal("set bounded fixture deadline")
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

// workspaceReaderInodeDescriptors counts actual open references to one inode to catch rejected SCM_RIGHTS leaks.
// It emits no proc paths or filesystem identities and performs no external API access.
// workspaceReaderInodeDescriptors считает actual open references одного inode для проверки SCM_RIGHTS leaks.
// Proc paths/filesystem identities не выводятся; external API не используется.
func workspaceReaderInodeDescriptors(t *testing.T, expected unix.Stat_t) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		var info unix.Stat_t
		if err == nil && unix.Fstat(fd, &info) == nil && info.Dev == expected.Dev && info.Ino == expected.Ino {
			count++
		}
	}
	return count
}

// TestWorkspaceReaderReceivesOnlyOneExactReadonlyCLOEXECFile exercises kernel credentials and SCM_RIGHTS.
// Wrong inode, writable descriptors, extra descriptors and malformed responses close every received FD.
// TestWorkspaceReaderReceivesOnlyOneExactReadonlyCLOEXECFile проверяет kernel credentials и SCM_RIGHTS.
// Wrong inode, writable/extra descriptors и malformed responses закрывают все received FD.
func TestWorkspaceReaderReceivesOnlyOneExactReadonlyCLOEXECFile(t *testing.T) {
	for _, name := range []string{"valid", "inode", "writable", "extra", "status", "truncated"} {
		t.Run(name, func(t *testing.T) {
			client, server := workspaceReaderConnections(t)
			uid, err := workspaceReaderPeerUID(client)
			if err != nil || uid != uint32(os.Geteuid()) {
				t.Fatal("peer did not come from kernel credentials")
			}
			path := filepath.Join(t.TempDir(), "private-source")
			if err := os.WriteFile(path, []byte("exact600"), 0o600); err != nil {
				t.Fatal(err)
			}
			mode := os.O_RDONLY
			if name == "writable" {
				mode = os.O_RDWR
			}
			file, err := os.OpenFile(path, mode, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			var expected unix.Stat_t
			if unix.Fstat(int(file.Fd()), &expected) != nil {
				t.Fatal("stat fixture source")
			}
			original := expected
			if name == "inode" {
				expected.Ino++
			}
			rights := unix.UnixRights(int(file.Fd()))
			if name == "extra" {
				rights = unix.UnixRights(int(file.Fd()), int(file.Fd()))
			}
			payload := []byte{1}
			if name == "status" {
				payload[0] = 0
			}
			if name == "truncated" {
				payload = []byte{1, 1, 1}
			}
			if _, _, err := server.WriteMsgUnix(payload, rights, nil); err != nil {
				t.Fatal(err)
			}
			received, err := receiveWorkspaceReaderFile(client, expected)
			if name == "valid" {
				if err != nil || received == nil {
					t.Fatal("exact read-only descriptor was rejected")
				}
				actual, readErr := io.ReadAll(received)
				closeErr := received.Close()
				if readErr != nil || closeErr != nil || !bytes.Equal(actual, []byte("exact600")) {
					t.Fatal("received descriptor changed private source bytes")
				}
			} else if err == nil || received != nil {
				t.Fatal("unsafe descriptor escaped validation")
			}
			if workspaceReaderInodeDescriptors(t, original) != 1 {
				t.Fatal("received descriptor leaked after consumption or rejection")
			}
		})
	}
}
