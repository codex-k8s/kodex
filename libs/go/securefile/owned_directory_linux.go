package securefile

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// OpenOwnedDirectoryAt opens one exact owned directory below an already authorized parent without links or mount crossings.
// Only private0700 or shared2770 are permitted. Creation is explicit, exclusive and durable; existing entries are never repaired.
// The caller owns the returned CLOEXEC descriptor, retains parent ownership, and separately proves lease/PVC/domain authority.
// Parent identity and access policy belong to that trusted opener; this helper does not authorize an arbitrary shared parent.
// OpenOwnedDirectoryAt открывает exact owned directory под заранее authorized parent без links/mount crossings.
// Допускаются только private0700/shared2770. Явное создание exclusive/durable; существующие entries не исправляются.
// Caller владеет returned CLOEXEC descriptor и отдельно доказывает lease/PVC/domain authority; parent остаётся у caller.
// Parent identity/access policy принадлежат trusted opener; helper не разрешает произвольный shared parent.
func OpenOwnedDirectoryAt(ctx context.Context, parent int, name string, mode uint32, owner OwnedRecordIdentity, create bool) (int, bool, error) {
	if ctx == nil || ctx.Err() != nil || parent < 0 || !ownedEntryName(name) ||
		mode != 0o700 && mode != 0o2770 || uint32(os.Geteuid()) != owner.UID {
		return -1, false, errInvalidBoundary
	}
	var parentInfo unix.Stat_t
	if unix.Fstat(parent, &parentInfo) != nil || parentInfo.Mode&unix.S_IFMT != unix.S_IFDIR {
		return -1, false, errUnsafe
	}
	created := false
	if create {
		err := unix.Mkdirat(parent, name, 0o700)
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, false, errUnavailable
		}
		created = err == nil
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if errors.Is(err, unix.ENOENT) {
		return -1, false, unix.ENOENT
	}
	if err != nil {
		return -1, false, errUnsafe
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = unix.Close(fd)
		}
	}()
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Uid != owner.UID || info.Dev != parentInfo.Dev {
		return -1, false, errUnsafe
	}
	if created && (unix.Fchown(fd, -1, int(owner.GID)) != nil || unix.Fchmod(fd, mode) != nil) {
		return -1, false, errUnsafe
	}
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR || info.Mode&0o7777 != mode ||
		info.Uid != owner.UID || info.Dev != parentInfo.Dev || mode == 0o2770 && info.Gid != owner.GID {
		return -1, false, errUnsafe
	}
	if create && (unix.Fsync(fd) != nil || unix.Fsync(parent) != nil) || ctx.Err() != nil {
		return -1, false, errUnavailable
	}
	accepted = true
	return fd, created, nil
}
