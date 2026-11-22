package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureArtifactAccess applies the same persisted capture provenance restriction
// to Go access evaluation, which must not bypass the canonical SQL catalog predicate.
// queryKubernetesCaptureArtifactAccess применяет persisted capture provenance restriction к Go
// access evaluation, который не должен обходить canonical SQL catalog predicate.
//
//go:embed sql/kubernetes_capture__artifact_access.sql
var queryKubernetesCaptureArtifactAccess string

// kubernetesCaptureArtifactAccess restricts visibility without granting any ordinary permission.
// Captured heads accept revisions only through the source owner, including for the original actor.
// kubernetesCaptureArtifactAccess ограничивает visibility, не выдавая ordinary permission.
// Captured heads принимают revisions только через source owner, включая original actor.
func kubernetesCaptureArtifactAccess(ctx context.Context, runner queryRunner, current scope, permission, artifactID string) (bool, error) {
	var allowed bool
	if err := runner.QueryRow(ctx, queryKubernetesCaptureArtifactAccess, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "artifact_id": artifactID, "permission": permission}).Scan(&allowed); err != nil {
		return false, errs.ErrUnavailable
	}
	return allowed, nil
}
