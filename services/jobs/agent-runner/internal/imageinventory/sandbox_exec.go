//go:build linux

package imageinventory

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"unsafe"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// SandboxExecMode selects the private child entrypoint that checks identity and kernel restrictions before executing one registered version command.
// SandboxExecMode выбирает частную дочернюю точку входа, проверяющую идентичность и ограничения ядра до выполнения одной зарегистрированной команды версии.
const SandboxExecMode = "image-tool-probe-exec"

var (
	// errProbeSandbox rejects unregistered probe arguments or unavailable kernel setup without exposing command content.
	// errProbeSandbox отклоняет незарегистрированные аргументы пробы или недоступную настройку ядра без раскрытия содержимого команды.
	errProbeSandbox = errors.New("image tool probe sandbox rejected")
	// errProbeSandboxIdentity marks a wrong probe UID/GID or nonempty supplementary group set before kernel restrictions and exec.
	// errProbeSandboxIdentity обозначает неверные UID и GID пробы либо непустые дополнительные группы до ограничений ядра и exec.
	errProbeSandboxIdentity = errors.New("image tool probe sandbox identity rejected")
	// errProbeSandboxCaps marks nonempty or unreadable effective, permitted, inheritable or ambient capabilities in the probe child.
	// errProbeSandboxCaps обозначает непустые либо нечитаемые effective, permitted, inheritable или ambient capabilities дочерней пробы.
	errProbeSandboxCaps = errors.New("image tool probe sandbox capabilities rejected")
	// errProbeSandboxDescriptors marks unsafe standard descriptors instead of accepting inherited writable files as probe input/output.
	// errProbeSandboxDescriptors обозначает небезопасные стандартные дескрипторы вместо принятия унаследованных записываемых файлов как ввода и вывода пробы.
	errProbeSandboxDescriptors = errors.New("image tool probe sandbox descriptors rejected")
	// errProbeSandboxFilesystem marks failed filesystem/metadata restrictions, descriptor close-on-exec fencing or working-directory setup.
	// errProbeSandboxFilesystem обозначает отказ ограничений файловой системы и метаданных, установки close-on-exec для дескрипторов либо рабочего каталога.
	errProbeSandboxFilesystem = errors.New("image tool probe sandbox filesystem rejected")
	// errProbeSandboxExec marks failure to replace the restricted child with its registered tool command; raw exec errors stay private.
	// errProbeSandboxExec обозначает невозможность заменить ограниченный дочерний процесс зарегистрированной командой инструмента; исходная ошибка exec остаётся частной.
	errProbeSandboxExec = errors.New("image tool probe sandbox exec rejected")
)

// probePipeNonblockRequest is the amd64/arm64 FIONBIO request allowed only for checked stdout/stderr pipes by the private seccomp filter.
// probePipeNonblockRequest задаёт запрос FIONBIO для amd64 и arm64, разрешённый частным seccomp-фильтром только для проверенных stdout и stderr pipe.
const probePipeNonblockRequest = 0x5421

// RunSandboxedTool checks registry-exact path/arguments and UID/GID 10001 without groups, locks the thread, then requires empty capabilities and safe standard descriptors.
// Installs filesystem/metadata restrictions, marks other descriptors close-on-exec and replaces the child with the tool under a fixed environment.
// Designed for a dedicated child; it is neither a general runtime sandbox nor an independent network policy.
// RunSandboxedTool проверяет точные путь и аргументы реестра, UID и GID 10001 без групп, закрепляет поток, затем требует пустые capabilities и безопасные стандартные дескрипторы.
// Устанавливает ограничения файловой системы и метаданных, помечает остальные дескрипторы close-on-exec и заменяет дочерний процесс инструментом с фиксированным окружением.
// Предназначен для отдельного дочернего процесса; это не общий sandbox выполнения и не самостоятельная сетевая политика.
func RunSandboxedTool(args []string) error {
	if !closedProbeArguments(args) {
		return errProbeSandbox
	}
	if os.Geteuid() != 10001 || os.Getegid() != 10001 {
		return errProbeSandboxIdentity
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 {
		return errProbeSandboxIdentity
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !probeCapabilitiesEmpty() {
		return errProbeSandboxCaps
	}
	if !probeStandardDescriptorsSafe() {
		return errProbeSandboxDescriptors
	}
	if restrictProbeFilesystem() != nil {
		return errProbeSandboxFilesystem
	}
	// Даже случайно унаследованный writable fd не должен пережить exec.
	if unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC) != nil {
		return errProbeSandboxFilesystem
	}
	if unix.Chdir("/") != nil {
		return errProbeSandboxFilesystem
	}
	environment := []string{"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/nonexistent",
		"LANG=C", "LC_ALL=C", "NO_COLOR=1", "COREPACK_ENABLE_NETWORK=0", "GOTOOLCHAIN=local",
		"GOROOT=/usr/local/go", "GOENV=off", "GOWORK=off"}
	if unix.Exec(args[0], args, environment) != nil {
		return errProbeSandboxExec
	}
	return nil
}

// closedProbeArguments matches one registered executable path and its entire ordered argument vector; extra arguments and unregistered shells are rejected.
// closedProbeArguments сопоставляет один зарегистрированный путь исполняемого файла со всем упорядоченным вектором аргументов; лишние аргументы и незарегистрированные оболочки отклоняются.
func closedProbeArguments(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, probe := range runtimecontract.ImageToolProbes() {
		if slices.Contains(probe.Paths, args[0]) && slices.Equal(probe.Args, args[1:]) {
			return true
		}
	}
	return false
}

// probeCapabilitiesEmpty requires both capability data words and every ambient capability through CAP_LAST_CAP to be empty; read errors reject the child.
// probeCapabilitiesEmpty требует пустые оба слова capabilities и каждую ambient capability до CAP_LAST_CAP; ошибка чтения отклоняет дочерний процесс.
func probeCapabilitiesEmpty() bool {
	var data [2]unix.CapUserData
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if unix.Capget(&header, &data[0]) != nil {
		return false
	}
	for _, item := range data {
		if item.Effective|item.Permitted|item.Inheritable != 0 {
			return false
		}
	}
	for capability := 0; capability <= unix.CAP_LAST_CAP; capability++ {
		value, err := unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, uintptr(capability), 0, 0)
		if err != nil || value != 0 {
			return false
		}
	}
	return true
}

