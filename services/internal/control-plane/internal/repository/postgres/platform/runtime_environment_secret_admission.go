package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeEnvironmentSecretBindingTarget freezes the requested current or explicit ENV version under its owner tuple; NOWAIT prevents a new agent-to-ENV wait inversion.
// queryRuntimeEnvironmentSecretBindingTarget закрепляет запрошенную current либо explicit ENV version в owner tuple; NOWAIT предотвращает новое обратное ожидание agent→ENV.
//
//go:embed sql/runtime_configuration__lock_environment_secret_target.sql
var queryRuntimeEnvironmentSecretBindingTarget string

// queryRuntimeEnvironmentSecretSnapshot reads an exact stored version under an ENV already locked by rollback or claim; the two selector forms are never a latest fallback.
// queryRuntimeEnvironmentSecretSnapshot читает exact stored version в ENV, уже заблокированном rollback либо claim; два вида selector не заменяются latest fallback.
//
//go:embed sql/runtime_configuration__environment_secret_snapshot.sql
var queryRuntimeEnvironmentSecretSnapshot string

// queryRuntimeSecretExactAdmissionLock acquires only owner-scoped metadata row locks before the canonical eligibility resolver; it never reads secret values or changes lifecycle fields.
// queryRuntimeSecretExactAdmissionLock берёт лишь owner-scoped metadata row locks перед каноническим eligibility resolver; он не читает значения секретов и не меняет lifecycle fields.
//
//go:embed sql/runtime_secret__lock_exact_admission.sql
var queryRuntimeSecretExactAdmissionLock string

// runtimeEnvironmentSecretTarget carries the server-resolved configuration owner and immutable version descriptors, independently of a SYSTEM run's project context.
// runtimeEnvironmentSecretTarget переносит разрешённого сервером владельца конфигурации и immutable version descriptors независимо от проектного контекста SYSTEM run.
type runtimeEnvironmentSecretTarget struct {
	scopeKind, projectID, versionRef string
	descriptors                      []entity.RuntimeSecretDescriptor
}

// scanRuntimeEnvironmentSecretTarget rejects missing or corrupt owner snapshots and keeps SQL/lock contention unavailable instead of treating it as an inactive Secret.
// scanRuntimeEnvironmentSecretTarget отклоняет отсутствующий либо повреждённый owner snapshot и сохраняет SQL/lock contention как Unavailable, а не неактивный Secret.
func scanRuntimeEnvironmentSecretTarget(row pgx.Row) (runtimeEnvironmentSecretTarget, error) {
	var target runtimeEnvironmentSecretTarget
	var raw []byte
	if err := row.Scan(&target.scopeKind, &target.projectID, &target.versionRef, &raw); errors.Is(err, pgx.ErrNoRows) {
		return target, errs.ErrNotFound
	} else if err != nil {
		return target, errs.ErrUnavailable
	}
	if target.versionRef == "" || target.scopeKind != "PROJECT" && target.scopeKind != "ORGANIZATION" ||
		(target.scopeKind == "ORGANIZATION") != (target.projectID == "") || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) ||
		!json.Valid(raw) || decodeStrict(raw, &target.descriptors) != nil || len(target.descriptors) > 128 {
		return target, errs.ErrUnavailable
	}
	return target, nil
}

// requireRuntimeEnvironmentSecretVersion resolves only an explicit immutable version ID or ref under its exact ENV/org; callers retain metadata locks, resource authorization and commit ownership.
// requireRuntimeEnvironmentSecretVersion разрешает лишь explicit immutable version ID либо ref в exact ENV/org; metadata locks, resource authorization и commit остаются у вызывающего.
func requireRuntimeEnvironmentSecretVersion(ctx context.Context, tx pgx.Tx, organizationID, environmentRef, versionID, versionRef string) error {
	if (versionID == "") == (versionRef == "") {
		return errs.ErrInvalid
	}
	target, err := scanRuntimeEnvironmentSecretTarget(tx.QueryRow(ctx, queryRuntimeEnvironmentSecretSnapshot, pgx.StrictNamedArgs{
		"organization_id": organizationID, "environment_ref": environmentRef, "version_id": versionID, "version_ref": versionRef,
	}))
	if err != nil {
		return err
	}
	return requireRuntimeEnvironmentSecretDescriptors(ctx, tx, organizationID, target)
}

// requireRuntimeEnvironmentSecretDescriptors locks sorted exact Secret/revision metadata without waiting, reuses the existing ACTIVE scoped resolver, and compares every stored material pin. It preserves historical revisions and never selects current/latest in place of an explicit descriptor. Locks survive to the enclosing reference/RuntimeRevision commit so terminal RC recovery sees the winning reference after its Secret lock wait.
// requireRuntimeEnvironmentSecretDescriptors без ожидания блокирует отсортированные exact Secret/revision metadata, переиспользует существующий ACTIVE scoped resolver и сравнивает каждый stored material pin. Historical revisions сохраняются без подстановки current/latest. Locks живут до внешнего reference/RuntimeRevision commit, чтобы terminal RC recovery после ожидания Secret lock увидел победившую ссылку.
func requireRuntimeEnvironmentSecretDescriptors(ctx context.Context, tx pgx.Tx, organizationID string, target runtimeEnvironmentSecretTarget) error {
	descriptors := append([]entity.RuntimeSecretDescriptor(nil), target.descriptors...)
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].SecretRef != descriptors[j].SecretRef {
			return descriptors[i].SecretRef < descriptors[j].SecretRef
		}
		return descriptors[i].Revision < descriptors[j].Revision
	})
	for _, expected := range descriptors {
		if expected.Revision < 1 || !strings.HasPrefix(expected.SecretRef, "sec_") || expected.Name == "" || expected.Namespace == "" {
			return errs.ErrConflict
		}
		pins := pgx.StrictNamedArgs{"organization_id": organizationID, "project_id": target.projectID, "scope_kind": target.scopeKind,
			"secret_ref": expected.SecretRef, "revision": expected.Revision}
		var revision int64
		if err := tx.QueryRow(ctx, queryRuntimeSecretExactAdmissionLock, pins).Scan(&revision); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
		if revision != expected.Revision {
			return errs.ErrUnavailable
		}
		actual := entity.RuntimeSecretDescriptor{Name: expected.Name}
		if err := tx.QueryRow(ctx, queryRuntimeSecretResolveBinding, pins).Scan(&actual.SecretRef, &actual.Namespace, &actual.Revision,
			&actual.SecretName, &actual.SecretKey, &actual.SecretUID, &actual.SecretResourceVersion, &actual.ContentSHA256); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
		if actual != expected {
			return errs.ErrConflict
		}
	}
	return nil
}
