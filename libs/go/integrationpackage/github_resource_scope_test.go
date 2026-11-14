package integrationpackage

import (
	"reflect"
	"testing"
)

// TestGitHubScopeRequiresExplicitSelection checks closed modes, immutable coordinate pairs, finite selections and malformed or ambiguous names.
// TestGitHubScopeRequiresExplicitSelection проверяет закрытые режимы, пары неизменяемых координат, конечный выбор и неверные либо неоднозначные имена.
func TestGitHubScopeRequiresExplicitSelection(t *testing.T) {
	for _, configuration := range []map[string]string{{}, {"scope_kind": ""}, {"scope_kind": "INSTANCE"}, {"scope_kind": "ALL_ACCESSIBLE", "repositories": "[]"}, {"scope_kind": "SELECTED", "repositories": "[]"}, {"scope_kind": "SELECTED", "repositories": `[{"id":1,"full_name":"team/repo","token":"secret"}]`}, {"scope_kind": "SELECTED", "repositories": `[{"id":1,"full_name":"team/repo"},{"id":1,"full_name":"team/other"}]`}, {"scope_kind": "SELECTED", "repositories": `[{"id":1,"full_name":"team/repo"},{"id":2,"full_name":"TEAM/REPO"}]`}, {"scope_kind": "SELECTED", "repositories": `[{"id":1,"full_name":"team/../repo"}]`}, {"scope_kind": "SELECTED", "repositories": `[{"id":9007199254740992,"full_name":"team/repo"}]`}} {
		if _, err := ParseGitHubResourceScope(configuration); err == nil {
			t.Fatalf("invalid selection accepted: %#v", configuration)
		}
	}
	scope, err := ParseGitHubResourceScope(map[string]string{"scope_kind": "SELECTED", "repositories": `[{"id":17,"full_name":"team/repo"},{"id":18,"full_name":"person/private"}]`})
	if err != nil {
		t.Fatal(err)
	}
	if !scope.AllowsRepository(17, "team/repo") || !scope.AllowsRepository(18, "person/private") || scope.AllowsRepository(18, "team/repo") || scope.AllowsRepository(17, "team/renamed") || scope.AllowsRepository(19, "other/repo") {
		t.Fatal("selection lost exact ID/name boundary")
	}
	all, err := ParseGitHubResourceScope(map[string]string{"scope_kind": "ALL_ACCESSIBLE"})
	if err != nil || !all.AllowsRepository(17, "team/repo") || all.AllowsRepository(0, "team/repo") {
		t.Fatal("explicit all scope malformed")
	}
}

// TestGitHubScopePackagePinsRepositoryAndApproval requires typed repository selectors for every resource operation and preserves the each-effect merge/bundle boundary.
// TestGitHubScopePackagePinsRepositoryAndApproval требует typed repository selectors для каждой resource operation и сохраняет each-effect границу merge/bundle.
func TestGitHubScopePackagePinsRepositoryAndApproval(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["github"]
	if definition.Metadata.Version != "6.5.0" || len(definition.Spec.Capabilities) != 49 {
		t.Fatal("GitHub successor package incomplete")
	}
	for _, capability := range definition.Spec.Capabilities {
		if capability.ResourceScope.Kind != "GITHUB_SCOPE" {
			t.Fatal("legacy scope leaked into successor")
		}
		if capability.Key == "github.identity.read" || capability.Key == "github.repository.list" {
			continue
		}
		if len(capability.InputFields) < 2 || capability.InputFields[0].Key != "repository_full_name" || !capability.InputFields[0].Required || capability.InputFields[1].Key != "repository_id" || !capability.InputFields[1].Required {
			t.Fatalf("missing repository selector: %s", capability.Key)
		}
		profile := CredentialResourcePermissionProfile(definition, capability)
		contract, exists := CredentialResourcePermissionContractForProfile("github", capability.Key, profile)
		if !exists || profile != CredentialResourcePermissionGitHubScopedV1 || contract.ResourceKind != "GITHUB_SCOPE" || len(contract.ReadCapabilities) != 1 || contract.ReadCapabilities[0] != "github.identity.read" {
			t.Fatalf("missing per-call consumer: %s", capability.Key)
		}
		if capability.Key == "github.pull_request.merge" || capability.Key == "github.repository.bundle.publish" {
			if capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" || capability.AllowsApprovalPolicy("HUMAN_SCOPED") || capability.AllowsApprovalPolicy("NONE") {
				t.Fatal("merge/bundle approval weakened")
			}
		}
	}
	historical, ok := trustedHistoricalPolicyPackage(definition, "6.4.0")
	if !ok || historical.Digest != "bd6d504e4fc1d19d5c083f8585115c04ec75852b7879ad19ed54dc41cbe9a1da" || historical.Spec.Capabilities[0].ResourceScope.Kind != "GITHUB_REPOSITORY" {
		t.Fatal("historical6.4 package changed")
	}
	for _, old := range historical.Spec.Capabilities {
		current, exists := definition.Capability(old.Key)
		if !exists || !reflect.DeepEqual(current.InputFields[2:], old.InputFields) || !reflect.DeepEqual(current.OutputFields, old.OutputFields) || !reflect.DeepEqual(current.Execution, old.Execution) {
			t.Fatalf("resource successor changed native operation protocol: %s", old.Key)
		}
	}

}

