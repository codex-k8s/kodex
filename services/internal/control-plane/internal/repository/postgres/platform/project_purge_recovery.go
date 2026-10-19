package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql/project_trash__recovery_authority.sql
var queryProjectPurgeRecoveryAuthority string

//go:embed sql/project_trash__recovery_readiness.sql
var queryProjectPurgeRecoveryReadiness string

//go:embed sql/project_purge_recovery__snapshot.sql
var queryProjectPurgeRecoverySnapshot string

//go:embed sql/project_purge_recovery__resolve.sql
var queryProjectPurgeRecoveryResolve string

//go:embed sql/project_purge_recovery__claim.sql
var queryProjectPurgeRecoveryClaim string

//go:embed sql/project_purge_recovery__pin.sql
var queryProjectPurgeRecoveryPin string

//go:embed sql/project_purge_recovery__finish.sql
var queryProjectPurgeRecoveryFinish string

//go:embed sql/project_purge_recovery__blocker.sql
var queryProjectPurgeRecoveryBlocker string

// ConfigureProjectPurgeStorage связывает cleanup только с тем хранилищем,
// куда была выдана owner resolution; смена credentials не меняет identity.
func (repository *Repository) ConfigureProjectPurgeStorage(endpoint, region, bucket string, pathStyle bool) error {
	if endpoint == "" || region == "" || bucket == "" {
		return errs.ErrInvalid
	}
	encoded, err := json.Marshal(struct {
		Endpoint, Region, Bucket string
		PathStyle                bool
	}{endpoint, region, bucket, pathStyle})
	if err != nil {
		return errs.ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	repository.projectPurgeStorageIdentity = hex.EncodeToString(digest[:])
	return nil
}

func (repository *Repository) authorizeProjectPurgeRecovery(ctx context.Context, tx pgx.Tx, current scope, ref string) error {
	var exists bool
	if err := tx.QueryRow(ctx, queryProjectPurgeRecoveryAuthority, current.organizationID, ref).Scan(&exists); err != nil {
		return errs.ErrUnavailable
	}
	if !exists {
		return errs.ErrNotFound
	}
	return nil
}

func decodeProjectPurgeRecovery(raw []byte) (entity.ProjectPurgeRecovery, error) {
	var item entity.ProjectPurgeRecovery
	if len(raw) == 0 {
		return item, errs.ErrNotFound
	}
	if json.Unmarshal(raw, &item) != nil || item.Version < 1 || !exactSHA256(item.SnapshotDigest) || item.ProjectRef == "" || item.UnknownContentCount < 0 || item.CleanupPendingCount < 0 || item.ProjectPresent != (item.ProjectVersion > 0) {
		return entity.ProjectPurgeRecovery{}, errs.ErrConflict
	}
	switch item.State {
	case "WAITING_OWNER", "QUEUED", "OBSERVING", "DELETED_CONFIRMED", "NONE", "BLOCKED":
	default:
		return entity.ProjectPurgeRecovery{}, errs.ErrConflict
	}
	if item.CanResolve && item.State != "WAITING_OWNER" {
		return entity.ProjectPurgeRecovery{}, errs.ErrConflict
	}
	return item, nil
}

func (repository *Repository) GetProjectPurgeRecovery(ctx context.Context, principal value.Principal, ref string) (entity.ProjectPurgeRecovery, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ProjectPurgeRecovery{}, err
	}
	if current.authorityProjectID != "" || current.role != "OWNER" && current.role != "ADMINISTRATOR" || principal.CallerWorkload != "control-api-gateway" {
		return entity.ProjectPurgeRecovery{}, errs.ErrNotFound
	}
	if repository.projectPurgeStorageIdentity == "" {
		return entity.ProjectPurgeRecovery{}, errs.ErrUnavailable
	}
	var raw []byte
	if err := repository.pool.QueryRow(ctx, queryProjectPurgeRecoverySnapshot, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": ref, "storage_identity": repository.projectPurgeStorageIdentity}).Scan(&raw); err != nil {
		return entity.ProjectPurgeRecovery{}, errs.ErrUnavailable
	}
	return decodeProjectPurgeRecovery(raw)
}

