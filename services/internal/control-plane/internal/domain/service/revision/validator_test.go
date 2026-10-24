package revision

import (
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"testing"
)

// TestValidateRejectsUnknownFieldsInEveryStructuredFormat checks unknown structured revision
// fields fail with ErrInvalid for JSON, YAML and TOML rather than entering semantic processing.
// TestValidateRejectsUnknownFieldsInEveryStructuredFormat проверяет отказ неизвестным полям
// структурированной ревизии с ErrInvalid для JSON, YAML и TOML до семантической обработки.
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

// TestValidateRejectsMultipleYAMLDocuments checks a second otherwise recognizable YAML revision
// document is rejected instead of silently selecting the first document.
// TestValidateRejectsMultipleYAMLDocuments проверяет отказ второму распознаваемому YAML документу
// ревизии вместо молчаливого выбора первого документа.
func TestValidateRejectsMultipleYAMLDocuments(t *testing.T) {
	content := "name: Image\nbaseImage: image@sha256:abc\n---\nname: Other\nbaseImage: other\n"
	if _, _, err := Validate(KindRoleImage, "YAML", content); !errors.Is(err, ErrInvalid) {
		t.Fatalf("multiple YAML documents accepted: %v", err)
	}
}

// TestValidateTypedIntegrationRegistry checks a shipped ready integration package produces a valid
// digest and its server-validated stable definition key.
// TestValidateTypedIntegrationRegistry проверяет получение правильного digest и проверенного
// сервером устойчивого ключа определения для поставляемого готового integration package.
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

// TestValidateSTTContainsNoCredentialValue checks a reference-only STT revision is accepted while
// an added API-key field is rejected as unknown structured input.
// TestValidateSTTContainsNoCredentialValue проверяет допуск STT ревизии только со ссылкой и отказ
// добавленному полю API key как неизвестному структурированному вводу.
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

// TestIntegrationRevisionRequiresExactRegisteredReadyPackage checks a changed package version and
// a NOT_READY adapter revision both fail validation against the shipped executable registry.
// TestIntegrationRevisionRequiresExactRegisteredReadyPackage проверяет отказ изменённой версии
// package и ревизии adapter NOT_READY при проверке по поставляемому исполняемому реестру.
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
