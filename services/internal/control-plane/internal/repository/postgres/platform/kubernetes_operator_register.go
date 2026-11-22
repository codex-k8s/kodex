package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesOperatorStored keeps a verified owner source separate from the deployment candidate.
// kubernetesOperatorStored отделяет verified owner source от deployment candidate.
type kubernetesOperatorStored struct {
	registry value.KubernetesOperatorRegistry
	digest   string
}

// scanKubernetesOperator verifies canonical bytes, digest and normalized columns before returning metadata.
// Missing or corrupt current state never falls back to an unregistered installation artifact.
// scanKubernetesOperator проверяет canonical bytes, digest и normalized columns до выдачи metadata.
// Missing либо corrupt current state не заменяется незарегистрированным installation artifact.
func scanKubernetesOperator(row pgx.Row) (kubernetesOperatorStored, error) {
	var revision int64
	var installation, namespace, digest, raw string
	if err := row.Scan(&revision, &installation, &namespace, &digest, &raw); errors.Is(err, pgx.ErrNoRows) {
		return kubernetesOperatorStored{}, errs.ErrNotFound
	} else if err != nil {
		return kubernetesOperatorStored{}, errs.ErrUnavailable
	}
	hash := sha256.Sum256([]byte(raw))
	registry, err := value.DecodeKubernetesOperatorRegistry([]byte(raw), time.Now().UTC())
	if err != nil || hex.EncodeToString(hash[:]) != digest || registry.Revision != revision ||
		registry.InstallationRef != installation || registry.IdentityNamespace != namespace {
		return kubernetesOperatorStored{}, errs.ErrUnavailable
	}
	return kubernetesOperatorStored{registry: registry, digest: digest}, nil
}

