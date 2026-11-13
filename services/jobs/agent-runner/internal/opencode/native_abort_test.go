package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNativeAbortClosesInterruptedToolBeforeExport qualifies the real native cancel lifecycle.
// A synthetic effect is accepted once while its HTTP response is withheld; no history status is patched by Kodex.
// TestNativeAbortClosesInterruptedToolBeforeExport проверяет настоящий native cancel lifecycle.
// Synthetic effect принят один раз при удержанном HTTP response; Kodex не исправляет history status вручную.
func TestNativeAbortClosesInterruptedToolBeforeExport(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{targetTools: 1}
	config := nativeToolConfiguration(t, fixture)
	accepted := make(chan struct{}, 1)
	release := make(chan struct{})
	managed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		var rpc struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(raw, &rpc)
		if rpc.Method != "tools/call" {
			fixture.mcp(writer, request)
			return
		}
		recorder := httptest.NewRecorder()
		fixture.mcp(recorder, request)
		select {
		case accepted <- struct{}{}:
		default:
		}
		select {
		case <-request.Context().Done():
		case <-release:
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(managed.Close)
	t.Cleanup(func() { close(release) })
	config["mcp"].(map[string]any)["kodex"].(map[string]any)["url"] = managed.URL
	server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	session, err := server.client.CreateSession(t.Context(), "Synthetic interrupted native tool")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_81234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Perform one synthetic counter step."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
		done <- err
	}()
	select {
	case <-accepted:
	case err := <-done:
		t.Fatal("native turn ended before managed effect", err)
	case <-time.After(10 * time.Second):
		t.Fatal("native managed effect did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("native interruption reported success", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native observer did not join after stop")
	}
	abortContext, cancelAbort := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelAbort()
	if server.client.Abort(abortContext, session.ID) != nil {
		t.Fatal("native abort was not acknowledged")
	}
	server.stop(t)
	export := filepath.Join(t.TempDir(), "interrupted-native.json")
	checkpoint, err := server.server.Export(abortContext, session.ID, export)
	if err != nil {
		t.Fatal("native abort retained an unconfirmed tool part", err)
	}
	fixture.mu.Lock()
	stoppedTools, stoppedCalls := fixture.tools, fixture.modelCalls
	fixture.mu.Unlock()
	if stoppedTools != 1 || stoppedCalls != 1 {
		t.Fatal("native stop repeated model call or accepted effect")
	}
	// The owner has reconciled the accepted synthetic effect and revoked tool authority.
	// Владелец подтвердил принятый synthetic effect и отозвал полномочия инструмента.
	replacement := emptyNativeRestoreHome(t)
	if err := restore(t.Context(), binary, replacement, export, session.ID, checkpoint.SHA256); err != nil {
		t.Fatal("official import rejected the joined cancelled checkpoint", err)
	}
	config["permission"] = map[string]string{"*": "deny"}
	resumed := startConfiguredNativeServer(t, binary, replacement, config)
	resumeContext, cancelResume := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancelResume()
	result, err := resumed.client.Execute(resumeContext, session.ID, Prompt{
		MessageID: "msg_91234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
		System: "SYNTHETIC_RECONCILED_READ_ONLY", Parts: []TextPart{{Type: "text", Text: "SYNTHETIC_RESUME_WITHOUT_REPEATING_EFFECT"}},
	}, func(string) bool { return false }, func(string, string) error { return nil })
	resumed.stop(t)
	if err != nil || result.FinalText != "SYNTHETIC_FINAL_COUNTER_1" {
		t.Fatal("reconciled native cancellation could not resume read-only", err)
	}
	reexport := filepath.Join(t.TempDir(), "resumed-native.json")
	if _, err := resumed.server.Export(t.Context(), session.ID, reexport); err != nil {
		t.Fatal("read-only continuation did not seal its native history", err)
	}
	raw, err := os.ReadFile(reexport)
	if err != nil || strings.Count(string(raw), "SYNTHETIC_RESUME_WITHOUT_REPEATING_EFFECT") != 1 {
		t.Fatal("native continuation lost or duplicated the new business input")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 1 || fixture.modelCalls != 2 || !strings.Contains(string(fixture.lastWire), "SYNTHETIC_RECONCILED_READ_ONLY") {
		t.Fatal("native replacement replayed the accepted effect or lost current read-only instructions")
	}
	t.Logf("accepted effect cancellation: tools=%d modelCalls=%d restoredNativeSession=true", fixture.tools, fixture.modelCalls)
}
