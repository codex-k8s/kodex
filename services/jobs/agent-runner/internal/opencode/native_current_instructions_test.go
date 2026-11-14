package opencode

import (
	"bytes"
	"context"
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
)

// TestNativeCurrentInstructionsSurviveCompaction checks every business request across native
// compaction and official export/import; the native summary keeps its own upstream prompt.
// TestNativeCurrentInstructionsSurviveCompaction проверяет каждый business request через native
// compaction и official export/import; native summary сохраняет свой upstream prompt.
func TestNativeCurrentInstructionsSurviveCompaction(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{}
	config := nativeToolConfiguration(t, fixture)
	markers := []string{"SYNTHETIC_CURRENT_OWNER_A", "SYNTHETIC_CURRENT_OWNER_B"}
	const skill = "SYNTHETIC_CURRENT_SKILL"
	const userData = "SYNTHETIC_USER_DATA_NOT_SYSTEM"
	const projectData = "SYNTHETIC_PROJECT_AGENTS_NOT_SYSTEM"
	const legacyOwner = "SYNTHETIC_PREVIOUS_MESSAGE_SYSTEM"
	skillPath := filepath.Join(t.TempDir(), "SKILL.md")
	if os.WriteFile(skillPath, []byte(skill), 0o400) != nil {
		t.Fatal("prepare synthetic skill")
	}
	var mu sync.Mutex
	current, previous := markers[0], markers[1]
	var normal, postCompaction, missing, summaries int
	observer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage
		}
		if json.Unmarshal(raw, &body) == nil {
			mu.Lock()
			if strings.Contains(string(raw), "SYNTHETIC_NATIVE_COMPACTION") {
				summaries++
			} else if len(body.Tools) > 0 {
				normal++
				if summaries > 0 {
					postCompaction++
				}
				var system string
				for _, message := range body.Messages {
					if message.Role == "system" {
						system += message.Content
					}
				}
				if strings.Count(system, current) != 1 || strings.Count(system, skill) != 1 {
					missing++
				}
				for _, forbidden := range []string{previous, legacyOwner, userData, projectData, "synthetic-not-a-credential"} {
					if strings.Contains(system, forbidden) {
						missing++
					}
				}
			}
			mu.Unlock()
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		fixture.model(writer, request)
	}))
	defer observer.Close()
	config["provider"].(map[string]any)["synthetic"].(map[string]any)["options"].(map[string]string)["baseURL"] = observer.URL + "/v1"
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	var sessionID, exportPath string
	var checkpoint Checkpoint
	for index := range markers {
		mu.Lock()
		current, previous = markers[index], markers[1-index]
		normal, postCompaction, missing, summaries = 0, 0, 0, 0
		mu.Unlock()
		fixture.mu.Lock()
		fixture.targetTools = 12 * (index + 1)
		fixture.sinceSummary = 0
		fixture.mu.Unlock()
		home := t.TempDir()
		if os.Chmod(home, 0o700) != nil {
			t.Fatal("prepare private native HOME mode")
		}
		for _, directory := range []string{"config", "cache", "data", "state"} {
			if os.Mkdir(filepath.Join(home, directory), 0o700) != nil {
				t.Fatal("prepare private native HOME")
			}
		}
		if index > 0 && restore(ctx, binary, home, exportPath, sessionID, checkpoint.SHA256) != nil {
			t.Fatal("restore exact native history")
		}
		instructionPath, err := InstallCurrentInstructions(home, markers[index])
		if err != nil || os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte(projectData), 0o400) != nil {
			t.Fatal("prepare trusted current instructions")
		}
		config["instructions"] = []string{instructionPath, skillPath}
		server := startConfiguredNativeServer(t, binary, home, config)
		if index == 0 {
			session, err := server.client.CreateSession(ctx, "Synthetic current instructions")
			if err != nil {
				t.Fatal(err)
			}
			sessionID = session.ID
			if _, err := server.client.SendPrompt(ctx, sessionID, Prompt{
				MessageID: "msg_91234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
				System: legacyOwner, NoReply: true,
				Parts: []TextPart{{Type: "text", Text: "Synthetic previously stored business input; no managed effect."}},
			}); err != nil {
				t.Fatal("prepare prior per-message system metadata through native API")
			}
		}
		result, err := server.client.Execute(ctx, sessionID, Prompt{
			MessageID: fmt.Sprintf("msg_%025d", index+81), Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
			Parts: []TextPart{{Type: "text", Text: fmt.Sprintf("%s_%d. Complete twelve more synthetic counter steps.", userData, index)}},
		}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
		server.stop(t)
		if err != nil || result.FinalText != fmt.Sprintf("SYNTHETIC_FINAL_COUNTER_%d", 12*(index+1)) {
			t.Fatal("native synthetic loop did not complete")
		}
		exportPath = filepath.Join(t.TempDir(), "native.json")
		checkpoint, err = server.server.Export(ctx, sessionID, exportPath)
		if err != nil {
			t.Fatal("export native history")
		}
		exported, err := os.ReadFile(exportPath)
		if err != nil || strings.Contains(string(exported), markers[0]) || strings.Contains(string(exported), markers[1]) {
			t.Fatal("ephemeral owner instructions entered native export")
		}
		if strings.Count(string(exported), legacyOwner) != 1 {
			t.Fatal("previously stored message metadata was lost or replayed")
		}
		for business := 0; business <= index; business++ {
			if strings.Count(string(exported), fmt.Sprintf("%s_%d", userData, business)) != 1 {
				t.Fatal("business input duplicated or lost during native restore")
			}
		}
		mu.Lock()
		if normal == 0 || summaries == 0 || postCompaction == 0 || missing != 0 {
			t.Fatalf("current instructions wire proof failed: turn=%d normal=%d summaries=%d post_compaction=%d missing_or_duplicate=%d", index, normal, summaries, postCompaction, missing)
		}
		t.Logf("turn=%d normal=%d compactions=%d post_compaction=%d; current owner and skill exactly once", index, normal, summaries, postCompaction)
		mu.Unlock()
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 24 || len(fixture.violations) != 0 {
		t.Fatal("managed effect count or identity changed")
	}
}
