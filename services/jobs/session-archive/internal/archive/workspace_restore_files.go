package archive

import (
	"context"
	"errors"
	"io"
	"os"
	"path"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// workspaceRestoreIdentity is supplied by the trusted process opener, never by an archive or IPC request.
// Production fixes the two UIDs; unprivileged filesystem tests use their own process identity explicitly.
// workspaceRestoreIdentity назначает доверенный process opener, не архив или IPC request.
// Production закрепляет два UID; непривилегированные filesystem-тесты явно используют собственную идентичность.
type workspaceRestoreIdentity struct{ actor, role, provider, group uint32 }

// workspaceRestoreFilesystem owns a duplicate root descriptor and bounded writes for one fixed UID.
// Its root must already be authorized and unpublished; this adapter does not infer task or PVC authority.
// workspaceRestoreFilesystem владеет копией root descriptor и ограничивает запись одним фиксированным UID.
// Root заранее авторизован и не опубликован; adapter не выводит полномочия из task или PVC.
type workspaceRestoreFilesystem struct {
	ctx            context.Context
	root           int
	device         uint64
	identity       workspaceRestoreIdentity
	policy         runtimecontract.RuntimeWorkspacePolicy
	bytes, entries int64
	openFiles      int
	failed, closed bool
}

// workspaceRestoreFile enforces exact size, ownership, path identity and durable close before acceptance.
// workspaceRestoreFile проверяет точный размер, владельца, path identity и durable close до принятия.
type workspaceRestoreFile struct {
	filesystem       *workspaceRestoreFilesystem
	file             *os.File
	parent           int
	name             string
	mode             uint32
	device           uint64
	inode            uint64
	size, used       int64
	closed, exported bool
}

// newWorkspaceRestoreFilesystem duplicates only a writable, private-generation root with the fixed role ownership.
// It cannot choose a path, acquire a lease, delete a tree or overwrite an existing entry.
// newWorkspaceRestoreFilesystem копирует descriptor только writable root частного поколения с fixed role ownership.
// Она не выбирает путь, не получает lease, не удаляет дерево и не перезаписывает существующие записи.
func newWorkspaceRestoreFilesystem(ctx context.Context, rootFD int, policy runtimecontract.RuntimeWorkspacePolicy, identity workspaceRestoreIdentity) (*workspaceRestoreFilesystem, error) {
	if ctx == nil || ctx.Err() != nil || rootFD < 0 || uint32(os.Geteuid()) != identity.actor ||
		identity.actor != identity.role && identity.actor != identity.provider || policy.Validate() != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	var info unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(rootFD, &info) != nil || info.Uid != identity.role || info.Gid != identity.group ||
		info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o7777 != 0o2770 || unix.Fstatfs(rootFD, &fs) != nil || fs.Flags&unix.ST_RDONLY != 0 {
		return nil, errWorkspaceObjectUnconfirmed
	}
	root, err := unix.FcntlInt(uintptr(rootFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, errWorkspaceObjectUnconfirmed
	}
	return &workspaceRestoreFilesystem{ctx: ctx, root: root, device: uint64(info.Dev), identity: identity, policy: policy}, nil
}

// usable stops all further mutation after cancellation, a failed write or closure.
// usable прекращает последующие изменения после отмены, ошибки записи либо закрытия.
func (files *workspaceRestoreFilesystem) usable(ctx context.Context) bool {
	return files != nil && !files.failed && !files.closed && ctx != nil && ctx.Err() == nil && files.ctx.Err() == nil
}

// parent opens only an existing temporary shared directory under the same root without links or mount crossings.
// parent открывает только существующий временный общий каталог внутри root, без ссылок и смены mount.
func (files *workspaceRestoreFilesystem) parent(name string) (int, string, error) {
	if !workspaceStreamPath(name) {
		return -1, "", errWorkspaceObjectUnconfirmed
	}
	parent, err := unix.Openat2(files.root, path.Dir(name), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return -1, "", errWorkspaceObjectUnconfirmed
	}
	var info unix.Stat_t
	if unix.Fstat(parent, &info) != nil || uint64(info.Dev) != files.device || info.Mode&0o7777 != 0o2770 ||
		info.Gid != files.identity.group || info.Uid != files.identity.role && info.Uid != files.identity.provider {
		_ = unix.Close(parent)
		return -1, "", errWorkspaceObjectUnconfirmed
	}
	return parent, path.Base(name), nil
}

// Directory creates one owned temporary directory and syncs it before descendants become writable.
// Directory создаёт собственный временный каталог и синхронизирует его до записи потомков.
func (files *workspaceRestoreFilesystem) Directory(ctx context.Context, name string) error {
	if !files.usable(ctx) || files.entries >= files.policy.MaximumFileCount {
		return errWorkspaceObjectUnconfirmed
	}
	parent, leaf, err := files.parent(name)
	if err != nil {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(parent)
	if unix.Mkdirat(parent, leaf, 0o2770) != nil {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	directory, err := unix.Openat2(parent, leaf, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(directory)
	var info unix.Stat_t
	if unix.Fchmod(directory, 0o2770) != nil || unix.Fstat(directory, &info) != nil || info.Uid != files.identity.actor ||
		info.Gid != files.identity.group || info.Mode&0o7777 != 0o2770 || uint64(info.Dev) != files.device ||
		unix.Fsync(directory) != nil || unix.Fsync(parent) != nil || !files.usable(ctx) {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	files.entries++
	return nil
}

// File creates one exclusive regular file; only the returned bounded writer may fill it before durable close.
// File создаёт единственный regular file; до durable close его заполняет только возвращённый bounded writer.
func (files *workspaceRestoreFilesystem) File(ctx context.Context, name string, mode uint32, size int64) (io.WriteCloser, error) {
	if !files.usable(ctx) || mode > 0o777 || size < 0 || size > files.policy.MaximumWritableBytes-files.bytes ||
		files.entries >= files.policy.MaximumFileCount || files.openFiles != 0 {
		return nil, errWorkspaceObjectUnconfirmed
	}
	parent, leaf, err := files.parent(name)
	if err != nil {
		files.failed = true
		return nil, errWorkspaceObjectUnconfirmed
	}
	fd, err := unix.Openat2(parent, leaf, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC,
		Mode: 0o600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		_ = unix.Close(parent)
		files.failed = true
		return nil, errWorkspaceObjectUnconfirmed
	}
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Uid != files.identity.actor || info.Gid != files.identity.group || info.Nlink != 1 ||
		info.Mode&unix.S_IFMT != unix.S_IFREG || uint64(info.Dev) != files.device || info.Size != 0 {
		_ = unix.Close(fd)
		_ = unix.Close(parent)
		files.failed = true
		return nil, errWorkspaceObjectUnconfirmed
	}
	files.bytes += size
	files.entries++
	files.openFiles++
	return &workspaceRestoreFile{filesystem: files, file: os.NewFile(uintptr(fd), "restoring-work-file"), parent: parent,
		name: leaf, mode: mode, device: uint64(info.Dev), inode: info.Ino, size: size}, nil
}

// Write acknowledges only complete bounded writes and permanently poisons this attempt on any uncertainty.
// Write подтверждает только полные bounded writes и закрывает попытку при любой неопределённости.
func (file *workspaceRestoreFile) Write(raw []byte) (int, error) {
	if file == nil || file.closed || file.exported || !file.filesystem.usable(file.filesystem.ctx) || int64(len(raw)) > file.size-file.used {
		if file != nil {
			file.filesystem.failed = true
		}
		return 0, errWorkspaceObjectUnconfirmed
	}
	n, err := file.file.Write(raw)
	file.used += int64(n)
	if err != nil || n != len(raw) {
		file.filesystem.failed = true
		return n, errWorkspaceObjectUnconfirmed
	}
	return n, nil
}

// Close validates the exact directory entry, applies original permissions and syncs bytes and parent before returning.
// Even failed or short writes close both descriptors; no partial file is removed or accepted.
// Close проверяет точную directory entry, возвращает исходные права и синхронизирует файл и parent.
// Ошибка или короткая запись тоже закрывает оба descriptor; partial file не удаляется и не принимается.
func (file *workspaceRestoreFile) Close() error {
	return file.closeVerified(file != nil && !file.exported && file.used == file.size)
}

// exportWriter grants only this newly created write-only descriptor; local writes are forbidden after the transfer.
// Its caller owns the authenticated peer and must request explicit completion after closing the received duplicate.
// exportWriter выдаёт только новый write-only descriptor; после передачи local writes запрещены.
// Caller владеет authenticated peer и запрашивает explicit completion после закрытия received duplicate.
func (file *workspaceRestoreFile) exportWriter() (int, error) {
	if file == nil || file.closed || file.exported || file.used != 0 || !file.filesystem.usable(file.filesystem.ctx) {
		return -1, errWorkspaceObjectUnconfirmed
	}
	file.exported = true
	return int(file.file.Fd()), nil
}

// completeExternal verifies the kernel-maintained shared write offset instead of fabricating local byte accounting.
// Only the dedicated authenticated writer protocol invokes it after exact bounded writes and descriptor closure.
// completeExternal проверяет kernel-maintained shared offset без подмены local byte accounting.
// Метод вызывает только отдельный authenticated writer protocol после bounded writes и закрытия descriptor.
func (file *workspaceRestoreFile) completeExternal() error {
	valid := file != nil && !file.closed && file.exported
	if valid {
		offset, err := file.file.Seek(0, io.SeekCurrent)
		valid = err == nil && offset == file.size
	}
	return file.closeVerified(valid)
}

// closeVerified applies the same inode/size/mode/durability boundary to local writes and explicitly completed external writes.
// A plain Close of an exported descriptor aborts and preserves partial evidence even when its size happens to match.
// closeVerified применяет общую inode/size/mode/durability boundary к local и explicit external completion.
// Обычный Close exported descriptor прерывает операцию и сохраняет partial evidence даже при совпадении size.
func (file *workspaceRestoreFile) closeVerified(complete bool) error {
	if file == nil || file.closed {
		return errWorkspaceObjectUnconfirmed
	}
	file.closed = true
	files := file.filesystem
	files.openFiles--
	var info unix.Stat_t
	valid := complete && files.usable(files.ctx) &&
		unix.Fstatat(file.parent, file.name, &info, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		uint64(info.Dev) == file.device && info.Ino == file.inode && info.Uid == files.identity.actor && info.Gid == files.identity.group &&
		info.Mode&unix.S_IFMT == unix.S_IFREG && info.Nlink == 1 && info.Size == file.size &&
		unix.Fchmod(int(file.file.Fd()), file.mode) == nil && unix.Fstat(int(file.file.Fd()), &info) == nil &&
		uint64(info.Dev) == file.device && info.Ino == file.inode && info.Uid == files.identity.actor && info.Gid == files.identity.group &&
		info.Mode&unix.S_IFMT == unix.S_IFREG && info.Mode&0o7777 == file.mode && info.Nlink == 1 && info.Size == file.size &&
		file.file.Sync() == nil && unix.Fsync(file.parent) == nil
	closeErr := file.file.Close()
	parentErr := unix.Close(file.parent)
	if !valid || closeErr != nil || parentErr != nil {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// SealDirectory restores one owned directory's final mode after all descendants have been closed.
// SealDirectory восстанавливает итоговый режим собственного каталога после закрытия всех потомков.
func (files *workspaceRestoreFilesystem) SealDirectory(ctx context.Context, name string, mode uint32) error {
	if !files.usable(ctx) || files.openFiles != 0 || mode > 0o777 {
		return errWorkspaceObjectUnconfirmed
	}
	parent, leaf, err := files.parent(name)
	if err != nil {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(parent)
	directory, err := unix.Openat2(parent, leaf, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	defer unix.Close(directory)
	var info unix.Stat_t
	if unix.Fstat(directory, &info) != nil || info.Uid != files.identity.actor || info.Gid != files.identity.group ||
		uint64(info.Dev) != files.device || info.Mode&0o7777 != 0o2770 || unix.Fchmod(directory, mode) != nil ||
		unix.Fstat(directory, &info) != nil || info.Uid != files.identity.actor || info.Gid != files.identity.group ||
		uint64(info.Dev) != files.device || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o7777 != mode ||
		unix.Fsync(directory) != nil || unix.Fsync(parent) != nil || !files.usable(ctx) {
		files.failed = true
		return errWorkspaceObjectUnconfirmed
	}
	return nil
}

// Close releases only this adapter's duplicate descriptor and reports pending or failed writes.
// Close освобождает только копию descriptor этого adapter и сообщает о незавершённых либо ошибочных записях.
func (files *workspaceRestoreFilesystem) Close() error {
	if files == nil {
		return nil
	}
	if files.closed {
		if files.failed || files.openFiles != 0 {
			return errWorkspaceObjectUnconfirmed
		}
		return nil
	}
	files.closed = true
	err := unix.Close(files.root)
	if err != nil || files.openFiles != 0 || files.failed {
		files.failed = true
		return errors.Join(errWorkspaceObjectUnconfirmed, err)
	}
	return nil
}
