package kubernetesoperation

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// lookup captures publication state under the registry lock; active metadata is immutable thereafter.
// lookup читает publication state под registry lock; active metadata после этого неизменны.
func (service *StreamOperations) lookup(key string) (*streamOperation, *ActiveStream, bool) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	entry := service.entries[key]
	if entry == nil {
		return nil, nil, false
	}
	select {
	case <-entry.done:
		return entry, entry.active, true
	default:
		return entry, entry.active, false
	}
}

// reserve claims local capacity before native preparation or effect Begin, independently from owner authority.
// Terminal metadata expires locally after thirty seconds; authoritative history remains in the owner.
// reserve занимает local capacity до native preparation/effect Begin независимо от owner authority.
// Terminal metadata удаляется локально через тридцать секунд; authoritative history остаётся у owner.
func (service *StreamOperations) reserve(key string, binding managedkubernetes.Binding) (*streamOperation, func(), error) {
	release, err := service.activities.Enter()
	if err != nil {
		return nil, nil, targetrepo.ErrOperationUnavailable
	}
	service.mutex.Lock()
	defer service.mutex.Unlock()
	now := time.Now()
	for ref, entry := range service.entries {
		if !entry.finishedAt.IsZero() && now.Sub(entry.finishedAt) > 30*time.Second {
			delete(service.entries, ref)
		}
	}
	if service.lifecycle.Err() != nil || service.unjoinedTransport.Load() || service.entries[key] != nil || len(service.entries) >= service.maximum*2 {
		release()
		return nil, nil, targetrepo.ErrOperationUnavailable
	}
	entry := &streamOperation{binding: binding, done: make(chan struct{})}
	service.entries[key] = entry
	return entry, release, nil
}

// remove discards only the exact untransferred reservation, never a replacement entry.
// remove удаляет только exact untransferred reservation, не заменившую её entry.
func (service *StreamOperations) remove(key string, entry *streamOperation) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if service.entries[key] == entry {
		delete(service.entries, key)
	}
}

// monitor joins the existing stream workers, records exact terminal evidence, then releases local capacity.
// It does not reconstruct a receipt from a projected status or repeat a native handshake.
// monitor присоединяет existing stream workers, записывает exact terminal evidence и освобождает local capacity.
// Он не восстанавливает receipt из projected status и не повторяет native handshake.
func (service *StreamOperations) monitor(_ string, entry *streamOperation, release func()) {
	defer release()
	<-entry.active.done
	receipt, joined := entry.active.TerminalEvidence()
	err := targetrepo.ErrOperationUnavailable
	if !joined {
		service.unjoinedTransport.Store(true)
	}
	if joined {
		err = service.finish(entry.binding, receipt, &entry.handshake, "ACTIVE")
	}
	service.mutex.Lock()
	entry.finishedAt, entry.completionErr = time.Now(), err
	close(entry.done)
	service.mutex.Unlock()
}

// Exchange forwards only an exact locally retained stream; a missing buffer never reconnects to the target.
// Exchange передаёт только exact locally retained stream; отсутствие buffer не приводит к reconnect target.
func (service *StreamOperations) Exchange(ctx context.Context, binding managedkubernetes.Binding, streamRef string, acknowledgedOutput uint64, input *value.KubernetesStreamFrame) (value.KubernetesStreamExchange, error) {
	if service == nil || binding.Validate() != nil || !managedkubernetes.ValidLocator(streamRef) {
		return value.KubernetesStreamExchange{}, targetrepo.ErrFrameUnavailable
	}
	linked, release, err := service.requests.Begin(ctx)
	if err != nil {
		return value.KubernetesStreamExchange{}, targetrepo.ErrFrameUnavailable
	}
	defer release()
	key, _ := managedkubernetes.BindingDigest(binding)
	entry, active, _ := service.lookup(key)
	if entry == nil || active == nil || entry.binding != binding || entry.grant.Ref != streamRef {
		return value.KubernetesStreamExchange{}, targetrepo.ErrFrameUnavailable
	}
	return active.Exchange(linked, binding, acknowledgedOutput, input)
}

