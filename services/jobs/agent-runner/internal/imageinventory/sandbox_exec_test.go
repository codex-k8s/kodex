//go:build linux

package imageinventory

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

func TestClosedProbeArguments(t *testing.T) {
	for _, probe := range runtimecontract.ImageToolProbes() {
		for _, path := range probe.Paths {
			args := append([]string{path}, probe.Args...)
			if !closedProbeArguments(args) {
				t.Fatalf("registered probe rejected: %s", probe.Name)
			}
			if closedProbeArguments(append(args, "unregistered-argument")) {
				t.Fatal("extra argument accepted")
			}
		}
	}
	for _, args := range [][]string{nil, {"/bin/sh", "-c", "true"}, {"/unregistered-tool", "--version"}} {
		if closedProbeArguments(args) || !errors.Is(RunSandboxedTool(args), errProbeSandbox) {
			t.Fatal("unregistered probe accepted")
		}
	}
}

func TestProbeSandboxKernelPreservesFilesystem(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture")
	original := []byte("unchanged fixture\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestProbeSandboxKernelChild$")
	command.Env = []string{"KODEX_PROBE_SANDBOX_TEST=restrict", "KODEX_PROBE_SANDBOX_FIXTURE=" + path}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("kernel sandbox child failed: %v: %s", err, output)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("fixture content changed")
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("fixture mode changed")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "fixture" {
		t.Fatal("fixture directory changed")
	}
}

// Ограничиваем только выделенный child и повторно exec, чтобы проверить
// наследование kernel fence без изменения потока основного test runner.
func TestProbeSandboxKernelChild(t *testing.T) {
	if os.Getenv("KODEX_PROBE_SANDBOX_TEST") != "restrict" {
		t.Skip("disposable subprocess only")
	}
	runtime.LockOSThread()
	if !probeStandardDescriptorsSafe() || !probeCapabilitiesEmpty() {
		t.Fatal("native null, output pipes or empty capabilities rejected")
	}
	if err := restrictProbeFilesystem(); err != nil {
		t.Fatal("kernel sandbox unavailable or rejected")
	}
	if err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		t.Fatal("descriptor fence rejected")
	}
	environment := []string{"KODEX_PROBE_SANDBOX_TEST=restricted", "KODEX_PROBE_SANDBOX_FIXTURE=" + os.Getenv("KODEX_PROBE_SANDBOX_FIXTURE")}
	if err := unix.Exec(os.Args[0], []string{os.Args[0], "-test.run=^TestProbeSandboxRestrictedExec$"}, environment); err != nil {
		t.Fatal("restricted exec failed")
	}
}

func TestProbeSandboxRestrictedExec(t *testing.T) {
	if os.Getenv("KODEX_PROBE_SANDBOX_TEST") != "restricted" {
		t.Skip("restricted subprocess only")
	}
	path := os.Getenv("KODEX_PROBE_SANDBOX_FIXTURE")
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "unchanged fixture\n" {
		t.Fatal("read-only fixture unavailable")
	}
	assertDenied := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
			t.Fatalf("%s not denied by sandbox: %v", name, err)
		}
	}
	assertDenied("write", os.WriteFile(path, []byte("changed"), 0600))
	assertDenied("create", os.WriteFile(path+".new", []byte("new"), 0600))
	assertDenied("remove", os.Remove(path))
	assertDenied("rename", os.Rename(path, path+".renamed"))
	assertDenied("mkdir", os.Mkdir(path+".directory", 0700))
	assertDenied("chmod", os.Chmod(path, 0400))
	assertDenied("fchmodat2", unix.Fchmodat(unix.AT_FDCWD, path, 0400, unix.AT_SYMLINK_NOFOLLOW))
	assertDenied("chown", os.Chown(path, os.Getuid(), os.Getgid()))
	assertDenied("xattr", unix.Setxattr(path, "user.sandbox-test", []byte("changed"), 0))
	assertDenied("utime", unix.UtimesNano(path, []unix.Timespec{{Sec: 1}, {Sec: 1}}))
	assertDenied("unshare", unix.Unshare(0))
	assertDenied("setpgid", unix.Setpgid(0, 0))
	_, err = unix.Setsid()
	assertDenied("setsid", err)
	// FIONBIO разрешён только для stdout/stderr pipes; проверяем flag и restore.
	for _, fd := range []int{1, 2} {
		if err := unix.IoctlSetPointerInt(fd, probePipeNonblockRequest, 1); err != nil {
			t.Fatal("output pipe nonblocking rejected")
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_NONBLOCK == 0 {
			t.Fatal("output pipe nonblocking flag missing")
		}
		if err := unix.IoctlSetPointerInt(fd, probePipeNonblockRequest, 0); err != nil {
			t.Fatal("output pipe blocking restore rejected")
		}
		assertDenied("output pipe foreign ioctl", unix.IoctlSetInt(fd, unix.TIOCEXCL, 1))
		value := int32(1)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(probePipeNonblockRequest)|(uintptr(1)<<32), uintptr(unsafe.Pointer(&value)))
		assertDenied("output pipe truncated request", errno)
		_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd)|(uintptr(1)<<32), probePipeNonblockRequest, uintptr(unsafe.Pointer(&value)))
		assertDenied("output pipe truncated descriptor", errno)
	}
	assertDenied("stdin nonblocking", unix.IoctlSetPointerInt(0, probePipeNonblockRequest, 1))
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		t.Fatal("private pipe unavailable")
	}
	defer unix.Close(pipe[0])
	defer unix.Close(pipe[1])
	assertDenied("nonstandard pipe nonblocking", unix.IoctlSetPointerInt(pipe[1], probePipeNonblockRequest, 1))
	fixture, err := os.Open(path)
	if err != nil {
		t.Fatal("read-only descriptor unavailable")
	}
	defer fixture.Close()
	assertDenied("fchmod", unix.Fchmod(int(fixture.Fd()), 0400))
	assertDenied("fchown", unix.Fchown(int(fixture.Fd()), os.Getuid(), os.Getgid()))
	assertDenied("fxattr", unix.Fsetxattr(int(fixture.Fd()), "user.sandbox-test", []byte("changed"), 0))
	assertDenied("fixture nonblocking", unix.IoctlSetPointerInt(int(fixture.Fd()), probePipeNonblockRequest, 1))
	file, err := os.OpenFile("/dev/null", os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal("native null unavailable")
	}
	defer file.Close()
	if _, err := file.Write([]byte("discarded")); err != nil {
		t.Fatal("native null write rejected")
	}
	assertDenied("ioctl", unix.IoctlSetInt(int(file.Fd()), unix.TIOCEXCL, 1))
}
