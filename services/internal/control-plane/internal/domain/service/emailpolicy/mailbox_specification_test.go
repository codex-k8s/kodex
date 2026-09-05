package emailpolicy

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func mailboxSpecificationFixture(t *testing.T) entity.EmailMailboxSpecification {
	t.Helper()
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration api.Configuration
	if err := api.Decode(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	m := configuration.Mailboxes[0]
	return entity.EmailMailboxSpecification{Enabled: m.Enabled, ReceiveProtocol: m.ReceiveProtocol,
		AllowedFolders: m.AllowedFolders, ArchiveFolder: m.ArchiveFolder, DraftsFolder: m.DraftsFolder,
		Folder: m.Folder, Sender: m.Sender, ReplyTo: m.ReplyTo, Recipients: m.Recipients,
		HelloName: m.HelloName, SMTP: m.Smtp, IMAP: m.Imap, POP: m.Pop, Limits: m.Limits, Policies: m.Policies}
}

func TestMailboxSpecificationOwnerBindingAndIsolation(t *testing.T) {
	spec := mailboxSpecificationFixture(t)
	spec.Policies[0].Folders = []string{"INBOX"}
	binding := MailboxBinding{Ref: "server-mailbox", OrganizationRef: "server-tenant", ConnectionRef: "server-connection", Revision: 7, CredentialGeneration: 11}
	mailbox, err := MaterializeMailbox(spec, binding)
	if err != nil {
		t.Fatal(err)
	}
	if mailbox.Id != binding.Ref || mailbox.TenantId != binding.OrganizationRef || mailbox.ConnectionId != binding.ConnectionRef || mailbox.Revision != 7 || mailbox.CredentialGeneration != 11 || mailbox.EnvelopeFrom != spec.Sender {
		t.Fatal("server-owned mailbox identity lost")
	}
	spec.IMAP.Host = "changed.example.test"
	spec.AllowedFolders[0] = "Changed"
	spec.Recipients[0] = "changed@example.test"
	spec.Policies[0].Folders[0] = "Changed"
	if mailbox.Imap.Host != "mail.example.test" || mailbox.AllowedFolders[0] != "INBOX" || mailbox.Recipients[0] != "recipient@example.test" || mailbox.Policies[0].Folders[0] != "INBOX" {
		t.Fatal("immutable mailbox aliases mutable input")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"tenant_id"`, `"connection_id"`, `"revision"`, `"credential_generation"`, `"managed_by"`, `"source"`, `"envelope_from"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("specification accepts owner field %s", forbidden)
		}
	}
}

func TestMailboxSpecificationDraftAndPublicationBounds(t *testing.T) {
	if err := BoundSpecification(entity.EmailMailboxSpecification{}); err != nil {
		t.Fatal("incomplete draft rejected", err)
	}
	binding := MailboxBinding{Ref: "server-mailbox", OrganizationRef: "server-tenant", ConnectionRef: "server-connection", Revision: 1, CredentialGeneration: 1}
	if _, err := MaterializeMailbox(entity.EmailMailboxSpecification{}, binding); err == nil {
		t.Fatal("incomplete publication accepted")
	}
	for _, mutate := range []func(*entity.EmailMailboxSpecification){
		func(s *entity.EmailMailboxSpecification) { s.SMTP.Port = 25 },
		func(s *entity.EmailMailboxSpecification) { s.SMTP.TlsMode = "implicit" },
		func(s *entity.EmailMailboxSpecification) { s.IMAP.Port = 465 },
		func(s *entity.EmailMailboxSpecification) { s.POP.TlsMode = "starttls" },
		func(s *entity.EmailMailboxSpecification) { s.SMTP.ServerName = "other.example.test" },
		func(s *entity.EmailMailboxSpecification) { s.Policies = s.Policies[:1] },
	} {
		spec := mailboxSpecificationFixture(t)
		mutate(&spec)
		if _, err := MaterializeMailbox(spec, binding); err == nil {
			t.Fatal("invalid publication accepted")
		}
	}
	spec := mailboxSpecificationFixture(t)
	for i := range spec.Policies {
		spec.Policies[i].Policy = "allow"
	}
	if _, err := MaterializeMailbox(spec, binding); err != nil {
		t.Fatal("explicit all-allow mailbox rejected", err)
	}
	spec.Sender = strings.Repeat("a", MaxMailboxSpecificationBytes)
	if BoundSpecification(spec) == nil {
		t.Fatal("oversized draft accepted")
	}
}

func TestTrustedMailboxConfigurationUsesExecutableNetworkMatrix(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration api.Configuration
	if err := api.Decode(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{25, 2525, 993} {
		configuration.Mailboxes[0].Smtp.Port = port
		encoded, err := json.Marshal(configuration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeConfiguration(encoded); err == nil {
			t.Fatalf("unexecutable SMTP port accepted: %d", port)
		}
	}
	configuration.Mailboxes[0].Smtp.Port = 465
	configuration.Mailboxes[0].Smtp.TlsMode = "implicit"
	encoded, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeConfiguration(encoded); err != nil {
		t.Fatal("supported implicit TLS rejected", err)
	}
}
