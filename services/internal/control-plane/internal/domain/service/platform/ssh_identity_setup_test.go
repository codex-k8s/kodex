package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// sshSetupRepositoryFixture records the specialized owner boundary after current principal resolution; its absent embedded generic methods make accidental TOKEN or unrelated repository fallback fail visibly.
// sshSetupRepositoryFixture фиксирует specialized owner boundary после current principal resolution; absent embedded generic methods явно выявляют accidental TOKEN либо unrelated repository fallback.
type sshSetupRepositoryFixture struct {
	platformrepo.Repository
	resolvedActor, lastActor, lastOperation string
	calls                                   int
}

// ResolvePrincipal replaces only the actor with the synthetic authoritative subject, distinguishing verified repository state from the caller's initial presentation.
// ResolvePrincipal заменяет лишь actor на synthetic authoritative subject, отличая verified repository state от initial presentation caller.
func (fixture *sshSetupRepositoryFixture) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	p.ActorID = fixture.resolvedActor
	return p, nil
}

// record keeps only safe routing assertions and never receives a private key or raw upload permit.
// record сохраняет лишь safe routing assertions и не получает private key либо raw upload permit.
func (fixture *sshSetupRepositoryFixture) record(p value.Principal, operation string) {
	fixture.calls++
	fixture.lastActor = p.ActorID
	fixture.lastOperation = operation
}

// BeginSSHIdentitySetup records entry to the specialized metadata owner without creating a synthetic credential materialization.
// BeginSSHIdentitySetup фиксирует вход в specialized metadata owner без создания synthetic credential materialization.
func (fixture *sshSetupRepositoryFixture) BeginSSHIdentitySetup(_ context.Context, p value.Principal, _ value.Mutation, _ command.SSHIdentitySetupBegin) (entity.SSHIdentitySetupAdmission, error) {
	fixture.record(p, "begin")
	return entity.SSHIdentitySetupAdmission{}, nil
}

// GetSSHIdentitySetup records the protected metadata read without exposing physical pins.
// GetSSHIdentitySetup фиксирует protected metadata read без physical pins.
func (fixture *sshSetupRepositoryFixture) GetSSHIdentitySetup(_ context.Context, p value.Principal, _ command.SSHIdentitySetupRead) (entity.SSHIdentitySetupView, error) {
	fixture.record(p, "get")
	return entity.SSHIdentitySetupView{}, nil
}

// CancelSSHIdentitySetup records only the dedicated cancel command, not a generic transition.
// CancelSSHIdentitySetup фиксирует лишь dedicated cancel command, а не generic transition.
func (fixture *sshSetupRepositoryFixture) CancelSSHIdentitySetup(_ context.Context, p value.Principal, _ value.Mutation, _ command.SSHIdentitySetupCancel) (entity.SSHIdentitySetupView, error) {
	fixture.record(p, "cancel")
	return entity.SSHIdentitySetupView{}, nil
}

// ResolveSSHIdentityUpload records private broker resolution without selecting a TOKEN descriptor.
// ResolveSSHIdentityUpload фиксирует private broker resolution без выбора TOKEN descriptor.
func (fixture *sshSetupRepositoryFixture) ResolveSSHIdentityUpload(_ context.Context, p value.Principal, _ command.SSHIdentityUploadResolve) (entity.SSHIdentityUploadSnapshot, error) {
	fixture.record(p, "resolve")
	return entity.SSHIdentityUploadSnapshot{}, nil
}

// ClaimSSHIdentityUpload records the sole purpose claim after the technical caller gate.
// ClaimSSHIdentityUpload фиксирует sole purpose claim после technical caller gate.
func (fixture *sshSetupRepositoryFixture) ClaimSSHIdentityUpload(_ context.Context, p value.Principal, _ command.SSHIdentityUploadClaim) (entity.SSHIdentityUploadAdmission, error) {
	fixture.record(p, "claim")
	return entity.SSHIdentityUploadAdmission{}, nil
}

// CompleteSSHIdentityUpload records a validated closed outcome union without implementing attachment in this domain routing fixture.
// CompleteSSHIdentityUpload фиксирует validated closed outcome union без реализации attachment в domain routing fixture.
func (fixture *sshSetupRepositoryFixture) CompleteSSHIdentityUpload(_ context.Context, p value.Principal, _ command.SSHIdentityUploadComplete) (entity.SSHIdentitySetupView, error) {
	fixture.record(p, "complete")
	return entity.SSHIdentitySetupView{}, nil
}

// sshSetupPrincipal provides only the ordinary required transport identity fields; each test supplies an exact technical route and operation rather than a payload actor.
// sshSetupPrincipal предоставляет лишь обычные required transport identity fields; test задаёт exact technical route и operation, а не payload actor.
func sshSetupPrincipal(workload, permission string) value.Principal {
	return value.Principal{ActorID: "initial-subject", AuthorityTenant: "organization-fixture", Permission: permission, CallerWorkload: workload, CorrelationRef: "correlation-fixture", CredentialRevision: 1}
}

