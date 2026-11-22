package integration

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// kubernetesOwnerTimeout bounds each metadata RPC independently from the business run deadline; it never extends an execution lease.
// kubernetesOwnerTimeout ограничивает каждый metadata RPC независимо от business run deadline; execution lease не продлевается этим бюджетом.
const kubernetesOwnerTimeout = 10 * time.Second

const (
	// kubernetesOwnerRequestRejected reports invalid local owner RPC inputs without disclosing their values.
	// kubernetesOwnerRequestRejected сообщает о неверных локальных owner RPC inputs без раскрытия значений.
	kubernetesOwnerRequestRejected = "INTEGRATION_REQUEST_REJECTED"
	// kubernetesOwnerUnavailable preserves transport uncertainty without exposing raw owner diagnostics.
	// kubernetesOwnerUnavailable сохраняет неопределённость transport без раскрытия raw owner diagnostics.
	kubernetesOwnerUnavailable = "INTEGRATION_UNAVAILABLE"
	// kubernetesOwnerResponseInvalid rejects malformed or mismatched acknowledgements before semantic use.
	// kubernetesOwnerResponseInvalid отклоняет malformed либо mismatched acknowledgements до семантического использования.
	kubernetesOwnerResponseInvalid = "INTEGRATION_RESPONSE_INVALID"
)

