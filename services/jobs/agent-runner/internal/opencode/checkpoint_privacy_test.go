package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCheckpointPrivacyChecksDecodedStrings prevents an escaped credential from reaching durable export without altering ordinary native text.
// TestCheckpointPrivacyChecksDecodedStrings запрещает escaped credential в durable export без изменения обычного native text.
func TestCheckpointPrivacyChecksDecodedStrings(t *testing.T) {
	credential := `synthetic-credential-"quoted"`
	for _, exposed := range []bool{false, true} {
		text := "synthetic ordinary history"
		if exposed {
			text += " " + credential
		}
		raw, _ := json.Marshal(map[string]any{"messages": []any{map[string]string{"text": text}}})
		path := filepath.Join(t.TempDir(), "native.json")
		if os.WriteFile(path, raw, 0600) != nil {
			t.Fatal("write private fixture")
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = checkpointExcludesCredentials(file, []string{credential})
		file.Close()
		if (err != nil) != exposed {
			t.Fatal("checkpoint privacy boundary changed")
		}
	}
}
