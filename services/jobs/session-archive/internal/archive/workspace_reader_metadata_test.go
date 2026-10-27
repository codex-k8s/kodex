package archive

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestWorkspaceReaderTypedOperationsKeepFileWireClosed preserves KWR1 bytes and requires distinct magic for directory and metadata observations.
// TestWorkspaceReaderTypedOperationsKeepFileWireClosed сохраняет KWR1 bytes и требует отдельный magic для directory/metadata observations.
func TestWorkspaceReaderTypedOperationsKeepFileWireClosed(t *testing.T) {
	for kind, magic := range map[workspaceReaderOperation]string{workspaceReaderFile: "KWR1", workspaceReaderDirectory: "KWD1", workspaceReaderMetadata: "KWS1"} {
		value := workspaceReaderRequest{taskSHA256: [32]byte{1}, device: 42, inode: 99, path: "private/state", kind: kind}
		raw, err := encodeWorkspaceReaderRequest(value)
		if err != nil || string(raw[:4]) != magic {
			t.Fatal("typed operation changed the closed wire version")
		}
		again, err := decodeWorkspaceReaderRequest(raw)
		if err != nil || again != value {
			t.Fatal("typed operation changed the pinned task or path")
		}
		value.hello = true
		if _, err := encodeWorkspaceReaderRequest(value); err == nil {
			t.Fatal("entry lookup became a new handshake")
		}
	}
	if _, err := encodeWorkspaceReaderRequest(workspaceReaderRequest{taskSHA256: [32]byte{1}, inode: 99, path: "file", kind: 3}); err == nil {
		t.Fatal("unknown typed operation accepted")
	}
}

// TestWorkspaceReaderMetadataIsBoundedAndRetainsKernelIdentity checks actual stat commitments and excludes unsupported inode metadata.
// TestWorkspaceReaderMetadataIsBoundedAndRetainsKernelIdentity проверяет actual stat commitments и исключает unsupported inode metadata.
func TestWorkspaceReaderMetadataIsBoundedAndRetainsKernelIdentity(t *testing.T) {
	root, rootFD := workspaceTreeFixture(t)
	if os.WriteFile(filepath.Join(root, "file"), []byte("exact"), 0o600) != nil {
		t.Fatal("prepare metadata source")
	}
	var info unix.Stat_t
	if unix.Fstatat(rootFD, "file", &info, unix.AT_SYMLINK_NOFOLLOW) != nil {
		t.Fatal("stat metadata source")
	}
	raw, err := encodeWorkspaceReaderMetadata(info)
	if err != nil || len(raw) != workspaceReaderMetadataBytes {
		t.Fatal("metadata escaped its fixed frame")
	}
	again, err := decodeWorkspaceReaderMetadata(raw)
	if err != nil || !workspaceTreeIdentityEqual(info, again) {
		t.Fatal("kernel identity changed on metadata wire")
	}
	for name, mutate := range map[string]func(*unix.Stat_t){
		"link":                func(s *unix.Stat_t) { s.Nlink = 2 },
		"symlink":             func(s *unix.Stat_t) { s.Mode = unix.S_IFLNK | 0o777 },
		"special":             func(s *unix.Stat_t) { s.Mode = unix.S_IFIFO | 0o600 },
		"special permissions": func(s *unix.Stat_t) { s.Mode |= 0o4000 },
		"missing inode":       func(s *unix.Stat_t) { s.Ino = 0 },
		"negative size":       func(s *unix.Stat_t) { s.Size = -1 },
		"mtime overflow":      func(s *unix.Stat_t) { s.Mtim.Nsec = 1e9 },
		"ctime negative":      func(s *unix.Stat_t) { s.Ctim.Nsec = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := info
			mutate(&changed)
			if _, err := encodeWorkspaceReaderMetadata(changed); err == nil {
				t.Fatal("unsupported metadata accepted")
			}
		})
	}
	for _, invalid := range [][]byte{nil, raw[:len(raw)-1], append(append([]byte{}, raw...), 0), append([]byte{4}, raw[1:]...)} {
		if _, err := decodeWorkspaceReaderMetadata(invalid); err == nil {
			t.Fatal("malformed metadata frame accepted")
		}
	}
}

