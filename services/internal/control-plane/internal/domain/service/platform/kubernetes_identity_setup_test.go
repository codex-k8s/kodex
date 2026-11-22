package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesSetupRepositoryFixture isolates service authority/syntax admission without real grants.
// Full resource ownership, transactional idempotency and external effects require native PG/wire tests.
// kubernetesSetupRepositoryFixture изолирует service authority/syntax admission без реальных grants.
// Resource ownership, transactional idempotency и external effects требуют native PG/wire tests.
type kubernetesSetupRepositoryFixture struct {
	platformrepo.Repository
	calls int
}

// ResolvePrincipal preserves only the synthetic verified test principal.
// ResolvePrincipal сохраняет только synthetic verified test principal.
func (*kubernetesSetupRepositoryFixture) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, nil
}

// BeginKubernetesIdentitySetup records forwarding without creating a real owner ticket.
// BeginKubernetesIdentitySetup фиксирует forwarding без создания реального owner ticket.
func (r *kubernetesSetupRepositoryFixture) BeginKubernetesIdentitySetup(context.Context, value.Principal, value.Mutation, command.KubernetesIdentitySetupBegin) (entity.KubernetesIdentitySetupAdmission, error) {
	r.calls++
	return entity.KubernetesIdentitySetupAdmission{}, nil
}

// GetKubernetesIdentitySetup records a safe metadata read without minting a permit.
// GetKubernetesIdentitySetup фиксирует safe metadata read без выдачи permit.
func (r *kubernetesSetupRepositoryFixture) GetKubernetesIdentitySetup(context.Context, value.Principal, command.KubernetesIdentitySetupRead) (entity.KubernetesIdentitySetupView, error) {
	r.calls++
	return entity.KubernetesIdentitySetupView{}, nil
}

// CancelKubernetesIdentitySetup records forwarding without simulating remote process rollback.
// CancelKubernetesIdentitySetup фиксирует forwarding без имитации rollback удалённого процесса.
func (r *kubernetesSetupRepositoryFixture) CancelKubernetesIdentitySetup(context.Context, value.Principal, value.Mutation, command.KubernetesIdentitySetupCancel) (entity.KubernetesIdentitySetupView, error) {
	r.calls++
	return entity.KubernetesIdentitySetupView{}, nil
}

// ResolveKubernetesIdentityUpload records private resolution without returning any credential material.
// ResolveKubernetesIdentityUpload фиксирует private resolution без возврата credential material.
func (r *kubernetesSetupRepositoryFixture) ResolveKubernetesIdentityUpload(context.Context, value.Principal, command.KubernetesIdentityUploadResolve) (entity.KubernetesIdentityUploadSnapshot, error) {
	r.calls++
	return entity.KubernetesIdentityUploadSnapshot{}, nil
}

// ClaimKubernetesIdentityUpload records forwarding without issuing external write credit.
// ClaimKubernetesIdentityUpload фиксирует forwarding без выдачи external write credit.
func (r *kubernetesSetupRepositoryFixture) ClaimKubernetesIdentityUpload(context.Context, value.Principal, command.KubernetesIdentityUploadClaim) (entity.KubernetesIdentityUploadAdmission, error) {
	r.calls++
	return entity.KubernetesIdentityUploadAdmission{}, nil
}

// CompleteKubernetesIdentityUpload records forwarding without attaching a real purpose identity.
// CompleteKubernetesIdentityUpload фиксирует forwarding без привязки реальной purpose identity.
func (r *kubernetesSetupRepositoryFixture) CompleteKubernetesIdentityUpload(context.Context, value.Principal, command.KubernetesIdentityUploadComplete) (entity.KubernetesIdentitySetupView, error) {
	r.calls++
	return entity.KubernetesIdentitySetupView{}, nil
}

