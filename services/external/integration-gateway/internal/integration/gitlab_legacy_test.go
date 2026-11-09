package integration

import (
	"os"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// legacyGitLabTestAdapter runs the original single-project catalog scenarios against their exact frozen contract; scoped successor tests use testAdapter and the current catalog explicitly.
// legacyGitLabTestAdapter выполняет прежние single-project catalog scenarios по exact frozen contract; scoped successor tests явно используют testAdapter и current catalog.
func legacyGitLabTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	adapter := testAdapter(t)
	definitions := make(map[string]integrationpackage.Package, len(adapter.definitions))
	for key, value := range adapter.definitions {
		definitions[key] = value
	}
	raw, err := os.ReadFile("../../../../../libs/go/integrationpackage/compatibility/gitlab-1.2.1.yaml")
	if err != nil {
		t.Fatal("read frozen single-project contract", err)
	}
	previous, err := integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal("parse frozen single-project contract", err)
	}
	current := adapter.definitions["gitlab"]
	if _, exists := integrationpackage.ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest); !exists {
		t.Fatal("frozen single-project contract lost exact executable compatibility")
	}
	definitions["gitlab"] = previous
	adapter.definitions = definitions
	return adapter
}
