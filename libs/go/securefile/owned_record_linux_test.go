package securefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// ownedRecordFixture returns one private owned parent descriptor and no domain authority or production data.
// ownedRecordFixture возвращает private owned parent descriptor без domain authority или production data.
func ownedRecordFixture(t *testing.T) (string, int, OwnedRecordIdentity) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal("prepare private record fixture")
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("open private record fixture")
	}
	t.Cleanup(func() {
		if syscall.Close(fd) != nil {
			t.Error("close private record fixture")
		}
	})
	return root, fd, OwnedRecordIdentity{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
}

// TestOwnedRecordRoundTripIsExclusive checks exact bytes/mode, explicit absence and no overwrite on any replay.
// TestOwnedRecordRoundTripIsExclusive проверяет точные bytes/mode, отсутствие и запрет перезаписи при любом replay.
func TestOwnedRecordRoundTripIsExclusive(t *testing.T) {
	root, fd, owner := ownedRecordFixture(t)
	if _, err := ReadOwnedRecordAt(t.Context(), fd, "source.json", 64, owner); !errors.Is(err, syscall.ENOENT) {
		t.Fatal("missing owned record was not distinguished")
	}
	wanted := []byte("immutable-source-proof")
	if err := WriteOwnedRecordAt(t.Context(), fd, "source.json", wanted, 64, owner); err != nil {
		t.Fatal("write owned record", err)
	}
	for _, replay := range [][]byte{wanted, []byte("different-source")} {
		if err := WriteOwnedRecordAt(t.Context(), fd, "source.json", replay, 64, owner); !errors.Is(err, syscall.EEXIST) {
			t.Fatal("record replay acquired overwrite credit")
		}
	}
	raw, err := ReadOwnedRecordAt(t.Context(), fd, "source.json", 64, owner)
	if err != nil || string(raw) != string(wanted) {
		t.Fatal("owned record bytes changed")
	}
	info, err := os.Stat(filepath.Join(root, "source.json"))
	if err != nil || info.Mode().Perm() != 0o440 {
		t.Fatal("owned record was not sealed read-only")
	}
	var parent syscall.Stat_t
	if syscall.Fstat(fd, &parent) != nil || parent.Mode&0o7777 != 0o700 {
		t.Fatal("record operation changed or closed caller parent")
	}
}

// TestOwnedRecordRejectsUnsafeEntries preserves partial files and rejects aliases or incorrect metadata.
// TestOwnedRecordRejectsUnsafeEntries сохраняет partial files и отклоняет aliases либо неверные metadata.
func TestOwnedRecordRejectsUnsafeEntries(t *testing.T) {
	for _, fault := range []string{"writable", "executable", "empty", "oversize", "symlink", "hardlink", "fifo", "directory", "owner", "group", "parent-mode"} {
		t.Run(fault, func(t *testing.T) {
			root, fd, owner := ownedRecordFixture(t)
			name := filepath.Join(root, "record")
			if err := os.WriteFile(name, []byte("retained"), 0o440); err != nil {
				t.Fatal("write unsafe record fixture")
			}
			switch fault {
			case "writable":
				if os.Chmod(name, 0o600) != nil {
					t.Fatal("prepare partial record mode")
				}
			case "executable":
				if os.Chmod(name, 0o550) != nil {
					t.Fatal("prepare executable record")
				}
			case "empty":
				if os.Chmod(name, 0o600) != nil || os.Truncate(name, 0) != nil || os.Chmod(name, 0o440) != nil {
					t.Fatal("prepare empty record")
				}
			case "oversize":
				if os.Chmod(name, 0o600) != nil || os.Truncate(name, 65) != nil || os.Chmod(name, 0o440) != nil {
					t.Fatal("prepare oversized record")
				}
			case "symlink", "fifo", "directory":
				if os.Remove(name) != nil {
					t.Fatal("prepare special record")
				}
				if fault == "symlink" && os.Symlink(filepath.Join(t.TempDir(), "outside"), name) != nil ||
					fault == "fifo" && syscall.Mkfifo(name, 0o440) != nil || fault == "directory" && os.Mkdir(name, 0o440) != nil {
					t.Fatal("create special record")
				}
			case "hardlink":
				if os.Link(name, filepath.Join(root, "alias")) != nil {
					t.Fatal("prepare aliased record")
				}
			case "owner":
				owner.UID++
			case "group":
				owner.GID++
			case "parent-mode":
				if os.Chmod(root, 0o750) != nil {
					t.Fatal("prepare shared parent")
				}
			}
			if _, err := ReadOwnedRecordAt(t.Context(), fd, "record", 64, owner); err == nil {
				t.Fatal("unsafe record was accepted")
			} else if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "retained") {
				t.Fatal("record failure disclosed private data")
			}
			if _, err := os.Lstat(name); err != nil {
				t.Fatal("rejected record was removed")
			}
		})
	}
}

// TestOwnedRecordRejectsPathAndBudgetBeforeCreate checks all input boundaries without changing the private directory.
// TestOwnedRecordRejectsPathAndBudgetBeforeCreate проверяет входные границы без изменения private directory.
func TestOwnedRecordRejectsPathAndBudgetBeforeCreate(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../outside", "nested/record", "/absolute", "bad\\name", "bad\x00name", "имя", strings.Repeat("a", 256)} {
		t.Run("name-"+strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			root, fd, owner := ownedRecordFixture(t)
			if err := WriteOwnedRecordAt(t.Context(), fd, name, []byte("x"), 64, owner); err == nil {
				t.Fatal("path-like record name was accepted")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected name changed the directory")
			}
		})
	}
	for _, maximum := range []int64{-1, 0, maximumOwnedRecordBytes + 1} {
		root, fd, owner := ownedRecordFixture(t)
		if err := WriteOwnedRecordAt(t.Context(), fd, "record", []byte("x"), maximum, owner); err == nil {
			t.Fatal("unbounded record write was accepted")
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatal("rejected limit created a record")
		}
	}
}

// TestOwnedRecordCancellationAndPartialReplay never converts an unsealed file or cancelled request into a receipt.
// TestOwnedRecordCancellationAndPartialReplay не превращает unsealed file или отменённый запрос в receipt.
func TestOwnedRecordCancellationAndPartialReplay(t *testing.T) {
	root, fd, owner := ownedRecordFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := WriteOwnedRecordAt(ctx, fd, "cancelled", []byte("x"), 64, owner); err == nil {
		t.Fatal("cancelled request created a record")
	}
	if _, err := os.Lstat(filepath.Join(root, "cancelled")); !os.IsNotExist(err) {
		t.Fatal("cancelled request left an entry")
	}
	if os.WriteFile(filepath.Join(root, "partial"), []byte("ret"), 0o600) != nil {
		t.Fatal("prepare partial record")
	}
	if err := WriteOwnedRecordAt(t.Context(), fd, "partial", []byte("retained"), 64, owner); !errors.Is(err, syscall.EEXIST) {
		t.Fatal("partial record was overwritten")
	}
	if _, err := ReadOwnedRecordAt(t.Context(), fd, "partial", 64, owner); err == nil {
		t.Fatal("partial record was accepted")
	}
	raw, err := os.ReadFile(filepath.Join(root, "partial"))
	if err != nil || string(raw) != "ret" {
		t.Fatal("partial evidence was damaged")
	}
}
