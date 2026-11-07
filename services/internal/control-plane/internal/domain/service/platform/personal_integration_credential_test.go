package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// personalCredentialFixture creates only synthetic owner-resolved metadata; no external API
// or secret backend is contacted and the returned descriptor contains no credential value.
// personalCredentialFixture создаёт только синтетические owner-resolved метаданные без внешнего
// API и secret backend; возвращённый descriptor не содержит значения ключа.
func personalCredentialFixture() (entity.IntegrationCredentialRequirement, entity.IntegrationCredentialInitiator, entity.PersonalIntegrationCredential, time.Time) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	requirement := entity.IntegrationCredentialRequirement{
		ConnectionRef: "int_fixture", CapabilityKey: "fixture.read", Policy: entity.IntegrationCredentialInitiatorRequired,
		CredentialRequired: true, DefinitionDigest: strings.Repeat("a", 64), ConfigurationDigest: strings.Repeat("b", 64),
	}
	initiator := entity.IntegrationCredentialInitiator{OrganizationID: "org_fixture", SubjectID: "usr_first", SubjectKind: "USER"}
	binding := entity.PersonalIntegrationCredential{
		Ref: "icb_fixture", OrganizationID: initiator.OrganizationID, SubjectID: initiator.SubjectID,
		ConnectionRef: requirement.ConnectionRef, Version: 3, Generation: 2, State: "READY",
		Credential: &entity.IntegrationCredentialRevision{
			Ref: "icr_fixture", Revision: 2, CreatedAt: now.Add(-time.Minute),
			SecretRef: "kodex-system/kodex-integration-credentials#integration-fixture",
			SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "42", ContentSHA256: strings.Repeat("c", 64),
		},
		VerifiedCredentialRef: "icr_fixture", VerifiedDefinitionDigest: requirement.DefinitionDigest,
		VerifiedConfigurationDigest: requirement.ConfigurationDigest, VerifiedCapabilities: []string{requirement.CapabilityKey},
	}
	return requirement, initiator, binding, now
}

// TestPersonalIntegrationCredentialSelection checks every known failure, exact observation pins
// and the absence of fallback even when a valid shared credential is available.
// TestPersonalIntegrationCredentialSelection проверяет все известные отказы, точные pins проверки
// и отсутствие fallback даже при наличии пригодного общего ключа.
func TestPersonalIntegrationCredentialSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		change  func(*entity.PersonalIntegrationCredential, time.Time)
		problem entity.IntegrationCredentialProblem
	}{
		{"ready", func(_ *entity.PersonalIntegrationCredential, _ time.Time) {}, ""},
		{"revoked", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.State = "REVOKED" }, entity.IntegrationCredentialRevoked},
		{"invalid", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.State = "INVALID" }, entity.IntegrationCredentialInvalid},
		{"unchecked", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.State = "CHECK_REQUIRED" }, entity.IntegrationCredentialCheckRequired},
		{"checking", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.State = "CHECKING" }, entity.IntegrationCredentialCheckRequired},
		{"expired", func(b *entity.PersonalIntegrationCredential, now time.Time) { b.ExpiresAt = &now }, entity.IntegrationCredentialExpired},
		{"missing-revision", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.Credential = nil }, entity.IntegrationCredentialMissing},
		{"rotation", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.Credential.Ref = "icr_rotated" }, entity.IntegrationCredentialCheckRequired},
		{"definition-change", func(b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.VerifiedDefinitionDigest = strings.Repeat("d", 64)
		}, entity.IntegrationCredentialCheckRequired},
		{"host-change", func(b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.VerifiedConfigurationDigest = strings.Repeat("e", 64)
		}, entity.IntegrationCredentialCheckRequired},
		{"denied-capability", func(b *entity.PersonalIntegrationCredential, _ time.Time) {
			b.DeniedCapabilities = []string{"fixture.read"}
		}, entity.IntegrationCredentialInsufficient},
		{"unconfirmed-write", func(b *entity.PersonalIntegrationCredential, _ time.Time) { b.VerifiedCapabilities = nil }, entity.IntegrationCredentialUnconfirmed},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requirement, initiator, binding, now := personalCredentialFixture()
			shared := *binding.Credential
			test.change(&binding, now)
			selection, err := ResolveIntegrationCredential(requirement, initiator, &shared, &binding, now)
			if err != nil || selection.Problem != test.problem {
				t.Fatalf("selection classification mismatch: %v", err)
			}
			if test.problem != "" && (selection.Credential != nil || selection.PersonalBindingRef != "" || selection.BindingGeneration != 0) {
				t.Fatal("unusable personal binding disclosed or selected a credential")
			}
			if test.problem == "" && (selection.Credential == nil || selection.PersonalBindingRef != binding.Ref || selection.BindingGeneration != binding.Generation) {
				t.Fatal("ready personal selection lost exact lineage")
			}
		})
	}
	requirement, initiator, binding, now := personalCredentialFixture()
	selection, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, nil, now)
	if err != nil || selection.Problem != entity.IntegrationCredentialMissing || selection.Credential != nil {
		t.Fatal("missing personal binding fell back to shared credential")
	}
}

