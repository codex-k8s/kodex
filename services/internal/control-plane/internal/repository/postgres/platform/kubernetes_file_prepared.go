package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// kubernetesFilePreparedSource carries the private original source allowance, never caller authority.
// Its deadline remains the earliest admitted permission/lease bound through AV, PUT and readback.
// kubernetesFilePreparedSource хранит private original source allowance, не caller authority.
// Deadline сохраняет earliest admitted permission/lease bound через AV, PUT и readback.
type kubernetesFilePreparedSource struct {
	ref             string
	authorizedUntil time.Time
}

// validKubernetesFilePreparedBinding admits only the closed normalized-upload owner recipe.
// Full current owner/catalog/ACL proof is independently required by source preparation and DB guards.
// validKubernetesFilePreparedBinding допускает лишь closed normalized-upload owner recipe.
// Полный current owner/catalog/ACL proof отдельно требуется source preparation и DB guards.
func validKubernetesFilePreparedBinding(b preparedContentBinding, source kubernetesFilePreparedSource) bool {
	return opaqueref.Valid(source.ref, "kfs") && b.SourceProfile == "KUBERNETES_FILE_SOURCE" && b.SourceProfileRef == source.ref &&
		b.OperationKey == "kubernetes.cp.upload" && b.IntentOperation == "kubernetes.file.prepare" && b.IdempotencyKey == source.ref &&
		b.TargetArtifactID == "" && b.TargetArtifactRef == "" && b.SourceRevisionRef != "" && b.ExpectedArtifactVersion == 0 &&
		source.authorizedUntil.After(time.Now())
}

// queryKubernetesFilePreparedLink attaches the already committed unique prepared intent before PUT.
// queryKubernetesFilePreparedLink привязывает committed unique prepared intent до PUT.
//
//go:embed sql/kubernetes_file__prepared_link.sql
var queryKubernetesFilePreparedLink string

// queryKubernetesFileWriterCurrent rechecks source eligibility after the prepared-row lock, without inverted owner locks.
// queryKubernetesFileWriterCurrent проверяет source eligibility после prepared-row lock без inverse owner locks.
//
//go:embed sql/kubernetes_file__writer_current.sql
var queryKubernetesFileWriterCurrent string

// linkKubernetesFilePrepared fixes the source-to-prepared edge before the only external write.
// No failed link can dispatch a PUT or create another prepared intent under a new key.
// linkKubernetesFilePrepared фиксирует source-to-prepared edge до единственной external write.
// Неуспешная привязка не начинает PUT и не создаёт другой prepared intent с новым key.
func (repository *Repository) linkKubernetesFilePrepared(ctx context.Context, source kubernetesFilePreparedSource, organizationID, preparedID string) error {
	if ctx.Err() != nil || !source.authorizedUntil.After(time.Now()) {
		return errs.ErrUnavailable
	}
	tag, err := repository.pool.Exec(ctx, queryKubernetesFilePreparedLink, pgx.StrictNamedArgs{"source_ref": source.ref, "organization_id": organizationID, "prepared_id": preparedID})
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// currentKubernetesFileWriter preserves the captured deadline and refreshes the same source predicate.
// It never renews authority from a newer grant, catalog, artifact revision or stream.
// currentKubernetesFileWriter сохраняет captured deadline и обновляет тот же source predicate.
// Не продлевает authority через новый grant, catalog, artifact revision либо stream.
func currentKubernetesFileWriter(ctx context.Context, runner queryRunner, source kubernetesFilePreparedSource, organizationID, preparedID string) error {
	if ctx.Err() != nil {
		return errs.ErrUnavailable
	}
	if !source.authorizedUntil.After(time.Now()) {
		return errs.ErrForbidden
	}
	var id string
	err := runner.QueryRow(ctx, queryKubernetesFileWriterCurrent, pgx.StrictNamedArgs{"source_ref": source.ref, "organization_id": organizationID, "prepared_id": preparedID}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if !source.authorizedUntil.After(time.Now()) {
		return errs.ErrForbidden
	}
	return nil
}
