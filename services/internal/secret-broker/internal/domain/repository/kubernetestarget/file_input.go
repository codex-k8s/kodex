package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// FileInputPreparation carries only exact owner source/version and the original finite allowance.
// It contains no artifact selector, file bytes, storage locator or native execution credit.
// FileInputPreparation хранит лишь exact owner source/version и original finite allowance.
// Artifact selector, file bytes, storage locator либо native execution credit отсутствуют.
type FileInputPreparation struct {
	SourceRef       string
	Version         int64
	AuthorizedUntil time.Time
}

// FileInputOwner prepares only the immutable approved artifact selected by the existing child.
// Absence is a closed incomplete consumer; an arbitrary callback or caller bytes cannot replace it.
// FileInputOwner подготавливает лишь immutable approved artifact существующего child.
// Отсутствие закрывает incomplete consumer; arbitrary callback либо caller bytes его не заменяют.
type FileInputOwner interface {
	// PrepareKubernetesFileInput precedes Begin and cannot itself admit a native copy attempt.
	// PrepareKubernetesFileInput предшествует Begin и сам не допускает native copy attempt.
	PrepareKubernetesFileInput(context.Context, managedkubernetes.Binding, value.KubernetesOperationSnapshot, OperationPlan, OperationChild) (FileInputPreparation, error)
}