// TestSSHSetupTechnicalRoutesUseResolvedActor checks all six specialized methods accept only their exact authenticated workload/permission and carry the current owner-resolved actor, never a request subject or generic credential path.
// TestSSHSetupTechnicalRoutesUseResolvedActor проверяет, что все six specialized methods принимают только exact authenticated workload/permission и передают current owner-resolved actor без request subject либо generic credential path.
func TestSSHSetupTechnicalRoutesUseResolvedActor(t *testing.T) {
	zero, one := int64(0), int64(1)
	mutation := value.Mutation{ExpectedVersion: &zero, IdempotencyKey: "ssh-setup-fixture"}
	digest := strings.Repeat("a", 64)
	cases := []struct {
		name, workload, permission string
		call                       func(*Service, value.Principal) error
	}{
		{"begin", "control-api-gateway", "platform.integrations.ssh.setup.begin", func(s *Service, p value.Principal) error {
			_, err := s.BeginSSHIdentitySetup(t.Context(), p, mutation, command.SSHIdentitySetupBegin{ConnectionRef: "connection_fixture", ExpectedConnectionVersion: 1, ContentSHA256: digest, ContentBytes: 1024})
			return err
		}},
		{"get", "control-api-gateway", "platform.integrations.ssh.setup.get", func(s *Service, p value.Principal) error {
			_, err := s.GetSSHIdentitySetup(t.Context(), p, command.SSHIdentitySetupRead{ConnectionRef: "connection_fixture", SetupRef: "ssu_fixture_setup"})
			return err
		}},
		{"cancel", "control-api-gateway", "platform.integrations.ssh.setup.cancel", func(s *Service, p value.Principal) error {
			m := mutation
			m.ExpectedVersion = &one
			_, err := s.CancelSSHIdentitySetup(t.Context(), p, m, command.SSHIdentitySetupCancel{ConnectionRef: "connection_fixture", SetupRef: "ssu_fixture_setup"})
			return err
		}},
		{"resolve", "secret-broker", "platform.runtime.ssh.setup.resolve", func(s *Service, p value.Principal) error {
			_, err := s.ResolveSSHIdentityUpload(t.Context(), p, command.SSHIdentityUploadResolve{SetupRef: "ssu_fixture_setup", PermitSHA256: digest})
			return err
		}},
		{"claim", "secret-broker", "platform.runtime.ssh.setup.claim", func(s *Service, p value.Principal) error {
			_, err := s.ClaimSSHIdentityUpload(t.Context(), p, command.SSHIdentityUploadClaim{SetupRef: "ssu_fixture_setup", PermitSHA256: digest, SnapshotDigest: digest, WorkloadInstance: "pod_fixture_instance", Key: managedssh.IdentityKeyMetadata{ContentSHA256: digest, AuthenticationSHA256: digest}})
			return err
		}},
		{"complete", "secret-broker", "platform.runtime.ssh.setup.complete", func(s *Service, p value.Principal) error {
			_, err := s.CompleteSSHIdentityUpload(t.Context(), p, command.SSHIdentityUploadComplete{SetupRef: "ssu_fixture_setup", AttemptRef: "ssa_fixture_attempt", WorkloadInstance: "pod_fixture_instance", Fence: "fence_fixture_value", Generation: 1, Unknown: true})
			return err
		}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fixture := &sshSetupRepositoryFixture{resolvedActor: "verified-current-subject"}
			service, err := New(fixture)
			if err != nil {
				t.Fatal(err)
			}
			principal := sshSetupPrincipal(item.workload, item.permission)
			if err := item.call(service, principal); err != nil || fixture.calls != 1 || fixture.lastActor != fixture.resolvedActor || fixture.lastOperation != item.name {
				t.Fatal("exact route lost current owner actor", err)
			}
			for _, change := range []func(*value.Principal){func(p *value.Principal) { p.CallerWorkload = "agent-runner" }, func(p *value.Principal) { p.Permission = "platform.runtime.generic.credential" }} {
				bad := principal
				change(&bad)
				if err := item.call(service, bad); !errors.Is(err, errs.ErrForbidden) || fixture.calls != 1 {
					t.Fatal("wrong technical route reached specialized owner", err)
				}
			}
		})
	}
}

