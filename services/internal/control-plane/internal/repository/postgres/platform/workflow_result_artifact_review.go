package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowArtifactReview proves numeric revision and fresh eligibility of an accepted input.
// queryWorkflowArtifactReview доказывает numeric revision и fresh eligibility accepted input.
//
//go:embed sql/workflow_result__artifact_review.sql
var queryWorkflowArtifactReview string

// resolveWorkflowArtifactReviewTx maps the accepted opaque revision reference to its exact number
// through owner history and the reviewer's frozen catalog; a locator never widens file authority.
// resolveWorkflowArtifactReviewTx сопоставляет accepted opaque revision ref с exact number через
// owner history и frozen catalog reviewer; locator не расширяет file authority.
func resolveWorkflowArtifactReviewTx(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, inputs []workflowResultDependency, requirement entity.WorkflowResultRequirement, declaration entity.WorkflowDeclaredResult) (entity.WorkflowAcceptedResult, bool, error) {
	subject, found := workflowArtifactReviewSubject(inputs, requirement)
	if !found || declaration.Source == nil || declaration.Source.Kind != "CAPTURED_ARTIFACT" || declaration.Source.ArtifactRef != subject.ArtifactRef || declaration.Source.Digest != "sha256:"+subject.Digest {
		return entity.WorkflowAcceptedResult{}, false, nil
	}
	pins := workflowResultPins(current, lease)
	pins["subject_step_key"], pins["subject_result_key"] = requirement.SubjectStepKey, requirement.SubjectResultKey
	pins["artifact_ref"], pins["artifact_revision_ref"], pins["artifact_revision"], pins["artifact_digest"] = subject.ArtifactRef, subject.ArtifactRevision, declaration.Source.Revision, declaration.Source.Digest
	rows, err := tx.Query(ctx, queryWorkflowArtifactReview, pins)
	if err != nil {
		return entity.WorkflowAcceptedResult{}, false, errs.ErrUnavailable
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var exact bool
		if rows.Scan(&exact) != nil {
			return entity.WorkflowAcceptedResult{}, false, errs.ErrUnavailable
		}
		if !exact {
			return entity.WorkflowAcceptedResult{}, false, errs.ErrConflict
		}
		count++
	}
	if rows.Err() != nil {
		return entity.WorkflowAcceptedResult{}, false, errs.ErrUnavailable
	}
	return subject, count == 1, nil
}
