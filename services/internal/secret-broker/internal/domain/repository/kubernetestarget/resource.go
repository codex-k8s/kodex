package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ResourceOwner combines current graph reads with exact child admission and completion.
// ResourceOwner объединяет current graph reads с exact child admission и completion.
type ResourceOwner interface {
	OperationResolver
	EffectOwner
}

// ResourcePlanOwner derives immutable child plans before any resource effect is admitted.
// ResourcePlanOwner выводит immutable child plans до допуска resource effect.
type ResourcePlanOwner interface {
	ResourceOwner
	OperationPlanner
}

// ResourcePreparer loads only the snapshot's private identity and performs bounded native observations.
// ResourcePreparer загружает только private identity snapshot и выполняет bounded native observations.
type ResourcePreparer interface {
	ResourceResultAccess
	// PrepareKubernetesResource never replaces failed selected credentials or issues an effect.
	// PrepareKubernetesResource не подменяет failed selected credentials и не совершает effect.
	PrepareKubernetesResource(context.Context, value.KubernetesOperationSnapshot) (ResourcePreparation, error)
}

// ResourceResultAccess rechecks selected-identity READ authority without rereading a stored result's resource.
// ResourceResultAccess повторно проверяет selected-identity READ authority без reread ресурса stored result.
type ResourceResultAccess interface {
	// AuthorizeKubernetesResourceResult performs authentication/SSAR only and returns bounded output credit.
	// AuthorizeKubernetesResourceResult выполняет лишь authentication/SSAR и возвращает bounded output credit.
	AuthorizeKubernetesResourceResult(context.Context, value.KubernetesOperationSnapshot, OperationChild) (time.Time, error)
}

// ResourcePreparationBinding binds a private native preparation to immutable owner metadata.
// ResourcePreparationBinding связывает private native preparation с immutable owner metadata.
type ResourcePreparationBinding struct {
	Operation, InputDigest, TargetDigest, SubjectDigest string
	Observation                                         managedkubernetes.ResourceObservation
}

// ResourcePreparation owns one native request and joins it before releasing private material.
// It never issues authority; even a ready preparation needs an exact fresh owner admission.
// ResourcePreparation владеет одним native request и присоединяет его до удаления private material.
// Он не выдаёт authority: даже ready preparation требует exact fresh owner admission.
type ResourcePreparation interface {
	// Binding returns only immutable metadata and refuses closed or unready preparations.
	// Binding возвращает immutable metadata и отклоняет closed либо unready preparations.
	Binding() (ResourcePreparationBinding, bool)
	// Execute performs at most one request under the supplied short owner deadline.
	// Execute выполняет не более одного request под коротким owner deadline.
	Execute(context.Context, time.Time) (value.ResourceOperationResult, error)
	// Close cancels and positively joins any native request before clearing material.
	// Close отменяет и положительно присоединяет native request до очистки material.
	Close()
}

// ResourceExecution reports a gate, reconciliation, or freshly admitted safe result, never raw transport data.
// ResourceExecution сообщает gate, reconciliation либо freshly admitted safe result без raw transport data.
type ResourceExecution struct {
	PlanRef, ChildRef string
	GateRef           string
	ReconcileOnly     bool
	EffectRef         string
	EffectGeneration  int64
	AuthorizedUntil   time.Time
	Result            *AcceptedEffectResult `json:"-"`
}

// String keeps business result bytes out of diagnostics.
// String исключает business result bytes из diagnostics.
func (ResourceExecution) String() string { return "[managed Kubernetes resource execution]" }

// GoString applies the same protection to detailed formatting.
// GoString сохраняет ту же защиту при detailed formatting.
func (result ResourceExecution) GoString() string { return result.String() }
