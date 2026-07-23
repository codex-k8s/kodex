package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const (
	trustedGOROOT       = "/usr/local/go"
	bootstrapGOROOT     = "/opt/mattercodex/bootstrap-go"
	protectedStaging    = "/opt/mattercodex/protected-artifacts"
	protectedTargetDir  = "/usr/local/bin"
	protectedRoot       = "/"
	trustedUID          = 0
	trustedGID          = 0
	prSetChildSubreaper = 36
)

var protectedTools = []string{
	"buf",
	"gofumpt",
	"goimports",
	"golangci-lint",
	"goose",
	"grpcurl",
	"mockgen",
	"oapi-codegen",
	"protoc-gen-go",
	"protoc-gen-go-grpc",
	"sqlc",
	"staticcheck",
	"yq",
}

func fail(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}

func bootstrapGuardPath() string {
	return filepath.Join(protectedRoot, "mattercodex-go-toolchain-guard")
}

func servicesEntrypointPath() string {
	return filepath.Join(protectedTargetDir, "mattercodex-init")
}

func servicesRunnerPath() string {
	return filepath.Join(protectedTargetDir, "matter-codex-agent-runner")
}

func trustedGoCommand(goExecutable string, arguments ...string) *exec.Cmd {
	command := exec.Command(goExecutable, arguments...)
	command.Env = []string{
		"PATH=/usr/local/go/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"GOROOT=" + trustedGOROOT,
		"GOENV=off",
		"GOFLAGS=",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
	}
	return command
}

func expectOutput(goExecutable, label, expected string, arguments ...string) {
	output, err := trustedGoCommand(goExecutable, arguments...).Output()
	if err != nil {
		fail("%s failed: %v", label, err)
	}
	if !bytes.Equal(output, []byte(expected)) {
		fail("%s mismatch: got %q, want %q", label, output, expected)
	}
}

func verifyGo(goExecutable string) {
	expectOutput(goExecutable, "GOVERSION", "go1.26.5\n", "env", "GOVERSION")
	expectOutput(goExecutable, "GOTOOLCHAIN", "local\n", "env", "GOTOOLCHAIN")
	expectOutput(goExecutable, "GOROOT", trustedGOROOT+"\n", "env", "GOROOT")
	expectOutput(goExecutable, "go tool compile", "compile version go1.26.5\n", "tool", "compile", "-V=full")
}

func protectedNames(profile string) []string {
	names := append([]string(nil), protectedTools...)
	switch profile {
	case "services":
		names = append(names, "matter-codex-agent-runner", "mattercodex-init")
	case "deploy":
		names = append(names, "mattercodex-shell")
	default:
		fail("unsupported profile %q", profile)
	}
	sort.Strings(names)
	return names
}

func requireTrustedIdentity() {
	if os.Geteuid() != trustedUID || os.Getegid() != trustedGID {
		fail("trusted guard requires uid:gid %d:%d", trustedUID, trustedGID)
	}
}

func stat(path string) (os.FileInfo, *syscall.Stat_t) {
	info, err := os.Lstat(path)
	if err != nil {
		fail("lstat %s: %v", path, err)
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		fail("stat %s: unsupported platform", path)
	}
	return info, status
}

func ensureDirectory(path string) {
	path = filepath.Clean(path)
	root := filepath.Clean(protectedRoot)
	if path == root {
		verifyDirectory(path)
		return
	}
	if root != "/" && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		fail("protected path %s escapes root %s", path, root)
	}
	parent := filepath.Dir(path)
	ensureDirectory(parent)
	info, err := os.Lstat(path)
	if err == nil && !info.IsDir() {
		if err := os.RemoveAll(path); err != nil {
			fail("remove unsafe path %s: %v", path, err)
		}
		info = nil
	} else if err != nil && !os.IsNotExist(err) {
		fail("lstat %s: %v", path, err)
	}
	if info == nil {
		if err := os.Mkdir(path, 0o755); err != nil && !os.IsExist(err) {
			fail("mkdir %s: %v", path, err)
		}
	}
	if err := os.Chown(path, trustedUID, trustedGID); err != nil {
		fail("chown %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		fail("chmod %s: %v", path, err)
	}
	verifyDirectory(path)
}

