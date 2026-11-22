package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesHealthRepositoryFixture isolates health dispatch from actual owner and target authority.
// kubernetesHealthRepositoryFixture изолирует health dispatch от actual owner и target authority.
type kubernetesHealthRepositoryFixture struct {
	kubernetesSetupRepositoryFixture
}

// BeginManagedKubernetesHealthProbe counts valid dispatch without fabricating a private admission.
// BeginManagedKubernetesHealthProbe считает valid dispatch без фиктивного private admission.
func (repository *kubernetesHealthRepositoryFixture) BeginManagedKubernetesHealthProbe(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.KubernetesIdentityHealthAdmission, error) {
	repository.calls++
	return entity.KubernetesIdentityHealthAdmission{}, nil
}

// CompleteManagedKubernetesHealthProbe counts finite facts without implying native persistence.
// CompleteManagedKubernetesHealthProbe считает finite facts без утверждения native persistence.
func (repository *kubernetesHealthRepositoryFixture) CompleteManagedKubernetesHealthProbe(context.Context, value.Principal, value.ManagedKubernetesBinding, command.KubernetesIdentityHealthCompletion) (entity.KubernetesIdentityHealthReceipt, error) {
	repository.calls++
	return entity.KubernetesIdentityHealthReceipt{}, nil
}

// TestKubernetesHealthSeparatesSubjectObservationFromEffectAuthority rejects mixed/future health facts.
// Invocation and cross-workload callers cannot borrow a test to bypass destructive child admission.
// TestKubernetesHealthSeparatesSubjectObservationFromEffectAuthority отвергает mixed/future health facts.
// Invocation и cross-workload caller не используют test для обхода destructive child admission.
func TestKubernetesHealthSeparatesSubjectObservationFromEffectAuthority(t *testing.T) {
	repository := &kubernetesHealthRepositoryFixture{}
	service, err := New(repository)
	if err != nil {
		t.Fatal("construct disposable health owner")
	}
	principal := value.Principal{ActorID: "synthetic-service", AuthorityTenant: "synthetic-tenant", CredentialRevision: 1,
		CorrelationRef: "synthetic-correlation", CallerWorkload: "secret-broker", Permission: "platform.runtime.kubernetes.health.begin"}
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "worker_synthetic", ConnectionTestRef: "pct_synthetic", LeaseRef: "lea_synthetic",
		Fence: "fnc_synthetic", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	if _, err := service.BeginManagedKubernetesHealthProbe(t.Context(), principal, binding); err != nil || repository.calls != 1 {
		t.Fatal("first health required a fabricated subject digest")
	}
	for _, mutate := range []func(*value.Principal, *value.ManagedKubernetesBinding){
		func(p *value.Principal, _ *value.ManagedKubernetesBinding) { p.CallerWorkload = "integration-gateway" },
		func(p *value.Principal, _ *value.ManagedKubernetesBinding) {
			p.Permission = "platform.runtime.kubernetes.resolve"
		},
		func(p *value.Principal, _ *value.ManagedKubernetesBinding) { p.ProjectRef = "project_synthetic" },
		func(_ *value.Principal, b *value.ManagedKubernetesBinding) {
			b.InvocationRef = "inv_synthetic"
			b.ConnectionTestRef = ""
		},
	} {
		p, b := principal, binding
		mutate(&p, &b)
		if _, err := service.BeginManagedKubernetesHealthProbe(t.Context(), p, b); err == nil || repository.calls != 1 {
			t.Fatal("health authority was borrowed by another workload or effect")
		}
	}
	principal.Permission = "platform.runtime.kubernetes.health.complete"
	valid := command.KubernetesIdentityHealthCompletion{AttemptRef: "khp_synthetic", SnapshotDigest: strings.Repeat("b", 64),
		Outcome: entity.PersonalIntegrationCredentialAuthenticated, ObservedSubjectDigest: strings.Repeat("c", 64)}
	for _, change := range []func(*command.KubernetesIdentityHealthCompletion){
		func(input *command.KubernetesIdentityHealthCompletion) { input.ObservedSubjectDigest = "" },
		func(input *command.KubernetesIdentityHealthCompletion) {
			input.Outcome = entity.PersonalIntegrationCredentialVerificationUnavailable
		},
		func(input *command.KubernetesIdentityHealthCompletion) { input.Outcome = "FUTURE" },
	} {
		input := valid
		change(&input)
		if _, err := service.CompleteManagedKubernetesHealthProbe(t.Context(), principal, binding, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 1 {
			t.Fatal("ambiguous authentication fact reached owner semantics")
		}
	}
	if _, err := service.CompleteManagedKubernetesHealthProbe(t.Context(), principal, binding, valid); err != nil || repository.calls != 2 {
		t.Fatal("closed observed authentication fact did not reach its owner")
	}
}
