package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/project_purge__due.sql
var queryProjectPurgeDue string

//go:embed sql/project_purge__pending.sql
var queryProjectPurgePending string

//go:embed sql/project_purge__inventory.sql
var queryProjectPurgeInventory string

//go:embed sql/project_purge__foreign_object.sql
var queryProjectPurgeForeignObject string

//go:embed sql/project_purge__archive_owner.sql
var queryProjectPurgeArchiveOwner string

//go:embed sql/project_purge__global_pvc_owner.sql
var queryProjectPurgeGlobalPVCOwner string

//go:embed sql/project_purge__subject_artifact_owner.sql
var queryProjectPurgeSubjectArtifactOwner string

//go:embed sql/project_purge__lock_receipt.sql
var queryProjectPurgeLockReceipt string

//go:embed sql/project_purge__lock_project.sql
var queryProjectPurgeLockProject string

//go:embed sql/project_purge__active_tasks.sql
var queryProjectPurgeActiveTasks string

//go:embed sql/project_purge__digest.sql
var queryProjectPurgeDigest string

//go:embed sql/project_purge__mark_objects_cleared.sql
var queryProjectPurgeMarkObjectsCleared string

//go:embed sql/project_purge__delete_database.sql
var queryProjectPurgeDeleteDatabase string

type ProjectPurgeCandidate struct {
	OrganizationID, OrganizationRef, ProjectID, ProjectRef string
}

type ProjectPurgeExternalItem struct {
	Kind, Target, Version string
}

func (repository *Repository) PromoteDueProjectPurges(ctx context.Context, limit int32) error {
	if limit < 1 || limit > 16 {
		return errs.ErrInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	type promotedProject struct {
		organizationID, organizationRef, projectID, projectRef string
		version                                                int64
	}
	rows, err := tx.Query(ctx, queryProjectPurgeDue, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	promoted := make([]promotedProject, 0, limit)
	for rows.Next() {
		var item promotedProject
		var receiptCount, cancelledTaskCount int64
		if err := rows.Scan(&item.organizationID, &item.organizationRef, &item.projectID,
			&item.projectRef, &item.version, &receiptCount, &cancelledTaskCount); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		if item.organizationID == "" || item.organizationRef == "" || item.projectID == "" ||
			item.projectRef == "" || item.version < 1 || receiptCount < 1 || cancelledTaskCount < 0 {
			rows.Close()
			return errs.ErrConflict
		}
		promoted = append(promoted, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return errs.ErrUnavailable
	}
	rows.Close()
	for _, item := range promoted {
		current := scope{
			organizationID: item.organizationID, organizationRef: item.organizationRef,
			correlationRef: item.projectRef,
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "PROJECT_CHANGED", item.projectRef,
			item.projectRef, "i18n:PROJECT_PURGE_REQUESTED", item.version, "PURGE_PENDING"); err != nil {
			return errs.ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) ListPendingProjectPurges(ctx context.Context, limit int32) ([]ProjectPurgeCandidate, error) {
	if limit < 1 || limit > 16 {
		return nil, errs.ErrInvalid
	}
	rows, err := repository.pool.Query(ctx, queryProjectPurgePending, pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ProjectPurgeCandidate, 0, limit)
	for rows.Next() {
		var item ProjectPurgeCandidate
		if err := rows.Scan(&item.OrganizationID, &item.OrganizationRef, &item.ProjectID, &item.ProjectRef); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func (repository *Repository) ProjectPurgeInventory(ctx context.Context, candidate ProjectPurgeCandidate) ([]ProjectPurgeExternalItem, error) {
	var blocked bool
	if err := repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryBlocker, pgx.StrictNamedArgs{"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID}).Scan(&blocked); err != nil {
		return nil, errs.ErrUnavailable
	}
	if blocked {
		return nil, errs.ErrConflict
	}
	var activeTasks int
	if err := repository.pool.QueryRow(ctx, queryProjectPurgeActiveTasks,
		candidate.OrganizationID, candidate.ProjectID).Scan(&activeTasks); err != nil {
		return nil, errs.ErrUnavailable
	}
	if activeTasks != 0 {
		return nil, errs.ErrConflict
	}
	rows, err := repository.pool.Query(ctx, queryProjectPurgeInventory, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ProjectPurgeExternalItem, 0)
	for rows.Next() {
		var item ProjectPurgeExternalItem
		if err := rows.Scan(&item.Kind, &item.Target, &item.Version); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func (repository *Repository) ProjectPurgeObjectIsShared(ctx context.Context, candidate ProjectPurgeCandidate, key string) (bool, error) {
	var shared bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeForeignObject, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID, "object_key": key,
	}).Scan(&shared)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return shared, nil
}

func (repository *Repository) ProjectPurgeArchiveIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, item ProjectPurgeExternalItem) (bool, error) {
	var owned bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeArchiveOwner, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
		"project_ref": candidate.ProjectRef, "object_key": item.Target, "object_version": item.Version,
	}).Scan(&owned)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return owned, nil
}

func (repository *Repository) ProjectPurgeSubjectArtifactIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, item ProjectPurgeExternalItem) (bool, error) {
	var owned bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeSubjectArtifactOwner, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
		"project_ref": candidate.ProjectRef, "object_key": item.Target, "object_version": item.Version,
	}).Scan(&owned)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return owned, nil
}

func (repository *Repository) ProjectPurgeGlobalPVCIsOwned(ctx context.Context, candidate ProjectPurgeCandidate, sessionRef string) (bool, error) {
	var owned bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeGlobalPVCOwner, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
		"project_ref": candidate.ProjectRef, "session_ref": sessionRef,
	}).Scan(&owned)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return owned, nil
}

