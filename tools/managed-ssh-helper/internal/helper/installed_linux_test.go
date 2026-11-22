package helper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// installedFixtureImage is an exact cache-only runtime fixture; no daemon, host socket, network, host account or production data is mounted inside it.
// installedFixtureImage является exact cache-only runtime fixture; внутрь не монтируются daemon, host socket, network, host account либо production data.
const installedFixtureImage = "docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7"

// installedFixture owns a private disposable container and the canonical operator profile for a deliberately synthetic account.
// installedFixture владеет private disposable container и canonical operator profile заведомо synthetic account.
type installedFixture struct {
	container string
	directory string
	profile   managedssh.Profile
	digest    string
}

// buildInstalledFixtureBinary compiles one real static command with the test's exact Go toolchain and private cache, returning only local fixture bytes and their complete digest.
// buildInstalledFixtureBinary собирает одну настоящую static command точным Go toolchain теста и private cache, возвращая только local fixture bytes и их полный digest.
func buildInstalledFixtureBinary(t *testing.T, directory, name, target string) ([]byte, string) {
	t.Helper()
	binary := filepath.Join(directory, name)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-p=1", "-trimpath", "-o", binary, target)
	build.Dir = filepath.Join("..", "..")
	for _, name := range []string{"HOME", "PATH", "LANG", "GOCACHE", "GOTMPDIR", "TMPDIR", "GOMAXPROCS", "GOMEMLIMIT"} {
		if value, ok := os.LookupEnv(name); ok {
			build.Env = append(build.Env, name+"="+value)
		}
	}
	build.Env = append(build.Env, "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0")
	if raw, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile isolated helper command: %v %s", err, raw)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(content)
	return content, hex.EncodeToString(hash[:])
}

// dockerFixture runs only the explicitly supplied local test command with bounded lifetime and returns no host environment or credential material.
// dockerFixture выполняет только явно переданную local test command с bounded lifetime без host environment либо credential material в ответе.
func dockerFixture(t *testing.T, arguments ...string) []byte {
	t.Helper()
	return dockerFixtureInput(t, nil, arguments...)
}

// dockerFixtureInput supplies bounded fixture bytes through stdin rather than Docker archive copying, keeping the container root filesystem read-only.
// dockerFixtureInput передаёт bounded fixture bytes через stdin вместо Docker archive copying, сохраняя read-only root filesystem container.
func dockerFixtureInput(t *testing.T, input []byte, arguments ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", arguments...)
	command.Stdin = bytes.NewReader(input)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated helper fixture failed: %v %s", err, raw[:min(len(raw), 2000)])
	}
	return raw
}

