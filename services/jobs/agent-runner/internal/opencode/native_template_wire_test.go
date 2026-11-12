package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestNativeMaterializedTemplatesOnWire consumes production-owner materializations through the real pinned OpenCode and SDK, including official export/import between turns. It verifies current system bytes, changed if/range dependencies and freshly resolved equal values on actual model HTTP requests without replaying business inputs.
// TestNativeMaterializedTemplatesOnWire передаёт production-материализацию владельца настоящим OpenCode и SDK, включая официальный export/import между ходами. Проверяет текущие system bytes, изменённые if/range и заново разрешённые равные значения в фактических model HTTP requests без повторной подачи business input.
func TestNativeMaterializedTemplatesOnWire(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	path := os.Getenv("KODEX_TEST_TEMPLATE_WIRE_FIXTURE")
	if path == "" {
		t.Skip("run make test-opencode-native with owner materialization fixtures")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 1<<20 {
		t.Fatal("owner wire fixtures unavailable")
	}
	var cases []struct {
		Name     string                      `json:"name"`
		Input    runtimecontract.RunnerInput `json:"input"`
		Expected string                      `json:"expected"`
		SHA256   string                      `json:"sha256"`
	}
	if json.Unmarshal(raw, &cases) != nil || len(cases) != 4 {
		t.Fatal("invalid owner wire fixtures")
	}
	var mu sync.Mutex
	var wires [][]byte
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if err != nil || len(body) > 1<<20 || r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Error("invalid synthetic template model request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		wires = append(wires, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		nativeAdapterFinalStream(w, "CHAT_COMPLETIONS")
	}))
	defer endpoint.Close()
	config := nativeConfiguration()
	provider := config["provider"].(map[string]any)["synthetic"].(map[string]any)
	provider["options"].(map[string]string)["baseURL"] = endpoint.URL + "/v1"
	provider["models"].(map[string]any)["tiny"] = map[string]any{"limit": map[string]int{"context": 16384, "output": 2048}}
	server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	session, err := server.client.CreateSession(ctx, "Synthetic owner template wire")
	if err != nil {
		t.Fatal(err)
	}
	for index, fixture := range cases {
		digest := sha256.Sum256([]byte(fixture.Input.Instructions))
		if hex.EncodeToString(digest[:]) != fixture.SHA256 {
			t.Fatal("owner materialization digest mismatch")
		}
		if _, err := runtimecontract.DecodePromptService(fixture.Input); err != nil {
			t.Fatal(err)
		}
		if index == 1 {
			server.stop(t)
			exportPath := filepath.Join(t.TempDir(), "native-export.json")
			checkpoint, err := server.server.Export(ctx, session.ID, exportPath)
			if err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			if err := os.Chmod(home, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, directory := range []string{"config", "cache", "data", "state"} {
				if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := restore(ctx, binary, home, exportPath, session.ID, checkpoint.SHA256); err != nil {
				t.Fatal(err)
			}
			server = startConfiguredNativeServer(t, binary, home, config)
		}
		result, err := server.client.Execute(ctx, session.ID, Prompt{
			MessageID: fmt.Sprintf("msg_T%024d", index), Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
			System: fixture.Input.Instructions, Parts: []TextPart{{Type: "text", Text: fmt.Sprintf("SYNTHETIC_TEMPLATE_BUSINESS_%d", index)}},
		}, func(string) bool { return false }, func(string, string) error { return nil })
		if err != nil || result.FinalText != "SYNTHETIC_ADAPTER_FINAL" {
			t.Fatalf("materialized native turn failed: case=%s err=%v", fixture.Name, err)
		}
		mu.Lock()
		captured := append([][]byte(nil), wires...)
		mu.Unlock()
		if len(captured) != index+1 {
			t.Fatal("unexpected model request count")
		}
		var body struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if json.Unmarshal(captured[index], &body) != nil {
			t.Fatal("model request is not inspectable")
		}
		var system string
		for _, message := range body.Messages {
			if message.Role == "system" {
				system += message.Content
			}
		}
		if strings.Count(system, fixture.Input.Instructions) != 1 || !strings.Contains(system, fixture.Expected) {
			t.Fatal("current owner materialization missing or changed on model wire")
		}
		for previous := 0; previous < index; previous++ {
			if cases[previous].Expected != fixture.Expected && strings.Contains(system, cases[previous].Expected) {
				t.Fatal("stale owner instructions retained in current model system input")
			}
		}
		for business := 0; business <= index; business++ {
			if strings.Count(string(captured[index]), fmt.Sprintf("SYNTHETIC_TEMPLATE_BUSINESS_%d", business)) != 1 {
				t.Fatal("native business input lost or duplicated")
			}
		}
		t.Logf("owner template wire: case=%s bytes=%d digest=%s", fixture.Name, len(captured[index]), fixture.SHA256)
	}
}
