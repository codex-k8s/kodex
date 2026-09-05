package component

import (
	"errors"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

func TestMailboxPolicyProfiles(t *testing.T) {
	for _, profile := range []string{"read-allow-send-gate", "all-gate", "all-allow"} {
		t.Run(profile, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, secrets, authority := service(t, f, "implicit", nil)
			approved := false
			authority.mutate = func(d *api.AuthorizationDecision) { d.GateApproved = approved }
			for i := range s.Config.Mailboxes[0].Policies {
				p := &s.Config.Mailboxes[0].Policies[i]
				if profile == "all-gate" || (profile == "read-allow-send-gate" && p.Operation == api.OperationSend) {
					p.Policy = api.HumanGate
				}
			}
			for _, command := range []api.Command{{Operation: api.OperationList, MailboxId: "mailbox"}, send(api.OperationSend, profile)} {
				needsGate := profile == "all-gate" || (profile == "read-allow-send-gate" && command.Operation == api.OperationSend)
				before := secrets.reads.Load()
				_, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", command)
				if needsGate {
					if !errors.Is(err, errs.Gate) || secrets.reads.Load() != before {
						t.Fatal("gate must precede credentials")
					}
					approved = true
					execute(t, s, command)
					approved = false
				} else if err != nil {
					t.Fatal(err)
				}
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.sent) != 1 {
				t.Fatal("policy profile effect count")
			}
		})
	}
}

func TestConfigurationTransportAndScope(t *testing.T) {
	for name, mutate := range map[string]func(*api.Configuration){
		"plaintext":         func(c *api.Configuration) { c.Mailboxes[0].Smtp.TlsMode = "none" },
		"wildcard":          func(c *api.Configuration) { c.Mailboxes[0].Pop.Host = "*.example.test" },
		"sni":               func(c *api.Configuration) { c.Mailboxes[0].Pop.ServerName = "foreign.example.test" },
		"ip":                func(c *api.Configuration) { c.Mailboxes[0].Pop.Host = "127.0.0.1" },
		"folder":            func(c *api.Configuration) { c.Mailboxes[0].Folder = "Sent" },
		"generation":        func(c *api.Configuration) { c.Mailboxes[0].CredentialGeneration = 0 },
		"unknown-operation": func(c *api.Configuration) { c.Mailboxes[0].Policies[0].Operation = "raw" },
		"unknown-policy":    func(c *api.Configuration) { c.Mailboxes[0].Policies[0].Policy = "always" },
		"missing-policy":    func(c *api.Configuration) { c.Mailboxes[0].Policies = c.Mailboxes[0].Policies[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			c := configuration("implicit")
			mutate(&c)
			if api.ValidateConfiguration(c) == nil {
				t.Fatal("invalid transport or scope accepted")
			}
		})
	}
}

func TestTLSUpgradeRefusalHasNoPlaintextFallback(t *testing.T) {
	f := newFixture(t, "starttls")
	f.rejectUpgrade.Store(true)
	s, _, _ := service(t, f, "starttls", nil)
	if result := execute(t, s, send(api.OperationSend, "no-upgrade")); result.Status != "unknown" {
		t.Fatal("unexpected upgrade refusal receipt")
	}
	if _, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "token", api.Command{Operation: api.OperationFetch, MailboxId: "mailbox", Uid: "uid-one"}); err == nil {
		t.Fatal("POP downgrade accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != 0 || f.deletes != 0 || f.insecureAuth.Load() != 0 {
		t.Fatal("plaintext credential or mail effect")
	}
}
