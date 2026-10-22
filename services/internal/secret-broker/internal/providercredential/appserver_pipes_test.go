package providercredential

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pipeFixture launches a caller-supplied synthetic shell script in its own process group with minimal
// environment, shortens cleanup phases to 100 ms and registers termination cleanup; it never launches
// provider code.
// pipeFixture запускает переданный synthetic shell script в собственной process group с минимальным
// окружением, сокращает cleanup фазы до 100 мс и регистрирует termination cleanup; provider код не
// запускается.
func pipeFixture(t *testing.T, script string) *appServer {
	t.Helper()
	command := exec.Command("/bin/sh", "-c", script)
	command.Env = []string{"PATH=/usr/bin:/bin"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	server, err := startAppServerCommand(command)
	if err != nil {
		t.Fatal("fixture start failed")
	}
	server.shutdownTimeout = 100 * time.Millisecond
	t.Cleanup(func() { _ = server.terminate() })
	return server
}

// TestAppServerOwnedPipesExitAndBufferedResponse waits for the fake child to exit before consuming its
// buffered JSON and diagnostic streams, then requires successful owned-pipe cleanup rather than losing
// data to Cmd.Wait.
// TestAppServerOwnedPipesExitAndBufferedResponse ждёт exit fake child до чтения его buffered JSON и
// diagnostic streams, затем требует успешного owned-pipe cleanup без потери данных через Cmd.Wait.
func TestAppServerOwnedPipesExitAndBufferedResponse(t *testing.T) {
	server := pipeFixture(t, `printf '{"id":1,"result":{}}\n'; printf 'synthetic' >&2`)
	// Лидер уже завершён; buffered stdout и stderr остаются у readers.
	err := <-server.wait
	replay := make(chan error, 1)
	replay <- err
	server.wait = replay
	select {
	case event := <-server.messages:
		if event.err != nil || string(event.message.Result) != "{}" {
			t.Fatal("buffered response lost")
		}
	case <-time.After(time.Second):
		t.Fatal("response read deadline")
	}
	if err := server.terminate(); err != nil {
		t.Fatal("ordinary exit became cleanup failure")
	}
}

// TestAppServerOwnedPipesFailureAndDescendantDeadline runs synthetic nonzero exit, diagnostic overflow,
// TERM-resistant leader and descendant-held-pipe cases, requiring a bounded failed cleanup and joined
// message reader.
// TestAppServerOwnedPipesFailureAndDescendantDeadline запускает synthetic nonzero exit, diagnostic
// overflow, TERM-resistant leader и удержание pipe потомком, требуя ограниченного failed cleanup и
// завершённого message reader.
func TestAppServerOwnedPipesFailureAndDescendantDeadline(t *testing.T) {
	for name, script := range map[string]string{
		"nonzero":         "exit 7",
		"overflow":        "head -c 1048577 /dev/zero >&2",
		"leader-timeout":  "trap '' TERM; sleep 20",
		"descendant-pipe": "sleep 20 & exit 0",
	} {
		t.Run(name, func(t *testing.T) {
			server := pipeFixture(t, script)
			start := time.Now()
			if server.terminate() == nil {
				t.Fatal("real process failure hidden")
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("shutdown unbounded")
			}
			select {
			case <-server.readerDone:
			default:
				t.Fatal("response reader not joined")
			}
		})
	}
}

// diagnosticFailureReader is an always-failing local reader used to prove stderr read errors cannot
// become successful cleanup.
// diagnosticFailureReader задаёт всегда ошибочный local reader для проверки невозможности успешного
// cleanup при stderr read ошибке.
type diagnosticFailureReader struct{}

// Read returns no bytes and os.ErrClosed immediately, exposing no synthetic or private diagnostic
// payload.
// Read немедленно возвращает ноль байтов и os.ErrClosed без synthetic и private diagnostic payload.
func (diagnosticFailureReader) Read([]byte) (int, error) { return 0, os.ErrClosed }

// gatedDiagnosticReader delays the first underlying read until a test-owned gate opens, separating child
// exit from parent reader lifetime.
// gatedDiagnosticReader задерживает первое underlying чтение до открытия test-owned gate, разделяя child
// exit и parent reader lifetime.
type gatedDiagnosticReader struct {
	gate   <-chan struct{}
	reader io.Reader
}

// Read waits for the gate and then forwards the original byte slice to the underlying reader without
// adding its own timeout or copying data.
// Read ждёт gate, затем передаёт исходный байтовый срез underlying reader без собственного таймаута и
// копирования данных.
func (r gatedDiagnosticReader) Read(p []byte) (int, error) { <-r.gate; return r.reader.Read(p) }

// TestAppServerDiagnosticJoinAndReadFailure verifies a failed diagnostic read is reported and a gated
// drain cannot finish before its test-owned reader is released.
// TestAppServerDiagnosticJoinAndReadFailure проверяет выдачу ошибки diagnostic чтения и невозможность
// завершить gated drain до освобождения test-owned reader.
func TestAppServerDiagnosticJoinAndReadFailure(t *testing.T) {
	if readAppServerDiagnostics(diagnosticFailureReader{}) == nil {
		t.Fatal("read error ignored")
	}
	gate := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- readAppServerDiagnostics(gatedDiagnosticReader{gate, strings.NewReader("synthetic")}) }()
	select {
	case <-done:
		t.Fatal("reader did not wait")
	default:
	}
	close(gate)
	if <-done != nil {
		t.Fatal("delayed reader failed")
	}
}

