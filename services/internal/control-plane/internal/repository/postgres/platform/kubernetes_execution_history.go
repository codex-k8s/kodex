package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesExecutionCleanup reads only monotonic closure metadata for the exact original claim.
// It does not resolve current credentials, call a provider, mutate state or create execution credit.
// queryKubernetesExecutionCleanup читает лишь monotonic closure metadata exact original claim.
// Не разрешает current credentials, не вызывает provider и не создаёт state либо execution credit.
//
//go:embed sql/kubernetes_execution__cleanup.sql
var queryKubernetesExecutionCleanup string

// ResolveManagedKubernetesCleanup authorizes stopping one already closed original stream or lease.
// A single statement observes the old immutable graph; no current grant, credential or result is returned.
// ResolveManagedKubernetesCleanup разрешает остановку already closed original stream либо lease.
// Один statement читает old immutable graph без current grant, credential либо result в ответе.
func (repository *Repository) ResolveManagedKubernetesCleanup(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, streamRef string) (result entity.ManagedKubernetesCleanupReceipt, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.resolve"); err != nil {
		return result, err
	}
	if !managedkubernetes.ValidLocator(streamRef) || len(streamRef) > 96 {
		return result, errs.ErrInvalid
	}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "input_digest": binding.InputDigest, "generation": binding.Generation, "binding_digest": digest, "stream_ref": streamRef, "lease_ref": binding.LeaseRef, "fence_digest": kubernetesHealthFenceDigest(binding.Fence), "instance": binding.WorkloadInstance}
	err = repository.pool.QueryRow(ctx, queryKubernetesExecutionCleanup, args).Scan(&result.InvocationRef, &result.StreamRef, &result.Generation, &result.BindingDigest, &result.SnapshotDigest, &result.State, &result.ExecutionClosed)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedKubernetesCleanupReceipt{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.ManagedKubernetesCleanupReceipt{}, errs.ErrUnavailable
	}
	if result.InvocationRef != binding.InvocationRef || result.StreamRef != streamRef || result.Generation != binding.Generation || result.BindingDigest != digest || !managedkubernetes.ValidDigest(result.SnapshotDigest) || !kubernetesHistoryStateKnown(result.State) {
		return entity.ManagedKubernetesCleanupReceipt{}, errs.ErrUnavailable
	}
	if !result.ExecutionClosed {
		return entity.ManagedKubernetesCleanupReceipt{}, errs.ErrForbidden
	}
	return result, nil
}

// kubernetesHistoryStateKnown rejects unregistered states even when a closed boolean is present.
// kubernetesHistoryStateKnown отклоняет unregistered states даже с closed boolean.
func kubernetesHistoryStateKnown(state enum.ManagedKubernetesStreamState) bool {
	return state == enum.KubernetesStreamPrepared || state == enum.KubernetesStreamActive || state.Terminal()
}

// history preserves original typed receipts and independently checks their persisted digests.
// Expired deadlines and owner terminal narrowing never invent a native handshake or remote success.
// history сохраняет original typed receipts и независимо сверяет persisted digests.
// Expired deadline и owner terminal narrowing не выдумывают native handshake либо remote success.
func (record kubernetesStreamRecord) history(binding value.ManagedKubernetesBinding, streamRef string) (*entity.ManagedKubernetesStreamHistory, error) {
	if record.generation != binding.Generation {
		return nil, errs.ErrUnavailable
	}
	return record.historyFromOrigin(record.origin(binding, streamRef))
}

// historyFromOrigin validates the same closed receipt parser for independently sealed capture pins.
// The origin must come from an owner record; this helper creates no execution or disclosure grant.
// historyFromOrigin использует тот же closed receipt parser для sealed capture pins владельца.
// Origin берётся из owner record; helper не создаёт execution либо disclosure grant.
func (record kubernetesStreamRecord) historyFromOrigin(origin managedkubernetes.StreamReceiptOrigin) (*entity.ManagedKubernetesStreamHistory, error) {
	if !kubernetesHistoryStateKnown(record.state) || record.hardDeadline.IsZero() || origin.Validate() != nil || origin != record.originFromDigest(origin.BindingDigest, origin.StreamRef) {
		return nil, errs.ErrUnavailable
	}
	result := &entity.ManagedKubernetesStreamHistory{Ref: origin.StreamRef, Generation: record.generation, State: record.state, HardDeadline: record.hardDeadline}
	if (record.handshakeDigest == "") != (len(record.handshakeJSON) == 0) || (record.terminalDigest == "") != (len(record.terminalJSON) == 0) {
		return nil, errs.ErrUnavailable
	}
	if record.handshakeDigest != "" {
		var receipt managedkubernetes.HandshakeReceipt
		if len(record.handshakeJSON) > 8192 || internalrpcauth.DecodeStrictJSON(record.handshakeJSON, &receipt) != nil || !record.acceptsHandshakeOrigin(receipt, origin) {
			return nil, errs.ErrUnavailable
		}
		hash, err := receipt.Digest()
		if err != nil || hash != record.handshakeDigest {
			return nil, errs.ErrUnavailable
		}
		result.Handshake = &receipt
	}
	if record.terminalDigest != "" {
		var receipt managedkubernetes.TerminalReceipt
		if len(record.terminalJSON) > 8192 || internalrpcauth.DecodeStrictJSON(record.terminalJSON, &receipt) != nil || receipt.Origin != origin || !record.state.Terminal() || record.state == enum.KubernetesStreamCompleted && receipt.LocalState != "COMPLETED" || receipt.RemoteOutcome != string(record.remoteOutcome) || receipt.HandshakeDigest != record.handshakeDigest || receipt.Protocol != record.expectedProtocol() {
			return nil, errs.ErrUnavailable
		}
		hash, err := receipt.Digest()
		if err != nil || hash != record.terminalDigest {
			return nil, errs.ErrUnavailable
		}
		result.Terminal = &receipt
	}
	return result, nil
}
