package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// personalVerificationFixture starts only a synthetic, owner-qualified verification without any
// external request, lease authority or production persistence; the worker path is qualified separately.
// personalVerificationFixture запускает только synthetic owner-qualified проверку без внешнего
// запроса, lease authority и production persistence; worker path квалифицируется отдельно.
func personalVerificationFixture(t *testing.T) (entity.IntegrationCredentialRequirement, entity.IntegrationCredentialInitiator, entity.PersonalIntegrationCredential, entity.PersonalIntegrationCredentialTestSnapshot, time.Time) {
	t.Helper()
	requirement, initiator, original, now := personalCredentialFixture()
	original.CredentialPolicy, original.ConnectionVersion = entity.IntegrationCredentialInitiatorRequired, 5
	spec := entity.PersonalIntegrationCredentialTestSpec{Ref: "pct_fixture", ConnectionVersion: original.ConnectionVersion,
		DefinitionDigest: requirement.DefinitionDigest, ConfigurationDigest: requirement.ConfigurationDigest,
		HealthCapabilityKey: "fixture.read", AllowedReadCapabilityKeys: []string{"fixture.read", "fixture.extra"}}
	binding, snapshot, err := BeginPersonalIntegrationCredentialVerification(original, initiator, original.Version, spec)
	if err != nil || binding.State != "CHECKING" || binding.Version != original.Version+1 || binding.Generation != original.Generation ||
		binding.Credential.Ref != original.Credential.Ref || binding.VerifiedCredentialRef != "" || len(binding.VerifiedCapabilities) != 0 ||
		binding.ExpiresAt != nil || original.VerifiedCredentialRef == "" {
		t.Fatal("verification did not invalidate prior proof with exact current pins", err)
	}
	spec.AllowedReadCapabilityKeys[0] = "changed.read"
	if snapshot.AllowedReadCapabilityKeys[0] != "fixture.read" {
		t.Fatal("test snapshot retained a mutable caller capability slice")
	}
	return requirement, initiator, binding, snapshot, now
}

// TestPersonalVerificationDoesNotInflatePermissions distinguishes exact approved observations,
// unknown write permissions, known denial, auth rejection, unavailable verification and known expiry.
// TestPersonalVerificationDoesNotInflatePermissions различает точные approved observations,
// неизвестные write permissions, подтверждённый отказ, auth rejection, недоступную проверку и expiry.
func TestPersonalVerificationDoesNotInflatePermissions(t *testing.T) {
	t.Parallel()
	requirement, initiator, checking, snapshot, now := personalVerificationFixture(t)
	observation := entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated,
		ConfirmedCapabilityKeys: []string{"fixture.read"}, DeniedCapabilityKeys: []string{"fixture.extra"}}
	ready, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation)
	if err != nil || ready.State != "READY" || ready.Version != checking.Version+1 || ready.Generation != checking.Generation || ready.ExpiresAt != nil ||
		ready.VerifiedCredentialRef != checking.Credential.Ref {
		t.Fatal("exact positive observation lost current pins or fabricated expiry", err)
	}
	observation.ConfirmedCapabilityKeys[0] = "changed.read"
	if ready.VerifiedCapabilities[0] != "fixture.read" {
		t.Fatal("stored verification retained a mutable observation slice")
	}
	selection, err := ResolveIntegrationCredential(requirement, initiator, nil, &ready, now)
	if err != nil || selection.Credential == nil {
		t.Fatal("verified READ operation was not eligible", err)
	}
	requirement.CapabilityKey = "fixture.write"
	selection, err = ResolveIntegrationCredential(requirement, initiator, checking.Credential, &ready, now)
	if err != nil || selection.Problem != entity.IntegrationCredentialUnconfirmed || selection.Credential != nil {
		t.Fatal("health success confirmed an unobserved write permission or shared fallback", err)
	}
	requirement.CapabilityKey = "fixture.extra"
	selection, err = ResolveIntegrationCredential(requirement, initiator, nil, &ready, now)
	if err != nil || selection.Problem != entity.IntegrationCredentialInsufficient {
		t.Fatal("explicit denied operation was lost", err)
	}
	for _, outcome := range []entity.PersonalIntegrationCredentialTestOutcome{entity.PersonalIntegrationCredentialAuthenticationRejected, entity.PersonalIntegrationCredentialVerificationUnavailable} {
		result, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest,
			entity.PersonalIntegrationCredentialTestObservation{Outcome: outcome})
		expected := "INVALID"
		if outcome == entity.PersonalIntegrationCredentialVerificationUnavailable {
			expected = "CHECK_REQUIRED"
		}
		if err != nil || result.State != expected || result.VerifiedCredentialRef != "" || len(result.VerifiedCapabilities) != 0 || result.ExpiresAt != nil {
			t.Fatal("unavailable verification became an auth rejection or retained old proof", err)
		}
	}
	expiry := now.Add(-time.Second)
	ready, err = CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest,
		entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"fixture.read"}, ExpiresAt: &expiry})
	if err != nil {
		t.Fatal(err)
	}
	expiry = now.Add(time.Hour)
	requirement.CapabilityKey = "fixture.read"
	selection, err = ResolveIntegrationCredential(requirement, initiator, nil, &ready, now)
	if err != nil || selection.Problem != entity.IntegrationCredentialExpired {
		t.Fatal("known expiry was not defensively preserved", err)
	}
}

