package sshcompose

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Request contains one signed owner permit and source bytes only for initial deploy.
// Request содержит owner permit и source bytes только для начального deploy.
type Request struct {
	Permit SignedPermit `json:"permit"`
	Bundle *Bundle      `json:"bundle,omitempty"`
}

// Response exposes safe readiness or a deployment receipt without command output.
// Response выдаёт безопасную readiness либо receipt без вывода команд.
type Response struct {
	Version       int      `json:"version"`
	TargetRef     string   `json:"targetRef"`
	Ready         bool     `json:"ready"`
	Receipt       *Receipt `json:"receipt,omitempty"`
	SafeErrorCode string   `json:"safeErrorCode,omitempty"`
}

// CommandRunner executes fixed helper-owned argument vectors with cancellation.
// CommandRunner выполняет фиксированные аргументы helper с поддержкой отмены.
type CommandRunner interface {
	Run(context.Context, []string) error
	Rootless(context.Context) bool
}

// DockerRunner uses the local rootless Docker context and never invokes a shell.
// DockerRunner использует локальный rootless Docker context без вызова shell.
type DockerRunner struct{}

// Run limits output and terminates the exact child process group on cancellation.
// Run ограничивает вывод и завершает точную группу процессов при отмене.
func (DockerRunner) Run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "docker" {
		return ErrorInvalid
	}
	command, err := dockerCommand(ctx, args[1:]...)
	if err != nil {
		return err
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.Cancel = func() error {
		if command.Process != nil {
			return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	command.WaitDelay = 2 * time.Second
	if err := command.Run(); err != nil {
		return ErrorInvalid
	}
	return nil
}

// Rootless refuses host-root Docker so the protocol cannot obtain host-admin access.
// Rootless отклоняет host-root Docker, не предоставляя протоколу host-admin доступ.
func (DockerRunner) Rootless(ctx context.Context) bool {
	if os.Geteuid() == 0 {
		return false
	}
	command, err := dockerCommand(ctx, "info", "--format", `{"securityOptions":{{json .SecurityOptions}},"cgroupDriver":{{json .CgroupDriver}},"cgroupVersion":{{json .CgroupVersion}}}`)
	if err != nil {
		return false
	}
	pipe, err := command.StdoutPipe()
	if err != nil {
		return false
	}
	command.Stderr = io.Discard
	if command.Start() != nil {
		return false
	}
	raw, readErr := io.ReadAll(io.LimitReader(pipe, 4097))
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil || len(raw) > 4096 {
		return false
	}
	var info struct {
		SecurityOptions []string `json:"securityOptions"`
		CgroupDriver    string   `json:"cgroupDriver"`
		CgroupVersion   string   `json:"cgroupVersion"`
	}
	if Decode(raw, &info, 4096) != nil || info.CgroupDriver != "systemd" || info.CgroupVersion != "2" {
		return false
	}
	for _, option := range info.SecurityOptions {
		if option == "name=rootless" {
			return true
		}
	}
	return false
}

// dockerCommand fixes the executable, rootless socket and environment independently of SSH client input.
// dockerCommand закрепляет executable, rootless socket и environment независимо от SSH client input.
func dockerCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	account, err := user.LookupId(strconv.Itoa(os.Geteuid()))
	if err != nil || os.Geteuid() == 0 || !filepath.IsAbs(account.HomeDir) {
		return nil, ErrorInvalid
	}
	runtimeDirectory := "/run/user/" + strconv.Itoa(os.Geteuid())
	command := exec.CommandContext(ctx, "/usr/bin/docker", args...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + account.HomeDir, "XDG_RUNTIME_DIR=" + runtimeDirectory, "DOCKER_HOST=unix://" + runtimeDirectory + "/docker.sock", "DOCKER_CONFIG=/etc/kodex-ssh-compose/docker-client"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.Cancel = func() error {
		if command.Process != nil {
			return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	command.WaitDelay = 2 * time.Second
	return command, nil
}

// Helper owns the forced-command endpoint with an independently provisioned public trust set.
// Helper владеет endpoint принудительной команды с отдельно установленными public trust keys.
type Helper struct {
	Target        Target
	OwnerKeys     map[string]ed25519.PublicKey
	RootDirectory string
	Runner        CommandRunner
	Health        func(context.Context, int, string) bool
}

// Serve verifies each control frame before state use and joins the deployment before returning.
// Serve проверяет каждый control frame до использования и ждёт завершения deploy.
func (helper Helper) Serve(ctx context.Context, input io.ReadCloser, output io.Writer) error {
	// Caller предоставляет Close-interruptible input. Watcher закрывает его и
	// до первого frame при cancel; output остаётся отдельным owner-owned stream.
	stopInput, joinedInput := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joinedInput)
		select {
		case <-ctx.Done():
			_ = input.Close()
		case <-stopInput:
		}
	}()
	defer func() { close(stopInput); _ = input.Close(); <-joinedInput }()
	if helper.Target.Validate() != nil || helper.Runner == nil || !filepath.IsAbs(helper.RootDirectory) || filepath.Clean(helper.RootDirectory) != helper.RootDirectory || strings.ContainsAny(helper.RootDirectory, "$:\r\n\x00") {
		return ErrorInvalid
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), MaximumBundleBytes+16384)
	if !scanner.Scan() {
		return ErrorInvalid
	}
	var request Request
	if Decode(scanner.Bytes(), &request, MaximumBundleBytes+16384) != nil {
		return ErrorInvalid
	}
	permit, err := Verify(request.Permit, helper.OwnerKeys, time.Now())
	if err != nil || permit.Target != helper.Target {
		return ErrorInvalid
	}
	if permit.Purpose == "STATUS" {
		if request.Bundle != nil {
			return ErrorInvalid
		}
		receipt, readErr := ReadReceipt(filepath.Join(helper.RootDirectory, "journal"), permit.DeploymentRef)
		if readErr != nil || !receipt.Matches(permit) {
			return ErrorInvalid
		}
		// A durable in-progress receipt is observable, but never claims a successful effect.
		// Durable in-progress receipt доступен для чтения, но не подтверждает успешный эффект.
		if receipt.State == "RESERVED" || receipt.State == "RUNNING" {
			receipt.State = "UNKNOWN_OUTCOME"
			receipt.SafeErrorCode = "SSH_DEPLOYMENT_OUTCOME_UNKNOWN"
		}
		return json.NewEncoder(output).Encode(Response{Version: ProtocolVersion, TargetRef: helper.Target.Ref, Ready: true, Receipt: &receipt})
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	ready := helper.Runner.Rootless(bounded)
	cancel()
	if !ready {
		return ErrorInvalid
	}
	if permit.Purpose == "INSPECT" {
		if request.Bundle != nil {
			return ErrorInvalid
		}
		return json.NewEncoder(output).Encode(Response{Version: ProtocolVersion, TargetRef: helper.Target.Ref, Ready: true})
	}
	journal, err := OpenJournal(filepath.Join(helper.RootDirectory, "journal"))
	if err != nil {
		return err
	}
	defer journal.Close()
	if permit.Purpose != "DEPLOY" || request.Bundle == nil {
		return ErrorInvalid
	}
	raw, manifest, err := request.Bundle.Canonical()
	if err != nil || manifest.Image != helper.Target.RuntimeImage || Digest(raw) != permit.BundleSHA256 || request.Bundle.Repository != permit.Target.Repository || request.Bundle.MergedSHA != permit.MergedSHA || request.Bundle.TreeSHA != permit.TreeSHA {
		return ErrorInvalid
	}
	receipt, fresh, err := journal.Reserve(permit, manifest.Image)
	if err != nil {
		return err
	}
	if !fresh {
		if receipt.State == "RESERVED" || receipt.State == "RUNNING" {
			receipt.State = "UNKNOWN_OUTCOME"
		}
		return json.NewEncoder(output).Encode(Response{Version: ProtocolVersion, TargetRef: helper.Target.Ref, Ready: true, Receipt: &receipt})
	}
	deployment, cancelDeployment := context.WithDeadline(ctx, time.Unix(permit.Deadline, 0))
	defer cancelDeployment()
	var mutex sync.Mutex
	expiresAt := permit.ExpiresAt
	watchDone := make(chan struct{})
	controlsDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		timer := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-deployment.Done():
				return
			case <-timer.C:
				mutex.Lock()
				expiry := expiresAt
				mutex.Unlock()
				if time.Now().Unix() >= expiry {
					cancelDeployment()
					return
				}
			}
		}
	}()
	go func() {
		defer close(controlsDone)
		for scanner.Scan() {
			var control Request
			if Decode(scanner.Bytes(), &control, 16384) != nil || control.Bundle != nil {
				cancelDeployment()
				return
			}
			renewal, verifyErr := Verify(control.Permit, helper.OwnerKeys, time.Now())
			if verifyErr != nil || renewal.Target != helper.Target || renewal.InvocationRef != permit.InvocationRef {
				cancelDeployment()
				return
			}
			mutex.Lock()
			updated, renewErr := journal.Renew(receipt, renewal)
			if renewErr == nil {
				receipt = updated
				expiresAt = renewal.ExpiresAt
			}
			mutex.Unlock()
			if renewErr != nil {
				cancelDeployment()
				return
			}
		}
		cancelDeployment()
	}()
	releaseDirectory := filepath.Join(helper.RootDirectory, "releases", permit.BundleSHA256)
	if err = extractBundle(releaseDirectory, *request.Bundle); err == nil {
		var compose []byte
		compose, err = Compose(manifest, helper.Target, filepath.Join(releaseDirectory, "source"))
		if err == nil {
			err = writeImmutable(filepath.Join(releaseDirectory, "compose.json"), compose, 0600)
		}
	}
	mutex.Lock()
	if err == nil && deployment.Err() == nil {
		receipt, err = journal.Complete(receipt, "RUNNING", "", false)
	}
	mutex.Unlock()
	started := false
	if err == nil && deployment.Err() == nil {
		started = true
		project := "kodex_" + Digest([]byte(helper.Target.ProjectRef + "\x00" + helper.Target.Ref))[:16]
		err = helper.Runner.Run(deployment, []string{"docker", "compose", "--project-name", project, "--file", filepath.Join(releaseDirectory, "compose.json"), "up", "--no-build", "--pull", "always", "--detach", "--wait", "--wait-timeout", strconv.Itoa(int(MaximumLifetime.Seconds() - 5))})
	}
	healthy := false
	if err == nil && deployment.Err() == nil {
		health := helper.Health
		if health == nil {
			health = localHealth
		}
		for deployment.Err() == nil {
			if health(deployment, helper.Target.PublishedPort, manifest.HealthPath) {
				healthy = true
				break
			}
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-deployment.Done():
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
		}
	}
	mutex.Lock()
	state, code := "FAILED", "SSH_DEPLOYMENT_FAILED"
	if err == nil && deployment.Err() == nil && healthy {
		state, code = "SUCCEEDED", ""
	} else if deployment.Err() != nil && started {
		state, code = "UNKNOWN_OUTCOME", "SSH_DEPLOYMENT_OUTCOME_UNKNOWN"
	} else if deployment.Err() != nil {
		state, code = "CANCELLED", "SSH_DEPLOYMENT_CANCELLED"
	}
	final, completeErr := journal.Complete(receipt, state, code, healthy)
	mutex.Unlock()
	cancelDeployment()
	_ = input.Close()
	<-watchDone
	<-controlsDone
	if completeErr != nil {
		return completeErr
	}
	return json.NewEncoder(output).Encode(Response{Version: ProtocolVersion, TargetRef: helper.Target.Ref, Ready: true, Receipt: &final})
}

