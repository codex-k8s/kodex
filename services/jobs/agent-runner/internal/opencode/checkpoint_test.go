package opencode

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkpointFixture contains only native identity and opaque synthetic transcript data.
// checkpointFixture содержит только native identity и непрозрачные synthetic transcript data.
func checkpointFixture() map[string]any {
	return map[string]any{"info": map[string]any{"id": "ses_native", "version": "1.19.5"}, "messages": []any{
		map[string]any{"info": map[string]any{"id": "msg_native", "sessionID": "ses_native", "role": "assistant"},
			"parts": []any{map[string]any{"id": "prt_native", "messageID": "msg_native", "sessionID": "ses_native", "type": "tool", "state": map[string]string{"status": "completed"}}}},
	}}
}

// TestCheckpointRejectsForeignIncompleteAndMalformedHistory prevents recovery from hiding uncertain effects.
// TestCheckpointRejectsForeignIncompleteAndMalformedHistory запрещает restore скрывать uncertain effects.
func TestCheckpointRejectsForeignIncompleteAndMalformedHistory(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(map[string]any)
		want   error
	}{
		{"version", func(record map[string]any) { record["info"].(map[string]any)["version"] = "unqualified" }, ErrCheckpoint},
		{"session", func(record map[string]any) { record["info"].(map[string]any)["id"] = "ses_foreign" }, ErrCheckpoint},
		{"message", func(record map[string]any) {
			record["messages"].([]any)[0].(map[string]any)["info"].(map[string]any)["sessionID"] = "ses_foreign"
		}, ErrCheckpoint},
		{"part", func(record map[string]any) {
			record["messages"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["messageID"] = "msg_foreign"
		}, ErrCheckpoint},
		{"pending", func(record map[string]any) {
			record["messages"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["state"] = map[string]string{"status": "pending"}
		}, ErrUncertainEffect},
		{"running", func(record map[string]any) {
			record["messages"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["state"] = map[string]string{"status": "running"}
		}, ErrUncertainEffect},
		{"duplicate-message", func(record map[string]any) {
			messages := record["messages"].([]any)
			record["messages"] = append(messages, messages[0])
		}, ErrCheckpoint},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := checkpointFixture()
			test.change(record)
			raw, _ := json.Marshal(record)
			if _, err := InspectCheckpoint(raw, "ses_native"); !errors.Is(err, test.want) {
				t.Fatalf("wrong checkpoint failure: %v", err)
			}
		})
	}
	raw, _ := json.Marshal(checkpointFixture())
	valid, err := InspectCheckpoint(raw, "ses_native")
	if err != nil || valid.Messages != 1 || valid.SizeBytes != int64(len(raw)) || len(valid.SHA256) != 64 {
		t.Fatal("valid native checkpoint rejected")
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), raw...), []byte(strings.Replace(string(raw), `"id":"ses_native"`, `"id":"ses_native","id":"ses_other"`, 1)), raw[:len(raw)-1]} {
		if _, err := InspectCheckpoint(invalid, "ses_native"); !errors.Is(err, ErrCheckpoint) {
			t.Fatal("malformed native checkpoint accepted")
		}
	}
}

// TestCheckpointFileRejectsLinksAndDigestSubstitution protects offline import before any native process starts.
// TestCheckpointFileRejectsLinksAndDigestSubstitution защищает offline import до запуска native-процесса.
func TestCheckpointFileRejectsLinksAndDigestSubstitution(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "checkpoint.json")
	raw, _ := json.Marshal(checkpointFixture())
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	valid, err := inspectCheckpointFile(path, "ses_native", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspectCheckpointFile(path, "ses_native", strings.Repeat("a", 64)); err == nil {
		t.Fatal("substituted digest accepted")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectCheckpointFile(link, "ses_native", valid.SHA256); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectCheckpointFile(path, "ses_native", valid.SHA256); err == nil {
		t.Fatal("shared inode accepted")
	}
}
