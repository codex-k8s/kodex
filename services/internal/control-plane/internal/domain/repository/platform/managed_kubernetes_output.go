package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesOutputRepository owns one-write private staging for exact broker-admitted frames.
// An accepted stage is not an artifact; publication additionally requires accepted contiguous ACKs and terminal join.
// ManagedKubernetesOutputRepository владеет one-write private staging exact broker-admitted frames.
// Accepted stage не является artifact; publication требует contiguous ACKs и terminal join.
type ManagedKubernetesOutputRepository interface {
	// AcceptManagedKubernetesOutputFrame materializes one owned chunk only after exact current frame admission.
	// AcceptManagedKubernetesOutputFrame materializes owned chunk только после exact current frame admission.
	AcceptManagedKubernetesOutputFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesAcceptOutputFrame) (entity.ManagedKubernetesOutputFrameReceipt, error)
	// GetManagedKubernetesOutputFrame reads original metadata without bytes, publication or renewed credit.
	// GetManagedKubernetesOutputFrame читает original metadata без bytes/publication/renewed credit.
	GetManagedKubernetesOutputFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesOutputFrameLookup) (entity.ManagedKubernetesOutputFrameReceipt, error)
}
