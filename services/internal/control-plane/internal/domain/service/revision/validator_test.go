package revision

import (
	"errors"
	"testing"
)

func TestValidateRejectsUnknownFieldsInEveryStructuredFormat(t *testing.T) {
	tests := []struct{ format, content string }{
		{"JSON", `{"name":"STT","unknown":true,"stt":{"providerAccountRef":"pacc_example","model":"whisper","language":"ru","permissionKey":"platform.stt.use"}}`},
		{"YAML", "name: Image\nbaseImage: registry/image@sha256:abc\nunknown: true\n"},
		{"TOML", "name='Image'\nbaseImage='registry/image@sha256:abc'\nunknown=true\n"},
	}
	for _, test := range tests {
		if _, _, err := Validate(KindRoleImage, test.format, test.content); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unknown %s field accepted: %v", test.format, err)
		}
	}
}

func TestValidateRejectsMultipleYAMLDocuments(t *testing.T) {
	content := "name: Image\nbaseImage: image@sha256:abc\n---\nname: Other\nbaseImage: other\n"
	if _, _, err := Validate(KindRoleImage, "YAML", content); !errors.Is(err, ErrInvalid) {
		t.Fatalf("multiple YAML documents accepted: %v", err)
	}
}

func TestValidateTypedIntegrationRegistry(t *testing.T) {
	content := `{"name":"GitHub","definition":{"key":"github","version":"1.0.0","adapter":"github","operations":[{"key":"issues.create","operation":"CREATE_ISSUE","risk":"WRITE","approval":"HUMAN_EACH_EFFECT","resourceKind":"GITHUB_REPOSITORY"}]}}`
	digest, diagnostics, err := Validate(KindIntegrationDefinition, "JSON", content)
	if err != nil || len(diagnostics) != 0 || len(digest) != 64 {
		t.Fatalf("typed integration definition rejected: digest=%q diagnostics=%#v err=%v", digest, diagnostics, err)
	}
	key, err := IntegrationDefinitionKey("JSON", content)
	if err != nil || key != "github" {
		t.Fatalf("integration definition key = %q, err=%v", key, err)
	}
}

func TestValidateSTTContainsNoCredentialValue(t *testing.T) {
	valid := `{"name":"System STT","stt":{"providerAccountRef":"pacc_example","model":"whisper-1","language":"ru","permissionKey":"platform.stt.use"}}`
	if _, _, err := Validate(KindSystemSTT, "JSON", valid); err != nil {
		t.Fatalf("valid STT rejected: %v", err)
	}
	invalid := `{"name":"System STT","stt":{"providerAccountRef":"pacc_example","model":"whisper-1","language":"ru","permissionKey":"platform.stt.use","apiKey":"secret"}}`
	if _, _, err := Validate(KindSystemSTT, "JSON", invalid); !errors.Is(err, ErrInvalid) {
		t.Fatal("credential field was accepted")
	}
}
