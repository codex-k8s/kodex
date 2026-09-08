package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

func legacyEmailRequest(t *testing.T, adapter *Adapter) Request {
	t.Helper()
	request := invocationRequest(t, adapter.definitions["email"], "email.message.send", map[string]any{"to": "recipient@example.test", "subject": "Fixture", "body_text": "Bounded fixture"}, nil)
	legacy, ok := integrationpackage.ResolveShippedRevision(adapter.definitions["email"], "1.4.0", "df52f45643b6e4464cf20901b6c069b88dac671303dc31e04f23b3d1ad4006fd")
	if !ok {
		t.Fatal("legacy package missing")
	}
	request.DefinitionPackage, _ = json.Marshal(legacy)
	request.DefinitionVersion = legacy.Metadata.Version
	request.DefinitionDigest = legacy.Digest
	request.Credential = &CredentialRevision{Ref: "icred_fixture", Revision: 1, SecretRef: exactCredentialSecretPrefix + "email-legacy", SecretUID: "60000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}
	return request
}
func TestLegacyEmailMetadataIsDiscardedAfterExactClaimValidation(t *testing.T) {
	adapter := testAdapter(t)
	calls := 0
	emailFixture(t, adapter, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.Contains(r.Header.Get("Authorization"), "email-legacy") {
			t.Fatal("generic metadata reached bridge")
		}
		_, _ = io.WriteString(w, `{"status":"accepted","message_id":"fixture"}`)
	})
	request := legacyEmailRequest(t, adapter)
	if _, err := adapter.Execute(t.Context(), request); err != nil || calls != 1 {
		t.Fatalf("legacy request rejected: %v calls=%d", err, calls)
	}
	for _, origin := range []string{integrationpackage.OriginUI, integrationpackage.OriginGit} {
		managed := legacyEmailRequest(t, adapter)
		definition, _ := integrationpackage.Parse(managed.DefinitionPackage)
		definition.Metadata.Origin = origin
		definition.Spec.HealthCheck.TimeoutSeconds--
		managed.DefinitionPackage, _ = json.Marshal(definition)
		definition, _ = integrationpackage.Parse(managed.DefinitionPackage)
		managed.DefinitionDigest = definition.Digest
		before := calls
		if _, err := adapter.Execute(t.Context(), managed); err != nil || calls != before+1 {
			t.Fatalf("managed legacy rejected: %s %v", origin, err)
		}
		for _, altered := range []string{"descriptor", "budget"} {
			badDefinition, _ := integrationpackage.Parse(managed.DefinitionPackage)
			if altered == "descriptor" {
				badDefinition.Spec.Credential.SecretKey = "foreign"
			} else {
				badDefinition.Spec.HealthCheck.TimeoutSeconds = 600
			}
			bad := managed
			bad.DefinitionPackage, _ = json.Marshal(badDefinition)
			badDefinition, _ = integrationpackage.Parse(bad.DefinitionPackage)
			bad.DefinitionDigest = badDefinition.Digest
			if _, err := adapter.Execute(t.Context(), bad); err == nil || calls != before+1 {
				t.Fatalf("managed expansion accepted: %s %s", origin, altered)
			}
		}
	}
	expectedCalls := calls
	for _, kind := range []string{"new-package", "digest", "descriptor", "revision", "secret-ref", "content-digest", "scope", "missing-lease", "expired"} {
		bad := legacyEmailRequest(t, adapter)
		switch kind {
		case "new-package":
			current := adapter.definitions["email"]
			bad.DefinitionPackage, _ = json.Marshal(current)
			bad.DefinitionVersion = current.Metadata.Version
			bad.DefinitionDigest = current.Digest
		case "digest":
			bad.DefinitionDigest = strings.Repeat("0", 64)
		case "descriptor":
			definition, _ := integrationpackage.Parse(bad.DefinitionPackage)
			definition.Spec.Credential.SecretKey = "other"
			bad.DefinitionPackage, _ = json.Marshal(definition)
			definition, _ = integrationpackage.Parse(bad.DefinitionPackage)
			bad.DefinitionDigest = definition.Digest
		case "revision":
			bad.Credential.Revision = 0
		case "secret-ref":
			bad.Credential.SecretRef = exactCredentialSecretPrefix + "../escape"
		case "content-digest":
			bad.Credential.ContentSHA256 = strings.Repeat("z", 64)
		case "scope":
			bad.ResourceScopeDigest = strings.Repeat("0", 64)
		case "missing-lease":
			bad.EmailExecution = nil
		case "expired":
			bad.EmailExecution.Lease.ExpiresAt = time.Now().Add(-time.Second)
		}
		if _, err := adapter.Execute(t.Context(), bad); err == nil || calls != expectedCalls {
			t.Fatalf("invalid %s reached bridge", kind)
		}
	}
}