func (repository *Repository) FinalizeProjectPurge(ctx context.Context, candidate ProjectPurgeCandidate, expected []ProjectPurgeExternalItem) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	err = tx.QueryRow(ctx, queryProjectPurgeLockReceipt,
		candidate.OrganizationID, candidate.ProjectID, candidate.ProjectRef).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if state == "DONE" {
		return nil
	}
	if state != "PENDING" {
		return errs.ErrConflict
	}
	var projectRef string
	var projectVersion int64
	err = tx.QueryRow(ctx, queryProjectPurgeLockProject,
		candidate.OrganizationID, candidate.ProjectID).Scan(&projectRef, &projectVersion)
	if err != nil || projectRef != candidate.ProjectRef {
		return errs.ErrConflict
	}
	rows, err := tx.Query(ctx, queryProjectPurgeInventory, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	actual := make([]ProjectPurgeExternalItem, 0, len(expected))
	for rows.Next() {
		var item ProjectPurgeExternalItem
		if err := rows.Scan(&item.Kind, &item.Target, &item.Version); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		actual = append(actual, item)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil || len(actual) != len(expected) {
		return errs.ErrConflict
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return errs.ErrConflict
		}
	}
	var activeTasks int
	if err := tx.QueryRow(ctx, queryProjectPurgeActiveTasks,
		candidate.OrganizationID, candidate.ProjectID).Scan(&activeTasks); err != nil || activeTasks != 0 {
		return errs.ErrConflict
	}
	var digest string
	if err := tx.QueryRow(ctx, queryProjectPurgeDigest,
		candidate.OrganizationID, candidate.ProjectID).Scan(&digest); err != nil || len(digest) != 64 {
		return errs.ErrConflict
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryProjectPurgeMarkObjectsCleared,
		candidate.OrganizationID, candidate.ProjectID, digest); err != nil {
		return errs.ErrUnavailable
	}
	var deletedRows int
	if err := tx.QueryRow(ctx, queryProjectPurgeDeleteDatabase,
		candidate.OrganizationID, candidate.ProjectID, digest).Scan(&deletedRows); err != nil || deletedRows < 0 {
		return errs.ErrConflict
	}
	current := scope{
		organizationID: candidate.OrganizationID, organizationRef: candidate.OrganizationRef,
		correlationRef: candidate.ProjectRef,
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "PROJECT_CHANGED", candidate.ProjectRef,
		candidate.ProjectRef, "i18n:PROJECT_PURGED", projectVersion, "PURGED"); err != nil {
		return errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrConflict
	}
	return nil
}
