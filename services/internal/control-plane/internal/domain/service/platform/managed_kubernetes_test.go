package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// managedKubernetesRepositoryFixture isolates pre-repository security guards without real I/O.
// Embedded unimplemented ports must never be reached by negative-input tests.
// managedKubernetesRepositoryFixture изолирует pre-repository security guards без реального I/O.
// Negative-input tests не должны достигать embedded unimplemented ports.
type managedKubernetesRepositoryFixture struct {
	platformrepo.Repository
	platformrepo.ManagedKubernetesRepository
	calls int
}

// ResolvePrincipal preserves the synthetic verified caller while leaving resource authority unmodeled.
// ResolvePrincipal сохраняет synthetic verified caller, не моделируя resource authority.
func (*managedKubernetesRepositoryFixture) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}

// ResolveManagedKubernetesOperation records forwarding only; it supplies no grant or credential material.
// ResolveManagedKubernetesOperation фиксирует только forwarding без grant либо credential material.
func (repository *managedKubernetesRepositoryFixture) ResolveManagedKubernetesOperation(context.Context, value.Principal, value.ManagedKubernetesBinding, ...string) (entity.ManagedKubernetesOperationSnapshot, error) {
	repository.calls++
	return entity.ManagedKubernetesOperationSnapshot{}, nil
}

// PrepareManagedKubernetesChildren records an accepted proposal without assigning real child effects.
// PrepareManagedKubernetesChildren фиксирует accepted proposal без реальных child effects.
func (repository *managedKubernetesRepositoryFixture) PrepareManagedKubernetesChildren(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesPrepareChildren) (entity.ManagedKubernetesPlan, error) {
	repository.calls++
	return entity.ManagedKubernetesPlan{}, nil
}

// BeginManagedKubernetesEffect records forwarding without issuing external credit in the fixture.
// BeginManagedKubernetesEffect фиксирует forwarding без external credit в fixture.
func (repository *managedKubernetesRepositoryFixture) BeginManagedKubernetesEffect(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesBeginEffect) (entity.ManagedKubernetesEffectAdmission, error) {
	repository.calls++
	return entity.ManagedKubernetesEffectAdmission{}, nil
}

// CompleteManagedKubernetesEffect records grammatical admission only; PG tests own result semantics.
// CompleteManagedKubernetesEffect фиксирует только grammatical admission; result semantics проверяют PG tests.
func (repository *managedKubernetesRepositoryFixture) CompleteManagedKubernetesEffect(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCompleteEffect) (entity.ManagedKubernetesEffectReceipt, error) {
	repository.calls++
	return entity.ManagedKubernetesEffectReceipt{}, nil
}

// BeginManagedKubernetesFrame records forwarding without granting a real remote write.
// BeginManagedKubernetesFrame фиксирует forwarding без grant реальной remote write.
func (repository *managedKubernetesRepositoryFixture) BeginManagedKubernetesFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesBeginFrame) (entity.ManagedKubernetesFrameAdmission, error) {
	repository.calls++
	return entity.ManagedKubernetesFrameAdmission{}, nil
}

// CloseManagedKubernetesStream records a typed report without claiming remote process termination.
// CloseManagedKubernetesStream фиксирует typed report без заявления об остановке remote process.
func (repository *managedKubernetesRepositoryFixture) CloseManagedKubernetesStream(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCloseStream) (entity.ManagedKubernetesStreamReceipt, error) {
	repository.calls++
	return entity.ManagedKubernetesStreamReceipt{}, nil
}

