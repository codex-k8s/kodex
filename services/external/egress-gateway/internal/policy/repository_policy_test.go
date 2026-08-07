package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

const repositoryPolicyDigest = "5c71fefd60e624d6891e857442302c2b119f21b76b474d3c34f1c6df330f62ae"

func TestRepositoryPolicyMatchesExpectedImmutableDigest(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..")
	value, err := os.ReadFile(filepath.Join(root, "deploy", "k8s", "base", "egress-gateway", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, actualDigest, err := parseAndDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	if actualDigest != repositoryPolicyDigest {
		t.Fatalf("repository policy digest mismatch: got %s", actualDigest)
	}
	active, err := Load(value, "2026-08-07.1", repositoryPolicyDigest)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"api.openai.com", "auth.openai.com", "chatgpt.com", "github.com"}
	if destinations := active.Destinations(); len(destinations) != len(expected) {
		t.Fatalf("unexpected destination count: %d", len(destinations))
	} else {
		for index, hostname := range expected {
			if destinations[index].Hostname != hostname || destinations[index].Port != 443 {
				t.Fatalf("unexpected destination: %+v", destinations[index])
			}
		}
	}
	schema, err := os.ReadFile(filepath.Join(root, "contracts", "egress", "v1", "egress-gateway-policy.schema.json"))
	if err != nil || !json.Valid(schema) {
		t.Fatalf("machine policy schema is not valid JSON: %v", err)
	}
	var compiled jsonschema.Schema
	decoder := json.NewDecoder(bytes.NewReader(schema))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&compiled); err != nil {
		t.Fatalf("machine policy schema is unsupported: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatal("machine policy schema contains trailing data")
	}
	resolved, err := compiled.Resolve(nil)
	if err != nil {
		t.Fatalf("machine policy schema cannot be resolved: %v", err)
	}
	var instance any
	instanceDecoder := json.NewDecoder(bytes.NewReader(value))
	if err := instanceDecoder.Decode(&instance); err != nil {
		t.Fatal(err)
	}
	if err := resolved.Validate(instance); err != nil {
		t.Fatalf("repository policy does not match machine schema: %v", err)
	}
	for _, alias := range []string{"API.OPENAI.COM", "api.openai.com."} {
		aliased := bytes.Replace(value, []byte("api.openai.com"), []byte(alias), 1)
		var aliasedInstance any
		if err := json.Unmarshal(aliased, &aliasedInstance); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(aliasedInstance); err == nil {
			t.Fatalf("machine schema accepted non-canonical hostname %q", alias)
		}
	}
}
