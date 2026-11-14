package platform

import (
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

// TestPersonalGitHubDiscoveryIsFencedAndCleared proves an exact requested page is immutable, cannot inflate write authority, and disappears on revoke or rotation.
// TestPersonalGitHubDiscoveryIsFencedAndCleared проверяет неизменность точной запрошенной страницы, отсутствие новых прав записи и очистку при отзыве либо ротации.
func TestPersonalGitHubDiscoveryIsFencedAndCleared(t *testing.T) {
	requirement, initiator, binding, now := personalCredentialFixture()
	_ = now
	binding.CredentialPolicy = entity.IntegrationCredentialInitiatorRequired
	binding.ConnectionVersion = 5
	spec := entity.PersonalIntegrationCredentialTestSpec{Ref: "pct_github_fixture", ConnectionVersion: 5, DefinitionDigest: requirement.DefinitionDigest, ConfigurationDigest: requirement.ConfigurationDigest, HealthCapabilityKey: "github.identity.read", AllowedReadCapabilityKeys: []string{"github.identity.read"}, GitHubDiscoveryPage: 2}
	checking, snapshot, err := BeginPersonalIntegrationCredentialVerification(binding, initiator, binding.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	page := &integrationpackage.GitHubDiscoveryPage{IdentityID: 7, Login: "synthetic", Page: 2, Items: []integrationpackage.GitHubDiscoveredRepository{{ID: 17, FullName: "team/repo", OwnerKind: "Organization", CanRead: true, CanPush: true}}}
	observation := entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"github.identity.read"}, GitHubDiscovery: page}
	ready, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation)
	if err != nil || ready.GitHubDiscovery == nil || ready.VerifiedCapabilities[0] != "github.identity.read" || len(ready.VerifiedCapabilities) != 1 {
		t.Fatal("safe discovery failed or became write authority", err)
	}
	page.Items[0].FullName = "other/repo"
	if ready.GitHubDiscovery.Items[0].FullName != "team/repo" {
		t.Fatal("stored discovery retained mutable worker memory")
	}
	for _, change := range []string{"page", "generation", "owner", "configuration"} {
		changed := checking
		different := snapshot
		observed := observation
		switch change {
		case "page":
			wrong := *page
			wrong.Page = 3
			observed.GitHubDiscovery = &wrong
		case "generation":
			changed.Generation++
		case "owner":
			different.SubjectID = "another-user"
		case "configuration":
			different.ConfigurationDigest = "changed"
		}
		if _, err := CompletePersonalIntegrationCredentialVerification(changed, different, requirement.DefinitionDigest, requirement.ConfigurationDigest, observed); err == nil {
			t.Fatalf("stale %s discovery accepted", change)
		}
	}
	revoked, err := RevokePersonalIntegrationCredential(ready, initiator, ready.Version)
	if err != nil || revoked.GitHubDiscovery != nil {
		t.Fatal("revoke retained discovery", err)
	}
	replacement := *ready.Credential
	replacement.Ref = "ipcr_replacement"
	replacement.Revision++
	rotated, err := ReplacePersonalIntegrationCredential(ready, initiator, ready.Version, replacement)
	if err != nil || rotated.GitHubDiscovery != nil {
		t.Fatal("rotation retained discovery", err)
	}
}