// TestWorkspaceReaderMetadataRejectsUnexpectedRights closes actual SCM_RIGHTS even on a correctly encoded metadata response.
// TestWorkspaceReaderMetadataRejectsUnexpectedRights закрывает actual SCM_RIGHTS даже у правильно encoded metadata response.
func TestWorkspaceReaderMetadataRejectsUnexpectedRights(t *testing.T) {
	for _, rights := range []int{0, 1, 3} {
		root, _ := workspaceTreeFixture(t)
		file, err := os.Create(filepath.Join(root, "file"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		var info unix.Stat_t
		if unix.Fstat(int(file.Fd()), &info) != nil {
			t.Fatal("stat metadata rights source")
		}
		client, server := workspaceReaderConnections(t)
		raw, err := encodeWorkspaceReaderMetadata(info)
		if err != nil {
			t.Fatal(err)
		}
		var ancillary []byte
		if rights == 1 {
			ancillary = unix.UnixRights(int(file.Fd()))
		} else if rights == 3 {
			ancillary = unix.UnixRights(int(file.Fd()), int(file.Fd()), int(file.Fd()))
		}
		before := workspaceReaderInodeDescriptors(t, info)
		if _, _, err := server.WriteMsgUnix(raw, ancillary, nil); err != nil {
			t.Fatal("send metadata rights fixture")
		}
		actual, err := receiveWorkspaceReaderMetadata(client)
		if rights == 0 && (err != nil || !workspaceTreeIdentityEqual(info, actual)) || rights != 0 && err == nil ||
			workspaceReaderInodeDescriptors(t, info) != before {
			t.Fatal("metadata accepted or leaked descriptor rights")
		}
	}
}

// TestWorkspaceReaderDirectoryFDDoesNotWidenRegularFileGrant checks real directory descriptors and zero-offset/read-only/one-FD requirements.
// TestWorkspaceReaderDirectoryFDDoesNotWidenRegularFileGrant проверяет actual directory descriptors и zero-offset/read-only/one-FD requirements.
func TestWorkspaceReaderDirectoryFDDoesNotWidenRegularFileGrant(t *testing.T) {
	for _, name := range []string{"valid", "file-only receiver", "extra descriptor", "path-only", "consumed cursor", "wrong inode"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if os.WriteFile(filepath.Join(root, "file"), nil, 0o600) != nil {
				t.Fatal("prepare directory descriptor fixture")
			}
			flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
			if name == "path-only" {
				flags = unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC
			}
			fd, err := unix.Open(root, flags, 0)
			if err != nil {
				t.Fatal(err)
			}
			source := os.NewFile(uintptr(fd), "directory-cursor-fixture")
			defer source.Close()
			var info unix.Stat_t
			if unix.Fstat(fd, &info) != nil {
				t.Fatal("stat directory descriptor")
			}
			if name == "consumed cursor" {
				if names, err := source.Readdirnames(1); err != nil || len(names) != 1 {
					t.Fatal("consume directory cursor")
				}
			}
			client, server := workspaceReaderConnections(t)
			rights := unix.UnixRights(fd)
			if name == "extra descriptor" {
				rights = unix.UnixRights(fd, fd, fd)
			}
			before := workspaceReaderInodeDescriptors(t, info)
			if _, _, err := server.WriteMsgUnix([]byte{1}, rights, nil); err != nil {
				t.Fatal("send typed directory descriptor")
			}
			expected := info
			if name == "wrong inode" {
				expected.Ino++
			}
			file, err := receiveWorkspaceReaderDescriptor(client, expected, true)
			if name == "file-only receiver" {
				if file != nil {
					_ = file.Close()
				}
				if _, _, err := server.WriteMsgUnix([]byte{1}, rights, nil); err != nil {
					t.Fatal("send file-only receiver fixture")
				}
				file, err = receiveWorkspaceReaderFile(client, expected)
			}
			if name == "valid" {
				if err != nil || file == nil {
					t.Fatal("exact read-only directory rejected")
				}
				names, err := file.Readdirnames(2)
				if err != nil || len(names) != 1 || names[0] != "file" {
					t.Fatal("directory grant could not list its own children")
				}
				if names, err := file.Readdirnames(1); err != io.EOF || len(names) != 0 {
					t.Fatal("directory grant did not retain its own cursor")
				}
			} else if err == nil || file != nil {
				t.Fatal("invalid typed descriptor accepted")
			}
			if file != nil {
				_ = file.Close()
			}
			if workspaceReaderInodeDescriptors(t, info) != before {
				t.Fatal("rejected directory descriptor leaked")
			}
		})
	}
}

// TestWorkspaceReaderTypedPeerBoundsEachOperation proves the actual peer dispatch and independent file/directory/metadata request budgets.
// TestWorkspaceReaderTypedPeerBoundsEachOperation доказывает actual peer dispatch и independent file/directory/metadata request budgets.
func TestWorkspaceReaderTypedPeerBoundsEachOperation(t *testing.T) {
	for _, kind := range []workspaceReaderOperation{workspaceReaderFile, workspaceReaderDirectory, workspaceReaderMetadata} {
		root, rootFD := workspaceTreeFixture(t)
		if os.Mkdir(filepath.Join(root, "private"), 0o700) != nil || os.WriteFile(filepath.Join(root, "private/state"), []byte("kept"), 0o600) != nil {
			t.Fatal("prepare typed peer source")
		}
		var info unix.Stat_t
		if unix.Fstat(rootFD, &info) != nil {
			t.Fatal("stat typed peer root")
		}
		uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
		client, server := workspaceReaderConnections(t)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		request := workspaceReaderRequest{taskSHA256: [32]byte{1}, device: uint64(info.Dev), inode: info.Ino}
		joined := make(chan error, 1)
		go func() { joined <- serveWorkspaceReaderPeer(ctx, server, rootFD, request.taskSHA256, uid, uid, gid, 1) }()
		if workspaceReaderHello(client, request, uid) != nil {
			t.Fatal("start typed peer fixture")
		}
		request.kind, request.path = kind, "private/state"
		maximum := 2
		if kind == workspaceReaderDirectory {
			maximum, request.path = 1, "private"
		} else if kind == workspaceReaderMetadata {
			maximum = 3
		}
		if unix.Fstatat(rootFD, request.path, &info, unix.AT_SYMLINK_NOFOLLOW) != nil {
			t.Fatal("stat typed peer entry")
		}
		for index := 0; index <= maximum; index++ {
			raw, err := encodeWorkspaceReaderRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := client.WriteMsgUnix(raw, nil, nil); err != nil {
				t.Fatal("send typed peer request")
			}
			if kind == workspaceReaderMetadata {
				_, err = receiveWorkspaceReaderMetadata(client)
			} else {
				var file *os.File
				file, err = receiveWorkspaceReaderDescriptor(client, info, kind == workspaceReaderDirectory)
				if file != nil {
					_ = file.Close()
				}
			}
			if index < maximum && err != nil || index == maximum && err == nil {
				t.Fatal("typed operation escaped or lost its bounded read-only budget")
			}
		}
		select {
		case err := <-joined:
			if err == nil {
				t.Fatal("exhausted peer reported success")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("exhausted typed peer did not join")
		}
	}
}
