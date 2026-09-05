package emailbridgeapi

import "testing"

func TestIntegrationInputBoundary(t *testing.T) {
	for _, raw := range []string{`{}`, `{"message_id":"id","effect_key":"effect"}`} {
		if _, err := CommandForIntegration("email.message.status.read", "box", "sender@example.test", "unrelated", []byte(raw)); err == nil {
			t.Fatal("ambiguous receipt accepted")
		}
	}
	command, err := CommandForIntegration("email.message.status.read", "box", "sender@example.test", "unrelated", []byte(`{"effect_key":"original"}`))
	if err != nil || command.EffectKey != "original" || command.Operation != OperationReceipt {
		t.Fatal("receipt scope mismatch")
	}
	for _, raw := range []string{`{"cc":"[\"invalid\"]"}`, `{"attachments":"[{\"filename\":\"x\",\"content_type\":\"text/plain\",\"content_base64\":\"eA==\",\"url\":\"https://foreign.example.test\"}]"}`, `{"headers":{"Authorization":"bad"}}`} {
		if _, err := CommandForIntegration("email.message.send", "box", "sender@example.test", "effect", []byte(raw)); err == nil {
			t.Fatal("untyped input accepted")
		}
	}
	command, err = CommandForIntegration("email.message.send", "box", "sender@example.test", "effect", []byte(`{"cc":"[\"copy@example.test\"]","bcc":"[\"copy@example.test\"]"}`))
	if err != nil || len(command.Message.Cc) != 1 || len(command.Message.Bcc) != 1 {
		t.Fatal("equal recipient lists collapsed")
	}
}
