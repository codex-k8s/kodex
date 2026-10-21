package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadBaseAllowlistAcceptsOnlyExactCatalogDigests loads two local bases and rejects a cross-repository digest match.
// The fixture verifies exact tuple membership without contacting either registry.
// TestLoadBaseAllowlistAcceptsOnlyExactCatalogDigests загружает две локальные базы и отклоняет дайджест от другого реестрового пути.
// Фикстура проверяет принадлежность точной пары без обращения к реестрам.
func TestLoadBaseAllowlistAcceptsOnlyExactCatalogDigests(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	path := filepath.Join(t.TempDir(), "catalog.json")
	document := `{"schemaVersion":1,"context":{},"environments":[` +
		`{"key":"standard","nameMessageKey":"role-environments.standard.name",` +
		`"descriptionMessageKey":"role-environments.standard.description","unavailableMessageKey":"",` +
		`"softwareMessageKeys":[],"recommended":true,"available":true,` +
		`"customInstallationAllowed":false,"baseImageReference":"registry.example/kodex/agent-runner",` +
		`"baseImageDigest":"` + digestA + `","platforms":[],"packages":[],"tools":[]},` +
		`{"key":"documents","nameMessageKey":"role-environments.documents.name",` +
		`"descriptionMessageKey":"role-environments.documents.description","unavailableMessageKey":"",` +
		`"softwareMessageKeys":[],"recommended":false,"available":true,` +
		`"customInstallationAllowed":false,"baseImageReference":"registry.example/kodex/role-base-documents",` +
		`"baseImageDigest":"` + digestB + `","platforms":[],"packages":[],"tools":[]}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	allowlist, err := loadBaseAllowlist(path)
	if err != nil {
		t.Fatalf("load exact catalog: %v", err)
	}
	if !allowlist.Allows("registry.example/kodex/agent-runner", digestA) ||
		!allowlist.Allows("registry.example/kodex/role-base-documents", digestB) ||
		allowlist.Allows("registry.example/kodex/role-base-documents", digestA) {
		t.Fatal("base allowlist did not preserve the exact repository and digest tuple")
	}
}

// TestLoadBaseAllowlistRejectsUnknownFieldsAndDuplicateBase checks rejection of an unknown field and repeated base tuple.
// Different catalog keys must not make the same reference/digest pair acceptable twice.
// TestLoadBaseAllowlistRejectsUnknownFieldsAndDuplicateBase проверяет отклонение неизвестного поля и повторной пары базы.
// Разные ключи каталога не должны позволять дважды принять одну пару ссылки и дайджеста.
func TestLoadBaseAllowlistRejectsUnknownFieldsAndDuplicateBase(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for name, document := range map[string]string{
		"unknown": `{"schemaVersion":1,"context":{},"unexpected":true,"environments":[]}`,
		"duplicate": `{"schemaVersion":1,"context":{},"environments":[` +
			`{"key":"a","nameMessageKey":"a.name","descriptionMessageKey":"a.description",` +
			`"unavailableMessageKey":"","softwareMessageKeys":[],"recommended":true,"available":true,` +
			`"customInstallationAllowed":false,"baseImageReference":"registry.example/base",` +
			`"baseImageDigest":"` + digest + `","platforms":[],"packages":[],"tools":[]},` +
			`{"key":"b","nameMessageKey":"b.name","descriptionMessageKey":"b.description",` +
			`"unavailableMessageKey":"","softwareMessageKeys":[],"recommended":false,"available":true,` +
			`"customInstallationAllowed":false,"baseImageReference":"registry.example/base",` +
			`"baseImageDigest":"` + digest + `","platforms":[],"packages":[],"tools":[]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.json")
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadBaseAllowlist(path); err == nil {
				t.Fatal("invalid base catalog was accepted")
			}
		})
	}
}
