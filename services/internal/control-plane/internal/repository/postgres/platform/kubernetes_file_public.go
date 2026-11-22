package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFileList discovers only the exact authorized invocation's bounded source set.
// queryKubernetesFileList обнаруживает лишь bounded source set exact authorized invocation.
//
//go:embed sql/kubernetes_file__list.sql
var queryKubernetesFileList string

// kubernetesFilePublicAllowance captures current root visibility before potentially blocking owner locks.
// It reuses the existing original-root origin read and preserves finite first bounds through COMMIT.
// kubernetesFilePublicAllowance фиксирует current root visibility до potentially blocking owner locks.
// Использует existing original-root origin read, сохраняет finite first bounds через COMMIT.
func (repository *Repository) kubernetesFilePublicAllowance(ctx context.Context, tx pgx.Tx, current scope, p value.Principal, inv string, first time.Time) (time.Time, error) {
	var id, connection, run, node string
	err := tx.QueryRow(ctx, queryKubernetesInputPublicOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": inv, "project_ref": p.ProjectRef}).Scan(&id, &connection, &run, &node)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errs.ErrNotFound
	}
	if err != nil {
		return time.Time{}, errs.ErrUnavailable
	}
	until := first
	for _, target := range []struct{ permission, kind, ref string }{{"run.view", "RUN", run}, {"integration.view", "INTEGRATION", connection}} {
		fresh, err := repository.requireAccessUntil(ctx, tx, current, target.permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: target.kind, ResourceRef: target.ref})
		if err != nil {
			return time.Time{}, concealAccessDenial(err, errs.ErrNotFound)
		}
		until = earlierAccessDeadline(until, fresh)
		if accessDeadlineExpired(until) {
			return time.Time{}, errs.ErrNotFound
		}
	}
	return until, nil
}

// readKubernetesFilePublicTx reads metadata only after root ownership and current artifact visibility.
// Revoked download authority blocks future private input separately; retained metadata creates no input credit.
// readKubernetesFilePublicTx читает metadata после root ownership и current artifact visibility.
// Revoked download authority отдельно блокирует будущий private input; metadata не создаёт input credit.
func (repository *Repository) readKubernetesFilePublicTx(ctx context.Context, tx pgx.Tx, current scope, inv, source string, first time.Time) (entity.KubernetesFileInputView, time.Time, error) {
	record, err := readKubernetesFileSourceTx(ctx, tx, current.organizationID, inv, source, "")
	if err != nil {
		return entity.KubernetesFileInputView{}, time.Time{}, err
	}
	fresh, err := repository.requireAccessUntil(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: record.view.ArtifactRef})
	if err != nil {
		return entity.KubernetesFileInputView{}, time.Time{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	until := earlierAccessDeadline(first, fresh)
	if accessDeadlineExpired(until) {
		return entity.KubernetesFileInputView{}, time.Time{}, errs.ErrNotFound
	}
	if _, err := repository.artifactRevisionReadHead(ctx, tx, current, record.view.ArtifactRef); err != nil {
		return entity.KubernetesFileInputView{}, time.Time{}, err
	}
	cleanup, err := readKubernetesFileCleanupStatus(ctx, tx, current.organizationID, record.id)
	if err != nil {
		return entity.KubernetesFileInputView{}, time.Time{}, err
	}
	record.view.Cleanup = &cleanup
	return record.view, until, nil
}

// GetKubernetesFileInput exposes one original source's provenance/progress without private bytes or preparation.
// All errors discard the view; an expired first permission cannot be restored by a later alternative grant.
// GetKubernetesFileInput раскрывает provenance/progress original source без private bytes либо preparation.
// При ошибке view отбрасывается; expired first permission не восстанавливается поздним alternative grant.
func (repository *Repository) GetKubernetesFileInput(ctx context.Context, p value.Principal, inv, source string) (result entity.KubernetesFileInputView, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.file.get"); err != nil {
		return result, err
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
	until, err := repository.kubernetesFilePublicAllowance(ctx, tx, current, p, inv, time.Time{})
	if err != nil {
		return result, err
	}
	if _, err := repository.lockPublicKubernetesTool(ctx, tx, current, p, inv); err != nil {
		return result, err
	}
	view, until, err := repository.readKubernetesFilePublicTx(ctx, tx, current, inv, source, until)
	if err != nil {
		return result, err
	}
	until, err = repository.kubernetesFilePublicAllowance(ctx, tx, current, p, inv, until)
	if err != nil {
		return result, err
	}
	if accessDeadlineExpired(until) {
		return result, errs.ErrNotFound
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if accessDeadlineExpired(until) {
		return result, errs.ErrNotFound
	}
	return view, nil
}

// ListKubernetesFileInputs uses the same actor/root/artifact rule for every exact-invocation source.
// Overflow or any inaccessible member closes the entire response rather than leaking a partial page.
// ListKubernetesFileInputs применяет same actor/root/artifact rule ко всем exact-invocation sources.
// Overflow либо inaccessible member закрывает whole response без partial page leak.
func (repository *Repository) ListKubernetesFileInputs(ctx context.Context, p value.Principal, inv string) (result []entity.KubernetesFileInputView, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.file.list"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	until, err := repository.kubernetesFilePublicAllowance(ctx, tx, current, p, inv, time.Time{})
	if err != nil {
		return nil, err
	}
	id, err := repository.lockPublicKubernetesTool(ctx, tx, current, p, inv)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, queryKubernetesFileList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": id})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := make([]string, 0)
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil || len(refs) > 256 {
		return nil, errs.ErrUnavailable
	}
	items := make([]entity.KubernetesFileInputView, 0, len(refs))
	for _, ref := range refs {
		var view entity.KubernetesFileInputView
		view, until, err = repository.readKubernetesFilePublicTx(ctx, tx, current, inv, ref, until)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	until, err = repository.kubernetesFilePublicAllowance(ctx, tx, current, p, inv, until)
	if err != nil {
		return nil, err
	}
	if accessDeadlineExpired(until) {
		return nil, errs.ErrNotFound
	}
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	if accessDeadlineExpired(until) {
		return nil, errs.ErrNotFound
	}
	return items, nil
}