// TestKubernetesSetupSeparatesPublicOwnerAndPrivateBroker refuses cross-workload authority even
// with a matching permission string and checks every new service entry before repository access.
// TestKubernetesSetupSeparatesPublicOwnerAndPrivateBroker отвергает cross-workload authority даже
// с подходящим permission string и проверяет каждый service entry до repository access.
func TestKubernetesSetupSeparatesPublicOwnerAndPrivateBroker(t *testing.T) {
	repository := &kubernetesSetupRepositoryFixture{}
	service, err := New(repository)
	if err != nil {
		t.Fatal("construct synthetic setup owner")
	}
	zero, one := int64(0), int64(1)
	mutation := value.Mutation{IdempotencyKey: "setup-synthetic-idempotency", ExpectedVersion: &zero}
	setup, attempt, hash := "ksu_synthetic_setup", "ksa_synthetic_attempt", strings.Repeat("a", 64)
	name, err := managedkubernetes.IdentitySecretName("kbi_synthetic_identity", 3)
	if err != nil {
		t.Fatal("derive synthetic private purpose name")
	}
	identity := value.ManagedKubernetesIdentityLocator{IdentityRef: "kbi_synthetic_identity", TargetRef: "kbt_synthetic_target", Revision: 3,
		Namespace: "broker-private", Name: name, UID: "18193f01-cfa5-40ad-9aac-9c0959401456", ResourceVersion: "42", ContentSHA256: hash}
	for _, test := range []struct {
		workload, permission string
		invoke               func(value.Principal) error
	}{
		{"control-api-gateway", "platform.integrations.kubernetes.setup.begin", func(p value.Principal) error {
			_, err := service.BeginKubernetesIdentitySetup(t.Context(), p, mutation, command.KubernetesIdentitySetupBegin{ConnectionRef: "con_synthetic", ExpectedConnectionVersion: 1, ContentSHA256: hash, ContentBytes: 1})
			return err
		}},
		{"control-api-gateway", "platform.integrations.kubernetes.setup.get", func(p value.Principal) error {
			_, err := service.GetKubernetesIdentitySetup(t.Context(), p, command.KubernetesIdentitySetupRead{ConnectionRef: "con_synthetic", SetupRef: setup})
			return err
		}},
		{"control-api-gateway", "platform.integrations.kubernetes.setup.cancel", func(p value.Principal) error {
			changed := mutation
			changed.ExpectedVersion = &one
			_, err := service.CancelKubernetesIdentitySetup(t.Context(), p, changed, command.KubernetesIdentitySetupCancel{ConnectionRef: "con_synthetic", SetupRef: setup})
			return err
		}},
		{"secret-broker", "platform.runtime.kubernetes.setup.resolve", func(p value.Principal) error {
			_, err := service.ResolveKubernetesIdentityUpload(t.Context(), p, command.KubernetesIdentityUploadResolve{SetupRef: setup, PermitSHA256: hash})
			return err
		}},
		{"secret-broker", "platform.runtime.kubernetes.setup.claim", func(p value.Principal) error {
			_, err := service.ClaimKubernetesIdentityUpload(t.Context(), p, command.KubernetesIdentityUploadClaim{SetupRef: setup, PermitSHA256: hash, SnapshotDigest: hash, ContentSHA256: hash, WorkloadInstance: "synthetic-broker-instance"})
			return err
		}},
		{"secret-broker", "platform.runtime.kubernetes.setup.complete", func(p value.Principal) error {
			_, err := service.CompleteKubernetesIdentityUpload(t.Context(), p, command.KubernetesIdentityUploadComplete{SetupRef: setup, AttemptRef: attempt, Generation: 1,
				Fence: "synthetic-private-fence", WorkloadInstance: "synthetic-broker-instance", Identity: &identity})
			return err
		}},
	} {
		t.Run(test.permission, func(t *testing.T) {
			p := value.Principal{ActorID: "synthetic-technical-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace",
				CredentialRevision: 1, CallerWorkload: test.workload, Permission: test.permission}
			before := repository.calls
			if err := test.invoke(p); err != nil || repository.calls != before+1 {
				t.Fatal("valid service route did not reach its owner repository")
			}
			for _, workload := range []string{"agent-runner", "integration-gateway", "control-api-gateway", "secret-broker"} {
				if workload == test.workload {
					continue
				}
				p.CallerWorkload = workload
				if err := test.invoke(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
					t.Fatal("another workload borrowed setup authority from a permission string")
				}
			}
			p.CallerWorkload, p.Permission = test.workload, "platform.runtime.kubernetes.resolve"
			if err := test.invoke(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
				t.Fatal("another operation borrowed setup authority")
			}
		})
	}
}