func verifyDirectory(path string) {
	info, status := stat(path)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		fail("protected directory %s is not a real directory", path)
	}
	if status.Uid != uint32(trustedUID) || status.Gid != uint32(trustedGID) {
		fail("protected directory %s is not root-owned", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		fail("protected directory %s is group/world-writable", path)
	}
}

func verifyParents(path string) {
	root := filepath.Clean(protectedRoot)
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		verifyDirectory(current)
		if current == root {
			return
		}
	}
}

func removeProtectedLeaves(profile string) {
	for _, name := range protectedNames(profile) {
		path := filepath.Join(protectedTargetDir, name)
		if err := os.RemoveAll(path); err != nil {
			fail("remove protected destination %s: %v", path, err)
		}
	}
}

func prepare(profile string) {
	requireTrustedIdentity()
	verifyBootstrapGuard()
	ensureDirectory(filepath.Dir(trustedGOROOT))
	ensureDirectory(filepath.Dir(bootstrapGOROOT))
	ensureDirectory(filepath.Dir(protectedStaging))
	ensureDirectory(protectedTargetDir)
	if err := os.RemoveAll(trustedGOROOT); err != nil {
		fail("clean %s: %v", trustedGOROOT, err)
	}
	if err := os.RemoveAll(bootstrapGOROOT); err != nil {
		fail("clean %s: %v", bootstrapGOROOT, err)
	}
	if err := os.RemoveAll(protectedStaging); err != nil {
		fail("clean %s: %v", protectedStaging, err)
	}
	ensureDirectory(trustedGOROOT)
	ensureDirectory(protectedStaging)
	removeProtectedLeaves(profile)
	verifyParents(trustedGOROOT)
	verifyParents(protectedStaging)
	verifyParents(protectedTargetDir)
}

func verifyRegular(path string) (os.FileInfo, *syscall.Stat_t) {
	info, status := stat(path)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		fail("protected artifact %s is not a regular non-symlink file", path)
	}
	if status.Uid != uint32(trustedUID) || status.Gid != uint32(trustedGID) {
		fail("protected artifact %s is not root-owned", path)
	}
	if status.Nlink != 1 {
		fail("protected artifact %s has %d hardlinks", path, status.Nlink)
	}
	if info.Mode().Perm()&0o022 != 0 {
		fail("protected artifact %s is group/world-writable", path)
	}
	return info, status
}

func verifyInstalledExecutable(path string) {
	info, _ := verifyRegular(path)
	if info.Mode().Perm() != 0o555 {
		fail("protected executable %s has mode %04o, want 0555", path, info.Mode().Perm())
	}
	verifyParents(filepath.Dir(path))
}

