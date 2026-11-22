package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureToolSummaries exposes only latest per-stream metadata after Tool ownership.
// queryKubernetesCaptureToolSummaries выдаёт latest per-stream metadata после проверки Tool ownership.
//
//go:embed sql/kubernetes_capture__tool_summaries.sql
var queryKubernetesCaptureToolSummaries string

// readKubernetesCaptureToolSummaries discovers retryable captures before any ArtifactRevision.
// The caller must already hold the canonical original-root Tool lock and current Tool visibility;
// metadata gives no artifact access, mutation permission, task binding or native execution credit.
// readKubernetesCaptureToolSummaries обнаруживает retryable captures до первой ArtifactRevision.
// Caller уже держит canonical original-root Tool lock с current Tool visibility; metadata не выдаёт
// artifact access, mutation permission, task binding либо native execution credit.
func readKubernetesCaptureToolSummaries(ctx context.Context, tx pgx.Tx, organizationID, invocationID string) ([]entity.KubernetesCaptureSummary, error) {
	rows, err := tx.Query(ctx, queryKubernetesCaptureToolSummaries, pgx.StrictNamedArgs{"organization_id": organizationID, "invocation_id": invocationID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := make([]entity.KubernetesCaptureSummary, 0)
	for rows.Next() {
		var v entity.KubernetesCaptureSummary
		var state string
		if rows.Scan(&v.CaptureRef, &v.StreamRef, &v.CaptureGeneration, &v.Version, &state, &v.ReasonCode) != nil {
			return nil, errs.ErrUnavailable
		}
		v.State = enum.ManagedKubernetesCaptureState(state)
		if len(result) >= 256 || v.Version < 1 || v.CaptureGeneration < 1 || !knownKubernetesCaptureState(v.State) || !managedkubernetes.ValidCaptureReason(v.ReasonCode) {
			return nil, errs.ErrUnavailable
		}
		result = append(result, v)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}