// newInstalledFixture compiles the real helper and installs it into root-owned tmpfs paths inside one networkless container; tmpfs proves process persistence only, not host-reboot durability.
// newInstalledFixture собирает настоящий helper и устанавливает его в root-owned tmpfs paths одного networkless container; tmpfs доказывает только process persistence, а не host-reboot durability.
func newInstalledFixture(t *testing.T) installedFixture {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		if os.Getenv("KODEX_MANAGED_SSH_NATIVE_REQUIRED") == "1" {
			t.Fatal("cache-only Docker runtime unavailable")
		}
		t.Skip("cache-only Docker runtime unavailable")
	}
	if err := exec.CommandContext(t.Context(), "docker", "image", "inspect", installedFixtureImage).Run(); err != nil {
		if os.Getenv("KODEX_MANAGED_SSH_NATIVE_REQUIRED") == "1" {
			t.Fatal("pinned helper runtime fixture unavailable")
		}
		t.Skip("pinned helper runtime fixture unavailable")
	}
	directory := t.TempDir()
	content, digest := buildInstalledFixtureBinary(t, directory, "helper", "./cmd/managed-ssh-helper")
	shell, shellDigest := buildInstalledFixtureBinary(t, directory, "shell", "./cmd/managed-ssh-shell")
	id := strings.TrimSpace(string(dockerFixture(t, "run", "--detach", "--pull=never", "--network=none", "--read-only", "--pids-limit=64", "--memory=64m", "--cap-drop=ALL", "--cap-add=CHOWN", "--security-opt=no-new-privileges", "--label=kodex.test=managed-ssh-helper", "--tmpfs=/etc/kodex:rw,nosuid,nodev,noexec,mode=0755,size=1m", "--tmpfs=/srv/kodex:rw,nosuid,nodev,noexec,mode=0755,size=16m", "--tmpfs=/opt/kodex:rw,nosuid,nodev,exec,mode=0755,size=32m", "--tmpfs=/usr/local/libexec:rw,nosuid,nodev,exec,mode=0755,size=32m", "--entrypoint=/bin/sleep", installedFixtureImage, "120")))
	if len(id) != 64 {
		t.Fatal("isolated runtime identity invalid")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer stop()
		if exec.CommandContext(cleanup, "docker", "rm", "--force", id).Run() != nil {
			t.Error("isolated helper runtime did not join")
		}
	})
	dockerFixtureInput(t, content, "exec", "--interactive", id, "/bin/sh", "-c", "cat > /opt/kodex/helper")
	dockerFixtureInput(t, content, "exec", "--interactive", id, "/bin/sh", "-c", "cat > /usr/local/libexec/kodex-ssh-target")
	dockerFixtureInput(t, shell, "exec", "--interactive", id, "/bin/sh", "-c", "cat > /usr/local/libexec/kodex-ssh-target-shell")
	groups := make([]uint32, 0)
	status := dockerFixture(t, "exec", "--user=10001:10001", id, "/bin/cat", "/proc/self/status")
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Groups:") {
			for _, field := range strings.Fields(strings.TrimPrefix(line, "Groups:")) {
				group, err := strconv.ParseUint(field, 10, 32)
				if err != nil || group == 0 {
					t.Fatal("isolated account groups invalid")
				}
				groups = append(groups, uint32(group))
			}
		}
	}
	profile := managedssh.Profile{Version: managedssh.Version, Revision: 1, ValidUntil: time.Now().Add(time.Hour).Unix(), UID: 10001, GID: 10001, Groups: groups, ExecutableDigest: digest, ShellDigest: shellDigest, StateDirectory: "/srv/kodex/receipts", ReceiptLimit: 128, Roots: []managedssh.RootScope{{Alias: "documents", Path: "/srv/kodex/documents"}}, Services: []managedssh.ServiceScope{}}
	f := installedFixture{container: id, directory: directory, profile: profile}
	f.install(t, profile)
	dockerFixture(t, "exec", id, "/bin/sh", "-c", "mkdir -p /srv/kodex/receipts /srv/kodex/documents && chmod 0700 /srv/kodex/receipts /srv/kodex/documents && chown 10001:10001 /srv/kodex/receipts /srv/kodex/documents && chmod 0555 /opt/kodex/helper /usr/local/libexec/kodex-ssh-target /usr/local/libexec/kodex-ssh-target-shell")
	profileDigest, err := profile.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.digest = profileDigest
	return f
}

// invokeShell executes the real static account-shell wrapper with controlled argv and stdin; it grants no Docker access to the process under test.
// invokeShell выполняет настоящий static account-shell wrapper с controlled argv и stdin без Docker access для проверяемого process.
func (fixture installedFixture) invokeShell(t *testing.T, arguments []string, input []byte) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	argv := append([]string{"exec", "--interactive", "--user=10001:10001", fixture.container, "/usr/local/libexec/kodex-ssh-target-shell"}, arguments...)
	command := exec.CommandContext(ctx, "docker", argv...)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// install copies only synthetic canonical configuration into the container and resets its root-owned immutable file permissions before every setup probe.
// install копирует только synthetic canonical configuration в container и восстанавливает root-owned immutable file permissions перед каждым setup probe.
func (fixture installedFixture) install(t *testing.T, profile managedssh.Profile) {
	t.Helper()
	raw, err := managedssh.EncodeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	dockerFixtureInput(t, raw, "exec", "--interactive", fixture.container, "/bin/sh", "-c", "rm -f /etc/kodex/ssh-target.json && cat > /etc/kodex/ssh-target.json && chmod 0444 /etc/kodex/ssh-target.json")
}