// Cancel closes an exact local transport only after a matching owner closure receipt, without private output.
// The joined flag proves only local reader/writer completion, never remote process termination.
// Cancel закрывает exact local transport лишь после matching owner closure receipt без private output.
// Joined flag доказывает лишь local reader/writer completion, не remote process termination.
func (service *StreamOperations) Cancel(ctx context.Context, binding managedkubernetes.Binding, streamRef string) (value.KubernetesStreamExchange, bool, error) {
	if service == nil || binding.Validate() != nil || !managedkubernetes.ValidLocator(streamRef) {
		return value.KubernetesStreamExchange{}, false, targetrepo.ErrFrameUnavailable
	}
	linked, release, err := service.requests.Begin(ctx)
	if err != nil {
		return value.KubernetesStreamExchange{}, false, targetrepo.ErrFrameUnavailable
	}
	defer release()
	key, _ := managedkubernetes.BindingDigest(binding)
	entry, active, _ := service.lookup(key)
	if entry == nil || active == nil || entry.binding != binding || entry.grant.Ref != streamRef {
		return value.KubernetesStreamExchange{}, false, targetrepo.ErrFrameUnavailable
	}
	resolver, ok := service.owner.(targetrepo.StreamCleanupResolver)
	if !ok {
		return value.KubernetesStreamExchange{}, false, targetrepo.ErrFrameUnavailable
	}
	read, cancel := context.WithTimeout(linked, outputOwnerTimeout)
	cleanup, err := resolver.ResolveKubernetesStreamCleanup(read, binding, streamRef)
	readErr := read.Err()
	cancel()
	if err != nil || readErr != nil || !validStreamCleanup(cleanup, binding, entry.handshake.Origin) {
		return value.KubernetesStreamExchange{}, false, targetrepo.ErrFrameUnavailable
	}
	active.stop("CANCELED", "CANCELED")
	select {
	case <-entry.done:
	case <-linked.Done():
		return value.KubernetesStreamExchange{}, false, targetrepo.ErrFrameUnavailable
	}
	result, err := active.snapshot()
	_, joined := active.TerminalEvidence()
	service.mutex.Lock()
	completionErr := entry.completionErr
	service.mutex.Unlock()
	return result, joined, errors.Join(err, completionErr)
}

// Close stops both request and stream admission, cancels owned work and observes positive join.
// Timeout preserves unjoined state; callers must keep dependencies alive until Joined becomes true.
// Close закрывает request/stream admission, отменяет owned work и ожидает positive join.
// Timeout сохраняет unjoined state; caller удерживает dependencies, пока Joined не станет true.
func (service *StreamOperations) Close(ctx context.Context) error {
	if service == nil || ctx == nil {
		return targetrepo.ErrOperationUnavailable
	}
	_ = service.requests.StopAdmission()
	_ = service.activities.Close()
	service.cancel()
	err := errors.Join(service.requests.Wait(ctx), service.activities.Wait(ctx))
	if service.unjoinedTransport.Load() {
		return errors.Join(err, targetrepo.ErrOperationUnavailable)
	}
	return err
}

// Joined requires every request and worker to finish and every transport to prove its positive join.
// An unresolved close error latches dependency retention for the remaining process lifetime.
// Joined требует завершения каждого request/worker и подтверждённого join каждого transport.
// Неопределённая ошибка Close удерживает зависимости до конца жизни процесса.
func (service *StreamOperations) Joined() bool {
	return service != nil && service.requests.Joined() && service.activities.Joined() && !service.unjoinedTransport.Load()
}

// Available reports only local admission health, not target permissions or a successful remote operation.
// Available сообщает лишь local admission health, не target permissions либо successful remote operation.
func (service *StreamOperations) Available() bool {
	return service != nil && !service.requests.Closed() && !service.unjoinedTransport.Load()
}

// validStreamCleanup accepts only a closed owner execution tied to the original retained native origin.
// It permits a still-active stream row when its original lease has already closed, never new execution credit.
// validStreamCleanup принимает лишь closed owner execution, связанный с original retained native origin.
// Допускает active stream row при уже закрытом original lease, но не новый execution credit.
func validStreamCleanup(receipt targetrepo.StreamCleanupReceipt, binding managedkubernetes.Binding, origin managedkubernetes.StreamReceiptOrigin) bool {
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil || !receipt.ExecutionClosed || receipt.InvocationRef != binding.InvocationRef || receipt.StreamRef != origin.StreamRef || receipt.Generation != binding.Generation || receipt.BindingDigest != digest || receipt.BindingDigest != origin.BindingDigest || receipt.SnapshotDigest != origin.SnapshotDigest {
		return false
	}
	switch receipt.StreamState {
	case "PREPARED", "ACTIVE", "COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST":
		return true
	default:
		return false
	}
}