// managedKubernetesServiceFixture creates valid transport metadata, never owner resource authority.
// managedKubernetesServiceFixture создаёт valid transport metadata, не owner resource authority.
func managedKubernetesServiceFixture(t *testing.T, permission string) (*Service, *managedKubernetesRepositoryFixture, value.Principal, value.ManagedKubernetesBinding) {
	t.Helper()
	repository := &managedKubernetesRepositoryFixture{}
	service, err := New(repository)
	if err != nil {
		t.Fatal("create isolated owner service fixture")
	}
	principal := value.Principal{ActorID: "synthetic-broker", AuthorityTenant: "synthetic-tenant", Permission: permission,
		CorrelationRef: "synthetic-trace", CallerWorkload: "secret-broker", CredentialRevision: 1}
	binding := value.ManagedKubernetesBinding{WorkloadInstance: "synthetic-worker", InvocationRef: "inv_synthetic", LeaseRef: "lea_synthetic",
		Fence: "synthetic-fence-do-not-print", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	return service, repository, principal, binding
}

// TestManagedKubernetesRejectsWrongTechnicalCaller proves valid-looking locators never select authority.
// TestManagedKubernetesRejectsWrongTechnicalCaller доказывает, что valid-looking locators не выбирают authority.
func TestManagedKubernetesRejectsWrongTechnicalCaller(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.resolve")
	for _, mutate := range []func(*value.Principal){
		func(principal *value.Principal) { principal.CallerWorkload = "integration-gateway" },
		func(principal *value.Principal) { principal.CallerWorkload = "runtime-controller" },
		func(principal *value.Principal) { principal.Permission = "platform.runtime.kubernetes.effect.begin" },
	} {
		changed := principal
		mutate(&changed)
		if _, err := service.ResolveManagedKubernetesOperation(context.Background(), changed, binding); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("wrong caller or permission reached the Kubernetes owner port")
		}
	}
	if repository.calls != 0 {
		t.Fatal("forbidden technical caller reached resource resolution")
	}
	if _, err := service.ResolveManagedKubernetesOperation(context.Background(), principal, binding); err != nil || repository.calls != 1 {
		t.Fatal("correct technical caller did not reach owner resource checks")
	}
}

// TestManagedKubernetesHealthClaimCannotBecomeEffect prevents reusing a READ test claim for writes.
// TestManagedKubernetesHealthClaimCannotBecomeEffect запрещает reuse READ test claim для writes.
func TestManagedKubernetesHealthClaimCannotBecomeEffect(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.effect.begin")
	binding.InvocationRef, binding.ConnectionTestRef = "", "ict_synthetic"
	input := command.ManagedKubernetesBeginEffect{PlanRef: "kpl_synthetic", PlanRevision: 1, ChildRef: "kch_synthetic",
		PlanDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64)}
	if _, err := service.BeginManagedKubernetesEffect(context.Background(), principal, binding, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
		t.Fatal("health-test binding acquired effect credit")
	}
}

