package platform

import (
	"context"
	"math"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// managedKubernetesOwner verifies the technical caller independently from all transported refs.
// The repository must then resolve the originating human and complete current owner graph itself.
// managedKubernetesOwner проверяет technical caller независимо от transported refs.
// Repository затем самостоятельно разрешает originating human и полный current owner graph.
func (service *Service) managedKubernetesOwner(ctx context.Context, principal value.Principal, permission string) (platformrepo.ManagedKubernetesRepository, value.Principal, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, principal, err
	}
	if principal.CallerWorkload != "secret-broker" || principal.Permission != permission {
		return nil, principal, errs.ErrForbidden
	}
	repository, ok := service.repository.(platformrepo.ManagedKubernetesRepository)
	if !ok {
		return nil, principal, errs.ErrUnavailable
	}
	return repository, principal, nil
}

// ResolveManagedKubernetesOperation admits only a complete invocation/test locator, not an actor
// or credential chosen by a gateway request. Fresh graph validation belongs to the owner repository.
// ResolveManagedKubernetesOperation допускает complete invocation/test locator, не actor либо
// credential из gateway request. Fresh graph validation выполняет owner repository.
func (service *Service) ResolveManagedKubernetesOperation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, streamRefs ...string) (entity.ManagedKubernetesOperationSnapshot, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.resolve")
	if err != nil {
		return entity.ManagedKubernetesOperationSnapshot{}, err
	}
	if binding.Validate() != nil || len(streamRefs) > 1 || len(streamRefs) == 1 && streamRefs[0] != "" && (len(streamRefs[0]) > 96 || !managedkubernetes.ValidLocator(streamRefs[0])) {
		return entity.ManagedKubernetesOperationSnapshot{}, errs.ErrInvalid
	}
	return repository.ResolveManagedKubernetesOperation(ctx, p, binding, streamRefs...)
}

// PrepareManagedKubernetesChildren bounds discovery proposals without allowing them to create
// actor/tenant/operation authority. The repository assigns and validates every child in one transaction.
// PrepareManagedKubernetesChildren ограничивает discovery proposals, не позволяя создать
// actor/tenant/operation authority. Repository назначает и проверяет children одной transaction.
func (service *Service) PrepareManagedKubernetesChildren(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesPrepareChildren) (entity.ManagedKubernetesPlan, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.children.prepare")
	if err != nil {
		return entity.ManagedKubernetesPlan{}, err
	}
	if binding.Validate() != nil || binding.ConnectionTestRef != "" || len(input.Candidates) > 256 ||
		!managedkubernetes.ValidDigest(input.SnapshotDigest) || !managedkubernetes.ValidDigest(input.ProposalDigest) {
		return entity.ManagedKubernetesPlan{}, errs.ErrInvalid
	}
	seen := make(map[value.ManagedKubernetesResourceAddress]struct{}, len(input.Candidates))
	for _, candidate := range input.Candidates {
		if candidate.ValidateProposal() != nil {
			return entity.ManagedKubernetesPlan{}, errs.ErrInvalid
		}
		identity := candidate.Resource
		identity.UID, identity.ResourceVersion = "", ""
		if _, exists := seen[identity]; exists {
			return entity.ManagedKubernetesPlan{}, errs.ErrInvalid
		}
		seen[identity] = struct{}{}
	}
	return repository.PrepareManagedKubernetesChildren(ctx, p, binding, input)
}

// BeginManagedKubernetesEffect checks one exact owner child before any external attempt credit.
// Health tests cannot enter this effect path, including through a forged child ref.
// BeginManagedKubernetesEffect проверяет exact owner child до external attempt credit.
// Health tests не допускаются в effect path, включая forged child ref.
func (service *Service) BeginManagedKubernetesEffect(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginEffect) (entity.ManagedKubernetesEffectAdmission, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.effect.begin")
	if err != nil {
		return entity.ManagedKubernetesEffectAdmission{}, err
	}
	if binding.Validate() != nil || binding.ConnectionTestRef != "" || input.PlanRevision < 1 ||
		!managedkubernetes.ValidLocator(input.PlanRef) || !managedkubernetes.ValidLocator(input.ChildRef) ||
		!managedkubernetes.ValidDigest(input.PlanDigest) || !managedkubernetes.ValidDigest(input.ChildInputDigest) {
		return entity.ManagedKubernetesEffectAdmission{}, errs.ErrInvalid
	}
	if (input.FileSourceRef == "") != (input.FileSourceVersion == 0) || input.FileSourceVersion < 0 || input.FileSourceRef != "" && !validKubernetesSetupRef(input.FileSourceRef, "kfs_") {
		return entity.ManagedKubernetesEffectAdmission{}, errs.ErrInvalid
	}
	return repository.BeginManagedKubernetesEffect(ctx, p, binding, input)
}