// TestSSHSetupCompletionRejectsMixedAndForeignEvidence checks the security-relevant outcome union before any owner transaction, including missing rejected witnesses and cross-purpose physical locators.
// TestSSHSetupCompletionRejectsMixedAndForeignEvidence проверяет security-relevant outcome union до owner transaction, включая missing rejected witnesses и cross-purpose physical locators.
func TestSSHSetupCompletionRejectsMixedAndForeignEvidence(t *testing.T) {
	fixture := &sshSetupRepositoryFixture{resolvedActor: "verified-current-subject"}
	service, _ := New(fixture)
	principal := sshSetupPrincipal("secret-broker", "platform.runtime.ssh.setup.complete")
	original := command.SSHIdentityUploadComplete{SetupRef: "ssu_fixture_setup", AttemptRef: "ssa_fixture_attempt", WorkloadInstance: "pod_fixture_instance", Fence: "fence_fixture_value", Generation: 1, Unknown: true}
	for _, change := range []func(*command.SSHIdentityUploadComplete){
		func(i *command.SSHIdentityUploadComplete) { i.Unknown = false },
		func(i *command.SSHIdentityUploadComplete) {
			i.Rejected = &command.SSHIdentityRejectedAcknowledgement{ResponseSHA256: strings.Repeat("b", 64)}
		},
		func(i *command.SSHIdentityUploadComplete) {
			i.Unknown = false
			i.Rejected = &command.SSHIdentityRejectedAcknowledgement{}
		},
		func(i *command.SSHIdentityUploadComplete) {
			i.Unknown = false
			i.Identity = &managedssh.IdentityLocator{Purpose: "TOKEN"}
		},
		func(i *command.SSHIdentityUploadComplete) { i.Generation = 9 },
		func(i *command.SSHIdentityUploadComplete) { i.AttemptRef = "ksu_foreign_purpose" },
	} {
		input := original
		change(&input)
		if _, err := service.CompleteSSHIdentityUpload(t.Context(), principal, input); !errors.Is(err, errs.ErrInvalid) || fixture.calls != 0 {
			t.Fatal("malformed outcome reached owner", err)
		}
	}
	rejected := original
	rejected.Unknown = false
	rejected.Rejected = &command.SSHIdentityRejectedAcknowledgement{ResponseSHA256: strings.Repeat("b", 64), ObservedUID: "known-original-uid"}
	if _, err := service.CompleteSSHIdentityUpload(t.Context(), principal, rejected); err != nil || fixture.calls != 1 {
		t.Fatal("complete terminal acknowledgement fact rejected", err)
	}
}

// TestSSHSetupReadAndOCCCannotBeAmbiguous checks begin's explicit absent-binding version, cancel's positive version and the exclusive addressed/idempotency read selectors without issuing or replaying a credential capability.
// TestSSHSetupReadAndOCCCannotBeAmbiguous проверяет explicit absent-binding version Begin, positive version Cancel и exclusive addressed/idempotency read selectors без выдачи либо replay credential capability.
func TestSSHSetupReadAndOCCCannotBeAmbiguous(t *testing.T) {
	fixture := &sshSetupRepositoryFixture{resolvedActor: "verified-current-subject"}
	service, _ := New(fixture)
	principal := sshSetupPrincipal("control-api-gateway", "platform.integrations.ssh.setup.get")
	for _, input := range []command.SSHIdentitySetupRead{{ConnectionRef: "connection_fixture"}, {ConnectionRef: "connection_fixture", SetupRef: "ssu_fixture_setup", BeginIdempotencyKey: "begin-fixture"}, {ConnectionRef: "connection_fixture", BeginIdempotencyKey: "bad\tkey-long-enough"}} {
		if _, err := service.GetSSHIdentitySetup(t.Context(), principal, input); !errors.Is(err, errs.ErrInvalid) || fixture.calls != 0 {
			t.Fatal("ambiguous or control-character locator admitted", err)
		}
	}
	for _, version := range []*int64{nil, new(int64(-1))} {
		principal.Permission = "platform.integrations.ssh.setup.begin"
		_, err := service.BeginSSHIdentitySetup(t.Context(), principal, value.Mutation{ExpectedVersion: version, IdempotencyKey: "begin-fixture"}, command.SSHIdentitySetupBegin{ConnectionRef: "connection_fixture", ExpectedConnectionVersion: 1, ContentSHA256: strings.Repeat("a", 64), ContentBytes: 1})
		if !errors.Is(err, errs.ErrInvalid) || fixture.calls != 0 {
			t.Fatal("missing binding OCC admitted", err)
		}
	}
	principal.Permission = "platform.integrations.ssh.setup.cancel"
	if _, err := service.CancelSSHIdentitySetup(t.Context(), principal, value.Mutation{ExpectedVersion: new(int64(0)), IdempotencyKey: "cancel-fixture"}, command.SSHIdentitySetupCancel{ConnectionRef: "connection_fixture", SetupRef: "ssu_fixture_setup"}); !errors.Is(err, errs.ErrInvalid) || fixture.calls != 0 {
		t.Fatal("cancel accepted absent setup OCC", err)
	}
}
