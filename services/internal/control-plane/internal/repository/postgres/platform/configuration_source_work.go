package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/configuration_source__work_candidates.sql
var queryConfigurationSourceWorkCandidates string

//go:embed sql/configuration_source__lock_work.sql
var queryConfigurationSourceLockWork string

//go:embed sql/configuration_source__claim.sql
var queryConfigurationSourceClaim string

//go:embed sql/configuration_source__state.sql
var queryConfigurationSourceState string

//go:embed sql/configuration_source__renew.sql
var queryConfigurationSourceRenew string

//go:embed sql/configuration_source__finish.sql
var queryConfigurationSourceFinish string

type sourceWorkReceipt struct {
	Action, Digest string
	Result         entity.ManagedConfigurationGitSource
}

type sourceWorkRow struct {
	id, state, configurationRef, actorRef, organizationRef string
	work                                                   entity.ManagedConfigurationSourceWork
	lease                                                  entity.ManagedConfigurationSourceLease
	deadline                                               time.Time
	receipts                                               map[string]sourceWorkReceipt
}

func lockSourceWork(ctx context.Context, tx pgx.Tx, current scope, ref string) (sourceWorkRow, error) {
	var row sourceWorkRow
	var raw, receipts []byte
	var digest string
	var expires *time.Time
	row.lease.WorkRef = ref
	err := tx.QueryRow(ctx, queryConfigurationSourceLockWork, current.organizationID, ref).Scan(&row.id, &row.state, &row.lease.Attempt, &row.lease.ClaimGeneration, &row.lease.Claimant, &row.lease.Fence, &expires,
		&row.deadline, &raw, &digest, &receipts, &row.configurationRef, &row.actorRef, &row.organizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return sourceWorkRow{}, errs.ErrNotFound
	}
	if err != nil {
		return sourceWorkRow{}, errs.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&row.work) != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(receipts, &row.receipts) != nil {
		return sourceWorkRow{}, errs.ErrUnavailable
	}
	computed := sha256.Sum256(asJSON(row.work))
	if hex.EncodeToString(computed[:]) != digest || row.work.Lease.WorkRef != ref || row.work.ConfigurationRef != row.configurationRef || !row.work.Deadline.Equal(row.deadline) {
		return sourceWorkRow{}, errs.ErrUnavailable
	}
	row.lease.SourceGeneration = row.work.Lease.SourceGeneration
	if expires != nil {
		row.lease.ExpiresAt = *expires
	}
	return row, nil
}

func (repository *Repository) sourceWorkEligibility(ctx context.Context, tx pgx.Tx, row sourceWorkRow, current scope) (configurationSource, scope, error) {
	source, err := readConfigurationSource(ctx, tx, current.organizationID, row.configurationRef)
	if err != nil {
		return configurationSource{}, scope{}, errs.ErrForbidden
	}
	if source.Generation != row.lease.SourceGeneration || source.Ref != row.work.SourceRef || source.State == entity.ConfigurationSourceDetached {
		return source, scope{}, errs.ErrForbidden
	}
	var actor scope
	if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, row.actorRef, row.organizationRef).Scan(
		&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role); err != nil {
		return source, scope{}, errs.ErrForbidden
	}
	actor.correlationRef = current.correlationRef
	if actor.organizationID != current.organizationID || actor.actorID != source.actorID {
		return source, actor, errs.ErrForbidden
	}
	kind := command.RefreshIntegrationDefinitionGitSource
	if row.work.Kind == "ROLE_IMAGE" {
		kind = command.RefreshRoleImageGitSource
	}
	set, err := repository.configurationSourceAuthority(ctx, tx, actor, command.Command{Kind: kind, Payload: command.ManagedConfigurationGitSourceInput{ConfigurationRef: row.configurationRef}})
	if err != nil || set.ManagedBy != "GIT" || set.Source != source.Ref {
		return source, actor, errs.ErrForbidden
	}
	payload := command.ManagedConfigurationGitSourceInput{ConfigurationRef: set.Ref, ConnectionRef: row.work.ConnectionRef, ExpectedConnectionVersion: row.work.ConnectionVersion,
		RepositoryRef: row.work.RepositoryRef, RefName: row.work.RefName, Path: row.work.Path, ContentFormat: row.work.ContentFormat}
	fresh, _, _, err := repository.sourceInput(ctx, tx, actor, set, payload)
	if err != nil {
		return source, actor, err
	}
	if fresh.DefinitionDigest != row.work.DefinitionDigest || fresh.DefinitionVersion != row.work.DefinitionVersion ||
		!bytes.Equal(asJSON(fresh.CredentialRevision), asJSON(row.work.CredentialRevision)) || !bytes.Equal(asJSON(fresh.PublicConfiguration), asJSON(row.work.PublicConfiguration)) {
		return source, actor, errs.ErrForbidden
	}
	return source, actor, nil
}

