package platform

import (
	"context"
	_ "embed"
	"math"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWarmSpecificationLock embeds the organization assistant-runtime desired specification row
// lock with current version/digest/ref. The caller compares executable dependencies and owns
// subsequent publication and commit.
// queryWarmSpecificationLock встраивает блокировку desired specification строки assistant-runtime
// организации с текущими version/digest/ref. Вызывающий код сравнивает исполняемые зависимости и
// отвечает за последующую публикацию и Commit.
//
//go:embed sql/workers_warm_specification_lock.sql
var queryWarmSpecificationLock string

// queryWarmSpecificationPublish embeds desired specification/ref publication, switching runtime to
// RECOVERING and incrementing assistant version only when dependencies changed. The owner row is
// already locked; returned version/state updates the caller projection.
// queryWarmSpecificationPublish встраивает публикацию desired specification/ref, переводя runtime
// в RECOVERING и увеличивая версию ассистента только при изменении зависимостей. Строка владельца
// уже заблокирована; возвращённые version/state обновляют проекцию caller.
//
//go:embed sql/workers_warm_specification_publish.sql
var queryWarmSpecificationPublish string

// warmSpecificationDigest shallow-copies the snapshot and substitutes fixed runtime revision
// identity/version before computing the canonical executable-dependency digest. It excludes
// self-identity churn without mutating the original snapshot or dropping executable dependency
// fields.
// warmSpecificationDigest поверхностно копирует снимок и подставляет фиксированные идентичность и
// версию runtime-ревизии перед вычислением канонического дайджеста исполняемых зависимостей.
// Исключает изменение собственной идентичности, не меняя исходный снимок и не удаляя исполняемые
// зависимости.
func warmSpecificationDigest(snapshot map[string]any) (string, error) {
	material := make(map[string]any, len(snapshot))
	for key, value := range snapshot {
		material[key] = value
	}
	material["runtimeRevisionRef"] = "system-assistant-core-v1"
	material["runtimeRevisionVersion"] = int64(1)
	return runtimeRevisionDigestFromSnapshot(material)
}

// bindWarmSpecification locks desired specification state, computes dependency change and advances
// ref/version only when needed, rejecting version overflow. It updates assistant/snapshot pins and
// emits audit plus SYSTEM_ASSISTANT_CHANGED only on change in the same transaction. Later errors
// may follow local mutations; no independent commit or runtime start occurs.
// bindWarmSpecification блокирует desired specification, вычисляет изменение зависимостей и
// продвигает ref/version только при необходимости, отклоняя переполнение версии. Обновляет
// assistant/snapshot pins и записывает audit и SYSTEM_ASSISTANT_CHANGED только при изменении в той
// же транзакции. Поздние ошибки возможны после локальных изменений; отдельного Commit и запуска
// runtime нет.
func (repository *Repository) bindWarmSpecification(ctx context.Context, tx pgx.Tx, current scope, assistant *entity.SystemAssistant, snapshot map[string]any) (bool, error) {
	digest, err := warmSpecificationDigest(snapshot)
	if err != nil {
		return false, errs.ErrConflict
	}
	var version int64
	var previousDigest, ref string
	if err := tx.QueryRow(ctx, queryWarmSpecificationLock, current.organizationID).Scan(&version, &previousDigest, &ref); err != nil {
		return false, errs.ErrUnavailable
	}
	changed := digest != previousDigest
	if changed {
		if version == math.MaxInt64 {
			return false, errs.ErrConflict
		}
		version++
		ref, err = newRef("rrev")
		if err != nil {
			return false, err
		}
	}
	if err := tx.QueryRow(ctx, queryWarmSpecificationPublish, current.organizationID, version, digest, ref, changed).Scan(&assistant.Version, &assistant.RuntimeState); err != nil {
		return false, errs.ErrUnavailable
	}
	assistant.DesiredRuntimeRevision = ref
	snapshot["runtimeRevisionRef"], snapshot["runtimeRevisionVersion"] = ref, version
	if changed {
		auditRef, err := newRef("aud")
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil, current.actorID,
			"controlplane.reconcile_warm_specification", "SYSTEM_ASSISTANT", assistant.StableKey, "i18n:ASSISTANT_RECOVERY_REQUESTED", current.correlationRef); err != nil {
			return false, errs.ErrUnavailable
		}
		if err := repository.emitPlatformEvent(ctx, tx, current, "SYSTEM_ASSISTANT_CHANGED", "", assistant.StableKey, "i18n:ASSISTANT_RECOVERY_REQUESTED"); err != nil {
			return false, err
		}
	}
	return changed, nil
}