// TestPersonalVerificationRejectsLateOrUnapprovedCompletion checks revoke, rotation and
// configuration drift fences, self ownership and counterfactual reports outside approved probes.
// TestPersonalVerificationRejectsLateOrUnapprovedCompletion проверяет ограждение revoke, rotation и
// configuration drift, self ownership и контрфактические reports вне approved probes.
func TestPersonalVerificationRejectsLateOrUnapprovedCompletion(t *testing.T) {
	t.Parallel()
	requirement, initiator, checking, snapshot, _ := personalVerificationFixture(t)
	valid := entity.PersonalIntegrationCredentialTestObservation{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"fixture.read"}}
	revoked, err := RevokePersonalIntegrationCredential(checking, initiator, checking.Version)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := CompletePersonalIntegrationCredentialVerification(revoked, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, valid); !errors.Is(err, errs.ErrConflict) || result.Ref != "" {
		t.Fatal("late worker completion resurrected a revoked binding", err)
	}
	replacement := *checking.Credential
	replacement.Ref, replacement.Revision = "icr_replacement", replacement.Revision+1
	rotated, err := ReplacePersonalIntegrationCredential(checking, initiator, checking.Version, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompletePersonalIntegrationCredentialVerification(rotated, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, valid); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("late worker completion verified a replacement key", err)
	}
	foreign := snapshot
	foreign.SubjectID = "usr_other"
	if _, err := CompletePersonalIntegrationCredentialVerification(checking, foreign, requirement.DefinitionDigest, requirement.ConfigurationDigest, valid); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign verification snapshot crossed self ownership", err)
	}
	if _, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, snapshot.DefinitionDigest, valid); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("changed configuration retained credential verification", err)
	}
	for _, observation := range []entity.PersonalIntegrationCredentialTestObservation{
		{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"fixture.write"}},
		{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"fixture.extra"}},
		{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"fixture.read", "fixture.read"}},
		{Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{"fixture.read"}, DeniedCapabilityKeys: []string{"fixture.read"}},
		{Outcome: entity.PersonalIntegrationCredentialVerificationUnavailable, ConfirmedCapabilityKeys: []string{"fixture.read"}},
		{Outcome: "UNKNOWN"},
	} {
		if result, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation); !errors.Is(err, errs.ErrInvalid) || result.Ref != "" {
			t.Fatal("unapproved or inconsistent verification report was accepted", err)
		}
	}
	other := initiator
	other.SubjectID = "usr_other"
	if _, _, err := BeginPersonalIntegrationCredentialVerification(checking, other, 0, snapshot.PersonalIntegrationCredentialTestSpec); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign self check occurred after OCC disclosure", err)
	}
	for _, keys := range [][]string{{"fixture.read", "fixture..write"}, {"fixture.read", "fixture.read"}, {"fixture.read", "1fixture"}, {"fixture.read", "fixture.read/other"}} {
		invalidSpec := snapshot.PersonalIntegrationCredentialTestSpec
		invalidSpec.AllowedReadCapabilityKeys = keys
		candidate := checking
		candidate.State = "CHECK_REQUIRED"
		if _, _, err := BeginPersonalIntegrationCredentialVerification(candidate, initiator, candidate.Version, invalidSpec); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("invalid capability set entered an immutable verification snapshot", err)
		}
	}
}
