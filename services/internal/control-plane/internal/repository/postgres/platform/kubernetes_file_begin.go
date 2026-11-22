package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFileBindStream fixes the prepared source to the single native stream reservation.
// queryKubernetesFileBindStream закрепляет prepared source за единственной native stream reservation.
//
//go:embed sql/kubernetes_file__bind_stream.sql
var queryKubernetesFileBindStream string

// kubernetesFileBeginSource preserves private source identity and independent permission/dispatch limits.
// kubernetesFileBeginSource сохраняет private source identity и отдельные permission/dispatch limits.
type kubernetesFileBeginSource struct {
	id                 string
	until, accessUntil time.Time
}

// checkKubernetesFileBeginTx proves READY exact source/version and fresh original artifact authority.
// A source ref is never authority; the exact child, original snapshot and typed adoption must match.
// checkKubernetesFileBeginTx доказывает READY exact source/version и fresh original artifact authority.
// Source ref не является authority; exact child, original snapshot и typed adoption обязаны совпасть.
func (repository *Repository) checkKubernetesFileBeginTx(ctx context.Context, tx pgx.Tx, owner kubernetesOperationOwner, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginEffect, record kubernetesEffectRecord) (kubernetesFileBeginSource, error) {
	var result kubernetesFileBeginSource
	if record.operation != "kubernetes.cp.upload" {
		if input.FileSourceRef != "" || input.FileSourceVersion != 0 {
			return result, errs.ErrInvalid
		}
		return result, nil
	}
	if input.FileSourceRef == "" || input.FileSourceVersion < 1 {
		return result, errs.ErrForbidden
	}
	source, err := readKubernetesFileSourceTx(ctx, tx, owner.current.organizationID, binding.InvocationRef, input.FileSourceRef, input.ChildRef)
	if err != nil {
		return result, err
	}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return result, errs.ErrForbidden
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return result, err
	}
	if source.view.State != "READY" || source.view.Version != input.FileSourceVersion || source.view.PlanRef != input.PlanRef || source.inputDigest != record.inputDigest || source.snapshotDigest != snapshot || source.bindingDigest != digest || source.generation != binding.Generation || source.preparedID == "" || source.purgedAt != nil || source.authorizedUntil == nil || source.view.StreamRef != "" {
		return result, errs.ErrForbidden
	}
	origin, until, err := repository.readKubernetesFileOriginTx(ctx, tx, owner, *source.authorizedUntil)
	if err != nil {
		return result, err
	}
	if origin.artifactRef != source.view.ArtifactRef || origin.revisionRef != source.view.ArtifactRevisionRef {
		return result, errs.ErrForbidden
	}
	result.id, result.until = source.id, until
	result.accessUntil = origin.accessUntil
	if source.accessUntil != nil {
		result.accessUntil = earlierAccessDeadline(result.accessUntil, *source.accessUntil)
	}
	if err := owner.creditError(ctx, result.until); err != nil {
		return kubernetesFileBeginSource{}, err
	}
	if !result.accessUntil.IsZero() && !result.accessUntil.After(time.Now()) {
		return kubernetesFileBeginSource{}, errs.ErrForbidden
	}
	return result, nil
}
