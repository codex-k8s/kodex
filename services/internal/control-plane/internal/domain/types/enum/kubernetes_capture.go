package enum

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// ManagedKubernetesCaptureState records technical capture progress, never native business success.
// ManagedKubernetesCaptureState описывает technical capture progress, не native business success.
type ManagedKubernetesCaptureState = managedkubernetes.CaptureState

const (
	// KubernetesCaptureReady awaits its first or newly admitted artifact worker attempt.
	// KubernetesCaptureReady ожидает первый либо вновь допущенный artifact worker attempt.
	KubernetesCaptureReady ManagedKubernetesCaptureState = managedkubernetes.CaptureReady
	// KubernetesCaptureClaimed retains one current fenced artifact worker.
	// KubernetesCaptureClaimed сохраняет один current fenced artifact worker.
	KubernetesCaptureClaimed ManagedKubernetesCaptureState = managedkubernetes.CaptureClaimed
	// KubernetesCaptureComplete means publication finished; Kind still distinguishes FULL/PARTIAL.
	// KubernetesCaptureComplete означает finished publication; Kind различает FULL/PARTIAL.
	KubernetesCaptureComplete ManagedKubernetesCaptureState = managedkubernetes.CaptureComplete
	// KubernetesCaptureUnknown retains ambiguous preparation without permission for another PUT.
	// KubernetesCaptureUnknown сохраняет ambiguous preparation без разрешения другого PUT.
	KubernetesCaptureUnknown ManagedKubernetesCaptureState = managedkubernetes.CaptureUnknown
	// KubernetesCaptureRejected refuses unsafe content or changed original eligibility.
	// KubernetesCaptureRejected отклоняет unsafe content либо изменённую original eligibility.
	KubernetesCaptureRejected ManagedKubernetesCaptureState = managedkubernetes.CaptureRejected
	// KubernetesCaptureWaitingOwner preserves exhausted infrastructure attempts for audited recovery.
	// KubernetesCaptureWaitingOwner сохраняет exhausted infrastructure attempts для audited recovery.
	KubernetesCaptureWaitingOwner ManagedKubernetesCaptureState = managedkubernetes.CaptureWaitingOwner
	// KubernetesCaptureSuperseded closes a task replaced by a later exact ACK source generation.
	// KubernetesCaptureSuperseded закрывает task после later exact ACK source generation.
	KubernetesCaptureSuperseded ManagedKubernetesCaptureState = managedkubernetes.CaptureSuperseded
)

// ManagedKubernetesCaptureKind separates complete capture from a retained incomplete prefix.
// ManagedKubernetesCaptureKind отделяет complete capture от retained incomplete prefix.
type ManagedKubernetesCaptureKind = managedkubernetes.CaptureKind

const (
	// KubernetesCaptureEmpty proves a complete zero-byte source, not merely a missing transcript.
	// KubernetesCaptureEmpty доказывает complete zero-byte source, не отсутствие transcript.
	KubernetesCaptureEmpty ManagedKubernetesCaptureKind = managedkubernetes.CaptureEmpty
	// KubernetesCaptureFull requires all output admissions, positive join and a typed completed stream.
	// KubernetesCaptureFull требует все output admissions, positive join и typed completed stream.
	KubernetesCaptureFull ManagedKubernetesCaptureKind = managedkubernetes.CaptureFull
	// KubernetesCapturePartial preserves only an immutable owner-confirmed prefix.
	// KubernetesCapturePartial сохраняет только immutable owner-confirmed prefix.
	KubernetesCapturePartial ManagedKubernetesCaptureKind = managedkubernetes.CapturePartial
)
