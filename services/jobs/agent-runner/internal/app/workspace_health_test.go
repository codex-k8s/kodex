package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

func TestWorkspaceCanaryProcessFixture(t *testing.T) {
	mode := os.Getenv("KODEX_CANARY_TEST_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "filesystem":
		err := workspacepolicy.RunCanary(t.Context(), os.Getenv("KODEX_CANARY_TEST_ROOT"), runtimecontract.RuntimeWorkspacePolicyV1())
		result := "OK"
		if err != nil {
			result = workspacepolicy.DenialReason(err)
		}
		_, _ = io.WriteString(os.Stdout, result)
	case "hang":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Minute)
	case "overflow":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("private-output", 100))
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func TestWorkspaceCanaryProcessIsBoundedAndReaped(t *testing.T) {
	for _, mode := range []string{"filesystem", "hang", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceCanaryProcessFixture$")
			command.Env = append(os.Environ(), "KODEX_CANARY_TEST_MODE="+mode, "KODEX_CANARY_TEST_ROOT="+root, "GORACE=atexit_sleep_ms=0")
			started := time.Now()
			err := runCanaryCommand(ctx, command)
			if (mode == "filesystem") != (err == nil) {
				t.Fatalf("process outcome: %v", err)
			}
			if err != nil && err.Error() != "workspace readiness denied: RUNTIME_IO_ERROR" {
				t.Fatalf("unsafe diagnostic: %v", err)
			}
			if time.Since(started) > 3*time.Second || command.ProcessState == nil {
				t.Fatal("canary exceeded its budget or was not reaped")
			}
			entries, readErr := os.ReadDir(filepath.Join(root, ".kodex/outbox"))
			if readErr != nil || len(entries) != 0 {
				t.Fatal("canary left temporary files")
			}
		})
	}
}

func TestWorkspaceReadinessUsesOnlyFreshSnapshot(t *testing.T) {
	state := &health{}
	state.ready.Store(true)
	handler := healthHandler(state)
	for _, test := range []struct {
		name   string
		result *workspaceHealth
		want   int
	}{
		{"missing", nil, http.StatusServiceUnavailable},
		{"fresh without filesystem", &workspaceHealth{checked: time.Now()}, http.StatusNoContent},
		{"stale", &workspaceHealth{checked: time.Now().Add(-2 * workspaceCanaryFreshness)}, http.StatusServiceUnavailable},
		{"future", &workspaceHealth{checked: time.Now().Add(time.Hour)}, http.StatusServiceUnavailable},
		{"denied", &workspaceHealth{checked: time.Now(), err: &workspacepolicy.Denial{Reason: runtimecontract.RuntimeWorkspaceReadOnly}}, http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			state.workspace.Store(test.result)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d", response.Code, test.want)
			}
		})
	}
}

func TestWorkspaceMonitorCancellationJoinsCheckAndClearsReadiness(t *testing.T) {
	state := &health{}
	entered, joined := make(chan struct{}), make(chan struct{})
	stop := startWorkspaceMonitor(t.Context(), state, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(joined)
		return ctx.Err()
	})
	<-entered
	stop()
	select {
	case <-joined:
	default:
		t.Fatal("monitor did not join its check")
	}
	if state.workspace.Load() != nil || state.workspaceStatus(time.Now()) == nil {
		t.Fatal("stopped monitor retained ready state")
	}
}
