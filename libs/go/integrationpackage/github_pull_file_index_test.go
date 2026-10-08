package integrationpackage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGitHubPullFileIndexVersionAndRequiredBoundedPins(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	if definition.Metadata.Version != "5.0.0" {
		t.Fatal("unexpected metadata-only contract revision")
	}
	if _, ok := ResolveShippedRevision(definition, "3.1.0", definition.Digest); ok {
		t.Fatal("legacy version silently reinterpreted")
	}
	capability, ok := definition.Capability("github.pull_request.file.list")
	if !ok || capability.Risk != "READ" || capability.Execution.Idempotency != "READ_ONLY" || capability.ApprovalPolicy != "NONE" {
		t.Fatal("index changed execution authority")
	}
	input := map[string]any{"pull_request_number": 3, "expected_head_sha": strings.Repeat("a", 40), "expected_base_sha": strings.Repeat("b", 40), "expected_changed_files": 3000, "limit": 4, "cursor": 750}
	encoded, _ := json.Marshal(input)
	if _, err := capability.ValidateInput(encoded); err != nil {
		t.Fatal("valid exact cap input rejected")
	}
	for _, key := range []string{"expected_head_sha", "expected_base_sha", "expected_changed_files"} {
		value := input[key]
		delete(input, key)
		encoded, _ = json.Marshal(input)
		if _, err := capability.ValidateInput(encoded); err == nil {
			t.Fatal("required pin missing accepted")
		}
		input[key] = value
	}
	for _, tc := range []struct {
		key   string
		value any
	}{{"limit", 5}, {"limit", 0}, {"expected_changed_files", 3001}, {"expected_changed_files", -1}, {"expected_changed_files", nil}, {"expected_head_sha", strings.Repeat("a", 41)}, {"expected_base_sha", true}, {"cursor", 10001}} {
		value := input[tc.key]
		input[tc.key] = tc.value
		encoded, _ = json.Marshal(input)
		if _, err := capability.ValidateInput(encoded); err == nil {
			t.Fatal("invalid bound or type accepted")
		}
		input[tc.key] = value
	}
	output := map[string]any{"items": "[]", "count": 0, "head_sha": strings.Repeat("a", 40), "base_sha": strings.Repeat("b", 40), "total_count": 0, "page_size": 4, "offset": 0, "eof": true}
	encoded, _ = json.Marshal(output)
	if _, err := capability.ValidateOutput(encoded); err != nil {
		t.Fatal("empty metadata index rejected")
	}
	for _, tc := range []struct {
		key   string
		value any
	}{{"count", 5}, {"page_size", 5}, {"total_count", 3001}, {"offset", 3001}, {"eof", 1}, {"source_read", true}} {
		original, exists := output[tc.key]
		output[tc.key] = tc.value
		encoded, _ = json.Marshal(output)
		if _, err := capability.ValidateOutput(encoded); err == nil {
			t.Fatal("invalid output or invented source completeness accepted")
		}
		if exists {
			output[tc.key] = original
		} else {
			delete(output, tc.key)
		}
	}
}

func TestGitHubPullSourcePinsAreReadOnlyAndMutationsKeepFullBody(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	for _, operation := range []string{"github.pull_request.read", "github.pull_request.create", "github.pull_request.update"} {
		capability, ok := definition.Capability(operation)
		if !ok {
			t.Fatal("PR operation missing")
		}
		fields := map[string]Field{}
		for _, field := range capability.OutputFields {
			fields[field.Key] = field
		}
		for _, key := range []string{"body", "state", "head", "base", "sha", "draft", "url"} {
			field, exists := fields[key]
			if !exists || !field.Required {
				t.Fatal("full PR projection lost a required field")
			}
		}
		if fields["body"].MaximumLength != 32768 || !fields["body"].AllowEmpty {
			t.Fatal("full body narrowed or truncated")
		}
		_, base := fields["base_sha"]
		_, total := fields["changed_files"]
		if base != (operation == "github.pull_request.read") || total != (operation == "github.pull_request.read") {
			t.Fatal("source metadata changed mutation outcome contract")
		}
	}
	for _, operation := range []string{"github.pull_request.list", "github.commit.read"} {
		capability, ok := definition.Capability(operation)
		if !ok {
			t.Fatal("unrelated operation disappeared")
		}
		for _, field := range capability.InputFields {
			if strings.HasPrefix(field.Key, "expected_") {
				t.Fatal("PR pins leaked into unrelated operation")
			}
		}
	}
}
