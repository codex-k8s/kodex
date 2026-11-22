package entity

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"time"
)

// KubernetesStreamInputSubmission contains only immutable owner-assigned input metadata and delivery facts, never payload or a repeatable write grant.
// KubernetesStreamInputSubmission содержит лишь immutable owner-assigned input metadata и delivery facts, не payload либо repeatable write grant.
type KubernetesStreamInputSubmission struct {
	Ref, InvocationRef, StreamRef, Channel, PayloadDigest string
	Generation                                            int64
	Sequence                                              uint64
	ByteCount                                             uint32
	EndOfInput                                            bool
	State                                                 enum.KubernetesStreamInputSubmissionState
}

// KubernetesStreamInputView discloses safe current stream coordinates and purpose-specific OCC; channel membership alone does not authorize a source or submission.
// KubernetesStreamInputView выдаёт safe current stream coordinates и purpose-specific OCC; channel membership сама по себе не разрешает source либо submission.
type KubernetesStreamInputView struct {
	Resource                                                                                        managedkubernetes.ResourceAddress
	TargetRef, ConnectionRef                                                                        string
	TargetRevision                                                                                  int64
	InvocationRef, StreamRef, RunRef, NodeRef, Operation, Namespace, PodName, Container, ReasonCode string
	Generation, Version                                                                             int64
	State                                                                                           enum.ManagedKubernetesStreamState
	InputChannels, ClosedInputChannels                                                              []string
	MaximumFrameBytes                                                                               uint32
	MaximumStagedBytes                                                                              uint64
	MaximumStagedObjects                                                                            uint32
	MaximumTotalBytes, AcknowledgedSequence                                                         uint64
	Pending                                                                                         *KubernetesStreamInputSubmission
	HardDeadline                                                                                    time.Time
}

// KubernetesStreamInputPage preserves an owner-scoped continuation without choosing an arbitrary first active child.
// KubernetesStreamInputPage сохраняет owner-scoped continuation без выбора произвольного первого active child.
type KubernetesStreamInputPage struct {
	Items []KubernetesStreamInputView
	Next  string
}

// KubernetesStreamInputReadback returns current metadata and an optional exact-key historical submission, never another payload delivery.
// KubernetesStreamInputReadback возвращает current metadata и optional exact-key historical submission, не новую payload delivery.
type KubernetesStreamInputReadback struct {
	View       KubernetesStreamInputView
	Submission *KubernetesStreamInputSubmission
}
