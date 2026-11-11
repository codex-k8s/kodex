package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// nativeBaselineBinarySHA256 is the extracted executable from the qualified1.19.5 Linux baseline asset.
// nativeBaselineBinarySHA256 — executable из проверенного Linux baseline asset версии1.19.5.
const nativeBaselineBinarySHA256 = "7df4e4136301f16589b0f30bf91a61380fca5f8ef22dd36570f5ebe1d4ec802c"

// nativeServer owns a real upstream process and its local client; no model is replaced by an engine stub.
// nativeServer владеет настоящим upstream-процессом и local client; engine не заменяется заглушкой.
type nativeServer struct {
	server  *Server
	command *exec.Cmd
	client  *Client
	stopped bool
}

// qualifiedNativeBinary requires an explicit exact executable; absent external qualification is SKIP.
// qualifiedNativeBinary требует явный точный executable; отсутствие внешней квалификации означает SKIP.
func qualifiedNativeBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native baseline executable requires linux/amd64")
	}
	path := os.Getenv("KODEX_TEST_OPENCODE_BINARY")
	if path == "" {
		t.Skip("set KODEX_TEST_OPENCODE_BINARY to the exact qualified executable")
	}
	metadata, err := os.Lstat(path)
	if err != nil || !metadata.Mode().IsRegular() || metadata.Mode()&0o111 == 0 {
		t.Fatal("qualified native executable is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	file.Close()
	if err != nil || hex.EncodeToString(hash.Sum(nil)) != nativeBaselineBinarySHA256 {
		t.Fatal("native executable does not match the pinned release asset")
	}
	return path
}

// nativeConfiguration supplies only a synthetic route and native compaction, never user credentials.
// nativeConfiguration задаёт только synthetic route и native compaction, без credentials пользователя.
func nativeConfiguration() map[string]any {
	return map[string]any{
		"model": "synthetic/tiny", "small_model": "synthetic/tiny", "enabled_providers": []string{"synthetic"},
		"autoupdate": false, "share": "disabled", "snapshot": false, "permission": map[string]string{"*": "deny"},
		"compaction": map[string]bool{"auto": true, "prune": false},
		"provider": map[string]any{"synthetic": map[string]any{"npm": "@ai-sdk/openai-compatible",
			"options": map[string]string{"baseURL": "http://127.0.0.1:1/v1", "apiKey": "synthetic-not-a-credential"},
			"models":  map[string]any{"tiny": map[string]any{"limit": map[string]int{"context": 4096, "output": 512}}}}},
	}
}

// startNativeServer uses separate HOME/XDG and disables unselected config/plugins/models fetching.
// startNativeServer использует отдельные HOME/XDG и отключает посторонние config/plugins/models fetch.
func startNativeServer(t *testing.T, binary, home string) *nativeServer {
	t.Helper()
	return startConfiguredNativeServer(t, binary, home, nativeConfiguration())
}

// startConfiguredNativeServer starts the same exact executable with one explicit synthetic profile.
// startConfiguredNativeServer запускает тот же точный executable с явным synthetic profile.
func startConfiguredNativeServer(t *testing.T, binary, home string, config map[string]any) *nativeServer {
	return startConfiguredNativeServerEnvironment(t, binary, home, config, nil)
}

// startConfiguredNativeServerEnvironment adds only explicitly synthetic hook configuration to an isolated process.
// startConfiguredNativeServerEnvironment добавляет лишь явно synthetic hook configuration изолированному процессу.
func startConfiguredNativeServerEnvironment(t *testing.T, binary, home string, config map[string]any, extra []string) *nativeServer {
	t.Helper()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"config", "cache", "data", "state", "work"} {
		if err := os.MkdirAll(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configuration, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	environment := append(offlineEnvironment(home), "OPENCODE_CONFIG_CONTENT="+string(configuration))
	environment = append(environment, extra...)
	owned, err := start(t.Context(), binary, home, environment)
	if err != nil {
		t.Fatal(err)
	}
	server := &nativeServer{server: owned, command: owned.command, client: owned.Client}
	t.Cleanup(func() { server.stop(t) })
	return server
}

// stop joins the actual process group before the test reopens native state in another process.
// stop ожидает завершения настоящей process group до открытия native state новым процессом.
func (server *nativeServer) stop(t *testing.T) {
	t.Helper()
	if server.stopped {
		return
	}
	server.stopped = true
	if err := server.server.Stop(); err != nil {
		t.Error(err)
	}
}

// TestNativeSessionSurvivesProcessReplacement verifies upstream persistence with no synthetic engine.
// TestNativeSessionSurvivesProcessReplacement проверяет upstream persistence без подмены engine.
func TestNativeSessionSurvivesProcessReplacement(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	home := t.TempDir()
	first := startNativeServer(t, binary, home)
	session, err := first.client.CreateSession(t.Context(), "Native session qualification")
	if err != nil {
		t.Fatal(err)
	}
	messageID := "msg_01234567890123456789012345"
	_, err = first.client.SendPrompt(t.Context(), session.ID, Prompt{
		MessageID: messageID, Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, NoReply: true,
		Parts: []TextPart{{Type: "text", Text: "Native durable marker synthetic-qualification-A"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first.stop(t)
	second := startNativeServer(t, binary, home)
	restored, err := second.client.Session(t.Context(), session.ID)
	if err != nil || restored.ID != session.ID {
		t.Fatalf("native session was not recovered: %v", err)
	}
	messages, err := second.client.Messages(t.Context(), session.ID, 10, "")
	if err != nil || len(messages) != 1 {
		t.Fatalf("native transcript was lost or duplicated: %d %v", len(messages), err)
	}
	var identity struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(messages[0].Info, &identity) != nil || identity.ID != messageID {
		t.Fatal("native message identity changed after process replacement")
	}
	var text TextPart
	if len(messages[0].Parts) != 1 || json.Unmarshal(messages[0].Parts[0], &text) != nil || text.Text != "Native durable marker synthetic-qualification-A" {
		t.Fatal("native message bytes changed after process replacement")
	}
}
