package platform

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// personalBrokerIdentityFixture converts the synthetic owner fixture into metadata-only transport;
// it never creates a generic descriptor, kubeconfig, key or real external identity.
// personalBrokerIdentityFixture преобразует synthetic owner fixture в metadata-only transport;
// generic descriptor, kubeconfig, ключ либо реальная external identity не создаются.
func personalBrokerIdentityFixture() (entity.IntegrationCredentialRequirement, entity.IntegrationCredentialInitiator, entity.PersonalIntegrationCredential, time.Time) {
	requirement, initiator, binding, now := personalCredentialFixture()
	requirement.Kind = entity.IntegrationCredentialBrokerIdentity
	requirement.BrokerPurpose = entity.IntegrationBrokerIdentityKubernetes
	requirement.BrokerTargetRef = "ktg_fixture"
	binding.Kind = entity.IntegrationCredentialBrokerIdentity
	binding.BrokerPurpose = requirement.BrokerPurpose
	binding.Credential = nil
	binding.BrokerIdentity = &entity.IntegrationBrokerIdentityRevision{
		Ref: "bir_fixture", TargetRef: requirement.BrokerTargetRef, Revision: 2,
		Purpose: requirement.BrokerPurpose, CreatedAt: now.Add(-time.Minute),
	}
	binding.VerifiedCredentialRef = ""
	binding.VerifiedBrokerIdentityRef = binding.BrokerIdentity.Ref
	return requirement, initiator, binding, now
}

// TestPersonalBrokerIdentitySelection proves current self ownership and exact metadata revision,
// with the same verification/generation rules and no generic descriptor or shared fallback.
// TestPersonalBrokerIdentitySelection доказывает current self ownership и exact metadata revision
// с теми же verification/generation rules без generic descriptor либо shared fallback.
func TestPersonalBrokerIdentitySelection(t *testing.T) {
	t.Parallel()
	requirement, initiator, binding, now := personalBrokerIdentityFixture()
	selection, err := ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
	if err != nil || selection.Problem != "" || selection.Kind != entity.IntegrationCredentialBrokerIdentity || selection.Credential != nil ||
		selection.BrokerIdentity == nil || *selection.BrokerIdentity != *binding.BrokerIdentity || selection.PersonalBindingRef != binding.Ref || selection.BindingGeneration != binding.Generation {
		t.Fatalf("metadata-only personal selection failed: %v", err)
	}
	selection.BrokerIdentity.Ref = "bir_changed_copy"
	if binding.BrokerIdentity.Ref != "bir_fixture" {
		t.Fatal("selection changed owner metadata")
	}
	for _, value := range []any{selection, entity.IntegrationCredentialSource{Kind: binding.Kind, BrokerIdentity: binding.BrokerIdentity}} {
		raw, err := json.Marshal(value)
		if err != nil || string(raw) != "{}" {
			t.Fatal("internal credential selection escaped a generic JSON view")
		}
	}
	shared := &entity.IntegrationCredentialSource{Kind: binding.Kind, BrokerIdentity: binding.BrokerIdentity}
	selected, err := ResolveIntegrationCredential(requirement, initiator, shared, nil, now)
	if err != nil || selected.Problem != entity.IntegrationCredentialMissing || selected.BrokerIdentity != nil {
		t.Fatal("missing personal identity selected a shared principal")
	}
	for _, mutate := range []func(*entity.IntegrationCredentialInitiator){
		func(value *entity.IntegrationCredentialInitiator) { value.OrganizationID = "org_foreign" },
		func(value *entity.IntegrationCredentialInitiator) { value.SubjectID = "usr_foreign" },
	} {
		foreign := initiator
		mutate(&foreign)
		if _, err := ResolveIntegrationCredential(requirement, foreign, shared, &binding, now); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("foreign owner selected a broker identity")
		}
	}
	initiator.SubjectKind = "SERVICE"
	selected, err = ResolveIntegrationCredential(requirement, initiator, shared, &binding, now)
	if err != nil || selected.Problem != entity.IntegrationCredentialInitiatorMissing {
		t.Fatal("service root inherited a personal broker identity")
	}
	requirement.Policy = entity.IntegrationCredentialProjectShared
	selected, err = ResolveIntegrationCredential(requirement, initiator, shared, nil, now)
	if err != nil || selected.Kind != entity.IntegrationCredentialBrokerIdentity || selected.Credential != nil || selected.BrokerIdentity == nil || selected.PersonalBindingRef != "" {
		t.Fatal("explicit shared identity was not kept separate from personal ownership")
	}
}

