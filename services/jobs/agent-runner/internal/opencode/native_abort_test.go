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

// interruptedNativeToolFixture accepts exactly one synthetic effect while withholding its
// response, so both observer and process cancellation exercise the same real MCP boundary.
// interruptedNativeToolFixture принимает один synthetic effect и удерживает ответ, чтобы
// отмена observer и process проверяла одну и ту же настоящую MCP-границу.
func interruptedNativeToolFixture(t *testing.T) (*nativeFixture, map[string]any, <-chan struct{}) {
	t.Helper()
	fixture := &nativeFixture{
		targetTools:      1,
		identityKey:      strings.Repeat("d", 64),
		identityRevision: strings.Repeat("b", 64),
		nativeIdentities: map[string]bool{},
	}
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
		fixture.mcp(httptest.NewRecorder(), request)
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
	return fixture, config, accepted
}

// TestNativeAbortClosesInterruptedToolBeforeExport qualifies the real native cancel lifecycle.
// A synthetic effect is accepted once while its HTTP response is withheld; no history status is patched by Kodex.
// TestNativeAbortClosesInterruptedToolBeforeExport проверяет настоящий native cancel lifecycle.
// Synthetic effect принят один раз при удержанном HTTP response; Kodex не исправляет history status вручную.
func TestNativeAbortClosesInterruptedToolBeforeExport(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture, config, accepted := interruptedNativeToolFixture(t)
	home := t.TempDir()
	if os.Chmod(home, 0o700) != nil {
		t.Fatal("prepare isolated cancellation HOME")
	}
	plugin, err := InstallManagedToolPlugin(home)
	if err != nil {
		t.Fatal("prepare actual image-owned hook", err)
	}
	config["plugin"] = []string{plugin}
	environment := []string{"KODEX_MCP_PROXY_TOKEN=" + fixture.identityKey, "KODEX_NATIVE_REVISION=" + fixture.identityRevision}
	server := startConfiguredNativeServerEnvironment(t, binary, home, config, environment)
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
	// Cleanup follows official capture in production and must not erase the stopped-writer proof.
	// Cleanup идёт после official capture в production и не должен терять stopped-writer proof.
	if err := RemoveManagedHome(home); err != nil {
		t.Fatal("joined cancellation HOME cleanup failed after successful export", err)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Fatal("joined cancellation HOME retained after successful cleanup")
	}
	// The owner has reconciled the accepted synthetic effect and revoked tool authority.
	// Владелец подтвердил принятый synthetic effect и отозвал полномочия инструмента.
	replacement := emptyNativeRestoreHome(t)
	if err := restore(t.Context(), binary, replacement, export, session.ID, checkpoint.SHA256); err != nil {
		t.Fatal("official import rejected the joined cancelled checkpoint", err)
	}
	plugin, err = InstallManagedToolPlugin(replacement)
	if err != nil {
		t.Fatal("prepare replacement native hook", err)
	}
	config["plugin"] = []string{plugin}
	config["permission"] = map[string]string{"*": "deny"}
	resumed := startConfiguredNativeServerEnvironment(t, binary, replacement, config, environment)
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
	if err := RemoveManagedHome(replacement); err != nil {
		t.Fatal("read-only replacement HOME cleanup failed", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 1 || fixture.modelCalls != 2 || !strings.Contains(string(fixture.lastWire), "SYNTHETIC_RECONCILED_READ_ONLY") {
		t.Fatal("native replacement replayed the accepted effect or lost current read-only instructions")
	}
	t.Logf("accepted effect cancellation: tools=%d modelCalls=%d restoredNativeSession=true", fixture.tools, fixture.modelCalls)
}

// TestNativeLifecycleCancellationRetainsJoinedExport cancels the same context that owns
// the real native process, matching production rather than only stopping the HTTP observer.
// TestNativeLifecycleCancellationRetainsJoinedExport отменяет context настоящего native
// process как в production, а не только HTTP observer; model/MCP остаются synthetic.
func TestNativeLifecycleCancellationRetainsJoinedExport(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture, config, accepted := interruptedNativeToolFixture(t)
	home := t.TempDir()
	if os.Chmod(home, 0o700) != nil {
		t.Fatal("prepare private native lifecycle HOME")
	}
	for _, directory := range []string{"config", "cache", "data", "state", "work"} {
		if os.MkdirAll(filepath.Join(home, directory), 0o700) != nil {
			t.Fatal("prepare private native lifecycle directory")
		}
	}
	plugin, err := InstallManagedToolPlugin(home)
	if err != nil {
		t.Fatal("prepare actual native lifecycle hook", err)
	}
	config["plugin"] = []string{plugin}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	environment := append(offlineEnvironment(home), "OPENCODE_CONFIG_CONTENT="+string(raw), "KODEX_MCP_PROXY_TOKEN="+fixture.identityKey, "KODEX_NATIVE_REVISION="+fixture.identityRevision)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owned, err := start(ctx, binary, home, environment)
	if err != nil {
		t.Fatal("start actual context-bound native lifecycle", err)
	}
	server := &nativeServer{server: owned, command: owned.command, client: owned.Client}
	t.Cleanup(func() { server.stop(t) })
	session, err := server.client.CreateSession(ctx, "Synthetic process lifecycle cancellation")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: "msg_71234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Perform one synthetic counter step."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
		done <- err
	}()
	select {
	case <-accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("native lifecycle did not accept its one synthetic effect")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled native lifecycle reported successful execution", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled native observer remained active")
	}
	abort, stopAbort := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	_ = server.client.Abort(abort, session.ID)
	stopAbort()
	server.stop(t)
	capture, stopCapture := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stopCapture()
	path := filepath.Join(t.TempDir(), "lifecycle-cancel-export.json")
	checkpoint, err := server.server.Export(capture, session.ID, path)
	if err != nil {
		t.Fatal("actual lifecycle cancellation lost official joined export", err)
	}
	if checkpoint.Messages < 1 {
		t.Fatal("joined native lifecycle export has no history")
	}
	if err := RemoveManagedHome(home); err != nil {
		t.Fatal("actual lifecycle cancellation failed HOME cleanup after export", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 1 || fixture.modelCalls != 1 {
		t.Fatal("native lifecycle cancellation repeated the accepted effect or model request")
	}
	t.Log("actual process context cancelled; native process joined; official export and private HOME cleanup passed")
}
