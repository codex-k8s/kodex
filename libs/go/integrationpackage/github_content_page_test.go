package integrationpackage

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestGitHubContentReadSeparatesSourceAndPageBudgets(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	if definition.Metadata.Version != "3.1.0" {
		t.Fatal("unexpected GitHub source budget revision")
	}
	if _, ok := ResolveShippedRevision(definition, "3.0.0", definition.Digest); ok {
		t.Fatal("old shipped revision was silently reinterpreted")
	}
	capability, ok := definition.Capability("github.repository.content.read")
	if !ok || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" ||
		capability.Execution.Idempotency != "READ_ONLY" {
		t.Fatal("source pagination changed the execution authority or effect policy")
	}
	for _, offset := range []int{65536, 65537, 536156, 1048576} {
		input := `{"path":"docs/source.md","ref":"` + strings.Repeat("a", 40) +
			`","expected_sha":"` + strings.Repeat("b", 40) + `","offset_bytes":` + strconv.Itoa(offset) + `,"maximum_bytes":2048}`
		if _, err := capability.ValidateInput([]byte(input)); err != nil {
			t.Fatalf("valid source offset rejected: %d, %v", offset, err)
		}
	}
	input := `{"path":"docs/source.md","ref":"` + strings.Repeat("a", 40) + `","offset_bytes":1048577}`
	if _, err := capability.ValidateInput([]byte(input)); err == nil {
		t.Fatal("source offset exceeded the separate one MiB budget")
	}
	output := map[string]any{
		"path": "docs/source.md", "type": "file", "sha": strings.Repeat("b", 40), "commit_sha": strings.Repeat("a", 40),
		"size": 1048576, "offset_bytes": 1048576, "next_offset_bytes": 1048576, "eof": true, "text": "",
		"source_digest": "sha256:" + strings.Repeat("c", 64), "chunk_digest": "sha256:" + strings.Repeat("d", 64),
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capability.ValidateOutput(encoded); err != nil {
		t.Fatalf("large source EOF rejected by output schema: %v", err)
	}
	for _, key := range []string{"size", "offset_bytes", "next_offset_bytes", "text"} {
		original := output[key]
		output[key] = 1048577
		if key == "text" {
			output[key] = strings.Repeat("x", 2049)
		}
		encoded, err := json.Marshal(output)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := capability.ValidateOutput(encoded); err == nil {
			t.Fatalf("output bypassed the separate source/page budget: %s", key)
		}
		output[key] = original
	}
}