// TestPersonalBrokerIdentityRejectsTypeConfusion closes unknown/mixed representations and exact
// target/purpose drift; authentication observations never widen one private resolver into another.
// TestPersonalBrokerIdentityRejectsTypeConfusion закрывает unknown/mixed representations и exact
// target/purpose drift; authentication observations не расширяют один private resolver до другого.
func TestPersonalBrokerIdentityRejectsTypeConfusion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*entity.IntegrationCredentialRequirement, *entity.PersonalIntegrationCredential, time.Time)
		err    error
		code   entity.IntegrationCredentialProblem
	}{
		{"unknown-required-kind", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential, _ time.Time) {
			r.Kind = "UNKNOWN"
		}, errs.ErrInvalid, ""},
		{"unknown-stored-kind", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.Kind = "UNKNOWN"
		}, errs.ErrUnavailable, ""},
		{"missing-stored-kind", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.Kind = ""
		}, errs.ErrUnavailable, ""},
		{"missing-kind", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential, _ time.Time) {
			r.Kind = ""
		}, errs.ErrInvalid, ""},
		{"unknown-purpose", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential, _ time.Time) {
			r.BrokerPurpose = "GENERIC_SECRET"
		}, errs.ErrInvalid, ""},
		{"missing-target", func(r *entity.IntegrationCredentialRequirement, _ *entity.PersonalIntegrationCredential, _ time.Time) {
			r.BrokerTargetRef = ""
		}, errs.ErrInvalid, ""},
		{"foreign-purpose", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.BrokerIdentity.Purpose = entity.IntegrationBrokerIdentitySSH
		}, errs.ErrUnavailable, ""},
		{"foreign-target", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.BrokerIdentity.TargetRef = "ktg_foreign"
		}, errs.ErrUnavailable, ""},
		{"future-revision", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, now time.Time) {
			b.BrokerIdentity.CreatedAt = now.Add(time.Minute)
		}, errs.ErrUnavailable, ""},
		{"invalid-revision", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.BrokerIdentity.Revision = 0
		}, errs.ErrUnavailable, ""},
		{"missing-metadata", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.BrokerIdentity = nil
		}, nil, entity.IntegrationCredentialMissing},
		{"mixed-source", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			_, _, token, _ := personalCredentialFixture()
			b.Credential = token.Credential
		}, errs.ErrUnavailable, ""},
		{"mixed-verification", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.VerifiedCredentialRef = "icr_fixture"
		}, nil, entity.IntegrationCredentialCheckRequired},
		{"rotated", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.BrokerIdentity.Ref = "bir_rotated"
		}, nil, entity.IntegrationCredentialCheckRequired},
		{"revoked", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.State = "REVOKED"
		}, nil, entity.IntegrationCredentialRevoked},
		{"expired", func(_ *entity.IntegrationCredentialRequirement, b *entity.PersonalIntegrationCredential, now time.Time) {
			b.ExpiresAt = &now
		}, nil, entity.IntegrationCredentialExpired},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requirement, initiator, binding, now := personalBrokerIdentityFixture()
			test.change(&requirement, &binding, now)
			selected, err := ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
			if !errors.Is(err, test.err) || selected.Problem != test.code || selected.Credential != nil || selected.BrokerIdentity != nil {
				t.Fatalf("invalid broker metadata crossed selection: %v", err)
			}
		})
	}
	requirement, initiator, binding, now := personalBrokerIdentityFixture()
	requirement.Policy = entity.IntegrationCredentialProjectShared
	_, _, token, _ := personalCredentialFixture()
	if _, err := ResolveIntegrationCredential(requirement, initiator, entity.TokenIntegrationCredentialSource(token.Credential), nil, now); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("generic token substituted a broker identity")
	}
	requirement, _, _, _ = personalCredentialFixture()
	requirement.Policy = entity.IntegrationCredentialProjectShared
	if _, err := ResolveIntegrationCredential(requirement, initiator, &entity.IntegrationCredentialSource{Kind: binding.Kind, BrokerIdentity: binding.BrokerIdentity}, nil, now); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("broker identity entered generic credential delivery")
	}
}

