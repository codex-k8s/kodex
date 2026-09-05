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
	if len(definitions) != 7 {
		t.Fatalf("LoadShipped() returned %d definitions; want 7", len(definitions))
	}
	github := definitions["github"]
	if github.Digest == "" || github.Metadata.Version != "2.2.0" || github.Spec.Credential.SecretKey != "token" {
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
	for _, key := range []string{"gitlab", "jira", "confluence", "email", "mattermost", "synthetic"} {
		definition := definitions[key]
		if definition.Digest == "" || definition.Spec.HealthCheck.Operation == "" || len(definition.Spec.NetworkDestinations) == 0 {
			t.Fatalf("definition %q does not have an executable boundary: %#v", key, definition.Spec)
		}
	}
	for key, definition := range definitions {
		executable := definition.ExecutableBy(OwnerIntegrationGateway, RouteManagedMCP)
		if key == "mattermost" {
			if executable || definition.Spec.AdapterOwner != string(OwnerInteractionGateway) ||
				definition.Spec.ExecutionRoute != string(RouteInteraction) || definition.Spec.Readiness != string(ReadinessNotReady) {
				t.Fatalf("Mattermost executable routing is invalid: %#v", definition.Spec)
			}
			continue
		}
		if !executable {
			t.Fatalf("integration-gateway definition %q is not executable", key)
		}
	}
}

func TestParseRejectsAdapterRoutingMismatch(t *testing.T) {
	t.Parallel()
	base := shippedYAML["mattermost.yaml"]
	for _, changed := range []string{
		strings.Replace(base, "adapterOwner: interaction-gateway", "adapterOwner: integration-gateway", 1),
		strings.Replace(base, "executionRoute: INTERACTION", "executionRoute: MANAGED_MCP", 1),
		strings.Replace(base, "readiness: NOT_READY", "readiness: READY", 1),
	} {
		if _, err := Parse([]byte(changed)); err == nil {
			t.Fatal("Parse() accepted mismatched adapter routing")
		}
	}
}

func TestInputSchemaDigestIsCanonicalAndClosed(t *testing.T) {
	t.Parallel()
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	capability, _ := definitions["github"].Capability("github.issue.update")
	schema, err := capability.InputSchema()
	digest, digestErr := capability.InputSchemaDigest()
	if err != nil || digestErr != nil || len(digest) != 64 ||
		!strings.Contains(string(schema), `"additionalProperties":false`) ||
		!strings.Contains(string(schema), `"issue_number"`) {
		t.Fatalf("InputSchema() = %s, %q, %v, %v", schema, digest, err, digestErr)
	}
}

func TestTypedOutput(t *testing.T) {
	t.Parallel()
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	capability, ok := definitions["email"].Capability("email.message.send")
	if !ok {
		t.Fatal("email.message.send capability is missing")
	}
	canonical, err := capability.ValidateOutput([]byte(`{"status":"accepted","message_id":"msg-7"}`))
	if err != nil || string(canonical) != `{"message_id":"msg-7","status":"accepted"}` {
		t.Fatalf("ValidateOutput() = %s, %v", canonical, err)
	}
	for _, invalid := range []string{
		`{"status":"accepted"}`,
		`{"message_id":"msg-7","status":"accepted","token":"secret"}`,
		`{"message_id":"msg-7","status":"accepted\nunsafe"}`,
	} {
		if _, err := capability.ValidateOutput([]byte(invalid)); err == nil {
			t.Fatalf("ValidateOutput() accepted %s", invalid)
		}
	}
}

func TestConfigurationRejectsUnsafeProviderOrigin(t *testing.T) {
	t.Parallel()
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	gitlab := definitions["gitlab"]
	for _, origin := range []string{
		"http://gitlab.example.com",
		"https://127.0.0.1",
		"https://user@gitlab.example.com",
		"https://gitlab.example.com/api/v4",
	} {
		if err := gitlab.ValidateConfiguration(map[string]string{"base_url": origin, "project_path": "org/project"}); err == nil {
			t.Fatalf("ValidateConfiguration() accepted unsafe origin %q", origin)
		}
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

func TestParseRejectsUnknownClosedRegistryValues(t *testing.T) {
	t.Parallel()
	base := shippedYAML["synthetic.yaml"]
	for _, replacement := range []struct{ old, new string }{
		{"adapter: SYNTHETIC_HTTP", "adapter: UNKNOWN"},
		{"risk: READ", "risk: UNKNOWN"},
		{"approvalPolicy: NONE", "approvalPolicy: UNKNOWN"},
		{"kind: SYNTHETIC_JOURNAL", "kind: UNKNOWN"},
		{"type: STRING", "type: UNKNOWN"},
		{"idempotency: READ_ONLY", "idempotency: UNKNOWN"},
	} {
		if _, err := Parse([]byte(strings.Replace(base, replacement.old, replacement.new, 1))); err == nil {
			t.Fatalf("Parse() accepted unknown registry value %q", replacement.new)
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
