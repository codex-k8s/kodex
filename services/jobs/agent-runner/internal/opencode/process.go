package opencode

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	// Executable is the immutable image-owned runtime, never selected by a prompt or restored archive.
	// Executable — immutable runtime из образа; prompt или восстановленный архив не выбирают программу.
	Executable = "/usr/local/bin/opencode"
	// processJoinTimeout bounds termination and join, independently of the business execution deadline.
	// processJoinTimeout ограничивает termination/join независимо от бизнес-срока исполнения.
	processJoinTimeout = 3 * time.Second
)

// ErrWriterActive prevents checkpoint publication when process-group termination is unproved.
// ErrWriterActive запрещает публикацию checkpoint без доказанной остановки process group.
var ErrWriterActive = errors.New("OpenCode native writer termination is unconfirmed")

// Server owns one private native process. Stop must join it before official export can read its DB.
// The enclosing broker remains responsible for trusted HOME ownership, sandbox and single-writer fencing.
// Server владеет одним приватным native-процессом. Stop ожидает его до чтения DB официальным export.
// Broker отвечает за доверенный HOME, sandbox и single-writer fencing.
type Server struct {
	Client      *Client
	command     *exec.Cmd
	home        string
	joined      bool
	joinFailure error
}

// Start launches only the pinned image executable; callers supply an explicit credential-confined env.
// It does not inherit host environment or expose the upstream server beyond authenticated loopback.
// A nonnil resource returned with an error must be retained until Stop positively joins its writer.
// Start запускает только закреплённый executable образа с явно ограниченным окружением credentials.
// Host environment не наследуется; upstream server доступен лишь по авторизованному loopback.
// Ненулевой resource при ошибке сохраняется до подтверждённого join writer вызовом Stop.
func Start(ctx context.Context, home string, environment []string) (*Server, error) {
	return start(ctx, Executable, home, environment)
}

