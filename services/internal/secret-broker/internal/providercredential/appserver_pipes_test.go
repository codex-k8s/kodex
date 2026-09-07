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

type diagnosticFailureReader struct{}

func (diagnosticFailureReader) Read([]byte) (int, error) { return 0, os.ErrClosed }

type gatedDiagnosticReader struct {
	gate   <-chan struct{}
	reader io.Reader
}

func (r gatedDiagnosticReader) Read(p []byte) (int, error) { <-r.gate; return r.reader.Read(p) }
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

func TestAppServerDeviceInitializationFailureCleanup(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("fixture binary unavailable")
	}
	root := t.TempDir()
	if os.Chmod(root, 0o700) != nil {
		t.Fatal("fixture mode unavailable")
	}
	process, err := NewAppServerProcess(binary, root)
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
