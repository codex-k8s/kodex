package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesFileCleanupRepository reuses the existing cleanup engine while keeping source-byte authority separate.
// Missing implementation cannot invent cleanup metadata, repair success or FILE execution readiness.
// KubernetesFileCleanupRepository использует existing cleanup engine отдельно от source-byte authority.
// Отсутствие реализации не выдумывает cleanup metadata, repair success либо FILE execution readiness.
type KubernetesFileCleanupRepository interface {
	// ListKubernetesFileCleanup discovers only exact original-root/run.view closure metadata before a stream exists.
	// ListKubernetesFileCleanup раскрывает только exact original-root/run.view closure metadata до появления stream.
	ListKubernetesFileCleanup(context.Context, value.Principal, string) ([]entity.KubernetesFileCleanupView, error)
	// GetKubernetesFileCleanup reads current metadata and an optional same-actor/source original-key acknowledgement.
	// GetKubernetesFileCleanup читает current metadata и optional same-actor/source original-key acknowledgement.
	GetKubernetesFileCleanup(context.Context, value.Principal, query.KubernetesFileCleanupRead) (entity.KubernetesFileCleanupReadback, error)
	// RepairKubernetesFileCleanup requires current scoped artifact.upload Until, OCC and terminal fenced eligibility.
	// RepairKubernetesFileCleanup требует current scoped artifact.upload Until, OCC и terminal fenced eligibility.
	RepairKubernetesFileCleanup(context.Context, value.Principal, value.Mutation, command.KubernetesFileCleanupRepair) (entity.KubernetesFileCleanupReadback, error)
}