// TestGitHubDiscoveryRejectsMalformedPages keeps untrusted provider fields and missing identity out of safe self-service observations.
// TestGitHubDiscoveryRejectsMalformedPages исключает недоверенные provider fields и отсутствующую identity из безопасных собственных наблюдений.
func TestGitHubDiscoveryRejectsMalformedPages(t *testing.T) {
	valid := `{"identity_id":7,"login":"synthetic","page":1,"next_page":2,"items":[{"id":17,"full_name":"team/repo","owner_kind":"Organization","private":true,"archived":false,"can_read":true,"can_push":true}]}`
	page, err := ParseGitHubDiscoveryPage([]byte(valid))
	if err != nil || page == nil || len(page.Items) != 1 {
		t.Fatal("valid safe page rejected")
	}
	for _, raw := range []string{`{}`, valid + `{}`, `{"identity_id":7,"login":"synthetic","page":1,"next_page":3,"items":[]}`, `{"identity_id":7,"login":"synthetic","page":1,"next_page":0,"items":[],"token":"secret"}`} {
		if _, err := ParseGitHubDiscoveryPage([]byte(raw)); err == nil {
			t.Fatal("malformed discovery page accepted")
		}
	}
}

// TestNativeGitHubScopePreservesCanonicalCase resolves normalized stored source coordinates to the unique server-observed canonical repository while keeping actual invocation selectors exact.
// TestNativeGitHubScopePreservesCanonicalCase разрешает нормализованные source coordinates в уникальный server-observed canonical repository, сохраняя точные invocation selectors.
func TestNativeGitHubScopePreservesCanonicalCase(t *testing.T) {
	configuration := map[string]string{"scope_kind": "SELECTED", "repositories": "[{\"id\":7,\"full_name\":\"Owner/MyRepo\"}]"}
	selected, err := NativeGitHubRepository(configuration, "owner/myrepo")
	if err != nil || selected.FullName != "Owner/MyRepo" || selected.ID != 7 {
		t.Fatal("native source lost canonical repository coordinates", err)
	}
	scope, err := ParseGitHubResourceScope(configuration)
	if err != nil || scope.AllowsRepository(7, "owner/myrepo") {
		t.Fatal("native name normalization widened invocation selector authority", err)
	}
}

// TestGitHubScopeRejectsAmbiguousNestedJSON refuses duplicate or case-aliased selection keys before typed decoding can apply a last-value rule.
// TestGitHubScopeRejectsAmbiguousNestedJSON отклоняет повторные либо case-aliased ключи выбора до last-value правила typed decode.
func TestGitHubScopeRejectsAmbiguousNestedJSON(t *testing.T) {
	for _, raw := range []string{`[{"id":1,"id":2,"full_name":"owner/repo"}]`, `[{"id":1,"full_name":"owner/repo","full_name":"other/repo"}]`, `[{"id":1,"ID":2,"full_name":"owner/repo"}]`, `[{"ID":1,"full_name":"owner/repo"}]`, `[{"id":1,"Full_Name":"owner/repo"}]`} {
		if _, err := ParseGitHubResourceScope(map[string]string{"scope_kind": "SELECTED", "repositories": raw}); err == nil {
			t.Fatal("ambiguous nested repository JSON was accepted")
		}
	}
}
