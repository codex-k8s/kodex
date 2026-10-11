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
	if definition.Metadata.Version != "6.0.0" {
		t.Fatal("unexpected GitHub source budget revision")
	}
	for _, version := range []string{"3.0.0", "3.1.0", "4.0.0", "5.0.0"} {
		if _, ok := ResolveShippedRevision(definition, version, definition.Digest); ok {
			t.Fatal("old shipped revision was silently reinterpreted")
		}
	}
	capability, ok := definition.Capability("github.repository.content.read")
	if !ok || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" ||
		capability.Execution.Idempotency != "READ_ONLY" {
		t.Fatal("source pagination changed the execution authority or effect policy")
	}
	const maximumSourceBytes = 8388608
	for _, offset := range []int{65536, 65537, 536156, 1048576, 1048577, 3590649, maximumSourceBytes} {
		input := `{"path":"docs/source.md","ref":"` + strings.Repeat("a", 40) +
			`","expected_sha":"` + strings.Repeat("b", 40) + `","offset_bytes":` + strconv.Itoa(offset) + `,"maximum_bytes":16384}`
		if _, err := capability.ValidateInput([]byte(input)); err != nil {
			t.Fatalf("valid source offset rejected: %d, %v", offset, err)
		}
	}
	for _, maximum := range []int{4, 2048, 16384, 16385} {
		input := `{"path":"docs/source.md","ref":"` + strings.Repeat("a", 40) + `","maximum_bytes":` + strconv.Itoa(maximum) + `}`
		_, err := capability.ValidateInput([]byte(input))
		if (err == nil) != (maximum <= 16384) {
			t.Fatalf("page input schema did not preserve exact bounds: %d, %v", maximum, err)
		}
	}
	schemaRaw, err := capability.InputSchema()
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Minimum int `json:"minimum"`
			Maximum int `json:"maximum"`
		} `json:"properties"`
	}
	if json.Unmarshal(schemaRaw, &schema) != nil || schema.Properties["maximum_bytes"].Minimum != 4 || schema.Properties["maximum_bytes"].Maximum != 16384 ||
		schema.Properties["offset_bytes"].Minimum != 0 || schema.Properties["offset_bytes"].Maximum != maximumSourceBytes {
		t.Fatal("native input schema lost the independent source/page bounds")
	}
	input := `{"path":"docs/source.md","ref":"` + strings.Repeat("a", 40) + `","offset_bytes":8388609}`
	if _, err := capability.ValidateInput([]byte(input)); err == nil {
		t.Fatal("source offset exceeded the separate eight MiB budget")
	}
	output := map[string]any{
		"path": "docs/source.md", "type": "file", "sha": strings.Repeat("b", 40), "commit_sha": strings.Repeat("a", 40),
		"size": maximumSourceBytes, "offset_bytes": maximumSourceBytes, "next_offset_bytes": maximumSourceBytes, "eof": true, "text": "",
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
		output[key] = maximumSourceBytes + 1
		if key == "text" {
			output[key] = strings.Repeat("x", 16385)
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
	for _, size := range []int{1048577, 3590649, maximumSourceBytes} {
		for _, eof := range []bool{false, true} {
			output["size"], output["eof"] = size, eof
			output["offset_bytes"], output["next_offset_bytes"], output["text"] = size-16385, size-1, strings.Repeat("x", 16384)
			if eof {
				output["offset_bytes"], output["next_offset_bytes"], output["text"] = size, size, ""
			}
			encoded, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := capability.ValidateOutput(encoded); err != nil {
				t.Fatalf("large source page/EOF rejected: size=%d eof=%t, %v", size, eof, err)
			}
		}
	}
	for _, fields := range [][]Field{capability.InputFields, capability.OutputFields} {
		for _, field := range fields {
			if field.Key == "offset_bytes" || field.Key == "next_offset_bytes" || field.Key == "size" {
				if field.Minimum != 0 || field.Maximum != maximumSourceBytes {
					t.Fatalf("source schema/adapter bound mismatch: %s", field.Key)
				}
			}
		}
	}
	t.Logf("canonical GitHub source package version=%s digest=%s", definition.Metadata.Version, definition.Digest)
}

func TestGitHubLargeSourceProtocolDoesNotExposeBlobSelectorOrExpandAuthority(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	capability, ok := definition.Capability("github.repository.content.read")
	if !ok || capability.ResourceScope.Kind != "GITHUB_REPOSITORY" ||
		strings.Join(capability.ResourceScope.ConnectionFields, ",") != "owner,repository" ||
		!definition.RequiresConnectionCredential() || !definition.ExecutableBy(OwnerIntegrationGateway, RouteManagedMCP) {
		t.Fatal("large source path expanded the connection-owned execution boundary")
	}
	for _, field := range []string{"blob_sha", "download_url", "raw_url", "hostname", "owner", "repository", "permission"} {
		input := `{"path":"docs/source.md","ref":"` + strings.Repeat("a", 40) + `","` + field + `":"untrusted"}`
		if _, err := capability.ValidateInput([]byte(input)); err == nil {
			t.Fatalf("caller-controlled blob authority accepted: %s", field)
		}
	}
	if len(definition.Spec.NetworkDestinations) != 2 || definition.Spec.NetworkDestinations[0].Hostname != "api.github.com" ||
		definition.Spec.NetworkDestinations[1].Hostname != "github.com" {
		t.Fatal("large source contract added a network destination")
	}
}