// OpenManagedKubernetesStream records one already-admitted handshake and does not create a process.
// OpenManagedKubernetesStream фиксирует already-admitted handshake и не создаёт process.
func (service *Service) OpenManagedKubernetesStream(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesOpenStream) (entity.ManagedKubernetesStreamGrant, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.stream.open")
	if err != nil {
		return entity.ManagedKubernetesStreamGrant{}, err
	}
	if binding.Validate() != nil || binding.ConnectionTestRef != "" || input.EffectGeneration < 1 ||
		!managedkubernetes.ValidLocator(input.EffectRef) || !managedkubernetes.ValidLocator(input.StreamRef) ||
		!managedkubernetes.ValidDigest(input.HandshakeReceiptDigest) {
		return entity.ManagedKubernetesStreamGrant{}, errs.ErrInvalid
	}
	digest, digestErr := input.HandshakeReceipt.Digest()
	bindingDigest, bindingErr := managedkubernetes.BindingDigest(binding)
	origin := input.HandshakeReceipt.Origin
	if digestErr != nil || bindingErr != nil || digest != input.HandshakeReceiptDigest || origin.BindingDigest != bindingDigest ||
		origin.EffectRef != input.EffectRef || origin.EffectGeneration != input.EffectGeneration || origin.StreamRef != input.StreamRef || origin.StreamGeneration != binding.Generation {
		return entity.ManagedKubernetesStreamGrant{}, errs.ErrInvalid
	}
	return repository.OpenManagedKubernetesStream(ctx, p, binding, input)
}

// CompleteManagedKubernetesEffect rejects malformed or ambiguous output before owner semantic use.
// The repository also verifies the registered safe projection and exact attempt/terminal graph.
// CompleteManagedKubernetesEffect отклоняет malformed/ambiguous output до owner semantic use.
// Repository также проверяет registered safe projection и exact attempt/terminal graph.
func (service *Service) CompleteManagedKubernetesEffect(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCompleteEffect) (entity.ManagedKubernetesEffectReceipt, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.effect.complete")
	if err != nil {
		return entity.ManagedKubernetesEffectReceipt{}, err
	}
	if binding.Validate() != nil || binding.ConnectionTestRef != "" || input.EffectGeneration < 1 ||
		!managedkubernetes.ValidLocator(input.EffectRef) || !managedkubernetes.ValidDigest(input.ChildInputDigest) ||
		(input.StreamRef != "" && !managedkubernetes.ValidLocator(input.StreamRef)) ||
		managedkubernetes.ValidateEffectResult(input.ResultJSON, input.ResultDigest, string(input.Outcome), input.ReasonCode) != nil {
		return entity.ManagedKubernetesEffectReceipt{}, errs.ErrInvalid
	}

	return repository.CompleteManagedKubernetesEffect(ctx, p, binding, input)
}

// BeginManagedKubernetesFrame bounds one sequence before the owner checks exact stream credit.
// BeginManagedKubernetesFrame ограничивает sequence до owner проверки exact stream credit.
func (service *Service) BeginManagedKubernetesFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginFrame) (entity.ManagedKubernetesFrameAdmission, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.frame.begin")
	if err != nil {
		return entity.ManagedKubernetesFrameAdmission{}, err
	}
	if !managedKubernetesValidFrame(binding, input.StreamRef, input.Direction, input.Sequence, input.PayloadDigest) ||
		input.ByteCount > managedkubernetes.MaximumFrameBytes || (input.ByteCount == 0 && !input.EndOfInput) ||
		(input.EndOfInput && input.Direction != enum.KubernetesFrameInput) || !managedkubernetes.ValidChannel(input.Channel) ||
		(input.Direction == enum.KubernetesFrameOutput && input.NativeAuthorizedUntil.IsZero()) || (input.Direction == enum.KubernetesFrameInput && !input.NativeAuthorizedUntil.IsZero()) {
		return entity.ManagedKubernetesFrameAdmission{}, errs.ErrInvalid
	}
	return repository.BeginManagedKubernetesFrame(ctx, p, binding, input)
}

