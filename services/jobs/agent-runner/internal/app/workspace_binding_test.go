package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestWorkspaceBindingFailureIsClosedBeforeCallback exercises real invalid-input entrypoints.
// Neither private input nor error text reaches logs; no callback or writer admission is attempted.
// TestWorkspaceBindingFailureIsClosedBeforeCallback проверяет реальные входы с invalid input.
// Private input и error text не попадают в log; callback и допуск writer не выполняются.
func TestWorkspaceBindingFailureIsClosedBeforeCallback(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	input := runtimecontract.RunnerInput{CallbackURL: server.URL, LeaseFence: "PRIVATE_FIXTURE_FENCE", Task: "PRIVATE_FIXTURE_TASK"}
	err := prepareBoundWorkspace(t.Context(), input)
	stage, reason := runtimecontract.InitializationErrorDetails(err)
	if err == nil || stage != "WORKSPACE_LAYOUT" || reason != "UNKNOWN" {
		t.Fatal("initialization failure changed")
	}
	if _, err := bindWorkspaceWriter(t.Context(), input); err == nil {
		t.Fatal("invalid writer admitted")
	}
	logWorkspaceBindingRejection(context.Background(), workspaceBindingStage(255))
	if calls.Load() != 0 || strings.Contains(output.String(), "PRIVATE_FIXTURE") || strings.Contains(output.String(), server.URL) {
		t.Fatal("rejection exposed private input or contacted callback")
	}
	decoder := json.NewDecoder(&output)
	for _, expected := range []string{"VOLUME_VERIFY", "INPUT", "UNKNOWN"} {
		var record map[string]any
		if decoder.Decode(&record) != nil || len(record) != 4 || record["level"] != "WARN" ||
			record["msg"] != "runtime workspace binding rejected" || record["binding_stage"] != expected {
			t.Fatal("closed rejection diagnostic missing")
		}
	}
}
