package revision

import (
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
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
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(definitions["github"])
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
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

func TestIntegrationRevisionRequiresExactRegisteredReadyPackage(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"github", "mattermost"} {
		definition := definitions[key]
		if key == "github" {
			definition.Metadata.Version = "99.0.0"
		} else {
			definition.Spec.Readiness = "NOT_READY"
		}
		raw, _ := json.Marshal(definition)
		if _, _, err := Validate(KindIntegrationDefinition, "JSON", string(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unregistered/unready definition accepted: %s", key)
		}
	}
}
