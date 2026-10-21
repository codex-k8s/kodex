package emailbridgeapi

import (
	"os"
	"testing"
)

// TestConfigurationExample verifies the checked-in mailbox example and
// independent descriptor rotation against the complete operation policy set.
// RU: TestConfigurationExample проверяет образец ящика и независимую ротацию
// дескрипторов при полном наборе политик операций.
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

// TestConfigurationSchema accepts supported JSON/YAML forms and rejects
// unknown fields, duplicate keys, aliases and unregistered schema versions.
// RU: TestConfigurationSchema принимает допустимые формы JSON/YAML и
// отклоняет неизвестные поля, повторные ключи, ссылки и чужие версии схемы.
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

// TestUnknownActionAndNestedFields keeps commands closed to registered
// operations and forbids unexpected nested message fields.
// RU: TestUnknownActionAndNestedFields допускает только зарегистрированные
// операции и запрещает неожиданные вложенные поля сообщения.
func TestUnknownActionAndNestedFields(t *testing.T) {
	for _, raw := range []string{`{"operation":"raw","mailbox_id":"box"}`, `{"operation":"send","mailbox_id":"box","message":{"from":"a@example.test","to":"b@example.test","subject":"s","body_text":"body","headers":{"Authorization":"bad"}}}`, `{"operation":"send","mailbox_id":"box","operation":"fetch"}`} {
		var c Command
		if Decode([]byte(raw), &c) == nil {
			t.Fatal("unknown action or field accepted")
		}
	}
}