func digest(path string) [sha256.Size]byte {
	file, err := os.Open(path)
	if err != nil {
		fail("open %s: %v", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		fail("hash %s: %v", path, err)
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func verifyBootstrapGuard() {
	path := bootstrapGuardPath()
	verifyInstalledExecutable(path)
	executable, err := os.Executable()
	if err != nil {
		fail("resolve running guard: %v", err)
	}
	if digest(path) != digest(executable) {
		fail("running guard digest differs from protected bootstrap guard")
	}
}

func copyProtected(source, target string) {
	sourceFile, err := os.Open(source)
	if err != nil {
		fail("open protected source %s: %v", source, err)
	}
	defer sourceFile.Close()
	targetFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o555)
	if err != nil {
		fail("create protected destination %s: %v", target, err)
	}
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		targetFile.Close()
		fail("copy protected artifact %s: %v", target, err)
	}
	if err := targetFile.Sync(); err != nil {
		targetFile.Close()
		fail("sync protected artifact %s: %v", target, err)
	}
	if err := targetFile.Chmod(0o555); err != nil {
		targetFile.Close()
		fail("chmod protected artifact %s: %v", target, err)
	}
	if err := targetFile.Close(); err != nil {
		fail("close protected artifact %s: %v", target, err)
	}
}

func verifyExactStaging(names []string) {
	entries, err := os.ReadDir(protectedStaging)
	if err != nil {
		fail("read protected staging: %v", err)
	}
	if len(entries) != len(names) {
		fail("protected staging has %d artifacts, want %d", len(entries), len(names))
	}
	for index, entry := range entries {
		if entry.Name() != names[index] {
			fail("protected staging artifact %q, want %q", entry.Name(), names[index])
		}
	}
}

func install(profile, goExecutable string) {
	requireTrustedIdentity()
	verifyBootstrapGuard()
	names := protectedNames(profile)
	verifyParents(protectedStaging)
	verifyParents(protectedTargetDir)
	verifyParents(trustedGOROOT)
	verifyExactStaging(names)
	sourceDigests := make(map[string][sha256.Size]byte, len(names))
	for _, name := range names {
		source := filepath.Join(protectedStaging, name)
		verifyRegular(source)
		sourceDigests[name] = digest(source)
	}
	removeProtectedLeaves(profile)
	for _, name := range names {
		source := filepath.Join(protectedStaging, name)
		target := filepath.Join(protectedTargetDir, name)
		copyProtected(source, target)
		verifyInstalledExecutable(target)
		if digest(target) != sourceDigests[name] {
			fail("protected artifact digest mismatch for %s", name)
		}
	}
	if err := os.RemoveAll(protectedStaging); err != nil {
		fail("remove protected staging: %v", err)
	}
	verifyParents(protectedTargetDir)
	for _, name := range names {
		target := filepath.Join(protectedTargetDir, name)
		verifyInstalledExecutable(target)
		if digest(target) != sourceDigests[name] {
			fail("final protected artifact digest mismatch for %s", name)
		}
	}
	verifyRegular(goExecutable)
	verifyGo(goExecutable)
}

func enableSubreaper() {
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0)
	if errno != 0 {
		fail("enable child subreaper: %v", errno)
	}
}

func forwardSignals(processGroup int, stop <-chan struct{}) {
	signals := make(chan os.Signal, 16)
	signal.Notify(
		signals,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
		syscall.SIGTSTP,
		syscall.SIGCONT,
		syscall.SIGWINCH,
	)
	defer signal.Stop(signals)
	for {
		select {
		case received := <-signals:
			if received == nil {
				continue
			}
			if typed, ok := received.(syscall.Signal); ok {
				_ = syscall.Kill(-processGroup, typed)
			}
		case <-stop:
			return
		}
	}
}

func exitCode(status syscall.WaitStatus) int {
	if status.Exited() {
		return status.ExitStatus()
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}

func runServicesEntrypoint(runner string, arguments []string) {
	if runner != servicesRunnerPath() {
		fail("protected entrypoint requires exact runner %s", servicesRunnerPath())
	}
	if filepath.Clean(os.Args[0]) != servicesEntrypointPath() {
		fail("protected entrypoint must run from %s", servicesEntrypointPath())
	}
	verifyInstalledExecutable(servicesEntrypointPath())
	verifyInstalledExecutable(runner)
	enableSubreaper()

	command := exec.Command(runner, arguments...)
	command.Env = os.Environ()
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		fail("start protected runner: %v", err)
	}

	stopForwarding := make(chan struct{})
	go forwardSignals(command.Process.Pid, stopForwarding)
	var runnerStatus syscall.WaitStatus
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, 0, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			close(stopForwarding)
			fail("wait for protected runner: %v", err)
		}
		if pid == command.Process.Pid {
			runnerStatus = status
			break
		}
	}
	close(stopForwarding)
	os.Exit(exitCode(runnerStatus))
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "prepare" {
		prepare(os.Args[2])
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "install" {
		install(os.Args[2], os.Args[3])
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "verify" {
		verifyGo(os.Args[2])
		return
	}
	if len(os.Args) >= 3 && os.Args[1] == "entrypoint" {
		runServicesEntrypoint(os.Args[2], os.Args[3:])
		return
	}
	fail("unsupported invocation")
}
