package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesInvocationRepository retains long operations inside the existing integration owner.
// It introduces no runtime/session storage semantics and grants no credential selection to the worker.
// ManagedKubernetesInvocationRepository сохраняет long operations в existing integration owner.
// Не вводит runtime/session storage semantics и не выдаёт worker credential selection.
type ManagedKubernetesInvocationRepository interface {
	// ClaimManagedKubernetesInvocations claims only the registered Kubernetes adapter lane.
	// ClaimManagedKubernetesInvocations захватывает лишь registered Kubernetes adapter lane.
	ClaimManagedKubernetesInvocations(context.Context, value.Principal, string, int32) ([]map[string]any, error)
	// RenewManagedKubernetesInvocation keeps the same lease after fresh full owner eligibility.
	// RenewManagedKubernetesInvocation сохраняет тот же lease после fresh full owner eligibility.
	RenewManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationLease, error)
}

// ManagedKubernetesRepository owns the complete invocation-child-stream graph, not Kubernetes I/O.
// Every command resolves trusted owner scope before matching locators, versions or idempotency.
// ManagedKubernetesRepository владеет полным invocation-child-stream graph, не Kubernetes I/O.
// Каждая команда разрешает trusted owner scope до locators, versions либо idempotency matching.
type ManagedKubernetesRepository interface {
	// ResolveManagedKubernetesOperation returns broker-only metadata after a fresh full owner-graph check.
	// ResolveManagedKubernetesOperation возвращает broker-only metadata после fresh full owner-graph check.
	ResolveManagedKubernetesOperation(context.Context, value.Principal, value.ManagedKubernetesBinding, ...string) (entity.ManagedKubernetesOperationSnapshot, error)
	// PrepareManagedKubernetesChildren binds one immutable finite plan to its originating claim and proposal.
	// PrepareManagedKubernetesChildren связывает immutable finite plan с originating claim и proposal.
	PrepareManagedKubernetesChildren(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesPrepareChildren) (entity.ManagedKubernetesPlan, error)
	// BeginManagedKubernetesEffect persists the first exact attempt before releasing external credit.
	// BeginManagedKubernetesEffect фиксирует first exact attempt до выдачи external credit.
	BeginManagedKubernetesEffect(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesBeginEffect) (entity.ManagedKubernetesEffectAdmission, error)
	// OpenManagedKubernetesStream activates only the already-admitted stream and exact handshake receipt.
	// OpenManagedKubernetesStream активирует только already-admitted stream и exact handshake receipt.
	OpenManagedKubernetesStream(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesOpenStream) (entity.ManagedKubernetesStreamGrant, error)
	// CompleteManagedKubernetesEffect retains partial or late evidence without reviving closed authority.
	// CompleteManagedKubernetesEffect сохраняет partial/late evidence без восстановления closed authority.
	CompleteManagedKubernetesEffect(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCompleteEffect) (entity.ManagedKubernetesEffectReceipt, error)
	// BeginManagedKubernetesFrame issues one bounded sequence credit after fresh dependency admission.
	// BeginManagedKubernetesFrame выдаёт один bounded sequence credit после fresh dependency admission.
	BeginManagedKubernetesFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesBeginFrame) (entity.ManagedKubernetesFrameAdmission, error)
	// CompleteManagedKubernetesFrame persists an exact ACK without retrying a remote write.
	// CompleteManagedKubernetesFrame сохраняет exact ACK без повторной remote write.
	CompleteManagedKubernetesFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCompleteFrame) (uint64, error)
	// CloseManagedKubernetesStream atomically closes all remaining frame and stream grants.
	// CloseManagedKubernetesStream атомарно закрывает оставшиеся frame и stream grants.
	CloseManagedKubernetesStream(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCloseStream) (entity.ManagedKubernetesStreamReceipt, error)
	// CheckManagedKubernetesOwnerReadiness checks installed schema and the same owner permission path.
	// CheckManagedKubernetesOwnerReadiness проверяет installed schema и тот же owner permission path.
	CheckManagedKubernetesOwnerReadiness(context.Context, value.Principal) (bool, error)
}

// ManagedKubernetesCleanupRepository reads exact closed-graph evidence independently of live source
// credentials. The sole operation never mutates state or issues target/output/frame authority.
// ManagedKubernetesCleanupRepository читает exact closed-graph evidence независимо от live source
// credentials. Единственная operation не меняет state и не выдаёт target/output/frame authority.
type ManagedKubernetesCleanupRepository interface {
	// ResolveManagedKubernetesCleanup verifies the original binding and closed stream or lease.
	// ResolveManagedKubernetesCleanup проверяет original binding и closed stream либо lease.
	ResolveManagedKubernetesCleanup(context.Context, value.Principal, value.ManagedKubernetesBinding, string) (entity.ManagedKubernetesCleanupReceipt, error)
}

// ManagedKubernetesInvocationCompletionRepository closes and reads the existing owner graph.
// The worker cannot provide a result or outcome; all accepted child evidence remains server-derived.
// ManagedKubernetesInvocationCompletionRepository закрывает и читает existing owner graph.
// Worker не передаёт result либо outcome; accepted child evidence выводится только сервером.
type ManagedKubernetesInvocationCompletionRepository interface {
	// CompleteManagedKubernetesInvocation finalizes only an exact complete stored graph or proven no-effect case.
	// CompleteManagedKubernetesInvocation завершает лишь exact complete stored graph либо proven no-effect case.
	CompleteManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesCompleteInvocation) (entity.ManagedKubernetesInvocationReceipt, error)
	// GetManagedKubernetesInvocation reads only claim-bound metadata without a new claim or output credit.
	// GetManagedKubernetesInvocation читает лишь claim-bound metadata без new claim либо output credit.
	GetManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error)
}

// ManagedIntegrationInvocationDeliveryRepository reports the common typed worker contract only.
// Its revision proves capacity/metadata/finalization schema support, never actor or target authority.
// ManagedIntegrationInvocationDeliveryRepository сообщает лишь common typed worker contract.
// Revision доказывает capacity/metadata/finalization schema support, не actor либо target authority.
type ManagedIntegrationInvocationDeliveryRepository interface {
	// ManagedIntegrationInvocationDeliveryContract stays independent from configured optional targets.
	// ManagedIntegrationInvocationDeliveryContract не зависит от configured optional targets.
	ManagedIntegrationInvocationDeliveryContract(context.Context, value.Principal) (uint32, error)
}

// ManagedKubernetesInvocationCancellationRepository stops only an exact original execution graph.
// It cannot cancel another claim, a whole Run, or issue credentials, output or replacement work.
// ManagedKubernetesInvocationCancellationRepository останавливает лишь exact original execution graph.
// Не отменяет другой claim либо весь Run, не выдаёт credentials/output/replacement work.
type ManagedKubernetesInvocationCancellationRepository interface {
	CancelManagedKubernetesInvocation(context.Context, value.Principal, value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error)
}
