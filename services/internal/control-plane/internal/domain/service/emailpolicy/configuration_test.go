package emailpolicy

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

func TestConfigurationProjectionPreservesPolicyWithoutCredentials(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	projection, err := DecodeConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Revision != 1 || !ValidDigest(projection.Digest) || len(projection.Mailboxes) != 1 {
		t.Fatal("configuration identity missing")
	}
	mailbox := projection.Mailboxes[0]
	if mailbox.CredentialGeneration != 1 || mailbox.Revision != 1 || mailbox.Sender != "sender@example.test" || len(mailbox.Policies) != len(api.Operations()) {
		t.Fatal("mailbox policy missing")
	}
	encoded, err := json.Marshal(mailbox)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password", "username", "smtp", "server_name", "mail.example.test"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("projection leaked %s", forbidden)
		}
	}
	var document api.Configuration
	if err := api.Decode(raw, &document); err != nil {
		t.Fatal(err)
	}
	document.Mailboxes[0].Smtp.Secret.Generation++
	changed, _ := json.Marshal(document)
	next, err := DecodeConfiguration(changed)
	if err != nil {
		t.Fatal(err)
	}
	if next.Digest == projection.Digest || next.Mailboxes[0].SourceDigest == mailbox.SourceDigest {
		t.Fatal("credential descriptor change lost commitment")
	}
	if next.Mailboxes[0].CredentialGeneration != mailbox.CredentialGeneration {
		t.Fatal("descriptor generation replaced connection binding generation")
	}
	if _, err := DecodeConfiguration(append(raw, []byte("\nunknown: true\n")...)); err == nil {
		t.Fatal("unknown configuration field accepted")
	}
}
