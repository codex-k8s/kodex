package securefile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

// maximumOwnedRecordBytes bounds metadata helpers independently of the caller's narrower record schema.
// maximumOwnedRecordBytes ограничивает metadata helpers независимо от более узкой схемы вызывающего кода.
const maximumOwnedRecordBytes int64 = 1 << 20

// OwnedRecordIdentity pins file ownership, not domain authority; callers obtain these IDs from trusted process policy.
// The parent must be private0700 and owned by UID; its group has no access and may differ from the file group.
// OwnedRecordIdentity закрепляет владельца файла, не domain authority; IDs берутся из доверенной process policy.
// Parent должен иметь private0700 и принадлежать UID; его группа не имеет доступа и может отличаться от группы файла.
type OwnedRecordIdentity struct{ UID, GID uint32 }

// ownedRecordParent rejects path-like names and untrusted parent descriptors before any file is opened.
// The descriptor remains owned by the caller and must already be bound to the authorized aggregate/generation.
// ownedRecordParent отклоняет path-like names и недоверенные parent descriptors до открытия файла.
// Descriptor остаётся у caller и заранее связывается с разрешённым aggregate/generation.
func ownedRecordParent(ctx context.Context, parent int, name string, maximum int64, owner OwnedRecordIdentity) (syscall.Stat_t, error) {
	var info syscall.Stat_t
	if ctx == nil || ctx.Err() != nil || parent < 0 || maximum < 1 || maximum > maximumOwnedRecordBytes ||
		!ownedEntryName(name) {
		return info, errInvalidBoundary
	}
	if syscall.Fstat(parent, &info) != nil || info.Mode&syscall.S_IFMT != syscall.S_IFDIR ||
		info.Mode&0o7777 != 0o700 || info.Uid != owner.UID {
		return info, errUnsafe
	}
	return info, nil
}

// ownedEntryName accepts one bounded ASCII component shared by private directory and record operations.
// ownedEntryName принимает один bounded ASCII component для private directory и record operations.
func ownedEntryName(name string) bool {
	if len(name) < 1 || len(name) > 255 || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	for _, character := range []byte(name) {
		if character < 'a' || character > 'z' {
			if character < 'A' || character > 'Z' {
				if character < '0' || character > '9' {
					if character != '.' && character != '_' && character != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

// validOwnedRecord requires one nonempty read-only0440 regular inode on the same filesystem as the private parent.
// validOwnedRecord требует один непустой read-only0440 regular inode на filesystem частного parent.
func validOwnedRecord(info, parent syscall.Stat_t, maximum int64, owner OwnedRecordIdentity) bool {
	return info.Mode&syscall.S_IFMT == syscall.S_IFREG && info.Mode&0o7777 == 0o440 &&
		info.Uid == owner.UID && info.Gid == owner.GID && info.Nlink == 1 && info.Dev == parent.Dev &&
		info.Size > 0 && info.Size <= maximum
}

// ReadOwnedRecordAt reads a private immutable record without links, blocking special-file opens or path disclosure.
// ENOENT remains distinguishable for an absent record; every other filesystem error is sanitized.
// ReadOwnedRecordAt читает private immutable record без ссылок, блокирующих special-file opens и раскрытия пути.
// ENOENT отдельно обозначает отсутствие записи; остальные filesystem errors очищаются.
func ReadOwnedRecordAt(ctx context.Context, parent int, name string, maximum int64, owner OwnedRecordIdentity) (raw []byte, resultErr error) {
	parentInfo, err := ownedRecordParent(ctx, parent, name, maximum, owner)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Openat(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, syscall.ENOENT
	}
	if err != nil {
		return nil, errUnavailable
	}
	file := os.NewFile(uintptr(fd), "owned-runtime-record")
	defer func() {
		if file.Close() != nil {
			clear(raw)
			raw, resultErr = nil, errUnavailable
		}
	}()
	var before, after syscall.Stat_t
	if syscall.Fstat(fd, &before) != nil || !validOwnedRecord(before, parentInfo, maximum, owner) {
		return nil, errUnsafe
	}
	raw, err = io.ReadAll(io.LimitReader(file, before.Size+1))
	if err != nil || int64(len(raw)) != before.Size || syscall.Fstat(fd, &after) != nil ||
		!validOwnedRecord(after, parentInfo, maximum, owner) || before.Dev != after.Dev || before.Ino != after.Ino ||
		before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim || ctx.Err() != nil {
		clear(raw)
		return nil, errUnsafe
	}
	if current, err := ownedRecordParent(ctx, parent, name, maximum, owner); err != nil || current.Dev != parentInfo.Dev || current.Ino != parentInfo.Ino {
		clear(raw)
		return nil, errUnsafe
	}
	return raw, nil
}

// WriteOwnedRecordAt exclusively creates one bounded record, syncs file/parent and verifies the actual readback.
// Partial or uncertain writes are preserved. EEXIST never authorizes overwrite; callers reconcile expected bytes separately.
// WriteOwnedRecordAt эксклюзивно создаёт bounded record, синхронизирует файл/parent и проверяет фактический readback.
// Partial/unknown writes сохраняются. EEXIST не разрешает перезапись; caller отдельно сверяет ожидаемые bytes.
func WriteOwnedRecordAt(ctx context.Context, parent int, name string, raw []byte, maximum int64, owner OwnedRecordIdentity) (resultErr error) {
	parentInfo, err := ownedRecordParent(ctx, parent, name, maximum, owner)
	if err != nil || owner.UID != uint32(os.Geteuid()) || len(raw) < 1 || int64(len(raw)) > maximum {
		return errUnsafe
	}
	fd, err := syscall.Openat(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0o600)
	if errors.Is(err, syscall.EEXIST) {
		return syscall.EEXIST
	}
	if err != nil {
		return errUnavailable
	}
	file := os.NewFile(uintptr(fd), "owned-runtime-record")
	closed := false
	defer func() {
		if !closed && file.Close() != nil {
			resultErr = errUnavailable
		}
	}()
	var info syscall.Stat_t
	if syscall.Fstat(fd, &info) != nil || info.Mode&syscall.S_IFMT != syscall.S_IFREG || info.Uid != owner.UID ||
		info.Nlink != 1 || info.Dev != parentInfo.Dev || info.Size != 0 || syscall.Fchown(fd, -1, int(owner.GID)) != nil {
		return errUnsafe
	}
	written := 0
	for written < len(raw) {
		if ctx.Err() != nil {
			return errUnavailable
		}
		n, err := file.Write(raw[written:])
		if err != nil || n <= 0 || n > len(raw)-written {
			return errUnavailable
		}
		written += n
	}
	if syscall.Fchmod(fd, 0o440) != nil || syscall.Fstat(fd, &info) != nil ||
		!validOwnedRecord(info, parentInfo, maximum, owner) || info.Size != int64(len(raw)) || file.Sync() != nil || ctx.Err() != nil {
		return errUnavailable
	}
	closed = true
	if file.Close() != nil || syscall.Fsync(parent) != nil || ctx.Err() != nil {
		return errUnavailable
	}
	actual, err := ReadOwnedRecordAt(ctx, parent, name, maximum, owner)
	defer clear(actual)
	if err != nil || !bytes.Equal(actual, raw) {
		return errUnsafe
	}
	return nil
}
