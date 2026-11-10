package managedworkspace

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestExecutorClientWaitsForListenerBeforeOneRequest starts the independent executor late;
// connection setup may wait, but exactly one initialization must reach the listener.
// TestExecutorClientWaitsForListenerBeforeOneRequest запускает executor позднее клиента;
// установка соединения может ждать, но listener получает INITIALIZE ровно один раз.
func TestExecutorClientWaitsForListenerBeforeOneRequest(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "executor.sock")
	client := newExecutorClient(socket)
	defer client.Close()
	var calls atomic.Int32
	result := make(chan error, 1)
	go func() {
		value, err := client.Call(t.Context(), ExecutorRequest{Action: "INITIALIZE", Binding: strings.Repeat("a", 64), Commands: []string{"bash"}})
		if err == nil && value.Status != "READY" {
			err = errExecutor
		}
		result <- err
	}()
	time.Sleep(75 * time.Millisecond)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		defer r.Body.Close()
		var request ExecutorRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Action != "INITIALIZE" {
			t.Error("unexpected request")
		}
		_ = json.NewEncoder(w).Encode(CommandResult{Status: "READY"})
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("client failed before delayed executor startup", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup exceeded test budget")
	}
	if calls.Load() != 1 {
		t.Fatal("initialization was repeated", calls.Load())
	}
}

// TestExecutorClientDoesNotReplayUnknownCommand loses the RUN reply on an established
// connection and verifies that transport recovery does not duplicate the accepted command.
// TestExecutorClientDoesNotReplayUnknownCommand теряет ответ RUN на рабочем соединении
// и проверяет отсутствие повторной доставки уже принятой команды.
func TestExecutorClientDoesNotReplayUnknownCommand(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "executor.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request ExecutorRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid request")
			return
		}
		if request.Action == "INITIALIZE" {
			_ = json.NewEncoder(w).Encode(CommandResult{Status: "READY"})
			return
		}
		runs.Add(1)
		connection, _, hijackErr := w.(http.Hijacker).Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		_ = connection.Close()
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	client := newExecutorClient(socket)
	defer client.Close()
	if _, err := client.Call(t.Context(), ExecutorRequest{Action: "INITIALIZE"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(t.Context(), ExecutorRequest{Action: "RUN", Command: "bash", Arguments: []string{"-c", "printf synthetic"}}); err == nil {
		t.Fatal("lost response became success")
	}
	if runs.Load() != 1 {
		t.Fatal("unknown command was repeated", runs.Load())
	}
}

// TestExecutorClientCancelsMissingListener bounds connection waiting by the caller deadline
// and cannot report success when there is no executor; no helper process is started.
// TestExecutorClientCancelsMissingListener ограничивает ожидание deadline caller и не
// возвращает успех без executor; helper process не запускается.
func TestExecutorClientCancelsMissingListener(t *testing.T) {
	client := newExecutorClient(filepath.Join(t.TempDir(), "absent.sock"))
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client.Call(ctx, ExecutorRequest{Action: "INITIALIZE"})
	if err == nil || time.Since(started) > time.Second {
		t.Fatal("missing executor accepted or cancellation ignored")
	}
}
