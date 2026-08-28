package integrationpackage

import (
	"strings"
	"testing"
)

func TestLoadShippedDefinitions(t *testing.T) {
	t.Parallel()
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 3 {
		t.Fatalf("LoadShipped() returned %d definitions; want 3", len(definitions))
	}
	github := definitions["github"]
	if github.Digest == "" || github.Metadata.Version != "1.0.0" || github.Spec.Credential.SecretKey != "token" {
		t.Fatalf("GitHub definition metadata is incomplete: %#v", github)
	}
	for _, key := range []string{"github.repository.metadata.read", "github.issue.create", "github.issue.update"} {
		if _, ok := github.Capability(key); !ok {
			t.Fatalf("GitHub capability %q is missing", key)
		}
	}
	write, _ := definitions["synthetic"].Capability("synthetic.journal.write")
	if write.Risk != "WRITE" || write.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
		t.Fatalf("synthetic write policy = %s/%s", write.Risk, write.ApprovalPolicy)
	}
}

func TestParseRejectsUnknownDuplicateAliasAndTrailingDocument(t *testing.T) {
	t.Parallel()
	base := shippedYAML["synthetic.yaml"]
	inputs := []string{
		strings.Replace(base, "  name:", "  unknown: true\n  name:", 1),
		strings.Replace(base, "  name:", "  name: duplicate\n  name:", 1),
		strings.Replace(base, "metadata:\n", "metadata: &metadata\n", 1),
		base + "\n---\n{}\n",
	}
	for _, input := range inputs {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatal("Parse() accepted unsafe integration package")
		}
	}
}

func TestDigestIsStableAndContentBound(t *testing.T) {
	t.Parallel()
	first, err := Parse([]byte(shippedYAML["synthetic.yaml"]))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse([]byte("# comment\n" + shippedYAML["synthetic.yaml"]))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := Parse([]byte(strings.Replace(shippedYAML["synthetic.yaml"], "testing", "test", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.Digest == changed.Digest {
		t.Fatalf("canonical digest mismatch: %q %q %q", first.Digest, second.Digest, changed.Digest)
	}
}

func TestTypedConfigurationScopeAndInput(t *testing.T) {
	t.Parallel()
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	github := definitions["github"]
	configuration := map[string]string{"owner": "codex-k8s", "repository": "integration-test"}
	if err := github.ValidateConfiguration(configuration); err != nil {
		t.Fatal(err)
	}
	capability, _ := github.Capability("github.issue.update")
	scope, err := capability.ResourceScopeValues(configuration)
	if err != nil || scope["owner"] != "codex-k8s" || scope["repository"] != "integration-test" {
		t.Fatalf("ResourceScopeValues() = %#v, %v", scope, err)
	}
	canonical, err := capability.ValidateInput([]byte(`{"issue_number":7,"title":"fixed"}`))
	if err != nil || string(canonical) != `{"issue_number":7,"title":"fixed"}` {
		t.Fatalf("ValidateInput() = %s, %v", canonical, err)
	}
	for _, invalid := range []string{
		`{"issue_number":0}`,
		`{"issue_number":7,"issue_number":8}`,
		`{"issue_number":7.5}`,
		`{"issue_number":7,"unknown":true}`,
	} {
		if _, err := capability.ValidateInput([]byte(invalid)); err == nil {
			t.Fatalf("ValidateInput() accepted %s", invalid)
		}
	}
}
