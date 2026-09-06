package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

func TestIntegrationGatePreviewBoundsAndOpaqueFields(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"to": "resolver@example.invalid", "subject": "Проверить", "body_text": strings.Repeat("я", 5000), "attachments": "PRIVATE_ATTACHMENT_BYTES", "headers": "PRIVATE_HEADERS", "credential": "PRIVATE_CREDENTIAL"}
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	preview, err := integrationGatePreview(definitions, "email", "email.message.send", "email.message.send", raw, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(preview)
	for _, secret := range []string{"PRIVATE_ATTACHMENT_BYTES", "PRIVATE_HEADERS", "PRIVATE_CREDENTIAL"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("private field leaked")
		}
	}
	if preview["contentComplete"] != false {
		t.Fatal("partial content presented as complete")
	}
	fields := preview["fields"].([]any)
	var foundBody, foundRecipient bool
	for _, item := range fields {
		field := item.(map[string]any)
		if field["key"] == "body_text" {
			text := field["value"].(string)
			foundBody = true
			if len(text) > gatePreviewFieldBytes || !utf8.ValidString(text) || field["truncated"] != true {
				t.Fatal("invalid bounded text")
			}
		}
		if field["key"] == "to" {
			foundRecipient = field["value"] == "resolver@example.invalid"
		}
	}
	if !foundBody || !foundRecipient {
		t.Fatal("approval content missing")
	}
	if _, err := integrationGatePreview(definitions, "email", "email.message.send", "email.message.send", raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("input digest mismatch accepted")
	}
	unknown, err := integrationGatePreview(definitions, "email", "email.message.send", "unknown.operation", raw, hex.EncodeToString(sum[:]))
	if err != nil || len(unknown["fields"].([]any)) != 0 || unknown["contentComplete"] != false {
		t.Fatal("unknown operation expanded preview")
	}
}

func TestGateConsequencesDistinguishExecutionAndDelivery(t *testing.T) {
	decisions := []string{"APPROVE", "REJECT", "CANCEL", "REQUEST_CHANGES"}
	for _, kind := range []string{"ordinary", "integration", "delivery"} {
		t.Run(kind, func(t *testing.T) {
			rows := gateConsequences(decisions, kind == "integration", kind == "delivery", false)
			if len(rows) != 4 {
				t.Fatal("consequences incomplete")
			}
			if rows[0].ExecutesExternalEffect != (kind != "ordinary") || rows[1].TerminalForRun != (kind == "ordinary") || rows[2].TerminalForRun != (kind == "ordinary") || rows[3].ExecutesExternalEffect || rows[3].TerminalForRun {
				t.Fatal("incorrect decision effect")
			}
		})
	}
}
