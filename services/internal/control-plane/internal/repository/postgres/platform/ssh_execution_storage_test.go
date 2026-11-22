package platform

import (
	"bytes"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestSSHExecutionStoredInputRespectsStrictContract reproduces PostgreSQL JSONB presentation without changing the public/native parser, then verifies the original owner digest through actual shipped capability validation.
// TestSSHExecutionStoredInputRespectsStrictContract воспроизводит PostgreSQL JSONB presentation без изменения public/native parser, затем проверяет original owner digest через actual shipped capability validation.
func TestSSHExecutionStoredInputRespectsStrictContract(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ operation, canonical, stored string }{
		{"ssh.service.restart", `{"resource":"service_synthetic"}`, `{"resource": "service_synthetic"}`},
		{"ssh.file.create", `{"content_base64":"dGVzdA==","path":"owned.txt","resource":"files_synthetic"}`, `{"path": "owned.txt", "resource": "files_synthetic", "content_base64": "dGVzdA=="}`},
	} {
		t.Run(test.operation, func(t *testing.T) {
			capability, exists := definitions[integrationpackage.SSHManagedDefinitionKey].Capability(test.operation)
			if !exists {
				t.Fatal("registered SSH capability is missing")
			}
			if _, err := capability.ValidateInput([]byte(test.stored)); err == nil {
				t.Fatal("public strict SSH input decoder unexpectedly changed")
			}
			stored, err := canonicalSSHStoredInput([]byte(test.stored))
			if err != nil {
				t.Fatal("decode authoritative JSONB presentation", err)
			}
			canonical, err := capability.ValidateInput(stored)
			if err != nil || !bytes.Equal(canonical, []byte(test.canonical)) || sshHealthHash(canonical) != sshHealthHash([]byte(test.canonical)) {
				t.Fatal("stored SSH input changed the original owner digest", err)
			}
		})
	}
}
