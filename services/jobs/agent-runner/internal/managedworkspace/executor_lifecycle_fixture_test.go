package managedworkspace

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain exposes only explicit disposable-container lifecycle modes; ordinary unit tests
// retain the normal runner. The server executes the unchanged production PID1/UID guards.
// TestMain открывает только явные lifecycle-режимы disposable container; обычные unit tests
// сохраняют прежний запуск. Server исполняет неизменённые production PID1/UID guards.
func TestMain(suite *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == CommandChildMode {
		if RunCommandChild(os.Args[2:]) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	mode := os.Getenv("KODEX_TEST_EXECUTOR_LIFECYCLE")
	if mode == "" {
		os.Exit(suite.Run())
	}
	if mode == "server" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		go func() {
			<-ctx.Done()
			_ = os.WriteFile("/run/kodex/workspace/draining", []byte("EXECUTOR_DRAINING\n"), 0o640)
		}()
		err := ServeExecutor(ctx)
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"state": "EXECUTOR_STOP_FAILED", "pid": os.Getpid(), "uid": os.Geteuid(), "gid": os.Getegid()})
			os.Exit(1)
		}
		if os.WriteFile("/run/kodex/workspace/stopped", []byte("EXECUTOR_STOPPED\n"), 0o640) != nil {
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"state": "EXECUTOR_STOPPED", "pid": os.Getpid(), "uid": os.Geteuid(), "gid": os.Getegid()})
		// Keep the namespace alive only for the independent late-client probe.
		// Namespace остаётся живым только для независимой проверки позднего client.
		time.Sleep(15 * time.Second)
		os.Exit(0)
	}
	if mode == "client" {
		foreign := os.Getenv("KODEX_TEST_FOREIGN_EXECUTOR_PEER") == "true"
		if (!foreign && (os.Geteuid() != 10001 || os.Getegid() != 29000)) || (foreign && (os.Geteuid() != 10003 || os.Getegid() != 29000)) {
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client := NewExecutorClient()
		defer client.Close()
		request := ExecutorRequest{Action: "QUIESCE", Binding: strings.Repeat("a", 64)}
		if os.Getenv("KODEX_TEST_FOREIGN_EXECUTOR_BINDING") == "true" {
			request.Binding = strings.Repeat("b", 64)
		}
		switch os.Getenv("KODEX_TEST_EXECUTOR_ACTION") {
		case "", "QUIESCE":
		case "INITIALIZE":
			request.Action, request.Commands = "INITIALIZE", []string{"python3"}
		case "RUN":
			request.Action, request.Command, request.Directory, request.TimeoutSeconds = "RUN", "python3", "work", 45
			request.Arguments = []string{"-c", `import time;open("started","w").write("started");time.sleep(40);open("survived","w").write("forbidden")`}
		default:
			os.Exit(1)
		}
		result, err := client.Call(ctx, request)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"state": result.Status, "call_failed": err != nil, "uid": os.Geteuid(), "gid": os.Getegid()})
		wantFailure := os.Getenv("KODEX_TEST_EXPECT_QUIESCE_FAILURE") == "true"
		wantStatus := os.Getenv("KODEX_TEST_EXECUTOR_STATUS")
		if wantStatus == "" {
			wantStatus = "QUIESCENT"
		}
		if wantFailure != (err != nil) || !wantFailure && result.Status != wantStatus {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(1)
}