// TestMissingCredentialKindNeverAuthorizesSetupOrExecution distinguishes a retained safe receipt
// projection from current owner decisions: missing kind cannot select or replace a token.
// TestMissingCredentialKindNeverAuthorizesSetupOrExecution отличает retained safe receipt
// projection от current owner decisions: missing kind не выбирает и не заменяет token.
func TestMissingCredentialKindNeverAuthorizesSetupOrExecution(t *testing.T) {
	t.Parallel()
	requirement, initiator, binding, now := personalCredentialFixture()
	binding.Kind = ""
	if _, err := ResolveIntegrationCredential(requirement, initiator, nil, &binding, now); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing kind was inferred as TOKEN for current execution")
	}
	if _, err := ReplacePersonalIntegrationCredential(binding, initiator, binding.Version, *binding.Credential); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing kind authorized generic token replacement")
	}
}

// TestPersonalBrokerIdentityGenerationAndPreflight preserves revoke/rotation lineage and rejects
// duplicate Connection snapshots with a different transport, even when both independently verify.
// TestPersonalBrokerIdentityGenerationAndPreflight сохраняет revoke/rotation lineage и отвергает
// повторные Connection snapshots с иным transport, даже если обе независимо прошли проверку.
func TestPersonalBrokerIdentityGenerationAndPreflight(t *testing.T) {
	t.Parallel()
	requirement, initiator, binding, now := personalBrokerIdentityFixture()
	identity := *binding.BrokerIdentity
	identity.Ref, identity.Revision = "bir_rotated", identity.Revision+1
	replaced, err := ReplacePersonalIntegrationBrokerIdentity(binding, initiator, binding.Version, identity)
	if err != nil || replaced.Credential != nil || replaced.Generation != binding.Generation+1 || replaced.State != "CHECK_REQUIRED" || replaced.VerifiedBrokerIdentityRef != "" || replaced.VerifiedDefinitionDigest != "" {
		t.Fatal("broker identity rotation retained stale observations")
	}
	if _, err := ReplacePersonalIntegrationBrokerIdentity(binding, initiator, binding.Version, *binding.BrokerIdentity); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("equal broker revision replayed as a new replacement")
	}
	if _, err := ReplacePersonalIntegrationBrokerIdentity(binding, initiator, binding.Version-1, identity); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("broker replacement ignored owner OCC")
	}
	revoked, err := RevokePersonalIntegrationCredential(binding, initiator, binding.Version)
	if err != nil || revoked.State != "REVOKED" || revoked.Generation != binding.Generation+1 || revoked.BrokerIdentity == nil || revoked.VerifiedBrokerIdentityRef != "" {
		t.Fatal("revoke did not preserve historical broker metadata and close future starts")
	}
	_, _, token, _ := personalCredentialFixture()
	if _, err := ReplacePersonalIntegrationCredential(binding, initiator, binding.Version, *token.Credential); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("generic token configuration overwrote a broker identity")
	}
	if _, _, err := BeginPersonalIntegrationCredentialVerification(binding, initiator, binding.Version, entity.PersonalIntegrationCredentialTestSpec{}); err == nil {
		t.Fatal("generic token worker accepted a broker verification")
	}
	other := requirement
	other.CapabilityKey = "fixture.second"
	other.BrokerTargetRef = "ktg_other"
	if _, err := PreflightIntegrationCredentials([]entity.IntegrationCredentialRequirement{requirement, other}, initiator, nil, map[string]*entity.PersonalIntegrationCredential{requirement.ConnectionRef: &binding}, now); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("preflight accepted conflicting current broker targets")
	}
}

