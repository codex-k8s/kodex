package kubernetesoperation

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// StreamOperations owns a bounded process-local registry, never durable authority or a replay journal.
// Worker lifecycle is explicit; private transports and their completion workers join before dependencies close.
// StreamOperations владеет bounded process-local registry, не durable authority либо replay journal.
// Worker lifecycle явный; private transports и completion workers присоединяются до закрытия dependencies.
type StreamOperations struct {
	owner             targetrepo.StreamPlanOwner
	native            targetrepo.PodStreamPreparer
	lifecycle         context.Context
	cancel            context.CancelFunc
	requests          *serviceruntime.RequestLifecycle
	activities        *serviceruntime.ActivityGate
	maximum           int
	unjoinedTransport atomic.Bool
	mutex             sync.Mutex
	entries           map[string]*streamOperation
}

// streamOperation keeps one exact binding and private transport; terminal entries retain no output payload.
// streamOperation хранит exact binding и private transport; terminal entries не сохраняют output payload.
type streamOperation struct {
	binding       managedkubernetes.Binding
	effect        targetrepo.ResourceExecution
	active        *ActiveStream
	access        *CurrentStreamAccess
	grant         value.KubernetesStreamGrant
	handshake     managedkubernetes.HandshakeReceipt
	done          chan struct{}
	finishedAt    time.Time
	completionErr error
}

