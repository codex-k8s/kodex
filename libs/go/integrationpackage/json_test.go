package integrationpackage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPackageJSONMatchesYAMLAndRejectsAmbiguousDocuments(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for key, definition := range definitions {
		raw, _ := json.Marshal(definition)
		parsed, err := Parse(raw)
		if err != nil || parsed.Digest != definition.Digest {
			t.Fatalf("JSON/YAML %s mismatch: %v", key, err)
		}
	}
	raw, _ := json.Marshal(definitions["github"])
	for name, source := range map[string]string{
		"duplicate root":    strings.Replace(string(raw), `"kind":`, `"kind":"IntegrationPackage","kind":`, 1),
		"duplicate nested":  strings.Replace(string(raw), `"origin":`, `"origin":"UI","origin":`, 1),
		"escaped duplicate": strings.Replace(string(raw), `"origin":`, `"\u006frigin":"UI","origin":`, 1),
		"case folded key":   strings.Replace(string(raw), `"origin":`, `"Origin":`, 1),
		"unknown":           strings.Replace(string(raw), `"origin":`, `"unexpected":false,"origin":`, 1),
		"trailing":          string(raw) + ` {}`,
		"truncated":         string(raw[:len(raw)-1]),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(source)); err == nil {
				t.Fatal("ambiguous JSON accepted")
			}
		})
	}
}