// TestPersonalIntegrationCredentialOwnerAndLifecycle preserves root ownership across caller changes,
// rejects foreign tenants/users/connections and rechecks revoke after a previous positive result.
// TestPersonalIntegrationCredentialOwnerAndLifecycle сохраняет владельца корня при смене caller,
// отклоняет чужие tenant/user/connection и повторно проверяет revoke после положительного результата.
func TestPersonalIntegrationCredentialOwnerAndLifecycle(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*entity.IntegrationCredentialInitiator, *entity.PersonalIntegrationCredential){
		func(i *entity.IntegrationCredentialInitiator, _ *entity.PersonalIntegrationCredential) {
			i.SubjectID = "usr_second"
		},
		func(i *entity.IntegrationCredentialInitiator, _ *entity.PersonalIntegrationCredential) {
			i.OrganizationID = "org_second"
		},
		func(_ *entity.IntegrationCredentialInitiator, b *entity.PersonalIntegrationCredential) {
			b.ConnectionRef = "int_other_project"
		},
	} {
		requirement, initiator, binding, now := personalCredentialFixture()
		change(&initiator, &binding)
		if _, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, &binding, now); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("foreign personal binding was accepted")
		}
	}
	requirement, initiator, binding, now := personalCredentialFixture()
	for _, kind := range []string{"SERVICE", "", "UNKNOWN"} {
		service := initiator
		service.SubjectKind = kind
		selection, err := ResolveIntegrationCredential(requirement, service, binding.Credential, &binding, now)
		if err != nil || selection.Problem != entity.IntegrationCredentialInitiatorMissing || selection.Credential != nil {
			t.Fatal("noninteractive personal policy was accepted")
		}
	}
	selection, err := ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
	if err != nil || selection.Credential == nil {
		t.Fatal("initial personal selection failed")
	}
	selection.Credential.SecretRef = "mutated-copy"
	if binding.Credential.SecretRef == "mutated-copy" {
		t.Fatal("selection mutated the authoritative descriptor")
	}
	binding.State = "REVOKED"
	selection, err = ResolveIntegrationCredential(requirement, initiator, nil, &binding, now)
	if err != nil || selection.Credential != nil || selection.Problem != entity.IntegrationCredentialRevoked {
		t.Fatal("previous readiness survived current revocation")
	}
	encoded, err := json.Marshal(selection)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("internal selection can leak through generic JSON readback")
	}
}

