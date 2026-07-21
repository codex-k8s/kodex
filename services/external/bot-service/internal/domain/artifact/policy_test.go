package artifact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeMetadataNameNeutralizesControlAndBidiCharacters(t *testing.T) {
	t.Parallel()
	name := SafeMetadataName("отчёт\n\u202esecret.txt")
	if strings.ContainsRune(name, '\n') || strings.ContainsRune(name, '\u202e') || !strings.ContainsRune(name, '�') {
		t.Fatalf("SafeMetadataName() = %q", name)
	}
}

func TestStageReaderDetectsJSONBeyondSniffPrefix(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(map[string]string{"value": strings.Repeat("x", 1024)})
	if err != nil {
		t.Fatal(err)
	}
	staged, err := stageReader(strings.NewReader(string(body)), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.closeAndRemove()
	if staged.mediaType != "application/json" {
		t.Fatalf("media type = %q", staged.mediaType)
	}
}
