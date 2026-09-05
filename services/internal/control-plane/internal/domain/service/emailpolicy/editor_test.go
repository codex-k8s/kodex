package emailpolicy

import "testing"

func TestMailboxEditorRejectsUnsafeDocuments(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":     "credentialValue: hidden\n",
		"owner field":       "tenantId: org_other\n",
		"duplicate key":     "sender: first\nsender: second\n",
		"alias":             "sender: &value first\nreplyTo: *value\n",
		"extra document":    "enabled: false\n---\nenabled: true\n",
		"non string key":    "1: value\n",
		"wrong scalar":      "enabled: text\n",
		"unknown protocol":  "receiveProtocol: ftp\n",
		"port narrowing":    "smtp: {port: 4294967296}\n",
		"limit narrowing":   "limits: {page_size: 4294967296}\n",
		"negative limit":    "limits: {scan_messages: -1}\n",
		"unsafe integer":    "limits: {message_bytes: 9007199254740992}\n",
		"unsafe generation": "smtp: {secret: {generation: 9007199254740992}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSpecification("YAML", content); err == nil {
				t.Fatal("unsafe mailbox document accepted")
			}
		})
	}
}

func TestMailboxEditorKeepsIncompleteDraft(t *testing.T) {
	if _, err := DecodeSpecification("JSON", `{"enabled":false,"enabled":true}`); err == nil {
		t.Fatal("duplicate JSON field accepted")
	}
	for _, format := range []string{"YAML", "JSON"} {
		spec, err := DecodeSpecification(format, "{}")
		if err != nil {
			t.Fatalf("incomplete %s draft rejected: %v", format, err)
		}
		if _, err := MaterializeMailbox(spec, MailboxBinding{}); err == nil {
			t.Fatal("incomplete draft became executable")
		}
		canonical, err := CanonicalYAML(spec)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeSpecification("YAML", canonical); err != nil {
			t.Fatalf("canonical draft cannot be reopened: %v", err)
		}
	}
}