func (repository *Repository) resolveProjectPurgeRecovery(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectPurgeRecoveryInput)
	if !ok || input.Mutation.ExpectedVersion == nil || !payload.AcknowledgeUnknownOutcome || !exactSHA256(payload.SnapshotDigest) {
		return commandOutcome{}, errs.ErrInvalid
	}
	if repository.projectPurgeStorageIdentity == "" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	resolutionRef, err := newRef("ppr")
	if err != nil {
		return commandOutcome{}, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, queryProjectPurgeRecoveryResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": payload.Ref, "expected_version": *input.Mutation.ExpectedVersion, "snapshot_digest": payload.SnapshotDigest, "actor_id": current.actorID, "resolution_ref": resolutionRef, "storage_identity": repository.projectPurgeStorageIdentity}).Scan(&raw)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "40001":
				return commandOutcome{}, errs.ErrVersionMismatch
			case "P0002", "42501":
				return commandOutcome{}, errs.ErrNotFound
			case "22023":
				return commandOutcome{}, errs.ErrInvalid
			case "P0001":
				return commandOutcome{}, errs.ErrConflict
			}
		}
		return commandOutcome{}, errs.ErrUnavailable
	}
	item, err := decodeProjectPurgeRecovery(raw)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome := commandOutcome{result: command.Result{ProjectPurgeRecovery: &item}, resourceKind: "PROJECT_PURGE_RECOVERY", resourceRef: payload.Ref, summary: "i18n:PROJECT_PURGE_RECOVERY_RESOLVED"}
	if item.ProjectPresent {
		var project entity.Project
		err = tx.QueryRow(ctx, queryProjectTrashLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": payload.Ref}).Scan(&outcome.projectID, &project.Ref, &project.Name, &project.Purpose, &project.Language, &project.Lifecycle, &project.Version, &project.CreatedAt, &project.UpdatedAt, &project.DeletedAt, &project.PurgeAfter)
		if err != nil || project.Version != item.ProjectVersion {
			return commandOutcome{}, errs.ErrConflict
		}
		outcome.projectRef = payload.Ref
		outcome.platformEvent = "PROJECT_CHANGED"
		outcome.platformAggregateVersion = project.Version
		outcome.platformState = project.Lifecycle
	}
	return outcome, nil
}

type ProjectPurgeRecoveryClaim struct {
	LedgerID, OrganizationID, ProjectRef string
	Key, Version, ETag, Digest           string
	SizeBytes, Generation                int64
	Deadline                             time.Time
}

func (repository *Repository) ClaimProjectPurgeRecovery(ctx context.Context, owner string, limit int32) ([]ProjectPurgeRecoveryClaim, error) {
	if owner == "" || limit < 1 || limit > 4 || repository.projectPurgeStorageIdentity == "" {
		return nil, errs.ErrInvalid
	}
	rows, err := repository.pool.Query(ctx, queryProjectPurgeRecoveryClaim, pgx.StrictNamedArgs{"owner": owner, "limit": limit, "storage_identity": repository.projectPurgeStorageIdentity})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]ProjectPurgeRecoveryClaim, 0, limit)
	for rows.Next() {
		var item ProjectPurgeRecoveryClaim
		if err := rows.Scan(&item.LedgerID, &item.OrganizationID, &item.ProjectRef, &item.Key, &item.Version, &item.ETag, &item.Digest, &item.SizeBytes, &item.Generation, &item.Deadline); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func (repository *Repository) PinProjectPurgeRecovery(ctx context.Context, owner string, claim ProjectPurgeRecoveryClaim, receipt objectstorage.Receipt) error {
	if receipt.Key != claim.Key || receipt.Digest != claim.Digest || receipt.SizeBytes != claim.SizeBytes || receipt.VersionID == "" || receipt.ETag == "" {
		return errs.ErrConflict
	}
	var pinned bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryPin, pgx.StrictNamedArgs{"id": claim.LedgerID, "owner": owner, "generation": claim.Generation, "storage_identity": repository.projectPurgeStorageIdentity, "object_version": receipt.VersionID, "object_etag": receipt.ETag}).Scan(&pinned)
	if err != nil {
		return errs.ErrUnavailable
	}
	if !pinned {
		return errs.ErrConflict
	}
	return nil
}

func (repository *Repository) FinishProjectPurgeRecovery(ctx context.Context, owner string, claim ProjectPurgeRecoveryClaim, outcome string) error {
	switch outcome {
	case "ABSENT", "DELETED", "FAILED":
	default:
		return errs.ErrInvalid
	}
	var finished bool
	err := repository.pool.QueryRow(ctx, queryProjectPurgeRecoveryFinish, pgx.StrictNamedArgs{"id": claim.LedgerID, "owner": owner, "generation": claim.Generation, "storage_identity": repository.projectPurgeStorageIdentity, "outcome": outcome}).Scan(&finished)
	if err != nil {
		return errs.ErrUnavailable
	}
	if !finished {
		return errs.ErrConflict
	}
	return nil
}