// localHealth performs one bounded loopback HTTP probe without redirects or proxies.
// localHealth выполняет ограниченный loopback HTTP probe без redirects и proxy.
func localHealth(ctx context.Context, port int, path string) bool {
	client := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode == http.StatusOK
}

// extractBundle writes only regular source files into a private immutable release.
// extractBundle записывает только обычные source files в private immutable release.
func extractBundle(directory string, bundle Bundle) error {
	// A full release directory is at most 2 MiB; retaining at most 32 bounds source disk use.
	// Полный release directory занимает до 2 MiB; предел 32 ограничивает расход диска.
	if err := privateDirectories(filepath.Dir(directory)); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Dir(directory))
	if err != nil {
		return err
	}
	if _, err := os.Lstat(directory); os.IsNotExist(err) && len(entries) >= 32 {
		return ErrorInvalid
	}
	if err := privateDirectories(filepath.Join(directory, "source")); err != nil {
		return err
	}
	for _, file := range bundle.Files {
		name := filepath.Join(directory, "source", filepath.FromSlash(file.Path))
		if err := privateDirectories(filepath.Dir(name)); err != nil {
			return err
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(file.Content)
		if err != nil {
			return ErrorInvalid
		}
		if err := writeImmutable(name, raw, 0644); err != nil {
			return err
		}
	}
	return nil
}

// privateDirectories refuses links on every existing ancestor and uses owner-only modes.
// privateDirectories отклоняет ссылки на каждом существующем предке и private modes.
func privateDirectories(directory string) error {
	if !filepath.IsAbs(directory) {
		return ErrorInvalid
	}
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(directory, "/"), "/") {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrorInvalid
		}
	}
	return nil
}

// writeImmutable accepts existing bytes only after same-descriptor identity checks.
// writeImmutable принимает существующие bytes только после проверки того же descriptor.
func writeImmutable(name string, raw []byte, mode os.FileMode) error {
	fd, err := syscall.Open(name, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, uint32(mode))
	if err == nil {
		file := os.NewFile(uintptr(fd), "release-file")
		_, writeErr := file.Write(raw)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if !os.IsExist(err) {
		return err
	}
	fd, err = syscall.Open(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "existing-release")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(raw)) {
		return ErrorInvalid
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Nlink != 1 || metadata.Uid != uint32(os.Geteuid()) {
		return ErrorInvalid
	}
	previous, err := io.ReadAll(io.LimitReader(file, int64(len(raw))+1))
	if err != nil || Digest(previous) != Digest(raw) {
		return ErrorInvalid
	}
	return nil
}
