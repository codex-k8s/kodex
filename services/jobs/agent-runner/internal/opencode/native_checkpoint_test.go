package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNativeLongHistoryCheckpointAndFreshContext exercises upstream compaction, official export/import
// into an empty HOME, exact continuation and fresh system input, with over 128 retained native messages.
// TestNativeLongHistoryCheckpointAndFreshContext проверяет upstream compaction, официальный export/import
// в пустой HOME, точное продолжение и свежий system input с более 128 сохранёнными native-сообщениями.
func TestNativeLongHistoryCheckpointAndFreshContext(t *testing.T) {
	binary := qualifiedNativeBinary(t)
	fixture := &nativeFixture{targetTools: 144}
	config := nativeToolConfiguration(t, fixture)
	skillPath := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("SYNTHETIC_SKILL_INSTRUCTIONS_A"), 0o600); err != nil {
		t.Fatal(err)
	}
	config["instructions"] = []string{skillPath}
	first := startConfiguredNativeServer(t, binary, t.TempDir(), config)
	session, err := first.client.CreateSession(t.Context(), "Native long-history qualification")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	_, err = first.client.SendPrompt(ctx, session.ID, Prompt{
		MessageID: "msg_21234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
		System: "SYNTHETIC_CURRENT_INSTRUCTIONS_A",
		Parts:  []TextPart{{Type: "text", Text: "SYNTHETIC_FIRST_BUSINESS_INPUT. Complete all managed counter steps."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first.stop(t)
	if err := os.WriteFile(skillPath, []byte("SYNTHETIC_SKILL_INSTRUCTIONS_B"), 0o600); err != nil {
		t.Fatal(err)
	}
	exportPath := filepath.Join(t.TempDir(), "native-export.json")
	checkpoint, err := first.server.Export(ctx, session.ID, exportPath)
	if err != nil || checkpoint.Messages <= 128 {
		t.Fatalf("full native transcript did not survive compaction: messages=%d error=%v", checkpoint.Messages, err)
	}
	// Only official export bytes enter a new HOME; no SQLite or WAL file is copied.
	// В новый HOME попадает только официальный export; SQLite и WAL не копируются.
	secondHome := t.TempDir()
	if err := os.Chmod(secondHome, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"config", "cache", "data", "state"} {
		if err := os.Mkdir(filepath.Join(secondHome, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := restore(ctx, binary, secondHome, exportPath, session.ID, checkpoint.SHA256); err != nil {
		t.Fatal(err)
	}
	second := startConfiguredNativeServer(t, binary, secondHome, config)
	if restored, err := second.client.Session(ctx, session.ID); err != nil || restored.ID != session.ID {
		t.Fatal("exact native checkpoint session was not restored")
	}
	_, err = second.client.SendPrompt(ctx, session.ID, Prompt{
		MessageID: "msg_31234567890123456789012345", Model: Model{ProviderID: "synthetic", ModelID: "tiny"},
		System: "SYNTHETIC_CURRENT_INSTRUCTIONS_B",
		Parts:  []TextPart{{Type: "text", Text: "SYNTHETIC_SECOND_BUSINESS_INPUT. Return the completed counter marker."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second.stop(t)
	reexportPath := filepath.Join(t.TempDir(), "native-export.json")
	restored, err := second.server.Export(ctx, session.ID, reexportPath)
	reexported, readErr := os.ReadFile(reexportPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil || restored.Messages <= checkpoint.Messages {
		t.Fatal("continued native history was lost or overwritten")
	}
	var record struct {
		Messages []Message `json:"messages"`
	}
	if json.Unmarshal(reexported, &record) != nil {
		t.Fatal("native checkpoint cannot be inspected")
	}
	counts := map[string]int{}
	for _, message := range record.Messages {
		var info struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(message.Info, &info)
		counts[info.ID]++
	}
	if counts["msg_21234567890123456789012345"] != 1 || counts["msg_31234567890123456789012345"] != 1 {
		t.Fatal("business input was duplicated or lost during native restore")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.tools != 144 || fixture.summaries < 2 || len(fixture.violations) != 0 || fixture.maximumWire > 64<<10 {
		t.Fatalf("long-history boundary failed: tools=%d summaries=%d maximumWire=%d violations=%v", fixture.tools, fixture.summaries, fixture.maximumWire, fixture.violations)
	}
	wire := string(fixture.lastWire)
	if !strings.Contains(wire, "SYNTHETIC_CURRENT_INSTRUCTIONS_B") || strings.Contains(wire, "SYNTHETIC_CURRENT_INSTRUCTIONS_A") || strings.Count(wire, "SYNTHETIC_SECOND_BUSINESS_INPUT") != 1 {
		t.Fatal("fresh instructions were not applied or business input was replayed")
	}
	if !strings.Contains(wire, "SYNTHETIC_SKILL_INSTRUCTIONS_B") || strings.Contains(wire, "SYNTHETIC_SKILL_INSTRUCTIONS_A") {
		t.Fatal("native instruction files were stale after process replacement")
	}
	t.Logf("forced threshold: tools=%d compactions=%d retainedMessages=%d exportBytes=%d maximumModelWireBytes=%d", fixture.tools, fixture.summaries, restored.Messages, len(reexported), fixture.maximumWire)
}
