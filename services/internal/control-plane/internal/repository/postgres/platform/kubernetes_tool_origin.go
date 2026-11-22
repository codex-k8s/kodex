package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// lockPublicKubernetesTool authenticates the original root actor and current run/Connection
// visibility before locking the Connection and invocation, in the same order as native admission.
// It requires no live execution credential and grants neither bytes nor permission to start work.
// lockPublicKubernetesTool проверяет исходного инициатора и текущую видимость запуска/подключения,
// затем блокирует подключение и вызов в порядке допуска исполнения. Действующий ключ исполнения
// не требуется; доступ к данным и разрешение на новые действия этот метод не выдаёт.
func (repository *Repository) lockPublicKubernetesTool(ctx context.Context, tx pgx.Tx, current scope, p value.Principal, inv string) (string, error) {
	var id, connection, run, node string
	err := tx.QueryRow(ctx, queryKubernetesInputPublicOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": inv, "project_ref": p.ProjectRef}).Scan(&id, &connection, &run, &node)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrNotFound
	}
	if err != nil {
		return "", errs.ErrUnavailable
	}
	for _, target := range []struct{ permission, kind, ref string }{{"run.view", "RUN", run}, {"integration.view", "INTEGRATION", connection}} {
		if err := repository.requireAccess(ctx, tx, current, target.permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: target.kind, ResourceRef: target.ref}); err != nil {
			return "", concealAccessDenial(err, errs.ErrNotFound)
		}
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, connection); err != nil {
		return "", err
	}
	if err := tx.QueryRow(ctx, queryKubernetesInputPublicLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": id}).Scan(&id); err != nil {
		return "", errs.ErrUnavailable
	}
	return id, nil
}