// TestManagedKubernetesProposalCannotDuplicateIdentity rejects conflicting metadata for one resource.
// TestManagedKubernetesProposalCannotDuplicateIdentity отклоняет conflicting metadata одного resource.
func TestManagedKubernetesProposalCannotDuplicateIdentity(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.children.prepare")
	candidate := command.ManagedKubernetesChildCandidate{ObservationState: managedkubernetes.ObservationPresent, Resource: value.ManagedKubernetesResourceAddress{Version: "v1", Resource: "pods", Namespaced: true,
		Namespace: "selected", Name: "selected", UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37"},
		ObservedResourceDigest: strings.Repeat("b", 64), RegisteredSchemaDigest: strings.Repeat("c", 64)}
	changed := candidate
	changed.Resource.UID, changed.Resource.ResourceVersion = "7fb0912f-a690-453f-be9a-3520d2e99a85", "38"
	input := command.ManagedKubernetesPrepareChildren{SnapshotDigest: strings.Repeat("a", 64), ProposalDigest: strings.Repeat("d", 64),
		Candidates: []command.ManagedKubernetesChildCandidate{candidate, changed}}
	if _, err := service.PrepareManagedKubernetesChildren(context.Background(), principal, binding, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
		t.Fatal("one resource appeared twice under conflicting immutable pins")
	}
}

// TestManagedKubernetesUnboundPodProposal reaches the owner only as a proposal, never final authority.
// TestManagedKubernetesUnboundPodProposal передаёт owner только proposal, не окончательную authority.
func TestManagedKubernetesUnboundPodProposal(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.children.prepare")
	candidate := command.ManagedKubernetesChildCandidate{ObservationState: managedkubernetes.ObservationPresent,
		Resource:               value.ManagedKubernetesResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "selected", Name: "selected", UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37"},
		ObservedResourceDigest: strings.Repeat("b", 64), RegisteredSchemaDigest: strings.Repeat("c", 64),
		PodWitness: &managedkubernetes.PodNodeWitness{NodeName: "node-one", Container: "worker"}}
	input := command.ManagedKubernetesPrepareChildren{SnapshotDigest: strings.Repeat("a", 64), Candidates: []command.ManagedKubernetesChildCandidate{candidate}}
	var err error
	input.ProposalDigest, err = managedkubernetes.ChildProposalDigest(input.SnapshotDigest, input.Candidates)
	if err != nil || candidate.Validate() == nil {
		t.Fatal("unbound proposal was treated as final authority")
	}
	if _, err = service.PrepareManagedKubernetesChildren(t.Context(), principal, binding, input); err != nil || repository.calls != 1 {
		t.Fatal("valid unbound proposal did not reach owner enrichment")
	}
	candidate.PodWitness.NodeUID = "invalid"
	if _, err = service.PrepareManagedKubernetesChildren(t.Context(), principal, binding, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 1 {
		t.Fatal("invalid supplied node identity reached owner")
	}
}

// TestManagedKubernetesResultRejectsAmbiguityAndRawDiagnostics closes parser and receipt error paths.
// TestManagedKubernetesResultRejectsAmbiguityAndRawDiagnostics закрывает parser/receipt error paths.
func TestManagedKubernetesResultRejectsAmbiguityAndRawDiagnostics(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.effect.complete")
	for _, test := range []struct {
		raw, reason string
		outcome     enum.ManagedKubernetesEffectOutcome
	}{
		{`{"ok":false,"ok":true}`, "", enum.KubernetesEffectSucceeded},
		{`{} {}`, "", enum.KubernetesEffectSucceeded},
		{`[]`, "", enum.KubernetesEffectSucceeded},
		{`null`, "", enum.KubernetesEffectSucceeded},
		{`{}`, "private upstream diagnostic", enum.KubernetesEffectFailed},
		{`{}`, "KUBERNETES_TIMEOUT", enum.KubernetesEffectSucceeded},
		{`{}`, "KUBERNETES_TIMEOUT", "CALLER_DEFINED"},
	} {
		digest := sha256.Sum256([]byte(test.raw))
		input := command.ManagedKubernetesCompleteEffect{EffectRef: "kef_synthetic", EffectGeneration: 1,
			ChildInputDigest: strings.Repeat("a", 64), ResultDigest: hex.EncodeToString(digest[:]), ResultJSON: []byte(test.raw),
			Outcome: test.outcome, ReasonCode: test.reason}
		if _, err := service.CompleteManagedKubernetesEffect(context.Background(), principal, binding, input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("ambiguous result or raw upstream diagnostic reached owner semantics")
		}
	}
	if repository.calls != 0 {
		t.Fatal("invalid result was persisted through the owner port")
	}
}

// TestManagedKubernetesFrameAndCloseDoNotInventCreditOrRemoteStop rejects unknown channels/states
// and a completed claim that has no local join or known remote outcome.
// TestManagedKubernetesFrameAndCloseDoNotInventCreditOrRemoteStop отклоняет unknown channels/states
// и completed claim без local join либо known remote outcome.
func TestManagedKubernetesFrameAndCloseDoNotInventCreditOrRemoteStop(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.frame.begin")
	frame := command.ManagedKubernetesBeginFrame{StreamRef: "kst_synthetic", Sequence: 1, Direction: enum.KubernetesFrameInput,
		ByteCount: 1, PayloadDigest: strings.Repeat("b", 64), Channel: "RAW_HTTP"}
	if _, err := service.BeginManagedKubernetesFrame(context.Background(), principal, binding, frame); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("caller-defined stream channel acquired credit")
	}
	frame.Channel = "STDOUT"
	frame.Direction = enum.KubernetesFrameOutput
	if _, err := service.BeginManagedKubernetesFrame(context.Background(), principal, binding, frame); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("OUTPUT without native witness reached owner")
	}
	frame.Direction = enum.KubernetesFrameInput
	frame.Channel = "STDIN"
	frame.NativeAuthorizedUntil = time.Now().Add(time.Second)
	if _, err := service.BeginManagedKubernetesFrame(context.Background(), principal, binding, frame); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("INPUT accepted output-only witness")
	}
	principal.Permission = "platform.runtime.kubernetes.stream.close"
	closeInput := command.ManagedKubernetesCloseStream{StreamRef: "kst_synthetic", ExpectedState: enum.KubernetesStreamActive,
		TerminalState: enum.KubernetesStreamCompleted, RemoteOutcome: enum.KubernetesEffectUnknown, TransportJoined: true,
		TerminalReceiptDigest: strings.Repeat("a", 64)}
	if _, err := service.CloseManagedKubernetesStream(context.Background(), principal, binding, closeInput); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("local transport join invented a proven completed remote outcome")
	}
	closeInput.RemoteOutcome, closeInput.TransportJoined = enum.KubernetesEffectSucceeded, false
	if _, err := service.CloseManagedKubernetesStream(context.Background(), principal, binding, closeInput); !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
		t.Fatal("unjoined transport became a completed stream")
	}
}

