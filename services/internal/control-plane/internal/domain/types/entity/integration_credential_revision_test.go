package entity

import (
	"strings"
	"testing"
)

// TestIntegrationCredentialRevisionDescriptor rejects arbitrary Secret paths and malformed
// immutable metadata using the same guard for new shared and stored personal revisions.
// TestIntegrationCredentialRevisionDescriptor отклоняет произвольные Secret paths и некорректные
// immutable metadata общим guard новых общих и сохранённых личных ревизий.
func TestIntegrationCredentialRevisionDescriptor(t *testing.T) {
	t.Parallel()
	valid := IntegrationCredentialRevision{
		SecretRef: "kodex-system/kodex-integration-credentials#integration-fixture",
		SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "4", ContentSHA256: strings.Repeat("a", 64),
	}
	if !valid.ValidDescriptor() {
		t.Fatal("exact integration descriptor was rejected")
	}
	for _, change := range []func(*IntegrationCredentialRevision){
		func(c *IntegrationCredentialRevision) {
			c.SecretRef = "other/kodex-integration-credentials#integration-fixture"
		},
		func(c *IntegrationCredentialRevision) { c.SecretRef += "/foreign" },
		func(c *IntegrationCredentialRevision) { c.SecretRef += " space" },
		func(c *IntegrationCredentialRevision) { c.SecretRef += "." },
		func(c *IntegrationCredentialRevision) { c.SecretUID = "not-a-uuid" },
		func(c *IntegrationCredentialRevision) { c.SecretResourceVersion = "" },
		func(c *IntegrationCredentialRevision) { c.ContentSHA256 = strings.Repeat("A", 64) },
	} {
		invalid := valid
		change(&invalid)
		if invalid.ValidDescriptor() {
			t.Fatal("malformed integration descriptor was accepted")
		}
	}
}
