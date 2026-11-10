package managedworkspace

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	"golang.org/x/sys/unix"
)

const (
	// ExecutorMode is a private secret-free container entrypoint, never a provider shell mode.
	// ExecutorMode — частная точка входа контейнера без секретов, не shell процесса провайдера.
	ExecutorMode = runtimecontract.WorkspaceExecutorMode
	// ExecutorSocket is mounted only into the trusted role and the secret-free executor.
	// ExecutorSocket монтируется только в доверенную роль и исполнитель без секретов.
	ExecutorSocket = runtimecontract.WorkspaceExecutorSocket
	// maximumCommandSeconds is a hard ceiling independent of model-selected timeouts.
	// maximumCommandSeconds — жёсткий предел независимо от выбранного моделью timeout.
	maximumCommandSeconds = runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds - 15
	// maximumCommandOutput limits combined stdout/stderr retained for the admitted initiator.
	// maximumCommandOutput ограничивает общий stdout/stderr для допущенного инициатора.
	maximumCommandOutput = 64 << 10
)

// errExecutor is the fixed boundary failure without raw commands, paths or process output.
// errExecutor — фиксированный boundary failure без сырых команд, путей и process output.
var errExecutor = errors.New("managed workspace command is unavailable")

// ExecutorRequest is a closed role-to-executor protocol. Initialization and quiescence are
// trusted lifecycle operations; model data is restricted to the RUN argument fields.
// ExecutorRequest — закрытый role-to-executor protocol. Initialization и quiescence принадлежат
// доверенному lifecycle; model data ограничены аргументами RUN.
type ExecutorRequest struct {
	Action         string   `json:"action"`
	Binding        string   `json:"binding"`
	Commands       []string `json:"commands,omitempty"`
	Command        string   `json:"command,omitempty"`
	Arguments      []string `json:"arguments,omitempty"`
	Directory      string   `json:"directory,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// CommandResult distinguishes observed exit, timeout and unknown outcomes. It never implies
// external CI success and is not automatically retried after transport or process uncertainty.
// CommandResult различает observed exit, timeout и unknown outcome. Успех внешнего CI не
// подразумевается; transport/process uncertainty никогда не вызывает автоматический повтор.
type CommandResult struct {
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// cappedOutput serializes concurrent stdout/stderr writes and discards overflow without blocking
// a child on logging. It retains no environment, authentication data or separate durable log.
// cappedOutput сериализует stdout/stderr и отбрасывает overflow без блокировки child на логах.
// Environment, auth data и отдельного durable log не хранит.
type cappedOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

// Write retains only the bounded prefix and acknowledges all input to avoid pipe-induced retries.
// Write сохраняет лишь ограниченный префикс и подтверждает весь input без pipe-induced retry.
func (output *cappedOutput) Write(value []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	n := min(len(value), maximumCommandOutput-len(output.data))
	output.data = append(output.data, value[:n]...)
	output.truncated = output.truncated || n < len(value)
	return len(value), nil
}

// commandExecutor serializes all commands and owns one immutable initialization. The production
// instance runs as PID 1 in its own container PID namespace and carries no credential mounts.
// commandExecutor сериализует команды и владеет одной immutable initialization. Production
// instance — PID 1 собственного container PID namespace без credential mounts.
type commandExecutor struct {
	mu       sync.Mutex
	binding  string
	commands []string
	closed   bool
	cleanup  func() error
	probe    func(context.Context) error
}

// executorIdentity verifies the private PID-1/UID/GID boundary before any namespace-wide kill.
// Pod render/observation independently prohibit host/shared PID and credential mounts.
// executorIdentity проверяет private PID-1/UID/GID boundary до namespace-wide kill. Pod render
// и observation независимо запрещают host/shared PID и credential mounts.
func executorIdentity() bool {
	return os.Getpid() == 1 && os.Geteuid() == 10002 && os.Getegid() == 29000
}

// reapExecutorChildren kills and reaps descendants in the private namespace after every command,
// including detached process groups. It refuses to run in a host/unit-test process; failure closes
// the executor, so further file access cannot assume quiescence.
// reapExecutorChildren убивает и собирает потомков в private namespace после каждой команды,
// включая detached process groups. В host/unit-test process не запускается; отказ закрывает
// executor, поэтому дальнейший file access не может предположить quiescence.
func reapExecutorChildren() error {
	if !executorIdentity() {
		return errExecutor
	}
	if err := unix.Kill(-1, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return errExecutor
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil)
		if errors.Is(err, unix.ECHILD) {
			return nil
		}
		if err != nil && !errors.Is(err, unix.EINTR) {
			return errExecutor
		}
		if pid == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return errExecutor
}

// ServeExecutor starts the fixed UDS only in the isolated PID-1 container, drops dumpability,
// bounds inherited process counts and joins owned HTTP requests on shutdown. A caller outside
// the role UID is rejected before reading a body; credentials and arbitrary environment are absent.
// ServeExecutor запускает fixed UDS лишь в isolated PID-1 container, запрещает dumpability,
// ограничивает inherited process count и завершает свои HTTP requests при shutdown. Peer вне
// role UID отклоняется до чтения body; credentials и произвольного environment нет.
func ServeExecutor(ctx context.Context) error {
	if !executorIdentity() || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return errExecutor
	}
	unix.Umask(0o007)
	if unix.Setrlimit(unix.RLIMIT_NPROC, &unix.Rlimit{Cur: 256, Max: 256}) != nil {
		return errExecutor
	}
	if !security.CommandFilesystemAvailable() {
		return errExecutor
	}
	parent, err := unix.Open("/run/kodex/workspace", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errExecutor
	}
	err = unix.Mkdirat(parent, "authority", 0o2770)
	unix.Close(parent)
	if err != nil {
		return errExecutor
	}
	directory, err := unix.Open(filepath.Dir(ExecutorSocket), unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errExecutor
	}
	defer unix.Close(directory)
	var info unix.Stat_t
	if unix.Fstat(directory, &info) != nil || info.Gid != 29000 || info.Mode&0o007 != 0 {
		return errExecutor
	}
	listener, err := net.Listen("unix", ExecutorSocket)
	if err != nil {
		return errExecutor
	}
	defer listener.Close()
	defer os.Remove(ExecutorSocket)
	if os.Chmod(ExecutorSocket, 0o660) != nil {
		return errExecutor
	}
	executor := &commandExecutor{cleanup: reapExecutorChildren, probe: probeExecutorSandbox}
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	closed := make(chan struct{})
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/execute" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
		var request ExecutorRequest
		if err != nil || len(raw) > 64<<10 || internalrpcauth.DecodeStrictJSON(raw, &request) != nil {
			http.Error(w, "invalid command request", http.StatusBadRequest)
			return
		}
		result, err := executor.handle(r.Context(), request)
		if err != nil {
			http.Error(w, "command unavailable", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
		if request.Action == "QUIESCE" {
			select {
			case <-closed:
			default:
				close(closed)
			}
		}
	})
	done := make(chan error, 1)
	go func() { done <- server.Serve(&executorPeerListener{Listener: listener}) }()
	select {
	case <-ctx.Done():
	case <-closed:
	case err := <-done:
		return err
	}
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if server.Shutdown(shutdown) != nil {
		_ = server.Close()
		return errExecutor
	}
	executor.mu.Lock()
	executor.closed = true
	cleanupErr := executor.cleanup()
	executor.mu.Unlock()
	if cleanupErr != nil {
		return cleanupErr
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return errExecutor
		}
	case <-shutdown.Done():
		return errExecutor
	}
	return nil
}

// executorPeerListener rejects every UDS peer except the trusted role UID. The provider and
// repository commands share no socket mount, even though their separate PID namespaces reuse UID.
// executorPeerListener отвергает все UDS peers кроме role UID. Provider и repository commands
// не имеют socket mount друг друга, хотя отдельные PID namespaces используют одинаковый UID.
type executorPeerListener struct{ net.Listener }

// Accept checks SO_PEERCRED before handing a connection to HTTP; unavailable identity fails shut.
// Accept проверяет SO_PEERCRED до передачи connection в HTTP; неизвестная identity закрывает допуск.
func (listener *executorPeerListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		allowed := false
		if unixConnection, ok := connection.(*net.UnixConn); ok {
			raw, e := unixConnection.SyscallConn()
			if e == nil {
				e = raw.Control(func(fd uintptr) {
					credential, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
					allowed = e == nil && credential.Uid == 10001
				})
				allowed = allowed && e == nil
			}
		}
		if allowed {
			return connection, nil
		}
		_ = connection.Close()
	}
}

// handle pins one role-selected command list and refuses unknown actions, rebinding and all work
// after quiescence. RUN is serialized; a cleanup failure permanently closes this instance.
// handle закрепляет один role-selected список команд и запрещает unknown actions, rebinding и
// работу после quiescence. RUN сериализован; cleanup failure навсегда закрывает instance.
func (executor *commandExecutor) handle(ctx context.Context, request ExecutorRequest) (CommandResult, error) {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.closed || len(request.Binding) != 64 {
		return CommandResult{}, errExecutor
	}
	if request.Action == "INITIALIZE" {
		if request.Command != "" || len(request.Arguments) != 0 || request.Directory != "" || request.TimeoutSeconds != 0 || len(request.Commands) > 128 {
			return CommandResult{}, errExecutor
		}
		if _, err := hexDigest(request.Binding); err != nil {
			return CommandResult{}, errExecutor
		}
		for _, command := range request.Commands {
			if _, err := commandPath(command); err != nil {
				return CommandResult{}, errExecutor
			}
		}
		if executor.binding != "" && (executor.binding != request.Binding || !slices.Equal(executor.commands, request.Commands)) {
			return CommandResult{}, errExecutor
		}
		if executor.binding == "" && (executor.probe == nil || executor.probe(ctx) != nil) {
			executor.closed = true
			return CommandResult{}, errExecutor
		}
		executor.binding = request.Binding
		executor.commands = append([]string(nil), request.Commands...)
		return CommandResult{Status: "READY", ExitCode: 0}, nil
	}
	if request.Action == "QUIESCE" && executor.binding == "" {
		if _, err := hexDigest(request.Binding); err != nil {
			return CommandResult{}, errExecutor
		}
		executor.binding = request.Binding
	}
	if request.Binding != executor.binding || len(request.Commands) != 0 {
		return CommandResult{}, errExecutor
	}
	if request.Action == "QUIESCE" {
		if request.Command != "" || len(request.Arguments) != 0 || request.Directory != "" || request.TimeoutSeconds != 0 {
			return CommandResult{}, errExecutor
		}
		executor.closed = true
		if executor.cleanup() == nil {
			return CommandResult{Status: "QUIESCENT", ExitCode: 0}, nil
		}
		return CommandResult{}, errExecutor
	}
	if request.Action != "RUN" || !slices.Contains(executor.commands, request.Command) || !validCommandArguments(request) {
		return CommandResult{}, errExecutor
	}
	result, err := runCommand(ctx, request, executor.cleanup)
	if err != nil {
		executor.closed = true
	}
	return result, err
}

// commandPath resolves only a registered immutable image tool from its fixed absolute candidates.
// PATH, repository files and model-provided executable paths never participate in selection.
// commandPath разрешает лишь registered image tool из фиксированных absolute candidates.
// PATH, файлы репозитория и model-provided executable paths в выборе не участвуют.
func commandPath(name string) (string, error) {
	for _, probe := range runtimecontract.ImageToolProbes() {
		if probe.Name != name && !slices.ContainsFunc(probe.Paths, func(candidate string) bool { return filepath.Base(candidate) == name }) {
			continue
		}
		for _, candidate := range probe.Paths {
			info, err := os.Stat(candidate)
			if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
				return candidate, nil
			}
		}
	}
	return "", errExecutor
}

// validCommandArguments bounds arguments and permits only canonical relative work directories.
// It does not classify source code: the container isolation remains mandatory for every command.
// validCommandArguments ограничивает аргументы и допускает canonical relative work directory.
// Исходный код не классифицирует: container isolation обязательна для каждой команды.
func validCommandArguments(request ExecutorRequest) bool {
	if request.TimeoutSeconds < 1 || request.TimeoutSeconds > maximumCommandSeconds || len(request.Arguments) > 64 || len(request.Directory) > 1024 ||
		(request.Directory != "work" && !strings.HasPrefix(request.Directory, "work/")) || filepath.Clean(request.Directory) != request.Directory || strings.ContainsAny(request.Directory, "\\\x00\n\r") {
		return false
	}
	total := 0
	for _, value := range request.Arguments {
		if strings.ContainsRune(value, 0) || len(value) > 8192 {
			return false
		}
		total += len(value)
	}
	return total <= 32768
}

// runCommand executes one selected binary with an explicit secret-free environment and a
// descriptor-pinned CWD. All descendants are quiesced before return, including on timeout;
// unknown cleanup never becomes success and no transport retry is performed here.
// runCommand запускает выбранный binary с явным environment без секретов и descriptor-pinned
// CWD. Все потомки останавливаются до возврата, включая timeout; неизвестный cleanup не
// становится успехом, transport retry здесь не выполняется.
func runCommand(parent context.Context, request ExecutorRequest, cleanup func() error) (CommandResult, error) {
	return runCommandInWork(parent, request, runtimecontract.RuntimeWorkspaceWorkingRoot, cleanup)
}

// runCommandInWork takes a trusted test/production root, never a model-selected absolute path.
// It does not add isolation; the production caller must establish the secret-free container.
// runCommandInWork получает доверенный test/production root, не model-selected absolute path.
// Изоляции не добавляет; production caller обязан подготовить контейнер без секретов.
func runCommandInWork(parent context.Context, request ExecutorRequest, workRoot string, cleanup func() error) (CommandResult, error) {
	executable, err := commandPath(request.Command)
	if err != nil {
		return CommandResult{}, err
	}
	root, err := unix.Open(workRoot, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return CommandResult{}, errExecutor
	}
	defer unix.Close(root)
	relative := strings.TrimPrefix(strings.TrimPrefix(request.Directory, "work"), "/")
	if relative == "" {
		relative = "."
	}
	fd, err := unix.Openat2(root, relative, &unix.OpenHow{Flags: unix.O_DIRECTORY | unix.O_RDONLY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return CommandResult{}, errExecutor
	}
	cwd := os.NewFile(uintptr(fd), "managed-command-cwd")
	defer cwd.Close()
	ctx, cancel := context.WithTimeout(parent, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, request.Arguments...)
	if workRoot == runtimecontract.RuntimeWorkspaceWorkingRoot {
		command = exec.CommandContext(ctx, "/usr/local/bin/kodex-agent-runner", append([]string{CommandChildMode, request.Command}, request.Arguments...)...)
	}
	command.Env = commandEnvironment()
	command.Dir = "/proc/self/fd/" + strconv.Itoa(int(cwd.Fd()))
	command.Stdin = bytes.NewReader(nil)
	output := &cappedOutput{}
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
	if command.Start() != nil {
		return CommandResult{}, errExecutor
	}
	err = command.Wait()
	if cleanup() != nil {
		return CommandResult{Status: "UNKNOWN", ExitCode: -1}, errExecutor
	}
	result := CommandResult{Status: "SUCCEEDED", ExitCode: 0, Output: strings.ToValidUTF8(string(output.data), "�"), Truncated: output.truncated}
	if ctx.Err() != nil {
		result.Status = "TIMED_OUT"
		if errors.Is(parent.Err(), context.Canceled) {
			result.Status = "CANCELLED"
		}
		result.ExitCode = -1
	} else if err != nil {
		result.Status = "FAILED"
		result.ExitCode = command.ProcessState.ExitCode()
	}
	return result, nil
}

// hexDigest validates the exact lower-case SHA-256 initialization binding without trusting it as
// execution authority; UDS peer identity and controller admission provide that boundary.
// hexDigest проверяет exact lower-case SHA-256 initialization binding, не считая его authority;
// эту границу обеспечивают UDS peer identity и controller admission.
func hexDigest(value string) ([]byte, error) {
	if len(value) != 64 || strings.ToLower(value) != value {
		return nil, errExecutor
	}
	data, err := hex.DecodeString(value)
	if err != nil {
		return nil, errExecutor
	}
	return data, nil
}

// probeExecutorSandbox verifies the actual child entrypoint's rules before any source command
// or model access. Probe failure closes initialization; it never retries a repository effect.
// probeExecutorSandbox проверяет actual child entrypoint до source command либо доступа модели.
// Probe failure закрывает initialization и никогда не повторяет repository effect.
func probeExecutorSandbox(parent context.Context) error {
	if !executorIdentity() {
		return errExecutor
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/local/bin/kodex-agent-runner", CommandChildMode, "--sandbox-probe")
	command.Env = commandEnvironment()
	command.Dir = runtimecontract.RuntimeWorkspaceWorkingRoot
	command.Stdin = bytes.NewReader(nil)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	err := command.Run()
	cleanupErr := reapExecutorChildren()
	if err != nil || cleanupErr != nil {
		return errExecutor
	}
	return nil
}