// start permits an exact executable override only to same-package native qualification tests.
// start допускает подмену exact executable только для native qualification tests этого пакета.
func start(ctx context.Context, binary, home string, environment []string) (*Server, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || ctx.Err() != nil {
		return nil, ErrProtocol
	}
	if validateHome(home) != nil {
		return nil, ErrProtocol
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, ErrUnavailable
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, ErrUnavailable
	}
	password := base64.RawURLEncoding.EncodeToString(random[:])
	command := exec.Command(binary, "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	command.Dir = home
	command.Env = append(append([]string{}, environment...), "OPENCODE_SERVER_USERNAME=kodex", "OPENCODE_SERVER_PASSWORD="+password)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = processJoinTimeout
	client, err := NewClient("http://127.0.0.1:"+strconv.Itoa(port), "kodex", password)
	if err != nil {
		return nil, err
	}
	if command.Start() != nil {
		client.Close()
		return nil, ErrUnavailable
	}
	server := &Server{Client: client, command: command, home: home}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		err := client.Health(probe)
		cancel()
		if err == nil {
			return server, nil
		}
		select {
		case <-ctx.Done():
			if server.Stop() != nil {
				return server, ErrWriterActive
			}
			return nil, ctx.Err()
		case <-deadline.C:
			if server.Stop() != nil {
				return server, ErrWriterActive
			}
			return nil, ErrUnavailable
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// Stop joins the leader and verifies the entire owned process group has gone. An exit status alone
// never proves workspace quiescence; surviving children keep checkpoint publication closed.
// Stop ожидает leader и проверяет отсутствие всей принадлежащей process group. Один exit status
// не доказывает quiescence workspace; оставшиеся children блокируют публикацию checkpoint.
func (server *Server) Stop() error {
	if server.joined {
		return nil
	}
	if server.joinFailure != nil {
		return server.joinFailure
	}
	server.Client.Close()
	if err := joinCommand(server.command); err != nil {
		server.joinFailure = err
		return err
	}
	server.joined = true
	server.command.Env = offlineEnvironment(server.home)
	return nil
}

// joinCommand terminates and reaps only its owned group, including adopted descendants where possible.
// joinCommand завершает и ожидает лишь принадлежащую command группу, включая доступных adopted descendants.
func joinCommand(command *exec.Cmd) error {
	if command == nil || command.Process == nil || command.Process.Pid <= 1 || command.SysProcAttr == nil || !command.SysProcAttr.Setpgid || command.SysProcAttr.Pgid != 0 {
		return ErrWriterActive
	}
	if command.ProcessState == nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case err := <-done:
			if errors.Is(err, exec.ErrWaitDelay) {
				return ErrWriterActive
			}
		case <-time.After(processJoinTimeout):
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			select {
			case err := <-done:
				if errors.Is(err, exec.ErrWaitDelay) {
					return ErrWriterActive
				}
			case <-time.After(processJoinTimeout):
				return ErrWriterActive
			}
		}
	}
	deadline := time.Now().Add(processJoinTimeout)
	for {
		if errors.Is(syscall.Kill(-command.Process.Pid, 0), syscall.ESRCH) {
			return nil
		}
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		for {
			var status syscall.WaitStatus
			pid, err := syscall.Wait4(-command.Process.Pid, &status, syscall.WNOHANG, nil)
			if err != nil || pid <= 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			return ErrWriterActive
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// offlineEnvironment excludes model/MCP credentials and arbitrary plugins when official CLI reads state.
// offlineEnvironment исключает model/MCP credentials и произвольные plugins при чтении state официальным CLI.
func offlineEnvironment(home string) []string {
	return []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"), "XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"OPENCODE_DISABLE_PROJECT_CONFIG=true", "OPENCODE_DISABLE_MODELS_FETCH=true", "OPENCODE_DISABLE_AUTOUPDATE=true",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=true", "OPENCODE_LOG_LEVEL=ERROR", `OPENCODE_CONFIG_CONTENT={"enabled_providers":[],"plugin":[],"permission":{"*":"deny"}}`}
}

// Export uses the official CLI after positive process join. Output goes directly to a new private file:
// upstream 1.19.5 can truncate large stdout pipes while returning zero. Validation never trusts exit alone.
// Export вызывает официальный CLI после positive process join и пишет прямо в новый private file:
// upstream 1.19.5 может обрезать большой stdout pipe с нулевым exit. Одного exit для проверки недостаточно.
func (server *Server) Export(ctx context.Context, sessionID, destination string, credentials ...string) (Checkpoint, error) {
	if !server.joined {
		return Checkpoint{}, ErrWriterActive
	}
	if !nativeSessionPattern.MatchString(sessionID) || !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return Checkpoint{}, ErrCheckpoint
	}
	output, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Checkpoint{}, ErrCheckpoint
	}
	defer output.Close()
	accepted := false
	defer func() {
		if !accepted {
			_ = os.Remove(destination)
		}
	}()
	command := exec.CommandContext(ctx, server.command.Path, "export", sessionID)
	command.Dir, command.Env = server.home, offlineEnvironment(server.home)
	command.Stdout, command.Stderr = output, io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
	runErr := command.Run()
	joinErr := joinCommand(command)
	if runErr != nil || joinErr != nil || output.Sync() != nil {
		return Checkpoint{}, ErrCheckpoint
	}
	checkpoint, err := inspectCheckpointDescriptor(output, sessionID, "")
	if err != nil {
		return Checkpoint{}, err
	}
	credentials = append(append([]string{}, credentials...), server.Client.password)
	if err := checkpointExcludesCredentials(output, credentials); err != nil {
		return Checkpoint{}, err
	}
	accepted = true
	return checkpoint, nil
}

// inspectCheckpointFile reads a protected bounded export and optionally checks its server-owned digest.
// inspectCheckpointFile читает protected bounded export и проверяет переданный серверный digest.
func inspectCheckpointFile(path, sessionID, expectedSHA256 string) (Checkpoint, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Checkpoint{}, ErrCheckpoint
	}
	defer file.Close()
	return inspectCheckpointDescriptor(file, sessionID, expectedSHA256)
}

// inspectCheckpointDescriptor verifies the same opened inode before an official offline operation.
// The owning broker separately confines its parent directory and excludes all other writers.
// inspectCheckpointDescriptor проверяет тот же открытый inode перед официальной offline-операцией.
// Broker отдельно ограничивает parent directory и исключает остальных writers.
func inspectCheckpointDescriptor(file *os.File, sessionID, expectedSHA256 string) (Checkpoint, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || (info.Mode().Perm() != 0o600 && info.Mode().Perm() != 0o640) || info.Size() < 1 || info.Size() > MaximumCheckpointBytes {
		return Checkpoint{}, ErrCheckpoint
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return Checkpoint{}, ErrCheckpoint
	}
	if info.Mode().Perm() == 0o640 && stat.Gid != 29000 {
		return Checkpoint{}, ErrCheckpoint
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Checkpoint{}, ErrCheckpoint
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaximumCheckpointBytes+1))
	if err != nil || int64(len(raw)) != info.Size() {
		return Checkpoint{}, ErrCheckpoint
	}
	checkpoint, err := InspectCheckpoint(raw, sessionID)
	if err != nil {
		return Checkpoint{}, err
	}
	if expectedSHA256 != "" && checkpoint.SHA256 != expectedSHA256 {
		return Checkpoint{}, ErrCheckpoint
	}
	return checkpoint, nil
}