// invoke drives the real installed process as the selected non-root account with one exact canonical request and bounded stdout/stderr capture.
// invoke запускает настоящий installed process под выбранной non-root account с одним exact canonical request и bounded stdout/stderr capture.
func (fixture installedFixture) invoke(t *testing.T, request managedssh.Request, root bool) ([]byte, []byte, error) {
	t.Helper()
	raw, err := managedssh.EncodeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	user := "10001:10001"
	if root {
		user = "0:0"
	}
	command := exec.CommandContext(ctx, "docker", "exec", "--interactive", "--user="+user, fixture.container, "/opt/kodex/helper")
	command.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// TestInstalledNonprivilegedHelperProcess verifies actual root-file provenance, UID/capability admission, independent-process receipts and expiry/digest/permission refusals.
// TestInstalledNonprivilegedHelperProcess проверяет actual root-file provenance, UID/capability admission, receipts независимых процессов и отказы expiry/digest/permissions.
func TestInstalledNonprivilegedHelperProcess(t *testing.T) {
	f := newInstalledFixture(t)
	request := managedssh.Request{Version: managedssh.Version, ProfileDigest: f.digest, AttemptID: "22222222-2222-4222-8222-222222222222", Operation: managedssh.Inspect}
	stdout, stderr, err := f.invoke(t, request, false)
	if err != nil {
		t.Fatalf("installed inspect rejected: %s", stderr)
	}
	reply, err := managedssh.DecodeReply(request, stdout)
	if err != nil || reply.Identity == nil || reply.Identity.UID != 10001 || reply.Identity.Executable != f.profile.ExecutableDigest {
		t.Fatal("installed identity mismatch")
	}
	request.Operation, request.Resource, request.Path, request.Content = managedssh.FileCreate, "documents", "created", []byte("isolated installed effect")
	stdout, _, err = f.invoke(t, request, false)
	reply, decode := managedssh.DecodeReply(request, stdout)
	if err != nil || decode != nil || reply.Status != managedssh.Applied {
		t.Fatal("installed effect failed")
	}
	request.Operation, request.Resource, request.Path, request.Content = managedssh.ReceiptRead, "", "", nil
	stdout, _, err = f.invoke(t, request, false)
	reply, decode = managedssh.DecodeReply(request, stdout)
	if err != nil || decode != nil || reply.Receipt.Status != managedssh.Applied {
		t.Fatal("independent process lost completed receipt")
	}
	request.Operation = managedssh.Inspect
	t.Run("account shell executes only pinned helper inode", func(t *testing.T) {
		raw, err := managedssh.EncodeRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := f.invokeShell(t, []string{"-c", installedHelperExecutable}, raw)
		reply, decode := managedssh.DecodeReply(request, stdout)
		if err != nil || len(stderr) != 0 || decode != nil || reply.Identity == nil || reply.Identity.UID != 10001 {
			t.Fatal("installed account-shell fixed exec failed")
		}
		for _, arguments := range [][]string{nil, {"-c", "id"}, {"-c", installedHelperExecutable + "; id"}, {"-c", "internal-sftp"}} {
			stdout, stderr, err := f.invokeShell(t, arguments, raw)
			if err == nil || len(stdout) != 0 || string(stderr) != "managed SSH helper rejected\n" {
				t.Fatal("installed account shell admitted another command")
			}
		}
		profile := f.profile
		profile.ShellDigest = strings.Repeat("9", 64)
		f.install(t, profile)
		stdout, stderr, err = f.invokeShell(t, []string{"-c", installedHelperExecutable}, raw)
		if err == nil || len(stdout) != 0 || string(stderr) != "managed SSH helper rejected\n" {
			t.Fatal("unqualified account-shell executable accepted")
		}
		f.install(t, f.profile)
	})
	for _, kind := range []string{"root", "expiry", "binary", "writable"} {
		profile := f.profile
		if kind == "expiry" {
			profile.ValidUntil = 1
		}
		if kind == "binary" {
			profile.ExecutableDigest = strings.Repeat("b", 64)
		}
		f.install(t, profile)
		if kind == "writable" {
			dockerFixture(t, "exec", f.container, "/bin/chmod", "0666", "/etc/kodex/ssh-target.json")
		}
		stdout, stderr, err = f.invoke(t, request, kind == "root")
		if err == nil || len(stdout) != 0 || string(stderr) != "managed SSH helper rejected\n" {
			t.Fatal("unqualified installed process accepted", kind)
		}
	}
	f.install(t, f.profile)
	t.Run("unterminated input obeys real process deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "docker", "exec", "--interactive", "--user=10001:10001", f.container, "/opt/kodex/helper")
		pipe, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer pipe.Close()
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		started := time.Now()
		err = command.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || time.Since(started) < 18*time.Second || ctx.Err() != nil || stdout.Len() != 0 || stderr.String() != "managed SSH helper rejected\n" {
			t.Fatal("real helper input deadline did not close and join")
		}
	})
}
