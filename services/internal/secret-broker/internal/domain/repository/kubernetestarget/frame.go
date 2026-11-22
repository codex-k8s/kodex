package kubernetestarget

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ErrFrameUnavailable hides transport/owner diagnostics without claiming an external rollback.
// ErrFrameUnavailable скрывает transport/owner diagnostics без утверждения external rollback.
var ErrFrameUnavailable = errors.New("managed Kubernetes frame is unavailable")

// FrameOwner is the existing control-plane frame ledger; no payload or local retry state enters it.
// FrameOwner представляет существующий control-plane frame ledger без payload либо local retry state.
type FrameOwner interface {
	// BeginKubernetesFrame checks current authority and spends a sequence before external input.
	// BeginKubernetesFrame проверяет current authority и расходует sequence до external input.
	BeginKubernetesFrame(context.Context, managedkubernetes.Binding, value.KubernetesFrameMetadata) (value.KubernetesFrameAdmission, error)
	// CompleteKubernetesFrame records the joined local send, not remote process execution.
	// CompleteKubernetesFrame сохраняет joined local send, а не remote process execution.
	CompleteKubernetesFrame(context.Context, managedkubernetes.Binding, value.KubernetesFrameMetadata) (uint64, error)
}

// FrameWriter belongs to one already admitted live transport and cannot choose a target or identity.
// FrameWriter принадлежит одному already admitted live transport без выбора target либо identity.
type FrameWriter interface {
	// WriteFrame joins one bounded local write and retains no caller-owned payload after returning.
	// WriteFrame завершает bounded local write и не сохраняет caller-owned payload после возврата.
	WriteFrame(context.Context, value.KubernetesStreamFrame) error
}

// PortForwardOpenWriter receives only an owner-issued retention snapshot after exact INPUT admission; it never reads caller JSON authority.
// PortForwardOpenWriter получает только owner-issued retention snapshot после exact INPUT admission; caller JSON authority не используется.
type PortForwardOpenWriter interface {
	// WritePortForwardOpen joins one fresh OPEN under original credit while pruning only obsolete local join facts.
	// WritePortForwardOpen завершает fresh OPEN под original credit, удаляя только obsolete local join facts.
	WritePortForwardOpen(context.Context, value.KubernetesStreamFrame, managedkubernetes.PortForwardRetentionSet) error
}