// CompleteManagedKubernetesFrame passes only exact frame metadata, never payload or replay authority.
// CompleteManagedKubernetesFrame передаёт exact frame metadata без payload либо replay authority.
func (service *Service) CompleteManagedKubernetesFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCompleteFrame) (uint64, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.frame.complete")
	if err != nil {
		return 0, err
	}
	if !managedKubernetesValidFrame(binding, input.StreamRef, input.Direction, input.Sequence, input.PayloadDigest) {
		return 0, errs.ErrInvalid
	}
	return repository.CompleteManagedKubernetesFrame(ctx, p, binding, input)
}

// CloseManagedKubernetesStream admits only registered states and keeps local join separate from
// remote outcome. Owner-side cancellation/revocation wins over any late completion receipt.
// CloseManagedKubernetesStream допускает только registered states, разделяя local join и
// remote outcome. Owner cancellation/revocation приоритетнее late completion receipt.
func (service *Service) CloseManagedKubernetesStream(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCloseStream) (entity.ManagedKubernetesStreamReceipt, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.stream.close")
	if err != nil {
		return entity.ManagedKubernetesStreamReceipt{}, err
	}
	knownExpected := input.ExpectedState == enum.KubernetesStreamPrepared || input.ExpectedState == enum.KubernetesStreamActive || input.ExpectedState.Terminal()
	if binding.Validate() != nil || binding.ConnectionTestRef != "" || !managedkubernetes.ValidLocator(input.StreamRef) ||
		!managedkubernetes.ValidDigest(input.TerminalReceiptDigest) || !knownExpected || !input.TerminalState.Terminal() || !managedKubernetesValidOutcome(input.RemoteOutcome) ||
		(input.TerminalState == enum.KubernetesStreamCompleted && (!input.TransportJoined || input.RemoteOutcome == enum.KubernetesEffectUnknown)) {
		return entity.ManagedKubernetesStreamReceipt{}, errs.ErrInvalid
	}
	digest, digestErr := input.TerminalReceipt.Digest()
	bindingDigest, bindingErr := managedkubernetes.BindingDigest(binding)
	origin := input.TerminalReceipt.Origin
	exitMatches := input.ObservedExitCode == nil && input.TerminalReceipt.ExitCode == nil ||
		input.ObservedExitCode != nil && input.TerminalReceipt.ExitCode != nil && *input.ObservedExitCode == *input.TerminalReceipt.ExitCode
	if digestErr != nil || bindingErr != nil || digest != input.TerminalReceiptDigest || origin.BindingDigest != bindingDigest || origin.StreamRef != input.StreamRef ||
		origin.StreamGeneration != binding.Generation || input.TerminalReceipt.TransportJoined != input.TransportJoined || !exitMatches ||
		input.TerminalReceipt.LocalState != string(input.TerminalState) || input.TerminalReceipt.RemoteOutcome != string(input.RemoteOutcome) {
		return entity.ManagedKubernetesStreamReceipt{}, errs.ErrInvalid
	}
	if input.ObservedHandshake != nil {
		observedDigest, observedErr := input.ObservedHandshake.Digest()
		if observedErr != nil || observedDigest != input.TerminalReceipt.HandshakeDigest || input.ObservedHandshake.Origin != origin || input.ObservedHandshake.Protocol != input.TerminalReceipt.Protocol {
			return entity.ManagedKubernetesStreamReceipt{}, errs.ErrInvalid
		}
	}
	return repository.CloseManagedKubernetesStream(ctx, p, binding, input)
}

// CheckManagedKubernetesOwnerReadiness uses the broker's real owner permission and repository path.
// It cannot turn a selected account, absent schema or unconfigured stream handler into readiness.
// CheckManagedKubernetesOwnerReadiness использует реальный broker owner permission и repository path.
// Не делает selected account, отсутствующую schema либо unconfigured stream handler готовыми.
func (service *Service) CheckManagedKubernetesOwnerReadiness(ctx context.Context, p value.Principal) (bool, error) {
	ready, err := service.CheckManagedKubernetesIdentityHealthReadiness(ctx, p)
	if err != nil || !ready {
		return false, err
	}
	outputRevision, err := service.CheckManagedKubernetesOutputContract(ctx, p)
	if err != nil {
		return false, err
	}
	inputRevision, err := service.CheckManagedKubernetesInputContract(ctx, p)
	if err != nil {
		return false, err
	}
	fileRevision, err := service.CheckManagedKubernetesFileInputContract(ctx, p)
	if err != nil {
		return false, err
	}
	return outputRevision == managedkubernetes.OutputContractRevision && inputRevision == managedkubernetes.InputContractRevision && fileRevision == managedkubernetes.FileInputContractRevision, nil
}

