package integrationpackage

import "testing"

// TestCredentialReadProbeRegistry excludes writes, inputs, aliases and arbitrary read names from
// personal verification, keeping one exact published health probe for unsupported adapters.
// TestCredentialReadProbeRegistry исключает записи, inputs, aliases и произвольные read names,
// сохраняя единственный опубликованный health для неподдержанных adapters.
func TestCredentialReadProbeRegistry(t *testing.T) {
	health := Capability{Key: "fixture.health", Operation: "gitlab.project.metadata.read", Risk: "READ", ApprovalPolicy: "NONE"}
	list := Capability{Key: "fixture.list", Operation: "gitlab.issue.list", Risk: "READ", ApprovalPolicy: "NONE"}
	p := Package{Spec: Spec{Adapter: "GITLAB", Capabilities: []Capability{health, list}, HealthCheck: HealthCheck{Operation: health.Operation}}}
	if got := CredentialReadProbes(p); len(got) != 2 || got[0].Key != health.Key || got[1].Key != list.Key {
		t.Fatal("registered read probes differ")
	}
	for _, change := range []func(*Capability){
		func(c *Capability) { c.Risk = "WRITE" },
		func(c *Capability) { c.ApprovalPolicy = "HUMAN_EACH_EFFECT" },
		func(c *Capability) { c.Operation = "gitlab.arbitrary.read" },
		func(c *Capability) { c.InputFields = []Field{{Key: "issue_id", Required: true}} },
	} {
		candidate := p
		candidate.Spec.Capabilities = append([]Capability(nil), p.Spec.Capabilities...)
		change(&candidate.Spec.Capabilities[1])
		if len(CredentialReadProbes(candidate)) != 1 {
			t.Fatal("unregistered probe admitted")
		}
	}
	alias := list
	alias.Key = "fixture.alias"
	p.Spec.Capabilities = append(p.Spec.Capabilities, alias)
	if len(CredentialReadProbes(p)) != 2 {
		t.Fatal("alias multiplied requests")
	}
	p.Spec.Adapter = "OPENAPI_MCP"
	if len(CredentialReadProbes(p)) != 1 {
		t.Fatal("unknown package widened probe registry")
	}
}