// TestAppServerExitBeforeDiagnosticRead lets the shell exit before releasing the stderr reader and
// verifies owned descriptors preserve buffered data and EOF through later cleanup.
// TestAppServerExitBeforeDiagnosticRead позволяет shell завершиться до освобождения stderr reader и
// проверяет сохранение buffered данных и EOF через owned descriptors до последующего cleanup.
func TestAppServerExitBeforeDiagnosticRead(t *testing.T) {
	gate := make(chan struct{})
	command := exec.Command("/bin/sh", "-c", "printf synthetic >&2")
	command.Env = []string{"PATH=/usr/bin:/bin"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	server, err := startAppServerWithDiagnosticReader(command, func(reader io.Reader) error {
		<-gate
		return readAppServerDiagnostics(reader)
	})
	if err != nil {
		t.Fatal("fixture start failed")
	}
	// Wait завершился раньше первого Read: owned pipe обязан сохранить данные и EOF.
	exit := <-server.wait
	replay := make(chan error, 1)
	replay <- exit
	server.wait = replay
	close(gate)
	if err := server.terminate(); err != nil {
		t.Fatal("exit closed diagnostic pipe before reader")
	}
}

// TestAppServerStartFailureClosesDescriptors repeatedly starts a deliberately nonexistent binary and
// compares local /proc/self/fd counts to detect leaked descriptors on initialization failure.
// TestAppServerStartFailureClosesDescriptors повторяет запуск намеренно отсутствующего binary и
// сравнивает local /proc/self/fd count для обнаружения утечки descriptors при ошибке инициализации.
func TestAppServerStartFailureClosesDescriptors(t *testing.T) {
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal("fd fixture unavailable")
	}
	for i := 0; i < 20; i++ {
		if _, err := startAppServerCommand(exec.Command("/nonexistent-kodex-test-command")); err == nil {
			t.Fatal("missing binary accepted")
		}
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal("fd readback unavailable")
	}
	if len(after) != len(before) {
		t.Fatal("start failure leaked descriptors")
	}
}

// TestAppServerDeviceSessionCloseAndReplay closes a synthetic shell-backed device session twice and
// requires stable success plus removal of its disposable private home.
// TestAppServerDeviceSessionCloseAndReplay дважды закрывает synthetic shell-backed device сессию и
// требует устойчивого успеха с удалением disposable private home.
func TestAppServerDeviceSessionCloseAndReplay(t *testing.T) {
	home := t.TempDir()
	server := pipeFixture(t, "cat >/dev/null; printf synthetic >&2")
	session := &deviceSession{server: server, home: home}
	if session.Close() != nil || session.Close() != nil {
		t.Fatal("device cleanup failed")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("device private home retained")
	}
}

// TestAppServerDeviceInitializationFailureCleanup uses the current test executable's unsupported
// device-code fixture path to require initialization failure and empty private root, without real device
// authorization.
// TestAppServerDeviceInitializationFailureCleanup использует неподдерживаемый device-code fixture путь
// текущего test executable для отказа инициализации и пустого private root без реальной device
// authorization.
func TestAppServerDeviceInitializationFailureCleanup(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("fixture binary unavailable")
	}
	root := t.TempDir()
	if os.Chmod(root, 0o700) != nil {
		t.Fatal("fixture mode unavailable")
	}
	process, err := NewAppServerProcess(binary, root, false)
	if err != nil {
		t.Fatal("fixture process unavailable")
	}
	// Catalog fixture не принимает device-code login; этот реальный child завершится с ошибкой.
	if _, err := process.StartDeviceAuthorization(t.Context(), "synthetic-attempt", "device"); err == nil {
		t.Fatal("device initialization failure hidden")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed device initialization retained private home")
	}
}
