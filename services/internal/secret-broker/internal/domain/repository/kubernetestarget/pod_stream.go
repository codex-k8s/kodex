package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// PodStreamPreparer observes the selected Pod and prepares an exact owner-bound native stream.
// No credential or target selector is supplied by the transport caller.
// PodStreamPreparer наблюдает выбранный Pod и готовит exact owner-bound native stream.
// Transport caller не передаёт credential либо target selector.
type PodStreamPreparer interface {
	// ObserveKubernetesPod returns a proposal before owner transport selection, without opening a stream.
	// ObserveKubernetesPod возвращает proposal до owner transport selection без открытия stream.
	ObserveKubernetesPod(context.Context, value.KubernetesOperationSnapshot) (managedkubernetes.ChildCandidate, error)
	// PrepareKubernetesPodStream verifies the complete final owner child before external admission.
	// PrepareKubernetesPodStream проверяет полный final owner child до external admission.
	PrepareKubernetesPodStream(context.Context, value.KubernetesOperationSnapshot, OperationChild) (PodStreamPreparation, error)
}

// StreamPlanOwner joins the existing plan, effect, stream and frame owners without introducing authority.
// StreamPlanOwner объединяет existing plan, effect, stream и frame owners без новой authority.
type StreamPlanOwner interface {
	ResourcePlanOwner
	OperationStreamResolver
	LiveStreamOwner
}

// PodStreamPreparationBinding pins the exact observation and finite duration without credential material.
// PodStreamPreparationBinding закрепляет exact observation и конечный срок без credential material.
type PodStreamPreparationBinding struct {
	SnapshotDigest, ChildRef, Operation, InputDigest, TargetDigest, SubjectDigest string
	Observation                                                                   managedkubernetes.ChildCandidate
	HardDeadline                                                                  time.Time
}

// OpenedPodStream owns an actual negotiated transport whose handshake can be bound to owner evidence.
// OpenedPodStream владеет actual negotiated transport, handshake которого связывается с owner evidence.
type OpenedPodStream interface {
	LiveStream
	// HandshakeReceipt exposes negotiated facts only, never credentials or remote diagnostic text.
	// HandshakeReceipt выдаёт только negotiated facts без credentials либо remote diagnostic text.
	HandshakeReceipt(managedkubernetes.StreamReceiptOrigin) (managedkubernetes.HandshakeReceipt, error)
}

// PodStreamPreparation is consumed once; Close joins a pending open, but not a transferred transport.
// The caller owns every returned transport and must close/join it even after lost owner replies.
// PodStreamPreparation расходуется однократно; Close присоединяет pending open, но не переданный transport.
// Caller владеет каждым returned transport и закрывает/присоединяет его даже при lost owner reply.
type PodStreamPreparation interface {
	// Binding returns immutable copied metadata; a closed or consumed preparation is not ready.
	// Binding возвращает immutable copied metadata; closed либо consumed preparation не является ready.
	Binding() (PodStreamPreparationBinding, bool)
	// TargetAccess checks fresh credentials and current Pod permissions for the same admitted stream.
	// TargetAccess проверяет свежие credentials и current Pod permissions того же admitted stream.
	TargetAccess() StreamTargetAccess
	// Open separates worker lifetime from the unary attempt and consumes exact fresh effect credit once.
	// Open разделяет worker lifetime и unary attempt, однократно расходуя exact fresh effect credit.
	Open(context.Context, context.Context, managedkubernetes.Binding, EffectAdmission) (OpenedPodStream, managedkubernetes.HandshakeReceipt, error)
	// Close cancels and joins a pending open before releasing preparation state.
	// Close отменяет и присоединяет pending open до освобождения preparation state.
	Close()
}
