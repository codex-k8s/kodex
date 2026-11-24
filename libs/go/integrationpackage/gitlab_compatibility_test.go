package integrationpackage

import (
	"encoding/json"
	"reflect"
	"testing"
)

// legacyGitLabPackage supplies the immutable previous release to its dedicated regression tests, independently of the current shipped catalog.
// legacyGitLabPackage передаёт immutable previous release в его regression tests независимо от current shipped catalog.
func legacyGitLabPackage(t *testing.T) Package {
	t.Helper()
	previous, err := Parse(gitLabOneTwoOneCompatibility)
	if err != nil {
		t.Fatal("historical GitLab package unavailable", err)
	}
	return previous
}

// TestGitLabGroupPredecessorKeepsExactCapabilities pins the SQL-compatible projection of 2.1.0 onto the previous group contract.
// TestGitLabGroupPredecessorKeepsExactCapabilities закрепляет совместимую с SQL проекцию 2.1.0 на прежний групповой контракт.
func TestGitLabGroupPredecessorKeepsExactCapabilities(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["gitlab"]
	previous, ok := ResolveShippedRevision(current, "2.0.0", "ce63966754a8575e0139b7f2887a6d0e4a8516e7fcfc0e4c4d888f134c51499e")
	if !ok || ValidateExecutableRevision(previous, current) != nil {
		t.Fatal("previous scoped release lost its pinned execution")
	}
	const approvalRead = "gitlab.merge_request.approval.read"
	approval, ok := current.Capability(approvalRead)
	if !ok || approval.Risk != "READ" || current.Digest != "ce4be39772c9051cedf9ec4f5ed41d38d0ded0e5352befa3e79576558ba07bbb" {
		t.Fatal("current digest or approval-read classification changed")
	}
	retained := make([]Capability, 0, len(previous.Spec.Capabilities))
	for _, entry := range current.Spec.Capabilities {
		if entry.Key != approvalRead {
			retained = append(retained, entry)
		}
	}
	if !reflect.DeepEqual(retained, previous.Spec.Capabilities) {
		t.Fatal("removing the new read operation must reproduce the previous capabilities exactly")
	}
	if _, ok := ResolveShippedRevision(current, "2.0.0", current.Digest); ok {
		t.Fatal("previous version accepted successor authority")
	}
}

// TestGitLabHistoricalRevisionDoesNotInheritScopes preserves single-project pins and rejects synthetic old versions paired with new permission authority.
// TestGitLabHistoricalRevisionDoesNotInheritScopes сохраняет single-project pins и отклоняет synthetic old versions с новыми полномочиями.
func TestGitLabHistoricalRevisionDoesNotInheritScopes(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["gitlab"]
	previous, err := Parse(gitLabOneTwoOneCompatibility)
	if err != nil || current.Metadata.Version != "2.1.0" || previous.Metadata.Version != "1.2.1" {
		t.Fatal("GitLab exact compatibility fixture unavailable")
	}
	resolved, ok := ResolveShippedRevision(current, previous.Metadata.Version, previous.Digest)
	if !ok || resolved.Digest != previous.Digest || ValidateExecutableRevision(previous, current) != nil {
		t.Fatal("historical GitLab pin was not executable")
	}
	if _, exists := resolved.Capability("gitlab.project.list"); exists || resolved.ValidateConfiguration(map[string]string{"base_url": "https://example.invalid", "scope_kind": "INSTANCE"}) == nil {
		t.Fatal("historical pin inherited discovery or instance scope")
	}
	if _, ok := ResolveShippedRevision(current, previous.Metadata.Version, current.Digest); ok {
		t.Fatal("old version accepted the new contract digest")
	}
	previous.Metadata.Origin = OriginUI
	previous.Metadata.Version = "1.2.2"
	raw, _ := json.Marshal(previous)
	managed, err := Parse(raw)
	if err != nil || ValidateExecutableRevision(managed, current) != nil {
		t.Fatal("historical owner-managed single-project definition lost execution", err)
	}
	managed.Spec.Capabilities[0].AllowedApprovalPolicies = []string{"NONE", "HUMAN_EACH_EFFECT"}
	managed.Spec.Capabilities[0].ApprovalScope = &ApprovalScopeContract{Name: "project", RequiredInputPaths: []string{}}
	raw, _ = json.Marshal(managed)
	modified, err := Parse(raw)
	if err != nil || ValidateExecutableRevision(modified, current) == nil {
		t.Fatal("managed historical definition mixed new policy and old resource authority", err)
	}
}
