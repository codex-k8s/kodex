package entity

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"

// KubernetesToolView exposes safe original-root metadata even before a native stream exists.
// It contains no private input, output, credential, lease or executable authority.
// KubernetesToolView выдаёт безопасные метаданные исходному инициатору до создания потока.
// Приватные данные, ключи, lease и полномочия на исполнение в ответ не входят.
type KubernetesToolView struct {
	InvocationRef, RunRef, NodeRef, ConnectionRef, TargetRef, Operation string
	TargetRevision, Version                                             int64
	State                                                               enum.KubernetesToolState
	WaitingForGate                                                      bool
	SafeReasonCode                                                      enum.KubernetesToolSafeReasonCode
	CaptureStatuses                                                     []KubernetesCaptureSummary
}

// KubernetesToolPage carries a bounded owner-filtered page and a continuation locator.
// KubernetesToolPage содержит ограниченную страницу доступных вызовов и ссылку продолжения.
type KubernetesToolPage struct {
	Items []KubernetesToolView
	Next  string
}
