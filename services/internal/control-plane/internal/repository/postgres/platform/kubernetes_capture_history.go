package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"time"
)

// queryKubernetesCaptureHistoryScope resolves the original actor's exact run/node without current integration authority.
// queryKubernetesCaptureHistoryScope разрешает exact run/node original actor без current integration authority.
//
//go:embed sql/kubernetes_capture__history_scope.sql
var queryKubernetesCaptureHistoryScope string

// queryKubernetesCaptureHistoryList selects only bounded original-lineage candidates for full owner rejoin.
// queryKubernetesCaptureHistoryList выбирает только bounded original-lineage candidates для полного owner rejoin.
//
//go:embed sql/kubernetes_capture__history_list.sql
var queryKubernetesCaptureHistoryList string

// ListKubernetesCaptures uses the exact Get predicate on every row and preserves the first finite
// run.view allowance across root/stream/capture waits and COMMIT. Page metadata cannot admit effects.
// ListKubernetesCaptures использует exact Get predicate для каждой строки и сохраняет первое finite
// run.view allowance через root/stream/capture waits и COMMIT. Page metadata не допускает effects.
func (repository *Repository) ListKubernetesCaptures(ctx context.Context, p value.Principal, input query.KubernetesCaptureList) (result entity.KubernetesCapturePage, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.capture.list"); err != nil {
		return result, err
	}
	limit := int(input.Page.Size)
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 || input.Page.Token != "" && (!managedkubernetes.ValidLocator(input.Page.Token) || len(input.Page.Token) > 96) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "project_ref": p.ProjectRef, "run_ref": input.RunRef, "node_ref": input.NodeRef}
	var rootID, rootRef, projectRef string
	readScope := func() error {
		err := tx.QueryRow(ctx, queryKubernetesCaptureHistoryScope, args).Scan(&rootID, &rootRef, &projectRef)
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		}
		if err != nil {
			return errs.ErrUnavailable
		}
		return nil
	}
	if err := readScope(); err != nil {
		return result, err
	}
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: rootRef, ProjectRef: projectRef}
	until, err := repository.requireAccessUntil(ctx, tx, current, "run.view", target)
	if err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	if _, err := lockOwnerGateRootTx(ctx, tx, rootID); err != nil {
		return result, err
	}
	if err := readScope(); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, queryKubernetesCaptureHistoryList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "root_id": rootID, "run_ref": input.RunRef, "node_ref": input.NodeRef, "after_ref": input.Page.Token, "limit": limit + 1})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	refs := make([]string, 0, limit+1)
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if len(refs) > limit {
		refs = refs[:limit]
		result.Next = refs[len(refs)-1]
	}
	result.Items = make([]entity.KubernetesCaptureStatus, 0, len(refs))
	for _, ref := range refs {
		record, err := repository.lockPublicKubernetesCapture(ctx, tx, current, p, ref)
		if err != nil {
			return entity.KubernetesCapturePage{}, err
		}
		if record.view.RunRef != input.RunRef || record.view.NodeRef != input.NodeRef {
			return entity.KubernetesCapturePage{}, errs.ErrUnavailable
		}
		until = earlierAccessDeadline(until, record.until)
		result.Items = append(result.Items, record.view)
	}
	fresh, err := repository.requireAccessUntil(ctx, tx, current, "run.view", target)
	if err != nil {
		return entity.KubernetesCapturePage{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	until = earlierAccessDeadline(until, fresh)
	if accessDeadlineExpired(until) {
		return entity.KubernetesCapturePage{}, errs.ErrNotFound
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesCapturePage{}, errs.ErrUnavailable
	}
	if accessDeadlineExpired(until) {
		return entity.KubernetesCapturePage{}, errs.ErrNotFound
	}
	return result, nil
}
