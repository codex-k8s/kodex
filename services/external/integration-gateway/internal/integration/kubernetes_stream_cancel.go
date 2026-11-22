package integration

import (
	"context"

	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// Cancel closes local exchange admission, interrupts an in-flight call and waits for its real return.
// The required callback first confirms authoritative original-owner closure. Only then is one broker
// Cancel sent under the caller's independent cleanup budget, capped at ten seconds; no reply proves remote process termination.
// The app creates that cleanup context separately from the canceled business activity. This method never resets its remaining budget.
// Cancel закрывает local exchange admission, отменяет in-flight call и ждёт его реального завершения.
// Обязательный callback сначала подтверждает authoritative original-owner closure; лишь затем один
// broker Cancel выполняется в independent cleanup budget вызывающего, не более десяти секунд. Reply не доказывает remote process termination.
// App создаёт cleanup context отдельно от отменённой business activity. Метод не начинает заново оставшийся бюджет.
func (consumer *KubernetesStreamConsumer) Cancel(ctx context.Context) (KubernetesStreamCleanup, error) {
	var empty KubernetesStreamCleanup
	if consumer == nil || ctx == nil || consumer.confirmClosure == nil {
		return empty, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	consumer.control.Lock()
	consumer.stopped = true
	if consumer.inflight != nil {
		consumer.inflight()
	}
	consumer.control.Unlock()
	cleanup, cancel := context.WithTimeout(ctx, kubernetesStreamCallTimeout)
	defer cancel()
	if !consumer.acquire(cleanup) {
		return empty, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	defer consumer.release()
	if cleanup.Err() != nil {
		return empty, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if err := consumer.confirmClosure(cleanup, proto.Clone(consumer.binding).(*sb.ManagedKubernetesInvocationBinding), consumer.descriptor.StreamRef); err != nil || cleanup.Err() != nil {
		return empty, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	response, err := consumer.broker.CancelManagedKubernetesOperation(cleanup, &sb.CancelManagedKubernetesOperationRequest{Binding: proto.Clone(consumer.binding).(*sb.ManagedKubernetesInvocationBinding), StreamRef: consumer.descriptor.StreamRef})
	if err != nil || cleanup.Err() != nil {
		return empty, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if !validKubernetesStreamCleanup(response) {
		return empty, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return KubernetesStreamCleanup{State: response.State, RemoteOutcome: response.RemoteOutcome, TransportJoined: response.TransportJoined}, nil
}

// validKubernetesStreamCleanup accepts explicit terminal facts without interpreting joined as remote success.
// Unjoined cleanup must retain UNKNOWN; it provides no accepted output or new exchange permission.
// validKubernetesStreamCleanup принимает explicit terminal facts, не считая joined remote success.
// Unjoined cleanup сохраняет UNKNOWN; accepted output либо новые exchange permissions не выдаются.
func validKubernetesStreamCleanup(response *sb.CancelManagedKubernetesOperationResponse) bool {
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.State == sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE {
		return false
	}
	switch response.State {
	case sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED:
		return response.TransportJoined && (response.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED || response.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED)
	case sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_REVOKED, sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_EXPIRED, sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_LOST:
		return response.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN || response.TransportJoined && (response.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED || response.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED)
	default:
		return false
	}
}
