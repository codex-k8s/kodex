package integrationpackage

import (
	"strings"
	"testing"
)

// TestSSHComposeLeaseCannotWidenOtherAdapters checks exact operation and approval bounds.
// TestSSHComposeLeaseCannotWidenOtherAdapters проверяет границы операции и owner gate.
func TestSSHComposeLeaseCannotWidenOtherAdapters(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	deploy, _ := definitions["ssh-compose"].Capability("ssh.compose.deploy")
	if !validExecutionProfile("SSH_COMPOSE", deploy) {
		t.Fatal("closed SSH profile rejected")
	}
	for _, adapter := range []string{"GITHUB", "OPENAPI_MCP", "SYNTHETIC_HTTP"} {
		if validExecutionProfile(adapter, deploy) {
			t.Fatal("extended lease escaped SSH adapter")
		}
	}
	for _, change := range []func(*Capability){
		func(c *Capability) { c.Operation = "ssh.arbitrary" },
		func(c *Capability) { c.ApprovalPolicy = "NONE" },
		func(c *Capability) { c.Execution.MaxAttempts = 2 },
		func(c *Capability) { c.Execution.TimeoutSeconds = 601 },
	} {
		changed := deploy
		change(&changed)
		if validExecutionProfile("SSH_COMPOSE", changed) {
			t.Fatal("unsafe SSH profile accepted")
		}
	}
}

// TestSSHDestinationIsExactPublicIP rejects DNS, CIDR, private and mapped addresses.
// TestSSHDestinationIsExactPublicIP отклоняет DNS, CIDR, private и mapped адреса.
func TestSSHDestinationIsExactPublicIP(t *testing.T) {
	field := Field{Type: "STRING", Format: "PUBLIC_IP", MaximumLength: 45}
	for _, address := range []string{"192.0.2.10", "2001:db8::10"} {
		if validateStringValue(field, address, false) != nil {
			t.Fatal("literal public destination rejected")
		}
	}
	for _, address := range []string{"example.com", "192.0.2.10/32", "10.0.0.1", "127.0.0.1", "::ffff:192.0.2.10", "fe80::1%eth0"} {
		if validateStringValue(field, address, false) == nil {
			t.Fatal("nonexact destination accepted")
		}
	}
	if _, err := Parse([]byte(strings.Replace(shippedYAML["github.yaml"], "tls: REQUIRED", "tls: SSH_PINNED", 1))); err == nil {
		t.Fatal("SSH network mode escaped adapter")
	}
}
