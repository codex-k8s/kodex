package entity

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// KubernetesPortForwardConnection discloses only owner-assigned coordinates and monotonic local facts; owner closure never substitutes for NativeJoined or remote success.
// KubernetesPortForwardConnection выдаёт лишь owner-assigned coordinates и monotonic local facts; owner closure не заменяет NativeJoined либо remote success.
type KubernetesPortForwardConnection struct {
	Ref, InvocationRef, StreamRef, ReasonCode       string
	StreamGeneration, Generation, InputVersion      int64
	Slot                                            uint32
	State                                           enum.KubernetesPortForwardConnectionState
	LocalWriteClosed, ReadEndObserved, NativeJoined bool
	HardDeadline                                    time.Time
}

// KubernetesPortForwardConnectionProfile reports the pinned transport and active-connection budget, never credentials, fallback permissions or target READY.
// KubernetesPortForwardConnectionProfile сообщает pinned transport и active-connection budget, не credentials, fallback permissions либо target READY.
type KubernetesPortForwardConnectionProfile struct {
	Protocol, AdmissionReasonCode               string
	MaximumActiveConnections, ActiveConnections uint32
	Ports                                       []managedkubernetes.ForwardPort
	TimeoutSeconds                              uint32
}

// KubernetesPortForwardConnectionReadback contains immutable Open/Close submission metadata, never a reusable native credit or TCP payload.
// KubernetesPortForwardConnectionReadback содержит immutable Open/Close submission metadata, не reusable native credit либо TCP payload.
type KubernetesPortForwardConnectionReadback struct {
	Connection KubernetesPortForwardConnection
	Input      *KubernetesStreamInputView
	Submission *KubernetesStreamInputSubmission
}

// KubernetesPortForwardConnectionPage supports reconnect discovery inside one original stream with an owner-scoped cursor.
// KubernetesPortForwardConnectionPage обеспечивает reconnect discovery внутри original stream с owner-scoped cursor.
type KubernetesPortForwardConnectionPage struct {
	Items   []KubernetesPortForwardConnection
	Next    string
	Profile KubernetesPortForwardConnectionProfile
}
