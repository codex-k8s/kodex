//go:build linux

package security

import (
	"errors"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// errCommandFilesystem rejects unavailable or incomplete kernel confinement without path details.
// errCommandFilesystem отклоняет недоступную либо неполную kernel confinement без path details.
var errCommandFilesystem = errors.New("workspace command filesystem confinement is unavailable")

// CommandFilesystemAvailable checks only the required Landlock ABI, without changing this thread
// or claiming a running command is confined. The child must install its own actual ruleset.
// CommandFilesystemAvailable проверяет лишь нужный Landlock ABI без изменения текущего thread
// и без заявления об изоляции команды. Child обязан установить настоящий ruleset.
func CommandFilesystemAvailable() bool {
	version, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	return errno == 0 && version >= 3
}

// RestrictCommandFilesystem irreversibly limits mutation to trusted work/tmp roots and null;
// all other filesystem paths, including the executor's control socket, remain mutation-denied.
// It locks the current OS thread until exec; the caller must immediately exec trusted selected
// argv, never run untrusted Go code or release that thread. It does not supply PID isolation,
// identity, network policy, secret removal, process cleanup or authorization.
// RestrictCommandFilesystem необратимо ограничивает mutation доверенными work/tmp и null;
// прочие пути, включая control socket исполнителя, остаются mutation-denied. Фиксирует OS thread
// до exec; caller обязан сразу исполнить trusted selected argv, не запускать недоверенный Go-код
// и не освобождать thread. PID isolation, identity, network policy, удаление secrets, cleanup
// процессов и authorization эта функция не обеспечивает.
func RestrictCommandFilesystem(work, tmp string) (resultErr error) {
	if !filepath.IsAbs(work) || !filepath.IsAbs(tmp) || filepath.Clean(work) != work || filepath.Clean(tmp) != tmp || work == "/" || tmp == "/" || work == tmp || !CommandFilesystemAvailable() {
		return errCommandFilesystem
	}
	runtime.LockOSThread()
	success := false
	defer func() {
		if !success {
			runtime.UnlockOSThread()
		}
	}()
	if unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return errCommandFilesystem
	}
	handled := uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM | unix.LANDLOCK_ACCESS_FS_REFER | unix.LANDLOCK_ACCESS_FS_TRUNCATE)
	attribute := unix.LandlockRulesetAttr{Access_fs: handled}
	rawRules, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attribute)), 8, 0)
	rules := int(rawRules)
	if errno != 0 {
		return errCommandFilesystem
	}
	defer unix.Close(rules)
	for _, root := range []string{work, tmp} {
		fd, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return errCommandFilesystem
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: handled, Parent_fd: int32(fd)}
		_, _, errno = unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(rules), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		unix.Close(fd)
		if errno != 0 {
			return errCommandFilesystem
		}
	}
	null, err := unix.Open("/dev/null", unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errCommandFilesystem
	}
	defer unix.Close(null)
	nullRule := unix.LandlockPathBeneathAttr{Allowed_access: unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE, Parent_fd: int32(null)}
	_, _, errno = unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(rules), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&nullRule)), 0, 0, 0)
	if errno != 0 {
		return errCommandFilesystem
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, uintptr(rules), 0, 0)
	if errno != 0 {
		return errCommandFilesystem
	}
	success = true
	return nil
}
