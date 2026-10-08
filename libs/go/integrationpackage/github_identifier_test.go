package integrationpackage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGitHubProviderIdentifiersUseExactJSONIntegerBounds(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	if definition.Metadata.Version != "4.0.0" {
		t.Fatal("unexpected GitHub package version")
	}
	if _, ok := ResolveShippedRevision(definition, "2.4.0", definition.Digest); ok {
		t.Fatal("stale package revision accepted")
	}
	ids := map[string]bool{"id": true, "comment_id": true, "review_id": true, "check_run_id": true, "workflow_id": true, "run_id": true, "job_id": true}
	count := 0
	for _, capability := range definition.Spec.Capabilities {
		for _, fields := range [][]Field{capability.InputFields, capability.OutputFields} {
			for _, field := range fields {
				if field.Type != "INTEGER" {
					continue
				}
				if ids[field.Key] {
					count++
					if field.Minimum != 1 || field.Maximum != 9007199254740991 {
						t.Fatalf("unsafe provider ID bound: %s/%s", capability.Operation, field.Key)
					}
				} else if field.Key == "issue_number" || field.Key == "pull_request_number" {
					if field.Maximum != 2147483647 {
						t.Fatal("repository-local number bound changed")
					}
				} else if field.Key == "limit" || field.Key == "count" {
					expected := int64(100)
					if capability.Operation == "github.repository.content.list" && field.Key == "count" {
						expected = 1000
					}
					if capability.Operation == "github.pull_request.file.list" {
						expected = 4
					}
					if field.Maximum != expected {
						t.Fatal("pagination cardinality changed")
					}
				} else if field.Key == "cursor" || field.Key == "next_cursor" {
					if field.Maximum != 10000 {
						t.Fatal("pagination cursor bound changed")
					}
				}
			}
		}
	}
	if count != 24 {
		t.Fatalf("provider ID registry incomplete: %d", count)
	}
}

func TestGitHubReviewIdentifiersRemainClosed(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	read, _ := definitions["github"].Capability("github.pull_request.review.read")
	create, _ := definitions["github"].Capability("github.pull_request.review.create")
	schema, err := read.InputSchema()
	if err != nil {
		t.Fatal(err)
	}
	var descriptor struct {
		Properties map[string]struct {
			Type    string `json:"type"`
			Maximum int64  `json:"maximum"`
		} `json:"properties"`
		AdditionalProperties bool `json:"additionalProperties"`
	}
	if json.Unmarshal(schema, &descriptor) != nil || descriptor.Properties["review_id"].Type != "integer" || descriptor.Properties["review_id"].Maximum != 9007199254740991 || descriptor.AdditionalProperties {
		t.Fatal("runtime tool schema lost exact closed identifier bounds")
	}
	old := create
	old.OutputFields = append([]Field(nil), create.OutputFields...)
	for i := range old.OutputFields {
		if old.OutputFields[i].Key == "id" {
			old.OutputFields[i].Maximum = 2147483647
		}
	}
	if _, err := old.ValidateOutput([]byte(`{"id":5436151248,"body":"","state":"COMMENTED","commit_id":"abc"}`)); err == nil {
		t.Fatal("historical int32 defect did not reproduce")
	}
	if create.Execution.MaxAttempts != 1 || create.Execution.Idempotency != "EFFECT_KEY" || create.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
		t.Fatal("review effect boundary changed")
	}
	for _, id := range []string{"5436151248", "9007199254740991"} {
		input := fmt.Sprintf(`{"pull_request_number":1799,"review_id":%s}`, id)
		if _, err := read.ValidateInput([]byte(input)); err != nil {
			t.Fatalf("valid ID rejected: %v", err)
		}
		output := fmt.Sprintf(`{"id":%s,"body":"%s","state":"COMMENTED","commit_id":"%s"}`, id, strings.Repeat("a", 1800), strings.Repeat("a", 40))
		for _, capability := range []Capability{read, create} {
			if _, err := capability.ValidateOutput([]byte(output)); err != nil {
				t.Fatalf("valid review rejected: %v", err)
			}
		}
	}
	for _, id := range []string{"0", "-1", "5436151248.5", `"not-an-id"`, "9007199254740992", "9223372036854775808"} {
		input := fmt.Sprintf(`{"pull_request_number":1799,"review_id":%s}`, id)
		if _, err := read.ValidateInput([]byte(input)); err == nil {
			t.Fatal("invalid input ID accepted")
		}
		output := fmt.Sprintf(`{"id":%s,"body":"","state":"COMMENTED","commit_id":"abc"}`, id)
		if _, err := create.ValidateOutput([]byte(output)); err == nil {
			t.Fatal("invalid output ID accepted")
		}
	}
	for _, raw := range []string{
		`{"pull_request_number":1799,"review_id":5436151248,"actor":"owner"}`,
		`{"pull_request_number":1799,"review_id":5436151248,"review_id":4}`,
	} {
		if _, err := read.ValidateInput([]byte(raw)); err == nil {
			t.Fatal("open or ambiguous input accepted")
		}
	}
	for _, raw := range []string{
		`{"id":5436151248,"body":"","state":"COMMENTED","commit_id":"abc","token":"private"}`,
		fmt.Sprintf(`{"id":5436151248,"body":"%s","state":"COMMENTED","commit_id":"abc"}`, strings.Repeat("a", 32769)),
	} {
		if _, err := create.ValidateOutput([]byte(raw)); err == nil {
			t.Fatal("invalid output envelope accepted")
		}
	}
}
