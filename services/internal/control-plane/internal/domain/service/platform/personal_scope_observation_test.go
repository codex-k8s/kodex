package platform

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestPersonalScopeObservationPins verifies exact requested metadata, denial membership, immutable
// copies, expiry and lifecycle clearing; metadata never confirms a globally reusable WRITE.
// TestPersonalScopeObservationPins проверяет exact requested metadata, denial membership, immutable
// copies, expiry и lifecycle clearing; metadata не подтверждают globally reusable WRITE.
func TestPersonalScopeObservationPins(t *testing.T) {
	requirement, initiator, checking, snapshot, now := personalVerificationFixture(t)
	profile := integrationpackage.CredentialObservationGitLabProjectWriteScopeV1
	snapshot.AllowedObservationProfiles = []string{profile}
	snapshot.AllowedDeniedWriteCapabilityKeys = []string{"gitlab.issue.create", "gitlab.branch.create", "gitlab.merge_request.create"}
	for _, scenario := range []string{"checked", "known denied", "unknown", "duplicate profile", "unrequested profile", "unrequested denial", "denial without profile", "write confirmed", "expired", "auth rejected with profile"} {
		t.Run(scenario, func(t *testing.T) {
			expiry := now.Add(time.Hour)
			observation := entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{snapshot.HealthCapabilityKey}, ObservedPermissionProfiles: []string{profile}, ExpiresAt: &expiry}
			invalid := false
			switch scenario {
			case "known denied":
				observation.DeniedCapabilityKeys = []string{"gitlab.branch.create"}
			case "unknown":
				observation.ObservedPermissionProfiles, observation.ExpiresAt = nil, nil
			case "duplicate profile":
				observation.ObservedPermissionProfiles = []string{profile, profile}
				invalid = true
			case "unrequested profile":
				observation.ObservedPermissionProfiles = []string{"UNKNOWN"}
				invalid = true
			case "unrequested denial":
				observation.DeniedCapabilityKeys = []string{"gitlab.project.delete"}
				invalid = true
			case "denial without profile":
				observation.ObservedPermissionProfiles = nil
				observation.DeniedCapabilityKeys = []string{"gitlab.issue.create"}
				invalid = true
			case "write confirmed":
				observation.ConfirmedCapabilityKeys = append(observation.ConfirmedCapabilityKeys, "gitlab.issue.create")
				invalid = true
			case "expired":
				expiry = now.Add(-time.Second)
			case "auth rejected with profile":
				observation.Outcome = entity.PersonalIntegrationCredentialAuthenticationRejected
				observation.ConfirmedCapabilityKeys = nil
				invalid = true
			}
			ready, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation)
			if invalid {
				if !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("unrequested metadata accepted", err)
				}
				return
			}
			if err != nil || ready.State != "READY" || slices.Contains(ready.VerifiedCapabilities, "gitlab.issue.create") || len(ready.ObservedPermissionProfiles) != len(observation.ObservedPermissionProfiles) {
				t.Fatal("metadata result changed authority", err)
			}
			if len(observation.ObservedPermissionProfiles) > 0 {
				observation.ObservedPermissionProfiles[0] = "MUTATED"
				if ready.ObservedPermissionProfiles[0] != profile {
					t.Fatal("stored metadata retained mutable input")
				}
			}
			if scenario == "expired" {
				selection, e := ResolveIntegrationCredential(requirement, initiator, nil, &ready, now)
				if e != nil || selection.Problem != entity.IntegrationCredentialExpired {
					t.Fatal("observed expiry did not close launch", e)
				}
			}
			ready.CredentialPolicy = entity.IntegrationCredentialInitiatorRequired
			revoked, e := RevokePersonalIntegrationCredential(ready, initiator, ready.Version)
			if e != nil || len(revoked.ObservedPermissionProfiles) != 0 || revoked.ExpiresAt != nil {
				t.Fatal("revoke retained old metadata", e)
			}
		})
	}
}
