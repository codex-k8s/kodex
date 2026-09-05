package emailbridgeapi

import (
	"os"
	"testing"
)

func TestConfigurationExample(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration Configuration
	if err = Decode(raw, &configuration); err != nil {
		t.Fatal(err)
	}
	if err = ValidateConfiguration(configuration); err != nil {
		t.Fatal(err)
	}
	if len(configuration.Mailboxes) != 1 || len(configuration.Mailboxes[0].Policies) != len(Operations()) || configuration.Mailboxes[0].Imap == nil {
		t.Fatal("incomplete mailbox example")
	}
	configuration.Mailboxes[0].Smtp.Secret.Generation = 2
	configuration.Mailboxes[0].Imap.Secret.Generation = 3
	configuration.Mailboxes[0].Imap.Ca.Generation = 4
	if err = ValidateConfiguration(configuration); err != nil {
		t.Fatal("independent descriptor rotation rejected")
	}
}

func TestConfigurationSchema(t *testing.T) {
	for _, raw := range []string{`{"version":"email-bridge/v1","revision":1,"managed_by":"git","source":"fixture","mailboxes":[]}`, "version: email-bridge/v1\nrevision: 1\nmanaged_by: ui\nsource: fixture\nmailboxes: []\n"} {
		var c Configuration
		if e := Decode([]byte(raw), &c); e != nil {
			t.Fatal(e)
		}
		if e := ValidateConfiguration(c); e != nil {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{`{"version":"email-bridge/v1","revision":1,"managed_by":"git","source":"fixture","mailboxes":[],"password":"not-allowed"}`, `{"version":"email-bridge/v1","revision":1,"revision":2,"managed_by":"git","source":"fixture","mailboxes":[]}`, `{"version":"email-bridge/v1","revision":1,"managed_by":"git","mailboxes":[]}`, "version: &v email-bridge/v1\nsource: *v\n", `{"version":"email-bridge/v2","revision":1,"managed_by":"git","source":"fixture","mailboxes":[]}`} {
		var c Configuration
		if e := Decode([]byte(raw), &c); e == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
func TestUnknownActionAndNestedFields(t *testing.T) {
	for _, raw := range []string{`{"operation":"raw","mailbox_id":"box"}`, `{"operation":"send","mailbox_id":"box","message":{"from":"a@example.test","to":"b@example.test","subject":"s","body_text":"body","headers":{"Authorization":"bad"}}}`, `{"operation":"send","mailbox_id":"box","operation":"fetch"}`} {
		var c Command
		if Decode([]byte(raw), &c) == nil {
			t.Fatal("unknown action or field accepted")
		}
	}
}