// ReadKubernetesInvocation reads only original-claim metadata, including after a lost completion acknowledgement; it provides no result bytes or new effect authority.
// ReadKubernetesInvocation читает только metadata исходного claim, включая потерянное completion acknowledgement; result bytes и новые полномочия на effect не выдаются.
func ReadKubernetesInvocation(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, owner cp.ManagedKubernetesOwnerServiceClient) (*cp.ManagedKubernetesInvocationReceipt, error) {
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	call, cancel := context.WithTimeout(ctx, kubernetesOwnerTimeout)
	defer cancel()
	if call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	response, err := owner.GetManagedKubernetesInvocation(call, &cp.GetManagedKubernetesInvocationRequest{Binding: wire})
	if err != nil || call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInvocationReceipt(response.Receipt, binding) {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return proto.Clone(response.Receipt).(*cp.ManagedKubernetesInvocationReceipt), nil
}

// CompleteKubernetesInvocation submits only exact previously read plan pins. The owner may return recorded completion, closed metadata or explicitly proven RUNNING/incomplete plan; none of these metadata grants new effect authority. An uncertain acknowledgement requires readback, not replay.
// CompleteKubernetesInvocation передаёт только точные ранее прочитанные plan pins. Владелец может вернуть recorded completion, closed metadata либо доказанный RUNNING/incomplete plan; metadata не выдаёт новые полномочия на effect. Неопределённое acknowledgement требует readback, не replay.
func CompleteKubernetesInvocation(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, observed *cp.ManagedKubernetesInvocationReceipt, owner cp.ManagedKubernetesOwnerServiceClient) (*cp.ManagedKubernetesInvocationReceipt, error) {
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil || !validKubernetesInvocationReceipt(observed, binding) || observed.CompletionRecorded {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	call, cancel := context.WithTimeout(ctx, kubernetesOwnerTimeout)
	defer cancel()
	if call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	response, err := owner.CompleteManagedKubernetesInvocation(call, &cp.CompleteManagedKubernetesInvocationRequest{Binding: wire, PlanRef: observed.PlanRef, PlanRevision: observed.PlanRevision, PlanDigest: observed.PlanDigest})
	if err != nil || call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInvocationReceipt(response.Receipt, binding) ||
		response.Receipt.PlanRef != observed.PlanRef || response.Receipt.PlanRevision != observed.PlanRevision || response.Receipt.PlanDigest != observed.PlanDigest || response.Receipt.SnapshotDigest != observed.SnapshotDigest {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	if response.Receipt.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING && response.Receipt.PlanRef == "" {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return proto.Clone(response.Receipt).(*cp.ManagedKubernetesInvocationReceipt), nil
}

// RenewKubernetesInvocation renews the same fence and generation before the known lease expires; lost or malformed responses never create local execution credit.
// RenewKubernetesInvocation продлевает тот же fence и generation до истечения известной lease; потерянные либо неверные ответы не создают локальный execution credit.
func RenewKubernetesInvocation(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, leaseUntil time.Time, owner cp.ManagedKubernetesOwnerServiceClient) (time.Time, error) {
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil || !leaseUntil.After(time.Now()) {
		return time.Time{}, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	deadline := time.Now().Add(kubernetesOwnerTimeout)
	if leaseUntil.Before(deadline) {
		deadline = leaseUntil
	}
	call, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if call.Err() != nil {
		return time.Time{}, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	response, err := owner.RenewManagedKubernetesInvocation(call, &cp.RenewManagedKubernetesInvocationRequest{Binding: wire})
	if err != nil || call.Err() != nil {
		return time.Time{}, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.LeaseRef != binding.LeaseRef || response.Generation != binding.Generation || response.ExpiresAt == nil || response.ExpiresAt.CheckValid() != nil || len(response.ExpiresAt.ProtoReflect().GetUnknown()) != 0 {
		return time.Time{}, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	expires := response.ExpiresAt.AsTime()
	now := time.Now()
	if !expires.After(now) || expires.After(now.Add(30*time.Second)) {
		return time.Time{}, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return expires, nil
}

// kubernetesOwnerBinding maps only a validated invocation variant without accepting a health test, unknown Proto fields or caller-provided identity selectors.
// kubernetesOwnerBinding преобразует только проверенный invocation variant без health test, неизвестных Proto fields либо переданных вызывающим identity selectors.
func kubernetesOwnerBinding(binding *sb.ManagedKubernetesInvocationBinding) (*cp.ManagedKubernetesOwnerBinding, error) {
	if binding == nil || len(binding.ProtoReflect().GetUnknown()) != 0 || binding.ConnectionTestRef != "" {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	value := managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}
	if value.Validate() != nil {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	return &cp.ManagedKubernetesOwnerBinding{WorkloadInstance: value.WorkloadInstance, InvocationRef: value.InvocationRef, LeaseRef: value.LeaseRef, Fence: value.Fence, Generation: value.Generation, InputDigest: value.InputDigest}, nil
}

// validKubernetesInvocationReceipt rejects foreign bindings, partial plan pins and false success; metadata never grants cached result delivery even when completion is recorded.
// validKubernetesInvocationReceipt отклоняет чужой binding, частичные plan pins и ложный успех; metadata не разрешает выдачу cached result даже при записанном completion.
func validKubernetesInvocationReceipt(receipt *cp.ManagedKubernetesInvocationReceipt, binding *sb.ManagedKubernetesInvocationBinding) bool {
	if receipt == nil || len(receipt.ProtoReflect().GetUnknown()) != 0 || binding == nil {
		return false
	}
	digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest})
	if err != nil || receipt.InvocationRef != binding.InvocationRef || receipt.Generation != binding.Generation || receipt.InputDigest != binding.InputDigest || receipt.BindingDigest != digest {
		return false
	}
	switch receipt.State {
	case cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING, cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_SUCCEEDED,
		cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED, cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_UNKNOWN_OUTCOME, cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED:
	default:
		return false
	}
	if receipt.PlanRef == "" {
		if receipt.PlanRevision != 0 || receipt.PlanDigest != "" || receipt.SnapshotDigest != "" || receipt.CompletionRecorded && receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED {
			return false
		}
	} else if !managedkubernetes.ValidLocator(receipt.PlanRef) || len(receipt.PlanRef) > 96 || receipt.PlanRevision < 1 || !managedkubernetes.ValidDigest(receipt.PlanDigest) || !managedkubernetes.ValidDigest(receipt.SnapshotDigest) {
		return false
	}
	switch receipt.CompletionPendingReason {
	case cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_NONE:
	case cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_CAPTURE_PENDING:
		if receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING || receipt.PlanRef == "" || receipt.CompletionRecorded || receipt.ReceiptRef != "" || receipt.ResultDigest != "" {
			return false
		}
	default:
		return false
	}
	if receipt.CompletionRecorded {
		return receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING && managedkubernetes.ValidLocator(receipt.ReceiptRef) && len(receipt.ReceiptRef) <= 96 && managedkubernetes.ValidDigest(receipt.ResultDigest)
	}
	return receipt.ReceiptRef == "" && receipt.ResultDigest == "" && receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_SUCCEEDED
}