// TestManagedKubernetesProposalPreservesAbsentAndCollection reaches the owner port only with
// explicit observation shape; the port must still bind it to the allowed original operation.
// TestManagedKubernetesProposalPreservesAbsentAndCollection достигает owner port только с
// explicit observation shape; port отдельно связывает её с допустимой исходной operation.
func TestManagedKubernetesProposalPreservesAbsentAndCollection(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.children.prepare")
	candidate := command.ManagedKubernetesChildCandidate{ObservationState: managedkubernetes.ObservationAbsent,
		Resource:               value.ManagedKubernetesResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "selected", Name: "new-object"},
		ObservedResourceDigest: strings.Repeat("b", 64), RegisteredSchemaDigest: strings.Repeat("c", 64)}
	input := command.ManagedKubernetesPrepareChildren{SnapshotDigest: strings.Repeat("a", 64), ProposalDigest: strings.Repeat("d", 64), Candidates: []command.ManagedKubernetesChildCandidate{candidate}}
	if _, err := service.PrepareManagedKubernetesChildren(context.Background(), principal, binding, input); err != nil || repository.calls != 1 {
		t.Fatal("explicit absent proposal failed shape admission", err)
	}
	candidate.ObservationState, candidate.Resource.Name = managedkubernetes.ObservationCollection, ""
	input.Candidates[0] = candidate
	if _, err := service.PrepareManagedKubernetesChildren(context.Background(), principal, binding, input); err != nil || repository.calls != 2 {
		t.Fatal("explicit scoped collection failed shape admission", err)
	}
	for _, invalid := range []managedkubernetes.ObservationState{"", "UNKNOWN", managedkubernetes.ObservationPresent} {
		input.Candidates[0].ObservationState = invalid
		if _, err := service.PrepareManagedKubernetesChildren(context.Background(), principal, binding, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 2 {
			t.Fatal("missing or fabricated presence reached owner port", err)
		}
	}
}

// TestManagedKubernetesCleanupCannotFallBackToCurrentSnapshot refuses unsupported cleanup reads.
// A correctly authenticated old claim never causes normal credential-bearing resolution.
// TestManagedKubernetesCleanupCannotFallBackToCurrentSnapshot отклоняет unsupported cleanup read.
// Корректно authenticated old claim не вызывает normal credential-bearing resolution.
func TestManagedKubernetesCleanupCannotFallBackToCurrentSnapshot(t *testing.T) {
	service, repository, principal, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.resolve")
	if _, err := service.ResolveManagedKubernetesCleanup(t.Context(), principal, binding, "kst_cleanup"); !errors.Is(err, errs.ErrUnavailable) || repository.calls != 0 {
		t.Fatal("unsupported cleanup fell back to current resource resolution")
	}
	principal.Permission = "platform.runtime.kubernetes.effect.begin"
	if _, err := service.ResolveManagedKubernetesCleanup(t.Context(), principal, binding, "kst_cleanup"); !errors.Is(err, errs.ErrForbidden) || repository.calls != 0 {
		t.Fatal("wrong owner method reached cleanup resolution")
	}
}