// RegisterKubernetesOperator accepts only the explicit startup artifact inside one serialized owner transaction.
// It preserves history, refuses rollback/retired-key reuse and invalidates affected verification.
// Commit uncertainty returns Unavailable; restart must reconcile the original source, never retry a write blindly.
// RegisterKubernetesOperator принимает explicit startup artifact в одной serialized owner transaction.
// Сохраняет history, отвергает rollback/retired-key reuse и сбрасывает affected verification.
// Неизвестный commit даёт Unavailable; restart сверяет исходный source без слепого повтора write.
func (repository *Repository) RegisterKubernetesOperator(ctx context.Context) (resultErr error) {
	if repository.kubernetesOperator == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := tx.Exec(ctx, queryKubernetesOperatorSerialize); err != nil {
		return errs.ErrUnavailable
	}
	previous, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesOperatorCurrentLock))
	if errors.Is(err, errs.ErrNotFound) {
		var empty bool
		if tx.QueryRow(ctx, queryKubernetesOperatorEmpty).Scan(&empty) != nil || !empty {
			return errs.ErrUnavailable
		}
	} else if err != nil {
		return err
	}
	candidate := *repository.kubernetesOperator
	if previous.registry.Revision != 0 {
		if candidate.InstallationRef != previous.registry.InstallationRef || candidate.IdentityNamespace != previous.registry.IdentityNamespace ||
			candidate.Revision < previous.registry.Revision {
			return errs.ErrConflict
		}
		if candidate.Revision == previous.registry.Revision {
			if repository.kubernetesOperatorDigest != previous.digest {
				return errs.ErrConflict
			}
			if tx.Commit(ctx) != nil {
				return errs.ErrUnavailable
			}
			return nil
		}
	}
	if candidate.UploadEndpoint.Validate(time.Now().UTC()) != nil {
		return value.ErrKubernetesOperator
	}
	var active bool
	if tx.QueryRow(ctx, queryKubernetesOperatorActive).Scan(&active) != nil {
		return errs.ErrUnavailable
	}
	if active {
		return errs.ErrConflict
	}
	raw, digest, err := kubernetesOperatorPayload(candidate)
	if err != nil || digest != repository.kubernetesOperatorDigest {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryKubernetesOperatorInsert, pgx.StrictNamedArgs{
		"revision": candidate.Revision, "installation_ref": candidate.InstallationRef,
		"identity_namespace": candidate.IdentityNamespace, "payload": raw, "content_sha256": digest,
	}); err != nil {
		return mapWriteError(err)
	}
	for _, target := range candidate.Targets {
		if err := registerKubernetesOperatorTargetTx(ctx, tx, candidate.Revision, previous.registry, target); err != nil {
			return err
		}
	}
	activeRefs := make([]string, 0, len(candidate.Nodes))
	// Import retirement evidence first so array ordering cannot hide a generation high-watermark.
	// Сначала импортируем retirement evidence: порядок массива не скрывает generation high-watermark.
	for _, retired := range []bool{true, false} {
		for _, node := range candidate.Nodes {
			if node.Retired != retired {
				continue
			}
			if err := registerKubernetesOperatorNodeTx(ctx, tx, candidate.Revision, node); err != nil {
				return err
			}
			if !node.Retired {
				activeRefs = append(activeRefs, node.Transport.Ref)
			}
		}
	}
	for _, query := range []string{queryKubernetesOperatorRetirePins, queryKubernetesOperatorRetireKeys} {
		if _, err := tx.Exec(ctx, query, pgx.StrictNamedArgs{"registry_revision": candidate.Revision, "active_refs": activeRefs}); err != nil {
			return mapWriteError(err)
		}
	}
	for _, query := range []string{queryKubernetesOperatorCancelPersonalTests, queryKubernetesOperatorCancelSharedTests, queryKubernetesOperatorResetShared, queryKubernetesOperatorResetBindings} {
		if _, err := tx.Exec(ctx, query); err != nil {
			return mapWriteError(err)
		}
	}
	if _, err := tx.Exec(ctx, queryKubernetesOperatorAdvance, pgx.StrictNamedArgs{"revision": candidate.Revision}); err != nil {
		return mapWriteError(err)
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// registerKubernetesOperatorTargetTx forbids stale reactivation, silent replacement and immutable-content drift.
// Cluster identity and cluster-wide append-only protected names are additionally enforced by the database trigger.
// registerKubernetesOperatorTargetTx запрещает stale reactivation, скрытую замену и immutable-content drift.
// Cluster identity и append-only protected names всего кластера дополнительно проверяет database trigger.
func registerKubernetesOperatorTargetTx(ctx context.Context, tx pgx.Tx, sourceRevision int64, previous value.KubernetesOperatorRegistry, entry value.KubernetesOperatorTarget) error {
	raw, digest, err := kubernetesOperatorPayload(entry)
	if err != nil {
		return err
	}
	var revision int64
	var storedDigest, storedRaw string
	err = tx.QueryRow(ctx, queryKubernetesOperatorTargetLatest, pgx.StrictNamedArgs{"ref": entry.Target.Ref}).Scan(&revision, &storedDigest, &storedRaw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if err == nil {
		present := false
		for _, old := range previous.Targets {
			present = present || old.Target.Ref == entry.Target.Ref && old.Target.Revision == revision
		}
		if entry.Target.Revision < revision || entry.Target.Revision == revision && (!present || digest != storedDigest || raw != storedRaw) {
			return errs.ErrConflict
		}
	}
	if _, err := tx.Exec(ctx, queryKubernetesOperatorTargetInsert, pgx.StrictNamedArgs{
		"ref": entry.Target.Ref, "revision": entry.Target.Revision, "registry_revision": sourceRevision,
		"cluster_uid": entry.Target.ClusterUID, "payload": raw, "content_sha256": digest,
	}); err != nil {
		return mapWriteError(err)
	}
	return nil
}

// registerKubernetesOperatorNodeTx binds a key globally and checks immutable pin/generation history.
// A retired ref/key cannot be revived through a new target alias, generation or deployment source.
// registerKubernetesOperatorNodeTx глобально привязывает key и проверяет immutable pin/generation history.
// Retired ref/key не восстанавливается через новый target alias, generation либо deployment source.
func registerKubernetesOperatorNodeTx(ctx context.Context, tx pgx.Tx, sourceRevision int64, entry value.KubernetesOperatorNode) error {
	pin := entry.Transport
	var targetRaw, targetDigest, clusterUID string
	if err := tx.QueryRow(ctx, queryKubernetesOperatorTarget, pgx.StrictNamedArgs{"ref": pin.TargetRef, "revision": pin.TargetRevision}).Scan(
		&targetDigest, &targetRaw, &clusterUID); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	} else if err != nil {
		return errs.ErrUnavailable
	}
	var target value.KubernetesOperatorTarget
	if json.Unmarshal([]byte(targetRaw), &target) != nil {
		return errs.ErrUnavailable
	}
	normalized, actualDigest, err := kubernetesOperatorPayload(target)
	if err != nil || normalized != targetRaw || actualDigest != targetDigest || target.Target.ClusterUID != clusterUID {
		return errs.ErrUnavailable
	}
	var ownerCluster, ownerNode string
	var keyRetired bool
	err = tx.QueryRow(ctx, queryKubernetesOperatorKey, pgx.StrictNamedArgs{"serving_key_sha256": pin.ServerPublicKeySHA256}).Scan(&ownerCluster, &ownerNode, &keyRetired)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if err == nil && (ownerCluster != clusterUID || ownerNode != pin.NodeUID || keyRetired && !entry.Retired) {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryKubernetesOperatorKeyInsert, pgx.StrictNamedArgs{
		"target_ref": pin.TargetRef, "target_revision": pin.TargetRevision, "serving_key_sha256": pin.ServerPublicKeySHA256,
		"cluster_uid": clusterUID, "node_uid": pin.NodeUID, "registry_revision": sourceRevision,
	}); err != nil {
		return mapWriteError(err)
	}
	raw, digest, err := kubernetesOperatorPayload(pin)
	if err != nil {
		return err
	}
	var generation int64
	var storedDigest string
	var retired bool
	err = tx.QueryRow(ctx, queryKubernetesOperatorNode, pgx.StrictNamedArgs{"ref": pin.Ref}).Scan(&generation, &storedDigest, &retired)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if err == nil && (generation != pin.Generation || storedDigest != digest || retired && !entry.Retired) {
		return errs.ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) && !entry.Retired {
		var maximum int64
		if tx.QueryRow(ctx, queryKubernetesOperatorNodeGeneration, pgx.StrictNamedArgs{
			"cluster_uid": clusterUID, "node_uid": pin.NodeUID, "protocol": string(pin.Protocol),
		}).Scan(&maximum) != nil {
			return errs.ErrUnavailable
		}
		if pin.Generation <= maximum {
			return errs.ErrConflict
		}
	}
	if _, err := tx.Exec(ctx, queryKubernetesOperatorNodeInsert, pgx.StrictNamedArgs{
		"ref": pin.Ref, "generation": pin.Generation, "target_ref": pin.TargetRef, "target_revision": pin.TargetRevision,
		"cluster_uid": clusterUID, "node_uid": pin.NodeUID, "protocol": string(pin.Protocol), "serving_key_sha256": pin.ServerPublicKeySHA256,
		"registry_revision": sourceRevision, "payload": raw, "content_sha256": digest,
	}); err != nil {
		return mapWriteError(err)
	}
	return nil
}
