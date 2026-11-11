package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNativePruningRetainsOriginalToolOutputs exercises upstream automatic pruning across three real turns without a custom history reducer.
// TestNativePruningRetainsOriginalToolOutputs проверяет upstream automatic pruning в трёх настоящих turns без собственного history reducer.
func TestNativePruningRetainsOriginalToolOutputs(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{targetTools: 144}
	config := nativeToolConfiguration(t, fixture)
	config["compaction"] = map[string]any{"auto": true, "prune": true}
	provider := config["provider"].(map[string]any)["synthetic"].(map[string]any)
	provider["models"].(map[string]any)["tiny"] = map[string]any{"limit": map[string]int{"context": 512000, "output": 2048}}
	server := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	session, err := server.client.CreateSession(ctx, "Synthetic native pruning")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"msg_81234567890123456789012345", "msg_91234567890123456789012345", "msg_A1234567890123456789012345"} {
		result, err := server.client.Execute(ctx, session.ID, Prompt{MessageID: id, Model: Model{ProviderID: "synthetic", ModelID: "tiny"}, Parts: []TextPart{{Type: "text", Text: "Complete the synthetic counter."}}}, func(name string) bool { return name == "kodex_counter" }, func(string, string) error { return nil })
		if err != nil || result.FinalText != "SYNTHETIC_FINAL_COUNTER_144" || result.Compactions != 0 {
			t.Fatal("native large-context turn failed", err)
		}
	}
	// Pruning is scheduled by upstream after its final reply; observe bounded convergence through native reads.
	// Upstream планирует pruning после final reply; bounded convergence наблюдается через native reads.
	deadline := time.Now().Add(5 * time.Second)
	pruned := false
	for time.Now().Before(deadline) && !pruned {
		cursor := ""
		for {
			page, err := server.client.MessagePage(ctx, session.ID, 100, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range page.Messages {
				for _, part := range message.Parts {
					var decoded struct {
						Type  string `json:"type"`
						State struct {
							Time struct {
								Compacted int64 `json:"compacted"`
							} `json:"time"`
						} `json:"state"`
					}
					if json.Unmarshal(part, &decoded) == nil && decoded.Type == "tool" && decoded.State.Time.Compacted > 0 {
						pruned = true
					}
				}
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if !pruned {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !pruned {
		t.Fatal("upstream automatic pruning was not observed")
	}
	server.stop(t)
	path := filepath.Join(t.TempDir(), "native.json")
	if _, err := server.server.Export(ctx, session.ID, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Messages []Message `json:"messages"`
	}
	if json.Unmarshal(raw, &record) != nil {
		t.Fatal("invalid native export")
	}
	observed := 0
	for _, message := range record.Messages {
		for _, part := range message.Parts {
			var decoded struct {
				Type  string `json:"type"`
				State struct {
					Output string `json:"output"`
					Time   struct {
						Compacted int64 `json:"compacted"`
					} `json:"time"`
				} `json:"state"`
			}
			if json.Unmarshal(part, &decoded) == nil && decoded.Type == "tool" && decoded.State.Time.Compacted > 0 {
				observed++
				if decoded.State.Output == "" {
					t.Fatal("pruning destroyed archived original tool output")
				}
			}
		}
	}
	if observed == 0 {
		t.Fatal("pruning markers did not survive official export")
	}
	t.Logf("native pruning: retainedMessages=%d prunedToolMarkers=%d exportBytes=%d", len(record.Messages), observed, len(raw))
}