// probeStandardDescriptorsSafe requires stdin to be the native null character device and stdout/stderr to be pipes, not regular writable files.
// probeStandardDescriptorsSafe требует stdin как штатное символьное нулевое устройство и stdout со stderr как pipe, а не обычные записываемые файлы.
func probeStandardDescriptorsSafe() bool {
	for fd := 0; fd <= 2; fd++ {
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil {
			return false
		}
		if fd == 0 {
			if stat.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(stat.Rdev) != 1 || unix.Minor(stat.Rdev) != 3 {
				return false
			}
		} else if stat.Mode&unix.S_IFMT != unix.S_IFIFO {
			return false
		}
	}
	return true
}

// restrictProbeFilesystem requires Landlock ABI 3 and no_new_privs, builds default-deny rules for handled filesystem mutation rights except null writes/truncation, then adds a metadata syscall fence.
// ABI 5 additionally handles device ioctl; unsupported setup fails closed. Restrictions apply to the calling thread and its future children/exec, not sibling threads.
// restrictProbeFilesystem требует Landlock ABI 3 и no_new_privs, создаёт запрещающие по умолчанию правила обрабатываемых прав изменения файловой системы, кроме записи и усечения null, затем добавляет границу системных вызовов метаданных.
// ABI 5 дополнительно обрабатывает ioctl устройств; неподдерживаемая настройка даёт закрытый отказ. Ограничения применяются к вызывающему потоку и будущим потомкам и exec, а не соседним потокам.
func restrictProbeFilesystem() error {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || abi < 3 || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return errProbeSandbox
	}
	handled := uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM |
		unix.LANDLOCK_ACCESS_FS_REFER | unix.LANDLOCK_ACCESS_FS_TRUNCATE)
	if abi >= 5 {
		handled |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), 8, 0)
	if errno != 0 {
		return errProbeSandbox
	}
	defer unix.Close(int(fd))
	null, err := unix.Open("/dev/null", unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errProbeSandbox
	}
	defer unix.Close(null)
	var stat unix.Stat_t
	if unix.Fstat(null, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR ||
		unix.Major(stat.Rdev) != 1 || unix.Minor(stat.Rdev) != 3 {
		return errProbeSandbox
	}
	rule := unix.LandlockPathBeneathAttr{Allowed_access: unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE, Parent_fd: int32(null)}
	_, _, errno = unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH,
		uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
	if errno != 0 {
		return errProbeSandbox
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return errProbeSandbox
	}
	return restrictProbeMetadataSyscalls()
}

// restrictProbeMetadataSyscalls installs an amd64/arm64 seccomp denylist for metadata changes, namespace/mount changes, process-group escape and io_uring.
// Rejects foreign/x32 architectures and allows only full-width FIONBIO on fd 1/2 as the ioctl exception; other syscalls remain outside this narrow fence.
// restrictProbeMetadataSyscalls устанавливает для amd64 и arm64 запрещающий seccomp-список изменений метаданных, пространств и монтирования, выхода из группы процессов и io_uring.
// Отклоняет чужую и x32 архитектуру, разрешая из ioctl только полноразрядный FIONBIO на fd 1 и 2; прочие системные вызовы остаются вне этой узкой границы.
func restrictProbeMetadataSyscalls() error {
	architecture := uint32(0)
	denied := []uint32{unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2,
		unix.SYS_FCHOWN, unix.SYS_FCHOWNAT, unix.SYS_UTIMENSAT,
		unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR,
		unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR,
		unix.SYS_IOCTL, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT,
		unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_CHROOT,
		unix.SYS_SETSID, unix.SYS_SETPGID,
		unix.SYS_FSOPEN, unix.SYS_FSCONFIG, unix.SYS_FSMOUNT, unix.SYS_MOVE_MOUNT,
		unix.SYS_OPEN_TREE, unix.SYS_OPEN_TREE_ATTR, unix.SYS_MOUNT_SETATTR,
		unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER,
		unix.SYS_SETXATTRAT, unix.SYS_REMOVEXATTRAT}
	switch runtime.GOARCH {
	case "amd64":
		architecture = unix.AUDIT_ARCH_X86_64
		// Legacy syscalls отсутствуют в arm64 ABI.
		denied = append(denied, 90, 92, 94, 132, 235, 261)
	case "arm64":
		architecture = unix.AUDIT_ARCH_AARCH64
	default:
		return errProbeSandbox
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: architecture, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		// x32 ABI не должен обходить amd64 deny registry.
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: 0x40000000, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		// libuv uv_pipe_open требует FIONBIO только для проверенных output pipes.
		// Полные 64-bit args исключают truncated-request/fd aliases.
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_IOCTL, Jf: 14},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 28},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 0, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: probePipeNonblockRequest, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 20},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 0, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 1, Jt: 2},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 2, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	for _, number := range denied {
		filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 1},
			unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)})
	}
	filter = append(filter, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return errProbeSandbox
	}
	return nil
}