// TestIntegrationCredentialPreflightCollectsAllAndRejectsPartialAuthority checks full problem
// collection, explicit shared compatibility, duplicate/conflicting snapshots and bounded input.
// TestIntegrationCredentialPreflightCollectsAllAndRejectsPartialAuthority проверяет полный список
// проблем, явный общий режим, повторные/противоречивые снимки и ограниченность входа.
func TestIntegrationCredentialPreflightCollectsAllAndRejectsPartialAuthority(t *testing.T) {
	t.Parallel()
	requirement, initiator, binding, now := personalCredentialFixture()
	write := requirement
	write.CapabilityKey = "fixture.write"
	other := requirement
	other.ConnectionRef = "int_missing"
	requirements := []entity.IntegrationCredentialRequirement{requirement, write, other}
	bindings := map[string]*entity.PersonalIntegrationCredential{requirement.ConnectionRef: &binding}
	issues, err := PreflightIntegrationCredentials(requirements, initiator, nil, bindings, now)
	if err != nil || len(issues) != 2 || issues[0].CapabilityKey != write.CapabilityKey ||
		issues[0].Problem != entity.IntegrationCredentialUnconfirmed || issues[1].Problem != entity.IntegrationCredentialMissing {
		t.Fatal("preflight omitted, reordered or fabricated a credential problem")
	}
	sharedRequirement := requirement
	sharedRequirement.Policy = entity.IntegrationCredentialProjectShared
	selection, err := ResolveIntegrationCredential(sharedRequirement, initiator, binding.Credential, nil, now)
	if err != nil || selection.Credential == nil || selection.PersonalBindingRef != "" {
		t.Fatal("explicit shared mode changed existing credential selection")
	}
	for _, invalid := range [][]entity.IntegrationCredentialRequirement{
		{requirement, requirement},
		{requirement, {ConnectionRef: requirement.ConnectionRef, CapabilityKey: write.CapabilityKey, Policy: entity.IntegrationCredentialProjectShared}},
		make([]entity.IntegrationCredentialRequirement, maximumIntegrationCredentialRequirements+1),
	} {
		issues, err := PreflightIntegrationCredentials(invalid, initiator, nil, bindings, now)
		if !errors.Is(err, errs.ErrInvalid) || issues != nil {
			t.Fatal("malformed preflight returned a partial result")
		}
	}
	for _, invalid := range []string{"", "UNKNOWN", "initiator_required"} {
		requirement.Policy = entity.IntegrationCredentialPolicy(invalid)
		if _, err := ResolveIntegrationCredential(requirement, initiator, binding.Credential, &binding, now); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("unknown policy fell back to another credential")
		}
	}
}

// TestPersonalIntegrationCredentialMaterializationIdentity checks that equal semantic retries
// rejoin, while another user, tenant, Connection or operation gets a distinct backend key.
// TestPersonalIntegrationCredentialMaterializationIdentity проверяет rejoin одинаковых semantic
// retries и отдельный backend key другого пользователя, tenant, подключения либо операции.
func TestPersonalIntegrationCredentialMaterializationIdentity(t *testing.T) {
	t.Parallel()
	requirement, initiator, _, _ := personalCredentialFixture()
	first, err := PersonalIntegrationCredentialMaterializationRef(initiator, requirement.ConnectionRef, "same-operation-key")
	if err != nil || !strings.HasPrefix(first, "integration-personal-") {
		t.Fatal("personal materialization identity failed")
	}
	replay, err := PersonalIntegrationCredentialMaterializationRef(initiator, requirement.ConnectionRef, "same-operation-key")
	if err != nil || replay != first {
		t.Fatal("equal operation did not preserve materialization identity")
	}
	otherUser, otherTenant := initiator, initiator
	otherUser.SubjectID = "usr_second"
	otherTenant.OrganizationID = "org_second"
	seen := map[string]bool{first: true}
	for _, test := range []struct {
		owner      entity.IntegrationCredentialInitiator
		connection string
		key        string
	}{
		{otherUser, requirement.ConnectionRef, "same-operation-key"},
		{otherTenant, requirement.ConnectionRef, "same-operation-key"},
		{initiator, "int_other", "same-operation-key"},
		{initiator, requirement.ConnectionRef, "new-operation-key"},
	} {
		ref, err := PersonalIntegrationCredentialMaterializationRef(test.owner, test.connection, test.key)
		if err != nil || seen[ref] || len(ref) > 253 {
			t.Fatal("distinct owner operation reused a materialization identity")
		}
		seen[ref] = true
	}
	initiator.SubjectKind = "SERVICE"
	if _, err := PersonalIntegrationCredentialMaterializationRef(initiator, requirement.ConnectionRef, "same-operation-key"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("service principal materialized a personal credential")
	}
}
