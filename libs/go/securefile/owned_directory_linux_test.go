package securefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestOwnedDirectoryCreatesAndReopensExactInode proves exclusive creation, durable exact modes and caller descriptor ownership.
// TestOwnedDirectoryCreatesAndReopensExactInode проверяет exclusive create, durable exact modes и владение caller descriptor.
func TestOwnedDirectoryCreatesAndReopensExactInode(t *testing.T) {
	for _, mode := range []uint32{0o700, 0o2770} {
		root, parent, owner := ownedRecordFixture(t)
		if fd, created, err := OpenOwnedDirectoryAt(t.Context(), parent, "generation", mode, owner, false); !errors.Is(err, unix.ENOENT) || fd != -1 || created {
			t.Fatal("read-only open did not preserve explicit absence")
		}
		fd, created, err := OpenOwnedDirectoryAt(t.Context(), parent, "generation", mode, owner, true)
		if err != nil || !created {
			t.Fatal("create owned generation")
		}
		var initial unix.Stat_t
		if unix.Fstat(fd, &initial) != nil || initial.Uid != owner.UID || initial.Gid != owner.GID || initial.Mode&0o7777 != mode {
			_ = unix.Close(fd)
			t.Fatal("created directory metadata differs")
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC == 0 || unix.Close(fd) != nil || os.WriteFile(filepath.Join(root, "generation", "retained"), []byte("retain"), 0o600) != nil {
			t.Fatal("directory capability or retained fixture preparation failed")
		}
		for _, create := range []bool{false, true} {
			fd, created, err := OpenOwnedDirectoryAt(t.Context(), parent, "generation", mode, owner, create)
			if err != nil || created {
				t.Fatal("reopen changed creation identity")
			}
			var current unix.Stat_t
			if unix.Fstat(fd, &current) != nil || current.Ino != initial.Ino || current.Dev != initial.Dev || unix.Close(fd) != nil {
				t.Fatal("reopen replaced the directory inode")
			}
		}
		raw, err := os.ReadFile(filepath.Join(root, "generation", "retained"))
		var current unix.Stat_t
		if err != nil || string(raw) != "retain" || unix.Fstat(parent, &current) != nil {
			t.Fatal("directory helper damaged data or closed caller parent")
		}
	}
}

// TestOwnedDirectoryNeverRepairsExistingEntries retains incorrect modes, aliases and unsupported nodes without granting a descriptor.
// TestOwnedDirectoryNeverRepairsExistingEntries сохраняет incorrect modes, aliases и unsupported nodes без выдачи descriptor.
func TestOwnedDirectoryNeverRepairsExistingEntries(t *testing.T) {
	for _, fault := range []string{"shared-private", "private-shared", "symlink", "regular", "fifo", "owner", "group"} {
		t.Run(fault, func(t *testing.T) {
			root, parent, owner := ownedRecordFixture(t)
			name := filepath.Join(root, "generation")
			mode := uint32(0o700)
			switch fault {
			case "regular":
				if os.WriteFile(name, []byte("retain"), 0o600) != nil {
					t.Fatal("prepare regular entry")
				}
			case "fifo":
				if unix.Mkfifo(name, 0o600) != nil {
					t.Fatal("prepare special entry")
				}
			case "symlink":
				if os.Symlink(t.TempDir(), name) != nil {
					t.Fatal("prepare linked entry")
				}
			default:
				if os.Mkdir(name, 0o700) != nil {
					t.Fatal("prepare directory entry")
				}
				if fault == "shared-private" || fault == "group" {
					if unix.Chmod(name, 0o2770) != nil {
						t.Fatal("prepare shared directory")
					}
				}
				if fault == "private-shared" || fault == "group" {
					mode = 0o2770
				}
				if fault == "owner" {
					owner.UID++
				} else if fault == "group" {
					owner.GID++
				}
			}
			before, err := os.Lstat(name)
			if err != nil {
				t.Fatal("observe rejected entry")
			}
			for _, create := range []bool{false, true} {
				fd, created, err := OpenOwnedDirectoryAt(t.Context(), parent, "generation", mode, owner, create)
				if err == nil || fd != -1 || created || strings.Contains(err.Error(), root) {
					t.Fatal("unsafe existing entry acquired a descriptor or exposed a path")
				}
			}
			after, err := os.Lstat(name)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("rejected entry was repaired, replaced or deleted")
			}
		})
	}
}

// TestOwnedDirectoryRejectsInputBeforeEffects checks cancellation and closed modes/names without mutating the parent.
// TestOwnedDirectoryRejectsInputBeforeEffects проверяет cancellation и closed modes/names без изменения parent.
func TestOwnedDirectoryRejectsInputBeforeEffects(t *testing.T) {
	root, parent, owner := ownedRecordFixture(t)
	for _, name := range []string{"", ".", "..", "../escape", "nested/entry", "/absolute", "bad\\name", "bad\x00name", "имя", strings.Repeat("a", 256)} {
		if fd, _, err := OpenOwnedDirectoryAt(t.Context(), parent, name, 0o700, owner, true); err == nil || fd != -1 {
			t.Fatal("unsafe directory component accepted")
		}
	}
	for _, mode := range []uint32{0, 0o600, 0o750, 0o777, 0o4700} {
		if fd, _, err := OpenOwnedDirectoryAt(t.Context(), parent, "generation", mode, owner, true); err == nil || fd != -1 {
			t.Fatal("unknown directory policy accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if fd, _, err := OpenOwnedDirectoryAt(ctx, parent, "generation", 0o700, owner, true); err == nil || fd != -1 {
			t.Fatal("missing or cancelled context created a directory")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected input changed the parent")
	}
}