// NewStreamOperations requires a caller-owned application lifecycle and finite local capacity.
// It starts no effects; Close must be called even when application startup later fails.
// NewStreamOperations требует caller-owned application lifecycle и конечную local capacity.
// Effects не запускаются; Close обязателен также при последующей ошибке startup.
func NewStreamOperations(lifecycle context.Context, owner targetrepo.StreamPlanOwner, native targetrepo.PodStreamPreparer, maximum int) (*StreamOperations, error) {
	if lifecycle == nil || lifecycle.Err() != nil || owner == nil || native == nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	gate, err := serviceruntime.NewActivityGate(maximum)
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	ctx, cancel := context.WithCancel(lifecycle)
	requests, err := serviceruntime.NewRequestLifecycle(ctx, maximum)
	if err != nil {
		cancel()
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &StreamOperations{owner: owner, native: native, lifecycle: ctx, cancel: cancel, requests: requests, activities: gate, maximum: maximum, entries: make(map[string]*streamOperation)}, nil
}

// Execute reads an existing owner plan before private material and dispatches only one exact fresh child.
// Missing local transports under ATTEMPTING remain reconciliation-only; no reconnect or command replay occurs.
// Execute читает existing owner plan до private material и запускает только exact fresh child.
// Отсутствующий local transport при ATTEMPTING остаётся reconciliation-only без reconnect/command replay.
func (service *StreamOperations) Execute(ctx context.Context, binding managedkubernetes.Binding) (targetrepo.OperationExecution, error) {
	var empty targetrepo.OperationExecution
	if service == nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return empty, targetrepo.ErrOperationUnavailable
	}
	linked, release, err := service.requests.Begin(ctx)
	if err != nil {
		return empty, targetrepo.ErrOperationUnavailable
	}
	defer release()
	ctx = linked
	current, err := service.resolve(ctx, binding)
	if err != nil || (current.Operation != "kubernetes.watch" && current.Operation != "kubernetes.watch.all-namespaces" && current.Operation != "kubernetes.logs" && current.Operation != "kubernetes.exec" && current.Operation != "kubernetes.attach" && current.Operation != "kubernetes.port-forward" && current.Operation != "kubernetes.cp.upload" && current.Operation != "kubernetes.cp.download") {
		return empty, targetrepo.ErrOperationUnavailable
	}
	key, _ := managedkubernetes.BindingDigest(binding)
	if entry, active, finished := service.lookup(key); entry != nil {
		if active == nil && !finished {
			return empty, targetrepo.ErrOperationUnavailable
		}
		if active != nil && !finished {
			if _, err := entry.access.AuthorizeStream(ctx, binding, entry.handshake.Origin); err != nil {
				return empty, targetrepo.ErrOperationUnavailable
			}
			grant := cloneStreamGrant(entry.grant)
			return targetrepo.OperationExecution{Effect: entry.effect, Stream: &grant}, nil
		}
	}
	var candidates []managedkubernetes.ChildCandidate
	if current.PlanRef == "" {
		candidate, err := service.native.ObserveKubernetesPod(ctx, current)
		if err != nil || candidate.ValidateProposal() != nil {
			return empty, targetrepo.ErrOperationUnavailable
		}
		candidates = []managedkubernetes.ChildCandidate{candidate}
		fresh, err := service.resolve(ctx, binding)
		before, valid := streamOwnerReadPin(current)
		after, afterValid := streamOwnerReadPin(fresh)
		if err != nil || !valid || !afterValid || before != after {
			return empty, targetrepo.ErrOperationUnavailable
		}
		current = fresh
		if current.PlanRef != "" {
			candidates = nil
		}
	}
	planCtx, finish := context.WithTimeout(ctx, outputOwnerTimeout)
	plan, err := service.owner.PrepareKubernetesChildren(planCtx, binding, current, candidates)
	planErr := planCtx.Err()
	finish()
	if err != nil || planErr != nil || len(plan.Children) != 1 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	child := plan.Children[0]
	if !validPodStreamChild(plan, child, current) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	before, valid := streamOwnerReadPin(current)
	current, err = service.resolve(ctx, binding)
	after, afterValid := streamOwnerReadPin(current)
	if err != nil || !valid || !afterValid || before != after || current.PlanRef != plan.Ref || current.PlanDigest != plan.Digest || current.PlanRevision != plan.Revision {
		return empty, targetrepo.ErrOperationUnavailable
	}
	switch child.State {
	case "ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED":
		return service.reconcile(ctx, binding, current, plan, child)
	case "READY", "WAITING_OWNER":
	default:
		return empty, targetrepo.ErrOperationUnavailable
	}
	entry, workerRelease, err := service.reserve(key, binding)
	if err != nil {
		return empty, err
	}
	transferred := false
	defer func() {
		if !transferred {
			service.remove(key, entry)
			workerRelease()
		}
	}()
	var fileSource targetrepo.FileInputPreparation
	if child.Operation == "kubernetes.cp.upload" {
		fileOwner, ok := service.owner.(targetrepo.FileInputOwner)
		if !ok {
			return empty, targetrepo.ErrOperationUnavailable
		}
		prepare, stop := context.WithTimeout(ctx, 45*time.Second)
		fileSource, err = fileOwner.PrepareKubernetesFileInput(prepare, binding, current, plan, child)
		prepareErr := prepare.Err()
		stop()
		if err != nil || prepareErr != nil || fileSource.SourceRef == "" || fileSource.Version < 1 || !fileSource.AuthorizedUntil.After(time.Now()) || fileSource.AuthorizedUntil.After(current.ExpiresAt) {
			return empty, targetrepo.ErrOperationUnavailable
		}
	}
	prepared, err := service.native.PrepareKubernetesPodStream(ctx, current, child)
	if errors.Is(err, targetrepo.ErrPortForwardProfileUnsupported) && child.Operation == "kubernetes.port-forward" {
		return empty, targetrepo.ErrPortForwardProfileUnsupported
	}
	if err != nil || prepared == nil {
		return empty, targetrepo.ErrOperationUnavailable
	}
	defer prepared.Close()
	pin, ready := prepared.Binding()
	if !ready || pin.SnapshotDigest != current.Digest || pin.ChildRef != child.Ref || pin.Operation != child.Operation || pin.InputDigest != child.InputDigest || pin.TargetDigest != current.Target.MetadataDigest() || pin.SubjectDigest != current.SubjectDigest || !reflect.DeepEqual(pin.Observation, *child.Observation) || !pin.HardDeadline.After(time.Now()) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	admit, stop := context.WithTimeout(ctx, outputOwnerTimeout)
	opening := streamEffectOpening(current, plan, child, pin.HardDeadline)
	if child.Operation == "kubernetes.cp.upload" {
		if !fileSource.AuthorizedUntil.After(time.Now()) {
			stop()
			return empty, targetrepo.ErrOperationUnavailable
		}
		opening.FileSourceRef, opening.FileSourceVersion = fileSource.SourceRef, fileSource.Version
		if fileSource.AuthorizedUntil.Before(opening.ExpiresAt) {
			opening.ExpiresAt = fileSource.AuthorizedUntil
		}
	}
	admission, err := service.owner.BeginKubernetesEffect(admit, binding, opening)
	admitErr := admit.Err()
	stop()
	if err != nil || admitErr != nil {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if admission.GateRef != "" {
		if !validStreamGate(admission) {
			return empty, targetrepo.ErrOperationUnavailable
		}
		return targetrepo.OperationExecution{Effect: targetrepo.ResourceExecution{PlanRef: plan.Ref, ChildRef: child.Ref, GateRef: admission.GateRef}}, nil
	}
	if admission.ReconcileOnly {
		return service.reconcileHistory(binding, current, plan, child, admission)
	}
	if !validStreamAdmission(binding, current, child, pin.HardDeadline, admission) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result, active, access, handshake, err := service.start(ctx, binding, current, plan, child, prepared, admission)
	if err != nil {
		return empty, err
	}
	service.mutex.Lock()
	entry.effect, entry.active, entry.access, entry.handshake, entry.grant = result.Effect, active, access, handshake, cloneStreamGrant(*result.Stream)
	service.mutex.Unlock()
	transferred = true
	go service.monitor(key, entry, workerRelease)
	return result, nil
}

// resolve performs a bounded current owner read and refuses stale or malformed metadata before private access.
// resolve выполняет bounded current owner read и отклоняет stale/malformed metadata до private access.
func (service *StreamOperations) resolve(ctx context.Context, binding managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error) {
	read, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	defer cancel()
	snapshot, err := service.owner.ResolveKubernetesOperation(read, binding)
	_, valid := streamOwnerReadPin(snapshot)
	if err != nil || read.Err() != nil || !valid || !snapshot.ExpiresAt.After(time.Now()) {
		return value.KubernetesOperationSnapshot{}, targetrepo.ErrOperationUnavailable
	}
	return snapshot, nil
}

// validPodStreamChild checks exact direct-child metadata without treating its state as dispatch authority.
// validPodStreamChild проверяет exact direct-child metadata, не считая state полномочием на dispatch.
func validPodStreamChild(plan targetrepo.OperationPlan, child targetrepo.OperationChild, snapshot value.KubernetesOperationSnapshot) bool {
	if !managedkubernetes.ValidLocator(plan.Ref) || !managedkubernetes.ValidDigest(plan.Digest) || plan.Revision < 1 || !managedkubernetes.ValidLocator(child.Ref) || child.Operation != snapshot.Operation || child.InputDigest != resourceResultDigest(child.NormalizedInput) || !bytes.Equal(child.NormalizedInput, snapshot.NormalizedInput) || child.Observation == nil || child.Observation.Validate() != nil || child.Observation.Resource != child.Resource {
		return false
	}
	if child.Operation == "kubernetes.watch" || child.Operation == "kubernetes.watch.all-namespaces" {
		return child.NodeTransport == nil && child.Observation.PodWitness == nil && child.Observation.ObservationState == managedkubernetes.ObservationCollection && child.Resource.AllNamespaces == (child.Operation == "kubernetes.watch.all-namespaces")
	}
	if child.Operation == "kubernetes.logs" {
		r := child.Resource
		return child.NodeTransport == nil && child.Observation.PodWitness == nil && child.Observation.ObservationState == managedkubernetes.ObservationPresent && r.Group == "" && r.Version == "v1" && r.Resource == "pods" && r.Namespaced && !r.AllNamespaces && r.Subresource == "" && r.UID != ""
	}
	if child.Observation.PodWitness == nil || child.NodeTransport == nil {
		return false
	}
	witness := child.Observation.PodWitness
	return witness.NodeUID == child.NodeTransport.NodeUID && witness.NodeName == child.NodeTransport.NodeName && child.NodeTransport.TargetRef == snapshot.Target.ID && child.NodeTransport.TargetRevision == int64(snapshot.Target.Revision)
}

// cloneStreamGrant detaches channel slices while preserving the exact immutable owner limits.
// cloneStreamGrant отделяет channel slices, сохраняя exact immutable owner limits.
func cloneStreamGrant(grant value.KubernetesStreamGrant) value.KubernetesStreamGrant {
	grant.InputChannels = append([]string(nil), grant.InputChannels...)
	grant.OutputChannels = append([]string(nil), grant.OutputChannels...)
	return grant
}