// Restore imports a verified server-selected export into a new isolated HOME using the official CLI.
// An initialized or uncertain native data directory is never merged, cleared or silently replaced.
// Restore импортирует проверенный серверный export в новый isolated HOME официальным CLI.
// Инициализированный или uncertain native data directory не объединяется, не очищается и не подменяется.
func Restore(ctx context.Context, home, source, sessionID, expectedSHA256 string) error {
	return restore(ctx, Executable, home, source, sessionID, expectedSHA256)
}

// restore exposes the executable override only to this package's real native qualification tests.
// restore допускает executable override только в настоящих native qualification tests этого пакета.
func restore(ctx context.Context, binary, home, source, sessionID, expectedSHA256 string) error {
	if len(expectedSHA256) != 64 || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return ErrCheckpoint
	}
	if validateHome(home) != nil {
		return ErrCheckpoint
	}
	entries, err := os.ReadDir(filepath.Join(home, "data"))
	if err != nil || len(entries) != 0 {
		return ErrCheckpoint
	}
	// The opened source is inherited as a descriptor, so a pathname replacement cannot redirect import.
	// Source передаётся открытым descriptor: замена pathname не перенаправит import.
	file, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ErrCheckpoint
	}
	defer file.Close()
	if _, err := inspectCheckpointDescriptor(file, sessionID, expectedSHA256); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ErrCheckpoint
	}
	before, err := file.Stat()
	if err != nil {
		return ErrCheckpoint
	}
	command := exec.CommandContext(ctx, binary, "import", "/proc/self/fd/3")
	command.Dir, command.Env = home, offlineEnvironment(home)
	command.ExtraFiles = []*os.File{file}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = processJoinTimeout
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGTERM) }
	runErr := command.Run()
	joinErr := joinCommand(command)
	if runErr != nil || joinErr != nil {
		return ErrCheckpoint
	}
	if _, err := inspectCheckpointDescriptor(file, sessionID, expectedSHA256); err != nil {
		return err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Sys().(*syscall.Stat_t).Ctim != after.Sys().(*syscall.Stat_t).Ctim {
		return ErrCheckpoint
	}
	return nil
}

// validateHome rejects shared or linked HOME/XDG directories; the broker pins their trusted ancestors.
// validateHome отклоняет shared или linked HOME/XDG directories; broker закрепляет доверенных родителей.
func validateHome(home string) error {
	for _, path := range []string{home, filepath.Join(home, "config"), filepath.Join(home, "cache"), filepath.Join(home, "data"), filepath.Join(home, "state")} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return ErrProtocol
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
			return ErrProtocol
		}
	}
	return nil
}
