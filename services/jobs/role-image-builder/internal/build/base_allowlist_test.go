package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBaseAllowlistAcceptsOnlyExactCatalogDigests(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	path := filepath.Join(t.TempDir(), "catalog.json")
	document := `{"schemaVersion":1,"context":{},"environments":[` +
		`{"key":"standard","nameMessageKey":"role-environments.standard.name",` +
		`"descriptionMessageKey":"role-environments.standard.description","unavailableMessageKey":"",` +
		`"softwareMessageKeys":[],"recommended":true,"available":true,` +
		`"customInstallationAllowed":false,"baseImageReference":"registry.example/mattercodex/agent-runner",` +
		`"baseImageDigest":"` + digestA + `","platforms":[],"packages":[],"tools":[]},` +
		`{"key":"documents","nameMessageKey":"role-environments.documents.name",` +
		`"descriptionMessageKey":"role-environments.documents.description","unavailableMessageKey":"",` +
		`"softwareMessageKeys":[],"recommended":false,"available":true,` +
		`"customInstallationAllowed":false,"baseImageReference":"registry.example/mattercodex/role-base-documents",` +
		`"baseImageDigest":"` + digestB + `","platforms":[],"packages":[],"tools":[]}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	allowlist, err := loadBaseAllowlist(path)
	if err != nil {
		t.Fatalf("load exact catalog: %v", err)
	}
	if !allowlist.Allows("registry.example/mattercodex/agent-runner", digestA) ||
		!allowlist.Allows("registry.example/mattercodex/role-base-documents", digestB) ||
		allowlist.Allows("registry.example/mattercodex/role-base-documents", digestA) {
		t.Fatal("base allowlist did not preserve the exact repository and digest tuple")
	}
}

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