func sourceLeaseMatches(row sourceWorkRow, lease entity.ManagedConfigurationSourceLease, now time.Time) bool {
	return row.state == "CLAIMED" && lease.WorkRef == row.lease.WorkRef && lease.SourceGeneration == row.lease.SourceGeneration &&
		lease.Attempt == row.lease.Attempt && lease.ClaimGeneration == row.lease.ClaimGeneration && lease.Claimant == row.lease.Claimant &&
		lease.Fence != "" && lease.Fence == row.lease.Fence && !lease.ExpiresAt.IsZero() && !lease.ExpiresAt.After(row.lease.ExpiresAt) &&
		now.Before(row.lease.ExpiresAt) && now.Before(row.deadline)
}

func sourceReceiptKey(lease entity.ManagedConfigurationSourceLease) string {
	return strconv.FormatInt(lease.Attempt, 10) + "/" + strconv.FormatInt(lease.ClaimGeneration, 10)
}

func (repository *Repository) sourceState(ctx context.Context, tx pgx.Tx, current scope, source configurationSource, state, failure string) (entity.ManagedConfigurationGitSource, error) {
	if state != entity.ConfigurationSourceBlocked {
		failure = ""
	}
	if _, err := tx.Exec(ctx, queryConfigurationSourceState, source.id, state, failure); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	source.State, source.FailureCode, source.Version = state, failure, source.Version+1
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil,
		current.actorID, "configuration-source."+state, "MANAGED_CONFIGURATION_SOURCE", source.Ref, "i18n:MANAGED_CONFIGURATION_CHANGED", current.correlationRef); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	return source.ManagedConfigurationGitSource, nil
}

func (repository *Repository) ClaimConfigurationSourceWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ManagedConfigurationSourceWork, error) {
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.refreshConfigurationSources(ctx, tx, current, limit); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, queryConfigurationSourceWorkCandidates, current.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]entity.ManagedConfigurationSourceWork, 0, len(refs))
	for _, ref := range refs {
		row, err := lockSourceWork(ctx, tx, current, ref)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if row.state != "QUEUED" && (row.state != "CLAIMED" || now.Before(row.lease.ExpiresAt)) {
			continue
		}
		source, actor, eligibilityErr := repository.sourceWorkEligibility(ctx, tx, row, current)
		if eligibilityErr != nil || !now.Before(row.deadline) || row.state == "CLAIMED" && row.lease.Attempt >= 3 {
			if source.id == "" {
				return nil, errs.ErrUnavailable
			}
			failure := entity.ConfigurationSourceAccess
			if eligibilityErr == nil {
				failure = entity.ConfigurationSourceUnavailable
			}
			if _, err := repository.sourceState(ctx, tx, current, source, entity.ConfigurationSourceBlocked, failure); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, queryConfigurationSourceFinish, row.id, "EXPIRED", asJSON(row.receipts), failure, ""); err != nil {
				return nil, errs.ErrUnavailable
			}
			continue
		}
		attempt := row.lease.Attempt
		if row.state == "CLAIMED" {
			attempt++
		}
		fence, err := newRef("mcsf")
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		expires := now.Truncate(time.Microsecond).Add(time.Minute)
		if expires.After(row.deadline) {
			expires = row.deadline
		}
		if _, err := tx.Exec(ctx, queryConfigurationSourceClaim, row.id, attempt, claimant, fence, expires); err != nil {
			return nil, errs.ErrConflict
		}
		if _, err := repository.sourceState(ctx, tx, actor, source, entity.ConfigurationSourceClaimed, ""); err != nil {
			return nil, err
		}
		row.work.Lease = entity.ManagedConfigurationSourceLease{WorkRef: ref, SourceGeneration: row.lease.SourceGeneration, Attempt: attempt, ClaimGeneration: row.lease.ClaimGeneration + 1, Claimant: claimant, Fence: fence, ExpiresAt: expires}
		result = append(result, row.work)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) RenewConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease) (entity.ManagedConfigurationSourceLease, error) {
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockSourceWork(ctx, tx, current, lease.WorkRef)
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !sourceLeaseMatches(row, lease, now) {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrForbidden
	}
	if _, _, err := repository.sourceWorkEligibility(ctx, tx, row, current); err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	expires := now.Add(time.Minute)
	if expires.After(row.deadline) {
		expires = row.deadline
	}
	if expires.Before(row.lease.ExpiresAt) {
		expires = row.lease.ExpiresAt
	}
	if _, err := tx.Exec(ctx, queryConfigurationSourceRenew, row.id, expires); err != nil {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrConflict
	}
	row.lease.ExpiresAt = expires
	return row.lease, nil
}
