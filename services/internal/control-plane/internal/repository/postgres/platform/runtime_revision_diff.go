package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeRevisionPublicPair embeds the requested/latest run revision and at most one previous
// revision in the same tenant/project/session, returning only public immutable identity/dependency
// metadata. Cross-run predecessor access is checked separately before issuance.
// queryRuntimeRevisionPublicPair встраивает запрошенную либо последнюю ревизию запуска и максимум
// одну прежнюю ревизию той же организации, проекта и сессии, возвращая только публичные
// неизменяемые метаданные идентичности и зависимостей. Доступ к предшественнику другого запуска
// проверяется отдельно до выдачи.
//
//go:embed sql/runtime_revision_public_pair.sql
var queryRuntimeRevisionPublicPair string

// GetRuntimeRevisionPublicPair reads the public current/previous pair in a five-second read-only
// repeatable-read snapshot after run.view and authority-project checks. A predecessor run is
// independently authorized before commit; empty results are NotFound and failures return an empty
// projection. It exposes no safe_snapshot/private contents or execution authority.
// GetRuntimeRevisionPublicPair читает публичную пару текущей и прежней ревизии в read-only
// RepeatableRead снимке с бюджетом пять секунд после run.view и проверки проекта полномочий.
// Запуск предшественника независимо авторизуется до Commit; пустой результат даёт NotFound, ошибки
// возвращают пустую проекцию. Не раскрывает safe_snapshot и закрытое содержимое и не выдаёт права
// исполнения.
func (repository *Repository) GetRuntimeRevisionPublicPair(ctx context.Context, principal value.Principal, runRef, revisionRef string) (entity.RuntimeRevisionPublicProjection, *entity.RuntimeRevisionPublicProjection, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var empty entity.RuntimeRevisionPublicProjection
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return empty, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	authorize := func(ref string) error {
		target, err := repository.resolveAccessTarget(ctx, tx, scope.organizationID, entity.AccessScope{ResourceKind: "RUN", ResourceRef: ref})
		if err != nil || scope.authorityProjectID != "" && scope.authorityProjectID != target.projectID {
			return errs.ErrNotFound
		}
		if err := repository.requireAccess(ctx, tx, scope, "run.view", target); err != nil {
			return errs.ErrNotFound
		}
		return nil
	}
	if err := authorize(runRef); err != nil {
		return empty, nil, err
	}
	rows, err := tx.Query(ctx, queryRuntimeRevisionPublicPair, pgx.StrictNamedArgs{"organization_id": scope.organizationID, "run_ref": runRef, "revision_ref": revisionRef})
	if err != nil {
		return empty, nil, errs.ErrUnavailable
	}
	items := make([]entity.RuntimeRevisionPublicProjection, 0, 2)
	for rows.Next() {
		var item entity.RuntimeRevisionPublicProjection
		err = rows.Scan(&item.Identity.Ref, &item.Identity.Version, &item.Identity.RunRef, &item.Identity.SessionRef,
			&item.Identity.TurnRef, &item.Identity.Attempt, &item.Identity.RevisionDigest, &item.Identity.CreatedAt,
			&item.Provider.Ref, &item.Model.Ref, &item.RuntimeProfile.Ref, &item.RuntimeProfile.Revision,
			&item.RuntimeConfiguration.Ref, &item.RuntimeConfiguration.Version, &item.RuntimeConfiguration.Digest,
			&item.ProviderPolicy.Ref, &item.ProviderPolicy.Version, &item.ProviderPolicy.Digest,
			&item.ConfigOverlay.Ref, &item.ConfigOverlay.Version, &item.ConfigOverlay.Digest,
			&item.Environment.Ref, &item.Environment.Version, &item.Environment.Digest,
			&item.EnvironmentBinding.Ref, &item.EnvironmentBinding.Version, &item.EnvironmentBinding.Digest,
			&item.Instruction.Ref, &item.Instruction.Digest, &item.IntegrationGrants.Digest, &item.Image.Digest)
		if err != nil {
			rows.Close()
			return empty, nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return empty, nil, errs.ErrUnavailable
	}
	if len(items) == 0 {
		return empty, nil, errs.ErrNotFound
	}
	var previous *entity.RuntimeRevisionPublicProjection
	if len(items) == 2 {
		if err := authorize(items[1].Identity.RunRef); err != nil {
			return empty, nil, err
		}
		previous = &items[1]
	}
	if tx.Commit(ctx) != nil {
		return empty, nil, errs.ErrUnavailable
	}
	return items[0], previous, nil
}