// TestPersonalBrokerVerificationPinsKindAndCurrentGeneration proves a metadata-only health
// observation uses the shared lifecycle without becoming generic token delivery or RBAC credit.
// TestPersonalBrokerVerificationPinsKindAndCurrentGeneration доказывает общий lifecycle для
// metadata-only health observation без generic token delivery либо выдачи RBAC credit.
func TestPersonalBrokerVerificationPinsKindAndCurrentGeneration(t *testing.T) {
	t.Parallel()
	requirement, initiator, binding, now := personalBrokerIdentityFixture()
	binding.CredentialPolicy = entity.IntegrationCredentialInitiatorRequired
	binding.ConnectionVersion = 7
	spec := entity.PersonalIntegrationCredentialTestSpec{
		Ref: "pct_broker_fixture", ConnectionVersion: binding.ConnectionVersion,
		DefinitionDigest: requirement.DefinitionDigest, ConfigurationDigest: requirement.ConfigurationDigest,
		HealthCapabilityKey: requirement.CapabilityKey, AllowedReadCapabilityKeys: []string{requirement.CapabilityKey},
		BrokerPurpose: requirement.BrokerPurpose, BrokerTargetRef: requirement.BrokerTargetRef,
	}
	checking, snapshot, err := BeginPersonalIntegrationBrokerIdentityVerification(binding, initiator, binding.Version, spec)
	if err != nil || checking.State != "CHECKING" || checking.Credential != nil || checking.Generation != binding.Generation || checking.Version != binding.Version+1 ||
		checking.VerifiedBrokerIdentityRef != "" || snapshot.Kind != entity.IntegrationCredentialBrokerIdentity || snapshot.CredentialRef != "" || snapshot.BrokerIdentityRef != binding.BrokerIdentity.Ref {
		t.Fatalf("broker verification lost exact metadata pins: %v", err)
	}
	observation := entity.PersonalIntegrationCredentialTestObservation{
		Outcome: entity.PersonalIntegrationCredentialAuthenticated, ConfirmedCapabilityKeys: []string{requirement.CapabilityKey},
	}
	ready, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation)
	if err != nil || ready.State != "READY" || ready.Credential != nil || ready.VerifiedCredentialRef != "" || ready.VerifiedBrokerIdentityRef != binding.BrokerIdentity.Ref {
		t.Fatalf("broker verification entered token metadata: %v", err)
	}
	selected, err := ResolveIntegrationCredential(requirement, initiator, nil, &ready, now)
	if err != nil || selected.Credential != nil || selected.BrokerIdentity == nil {
		t.Fatal("verified metadata-only health was unavailable")
	}
	write := requirement
	write.CapabilityKey = "fixture.write"
	selected, err = ResolveIntegrationCredential(write, initiator, nil, &ready, now)
	if err != nil || selected.Problem != entity.IntegrationCredentialUnconfirmed || selected.BrokerIdentity != nil {
		t.Fatal("health observation fabricated a mutation permission")
	}
	for _, change := range []func(*entity.PersonalIntegrationCredentialTestSnapshot){
		func(value *entity.PersonalIntegrationCredentialTestSnapshot) {
			value.Kind = entity.IntegrationCredentialToken
		},
		func(value *entity.PersonalIntegrationCredentialTestSnapshot) { value.Kind = "UNKNOWN" },
		func(value *entity.PersonalIntegrationCredentialTestSnapshot) { value.CredentialRef = "icr_injected" },
		func(value *entity.PersonalIntegrationCredentialTestSnapshot) { value.BrokerIdentityRef = "bir_other" },
		func(value *entity.PersonalIntegrationCredentialTestSnapshot) { value.BrokerTargetRef = "ktg_other" },
		func(value *entity.PersonalIntegrationCredentialTestSnapshot) {
			value.BrokerPurpose = entity.IntegrationBrokerIdentitySSH
		},
	} {
		changed := snapshot
		change(&changed)
		if err := ValidatePersonalIntegrationCredentialVerificationSnapshot(checking, changed, requirement.DefinitionDigest, requirement.ConfigurationDigest); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("changed broker verification kind/target/revision retained admission")
		}
	}
	revoked, err := RevokePersonalIntegrationCredential(checking, initiator, checking.Version)
	if err != nil {
		t.Fatal("synthetic revoke failed")
	}
	if _, err := CompletePersonalIntegrationCredentialVerification(revoked, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("late broker authentication revived a revoked binding")
	}
	rotatedIdentity := *checking.BrokerIdentity
	rotatedIdentity.Ref, rotatedIdentity.Revision = "bir_rotated", rotatedIdentity.Revision+1
	rotated, err := ReplacePersonalIntegrationBrokerIdentity(checking, initiator, checking.Version, rotatedIdentity)
	if err != nil {
		t.Fatal("synthetic rotation failed")
	}
	if _, err := CompletePersonalIntegrationCredentialVerification(rotated, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("late broker authentication verified a replacement identity")
	}
	observation.ConfirmedCapabilityKeys = append(observation.ConfirmedCapabilityKeys, "fixture.write")
	if _, err := CompletePersonalIntegrationCredentialVerification(checking, snapshot, requirement.DefinitionDigest, requirement.ConfigurationDigest, observation); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("broker observation widened owner-pinned probe keys")
	}
}