// CheckManagedKubernetesIdentityHealthReadiness retains the broker's exact core owner checks, installed ports, delivery compatibility and repository schema/operator predicate. It grants no resource execution readiness and does not inspect the optional OUTPUT publication contract.
// CheckManagedKubernetesIdentityHealthReadiness сохраняет exact core owner checks broker, installed ports, delivery compatibility и repository schema/operator predicate. Resource execution readiness не выдаётся; optional OUTPUT publication contract не проверяется.
func (service *Service) CheckManagedKubernetesIdentityHealthReadiness(ctx context.Context, p value.Principal) (bool, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.readiness")
	if err != nil {
		return false, err
	}
	if p.ProjectRef != "" {
		return false, errs.ErrForbidden
	}
	if _, ok := repository.(platformrepo.ManagedKubernetesCleanupRepository); !ok {
		return false, errs.ErrUnavailable
	}
	if _, ok := repository.(platformrepo.ManagedKubernetesInvocationCompletionRepository); !ok {
		return false, errs.ErrUnavailable
	}
	if _, ok := repository.(platformrepo.ManagedKubernetesInvocationCancellationRepository); !ok {
		return false, errs.ErrUnavailable
	}
	delivery, ok := repository.(platformrepo.ManagedIntegrationInvocationDeliveryRepository)
	if !ok {
		return false, errs.ErrUnavailable
	}
	revision, deliveryErr := delivery.ManagedIntegrationInvocationDeliveryContract(ctx, p)
	if deliveryErr != nil {
		return false, deliveryErr
	}
	if revision != 1 {
		return false, errs.ErrUnavailable
	}
	return repository.CheckManagedKubernetesOwnerReadiness(ctx, p)
}

// managedKubernetesValidOutcome excludes absent and unknown enum values from semantic receipts.
// managedKubernetesValidOutcome исключает absent/unknown enum values из semantic receipts.
func managedKubernetesValidOutcome(outcome enum.ManagedKubernetesEffectOutcome) bool {
	return outcome == enum.KubernetesEffectSucceeded || outcome == enum.KubernetesEffectFailed || outcome == enum.KubernetesEffectUnknown
}

// managedKubernetesValidFrame protects PostgreSQL sequence bounds and disallows health-test frames.
// managedKubernetesValidFrame защищает PostgreSQL sequence bounds и запрещает health-test frames.
func managedKubernetesValidFrame(binding value.ManagedKubernetesBinding, streamRef string, direction enum.ManagedKubernetesFrameDirection, sequence uint64, digest string) bool {
	return binding.Validate() == nil && binding.ConnectionTestRef == "" && managedkubernetes.ValidLocator(streamRef) &&
		(direction == enum.KubernetesFrameInput || direction == enum.KubernetesFrameOutput) &&
		sequence > 0 && sequence <= math.MaxInt64 && managedkubernetes.ValidDigest(digest)
}

// ResolveManagedKubernetesCleanup returns only monotonic closure evidence for the exact old graph.
// It never falls back to the credential-bearing normal snapshot when cleanup is unsupported.
// ResolveManagedKubernetesCleanup возвращает лишь monotonic closure evidence exact old graph.
// Не использует credential-bearing normal snapshot при unsupported cleanup.
func (service *Service) ResolveManagedKubernetesCleanup(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, streamRef string) (entity.ManagedKubernetesCleanupReceipt, error) {
	repository, p, err := service.managedKubernetesOwner(ctx, p, "platform.runtime.kubernetes.resolve")
	if err != nil {
		return entity.ManagedKubernetesCleanupReceipt{}, err
	}
	cleanup, ok := repository.(platformrepo.ManagedKubernetesCleanupRepository)
	if !ok {
		return entity.ManagedKubernetesCleanupReceipt{}, errs.ErrUnavailable
	}
	if binding.Validate() != nil || binding.ConnectionTestRef != "" || streamRef == "" || len(streamRef) > 96 || !managedkubernetes.ValidLocator(streamRef) {
		return entity.ManagedKubernetesCleanupReceipt{}, errs.ErrInvalid
	}
	return cleanup.ResolveManagedKubernetesCleanup(ctx, p, binding, streamRef)
}
