package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFilePreparedMutex serializes exact-profile intent insertion against closure absence proof.
// It carries no actor/permission authority and never substitutes a source or key.
// queryKubernetesFilePreparedMutex сериализует exact-profile intent insertion и closure absence proof.
// Actor/permission authority не несёт и не подменяет source либо key.
//
//go:embed sql/kubernetes_file__prepared_mutex.sql
var queryKubernetesFilePreparedMutex string

// queryKubernetesFileReservationCurrent rechecks closure after acquiring the shared source mutex.
// It takes no source-row lock and never substitutes for the INSERT trigger's complete original owner proof.
// queryKubernetesFileReservationCurrent повторно проверяет closure после shared source mutex.
// Source-row lock не берёт и не заменяет полную original owner проверку INSERT trigger.
//
//go:embed sql/kubernetes_file__reservation_current.sql
var queryKubernetesFileReservationCurrent string

// reserveKubernetesFilePrepared commits the existing unique intent under project -> source mutex ordering.
// It deliberately acquires no source-row lock: cleanup uses source -> mutex, avoiding root/project inversion.
// Ordinary/OUTPUT reservations retain the previous direct INSERT; commit uncertainty never permits another PUT.
// reserveKubernetesFilePrepared фиксирует existing unique intent в порядке project -> source mutex.
// Source-row lock не берёт: cleanup использует source -> mutex, избегая root/project inversion.
// Ordinary/OUTPUT reservations сохраняют прежний INSERT; commit uncertainty не разрешает повторный PUT.
func (repository *Repository) reserveKubernetesFilePrepared(ctx context.Context, source kubernetesFilePreparedSource, b preparedContentBinding, args pgx.StrictNamedArgs) (string, error) {
	if ctx.Err() != nil {
		return "", errs.ErrUnavailable
	}
	if !source.authorizedUntil.After(time.Now()) {
		return "", errs.ErrForbidden
	}
	if !validKubernetesFilePreparedBinding(b, source) {
		return "", errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := lockKubernetesPreparedProjectTx(ctx, tx, b); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, queryKubernetesFilePreparedMutex, pgx.StrictNamedArgs{"organization_id": b.OrganizationID, "source_ref": source.ref}); err != nil {
		return "", errs.ErrUnavailable
	}
	if ctx.Err() != nil {
		return "", errs.ErrUnavailable
	}
	if !source.authorizedUntil.After(time.Now()) {
		return "", errs.ErrForbidden
	}
	var sourceID string
	if err := tx.QueryRow(ctx, queryKubernetesFileReservationCurrent, pgx.StrictNamedArgs{"organization_id": b.OrganizationID, "source_ref": source.ref}).Scan(&sourceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errs.ErrForbidden
		}
		return "", errs.ErrUnavailable
	}
	var id string
	insertErr := tx.QueryRow(ctx, queryPreparedContentInsert, args).Scan(&id)
	if insertErr != nil && !errors.Is(insertErr, pgx.ErrNoRows) {
		return "", insertErr
	}
	if ctx.Err() != nil {
		return "", errs.ErrUnavailable
	}
	if !source.authorizedUntil.After(time.Now()) {
		return "", errs.ErrForbidden
	}
	if tx.Commit(ctx) != nil {
		return "", errs.ErrUnavailable
	}
	if ctx.Err() != nil {
		return "", errs.ErrUnavailable
	}
	if !source.authorizedUntil.After(time.Now()) {
		return "", errs.ErrForbidden
	}
	return id, insertErr
}
