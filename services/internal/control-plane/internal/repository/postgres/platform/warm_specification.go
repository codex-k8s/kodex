package platform

import (
	"context"
	_ "embed"
	"math"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/workers_warm_specification_lock.sql
var queryWarmSpecificationLock string

//go:embed sql/workers_warm_specification_publish.sql
var queryWarmSpecificationPublish string

// Fingerprint исключает только саму identity revision, но сохраняет все
// исполняемые зависимости canonical RuntimeRevisionDigest. Snapshot не меняется.
func warmSpecificationDigest(snapshot map[string]any) (string, error) {
	material := make(map[string]any, len(snapshot))
	for key, value := range snapshot {
		material[key] = value
	}
	material["runtimeRevisionRef"] = "system-assistant-core-v1"
	material["runtimeRevisionVersion"] = int64(1)
	return runtimeRevisionDigestFromSnapshot(material)
}

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