// TestKubernetesSetupRejectsAmbiguousReceiptsAndHidesPermit keeps two-selector replay, absent OCC
// and sensitive diagnostic formatting from becoming accepted setup commands or public metadata.
// TestKubernetesSetupRejectsAmbiguousReceiptsAndHidesPermit не допускает two-selector replay,
// absent OCC и sensitive diagnostic formatting в setup commands либо public metadata.
func TestKubernetesSetupRejectsAmbiguousReceiptsAndHidesPermit(t *testing.T) {
	repository := &kubernetesSetupRepositoryFixture{}
	service, err := New(repository)
	if err != nil {
		t.Fatal("construct synthetic setup service")
	}
	p := value.Principal{ActorID: "synthetic-technical-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1,
		CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.setup.get"}
	for _, input := range []command.KubernetesIdentitySetupRead{
		{ConnectionRef: "con_synthetic"},
		{ConnectionRef: "con_synthetic", SetupRef: "ksu_synthetic_setup", BeginIdempotencyKey: "synthetic-receipt"},
		{ConnectionRef: "con_synthetic", SetupRef: "ksu_../../foreign"},
		{ConnectionRef: "con_synthetic", BeginIdempotencyKey: "synthetic\nreceipt"},
	} {
		if _, err := service.GetKubernetesIdentitySetup(t.Context(), p, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
			t.Fatal("ambiguous receipt selector reached owner semantics")
		}
	}
	p.Permission = "platform.integrations.kubernetes.setup.begin"
	if _, err := service.BeginKubernetesIdentitySetup(t.Context(), p, value.Mutation{IdempotencyKey: "synthetic-receipt"},
		command.KubernetesIdentitySetupBegin{ConnectionRef: "con_synthetic", ExpectedConnectionVersion: 1, ContentSHA256: strings.Repeat("a", 64), ContentBytes: 1}); !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
		t.Fatal("missing OCC was treated as an explicit absent binding")
	}
	permit := []byte("synthetic-private-setup-permit")
	result := entity.KubernetesIdentitySetupAdmission{Permit: permit}
	raw, err := json.Marshal(result)
	if err != nil || strings.Contains(string(raw), "synthetic-private") || strings.Contains(fmt.Sprintf("%v %#v", result, result), string(permit)) {
		t.Fatal("owner-local permit escaped safe metadata or diagnostic formatting")
	}
}

// TestKubernetesSetupCompleteUnknownRequiresNoLocator preserves uncertain effects without fake pins.
// It checks command shape only; the real repository must still fence and retain the original intent.
// TestKubernetesSetupCompleteUnknownRequiresNoLocator сохраняет uncertain effects без фиктивных pins.
// Проверяет только форму command; реальный repository проверяет fence и хранит исходный intent.
func TestKubernetesSetupCompleteUnknownRequiresNoLocator(t *testing.T) {
	repository := &kubernetesSetupRepositoryFixture{}
	service, err := New(repository)
	if err != nil {
		t.Fatal("construct synthetic setup service")
	}
	p := value.Principal{ActorID: "synthetic-technical-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1,
		CallerWorkload: "secret-broker", Permission: "platform.runtime.kubernetes.setup.complete"}
	input := command.KubernetesIdentityUploadComplete{SetupRef: "ksu_synthetic_setup", AttemptRef: "ksa_synthetic_attempt", Generation: 1,
		Fence: "synthetic-private-fence", WorkloadInstance: "synthetic-broker-instance", Unknown: true}
	if _, err := service.CompleteKubernetesIdentityUpload(t.Context(), p, input); err != nil || repository.calls != 1 {
		t.Fatal("uncertain effect required a fabricated material locator")
	}
	for _, malformed := range []command.KubernetesIdentityUploadComplete{
		{SetupRef: input.SetupRef, AttemptRef: input.AttemptRef, Generation: 1, Fence: input.Fence, WorkloadInstance: input.WorkloadInstance},
		{SetupRef: input.SetupRef, AttemptRef: input.AttemptRef, Generation: 1, Fence: input.Fence, WorkloadInstance: input.WorkloadInstance,
			Unknown: true, Identity: &value.ManagedKubernetesIdentityLocator{}},
		{SetupRef: input.SetupRef, AttemptRef: input.AttemptRef, Generation: 1, Fence: input.Fence, WorkloadInstance: input.WorkloadInstance,
			Unknown: true, RejectedAcknowledgement: &value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: strings.Repeat("a", 64)}},
		{SetupRef: input.SetupRef, AttemptRef: input.AttemptRef, Generation: 1, Fence: input.Fence, WorkloadInstance: input.WorkloadInstance,
			RejectedAcknowledgement: &value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: strings.Repeat("a", 64), Truncated: true, ObservedUID: "2f5a4209-1ca4-45f2-9040-a14db14f78ed"}},
	} {
		if _, err := service.CompleteKubernetesIdentityUpload(t.Context(), p, malformed); !errors.Is(err, errs.ErrInvalid) || repository.calls != 1 {
			t.Fatal("missing or mixed material outcome reached the owner repository")
		}
	}
	rejected := input
	rejected.Unknown = false
	rejected.RejectedAcknowledgement = &value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: strings.Repeat("a", 64)}
	if _, err := service.CompleteKubernetesIdentityUpload(t.Context(), p, rejected); err != nil || repository.calls != 2 {
		t.Fatal("rejected successful acknowledgement required a fabricated material locator")
	}
}
