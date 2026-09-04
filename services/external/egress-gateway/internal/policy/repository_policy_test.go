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

const repositoryPolicyDigest = "8529a00f3e8923e59d1776ee64d1965ee1e8f891daa17b94927c816248d6f218"

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
	active, err := Load(value, "2026-09-05.1", repositoryPolicyDigest)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := active.ForProfile(STTProfileName)
	if err != nil || profile.Digest() != active.Digest() || profile.Revision() != active.Revision() ||
		!profile.Allows("api.openai.com", 443) || profile.Allows("auth.openai.com", 443) || profile.Allows("github.com", 443) ||
		len(profile.Destinations()) != 1 {
		t.Fatalf("unexpected STT profile: %v", err)
	}
	if name, workload, operation := profile.ProfileIdentity(); name != STTProfileName || workload != STTWorkload || operation != STTOperation {
		t.Fatal("STT profile identity mismatch")
	}
	profile.Destinations()[0].Hostname = "github.com"
	if profile.Destinations()[0].Hostname != "api.openai.com" {
		t.Fatal("profile destination snapshot is mutable")
	}
	if _, err := active.ForProfile("unknown"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	expected := []string{"api.github.com", "api.openai.com", "auth.openai.com", "chatgpt.com", "github.com"}
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
