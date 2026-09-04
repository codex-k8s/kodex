package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const (
	runtimeSecretOperationTTL = time.Minute
	runtimeSecretClaimLease   = 30 * time.Second
	runtimeSecretRevealMaxAge = 5 * time.Minute
	runtimeSecretKey          = "value"
	runtimeSecretRecoveryPage = int32(50)
	runtimeSecretRecoveryMax  = int32(100)
)

const runtimeSecretRecoveryCursorVersion = "v1"
const runtimeSecretListCursorVersion = "v1"

type runtimeSecretListCursor struct {
	Version, FilterDigest, Ref string
}

type lockedRuntimeSecret struct {
	id, ref, projectID, projectRef, name, description, valueType, state, namespace string
	version, currentRevision                                                       int64
	createdAt, updatedAt                                                           time.Time
}

type lockedRuntimeSecretOperation struct {
	id, ref, kind, state, projectID, secretID, actorID, correlationRef string
	projectRef, secretRef, name, description, valueType, namespace     string
	secretState                                                        string
	expectedContentSHA256, claimantID, failureCode, intentDigest       string
	targetRevision, expectedSecretVersion, expectedCurrentRevision     int64
	claimGeneration, secretVersion, secretCurrentRevision              int64
	grantExpiresAt, secretCreatedAt, secretUpdatedAt                   time.Time
	leaseDeadline                                                      *time.Time
	terminalSnapshot                                                   []byte
}

func (repository *Repository) ListRuntimeSecrets(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RuntimeSecret, string, error) {
	if principal.Permission != "secret.view" {
		return nil, "", errs.ErrForbidden
	}
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.ProjectRef == "" || len([]rune(filter.Query)) > 200 {
		return nil, "", errs.ErrInvalid
	}
	cursor, err := decodeRuntimeSecretListCursor(filter.Page.Token, filter.ProjectRef, filter.Query)
	if err != nil {
		return nil, "", err
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	project, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: filter.ProjectRef})
	if err != nil || principal.ProjectRef != "" && principal.ProjectRef != project.projectID {
		return nil, "", errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.view", project); err != nil {
		return nil, "", errs.ErrNotFound
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryRuntimeSecretsList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": filter.ProjectRef,
		"query": filter.Query, "cursor_ref": cursor.Ref, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RuntimeSecret, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanRuntimeSecret(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next, err = encodeRuntimeSecretListCursor(filter.ProjectRef, filter.Query, items[len(items)-1].Ref)
		if err != nil || next == filter.Page.Token {
			return nil, "", errs.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrConflict
	}
	return items, next, nil
}

func runtimeSecretListFilterDigest(projectRef, queryValue string) string {
	digest := sha256.Sum256([]byte(projectRef + "\x00" + queryValue))
	return hex.EncodeToString(digest[:])
}

func encodeRuntimeSecretListCursor(projectRef, queryValue, ref string) (string, error) {
	if !strings.HasPrefix(ref, "sec_") || len(ref) > 96 {
		return "", errs.ErrInvalid
	}
	raw, err := json.Marshal(runtimeSecretListCursor{Version: runtimeSecretListCursorVersion,
		FilterDigest: runtimeSecretListFilterDigest(projectRef, queryValue), Ref: ref})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeRuntimeSecretListCursor(token, projectRef, queryValue string) (runtimeSecretListCursor, error) {
	if token == "" {
		return runtimeSecretListCursor{}, nil
	}
	if len(token) > 512 {
		return runtimeSecretListCursor{}, errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return runtimeSecretListCursor{}, errs.ErrInvalid
	}
	var cursor runtimeSecretListCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != runtimeSecretListCursorVersion ||
		cursor.FilterDigest != runtimeSecretListFilterDigest(projectRef, queryValue) ||
		!strings.HasPrefix(cursor.Ref, "sec_") || len(cursor.Ref) > 96 {
		return runtimeSecretListCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}

func (repository *Repository) GetRuntimeSecret(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeSecret, error) {
	if principal.Permission != "secret.view" {
		return entity.RuntimeSecret{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "SECRET", ResourceRef: ref})
	if err != nil || principal.ProjectRef != "" && principal.ProjectRef != target.projectID {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.view", target); err != nil {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	}
	item, err := scanRuntimeSecret(tx.QueryRow(ctx, queryRuntimeSecretGet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "secret_ref": ref}))
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	return item, nil
}

func (repository *Repository) PrepareRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretPrepareInput) (platformrepo.RuntimeSecretPrepareResult, error) {
	expectedPermission := map[string]string{"CREATE": "secret.create", "ROTATE": "secret.rotate", "REVEAL": "secret.reveal", "REVOKE": "secret.revoke"}[input.Kind]
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != expectedPermission || input.Mutation.Validate() != nil || !validRuntimeSecretPrepare(input) {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrInvalid
	}
	now := time.Now().UTC()
	if input.Kind == "REVEAL" && (principal.CredentialAuthenticatedAt.IsZero() || principal.CredentialAuthenticatedAt.After(now.Add(30*time.Second)) || now.Sub(principal.CredentialAuthenticatedAt) > runtimeSecretRevealMaxAge) {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnauthorized
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()

	secret, err := repository.prepareRuntimeSecretTarget(ctx, tx, current, principal, input)
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	existing, found, err := repository.lockRuntimeSecretOperationByIdempotency(ctx, tx, current.organizationID, current.actorID, input.Kind, input.Mutation.IdempotencyKey)
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	if found {
		if existing.secretID != secret.id || existing.intentDigest != input.Mutation.IntentDigest {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
		}
		result, retryErr := repository.retryRuntimeSecretOperation(ctx, tx, existing, now)
		if retryErr != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, retryErr
		}
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
		}
		return result, nil
	}

	if err := repository.validateNewRuntimeSecretOperation(ctx, tx, current.organizationID, secret, input, now); err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	targetRevision := secret.currentRevision
	if input.Kind == "CREATE" || input.Kind == "ROTATE" {
		if err := tx.QueryRow(ctx, queryRuntimeSecretMaximumRevision, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&targetRevision); err != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnavailable
		}
		targetRevision++
	}
	grant, tokenDigest, err := newRuntimeSecretGrant()
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	operationRef, err := newRef("secop")
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	expiresAt := now.Add(runtimeSecretOperationTTL)
	var expectedHash any
	if input.ExpectedContentSHA256 != "" {
		expectedHash = input.ExpectedContentSHA256
	}
	if _, err := tx.Exec(ctx, queryRuntimeSecretOperationInsert, pgx.StrictNamedArgs{
		"ref": operationRef, "organization_id": current.organizationID, "project_id": secret.projectID,
		"actor_id": current.actorID, "secret_id": secret.id, "kind": input.Kind, "target_revision": targetRevision,
		"expected_secret_version": secret.version, "expected_current_revision": secret.currentRevision,
		"expected_content_sha256": expectedHash, "token_digest": tokenDigest,
		"idempotency_key": input.Mutation.IdempotencyKey, "intent_digest": input.Mutation.IntentDigest,
		"correlation_ref": principal.CorrelationRef, "grant_expires_at": expiresAt,
	}); err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	return platformrepo.RuntimeSecretPrepareResult{
		OperationRef: operationRef, OperationGrant: grant, State: "PREPARED",
		ExpiresAt: expiresAt, ValueType: secret.valueType,
	}, nil
}

func (repository *Repository) prepareRuntimeSecretTarget(ctx context.Context, tx pgx.Tx, current scope, principal value.Principal, input platformrepo.RuntimeSecretPrepareInput) (lockedRuntimeSecret, error) {
	var secret lockedRuntimeSecret
	var err error
	if input.Kind == "CREATE" {
		if err := tx.QueryRow(ctx, queryRuntimeSecretLockProject, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": input.ProjectRef}).Scan(&secret.projectID, &secret.projectRef); errors.Is(err, pgx.ErrNoRows) {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		} else if err != nil {
			return lockedRuntimeSecret{}, errs.ErrUnavailable
		}
		if principal.ProjectRef != "" && principal.ProjectRef != secret.projectID {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		}
		project, targetErr := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: secret.projectRef})
		if targetErr != nil || repository.requireAccess(ctx, tx, current, "secret.create", project) != nil {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		}
		projectID, projectRef := secret.projectID, secret.projectRef
		secret, err = repository.lockRuntimeSecretByName(ctx, tx, current.organizationID, projectID, input.Name)
		if errors.Is(err, errs.ErrNotFound) {
			secret = lockedRuntimeSecret{projectID: projectID, projectRef: projectRef, name: input.Name, description: input.Description, valueType: input.ValueType, state: "PROVISIONING", namespace: repository.runtimeSecretNamespace}
			secret.ref, err = newRef("sec")
			if err != nil {
				return lockedRuntimeSecret{}, err
			}
			if err := tx.QueryRow(ctx, queryRuntimeSecretInsert, pgx.StrictNamedArgs{
				"ref": secret.ref, "organization_id": current.organizationID, "project_id": secret.projectID,
				"namespace": secret.namespace, "name": secret.name, "description": secret.description,
				"value_type": secret.valueType, "actor_id": current.actorID,
			}).Scan(&secret.id, &secret.version, &secret.currentRevision, &secret.createdAt, &secret.updatedAt); err != nil {
				return lockedRuntimeSecret{}, mapWriteError(err)
			}
			return secret, nil
		}
		if err != nil {
			return lockedRuntimeSecret{}, err
		}
		return secret, nil
	}

	secret, err = repository.lockRuntimeSecret(ctx, tx, current.organizationID, input.SecretRef)
	if err != nil || principal.ProjectRef != "" && principal.ProjectRef != secret.projectID {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	permission := map[string]string{"ROTATE": "secret.rotate", "REVEAL": "secret.reveal", "REVOKE": "secret.revoke"}[input.Kind]
	target, targetErr := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "SECRET", ResourceRef: secret.ref})
	if targetErr != nil || repository.requireAccess(ctx, tx, current, permission, target) != nil {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	return secret, nil
}

func (repository *Repository) validateNewRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID string, secret lockedRuntimeSecret, input platformrepo.RuntimeSecretPrepareInput, now time.Time) error {
	if input.Kind == "CREATE" {
		if secret.state != "PROVISIONING" || secret.description != input.Description || secret.valueType != input.ValueType || secret.namespace != repository.runtimeSecretNamespace {
			return errs.ErrConflict
		}
	} else {
		if secret.state != "ACTIVE" {
			return errs.ErrNotFound
		}
		if input.Kind == "ROTATE" && input.ValueType != secret.valueType {
			return errs.ErrConflict
		}
		if input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != secret.version {
			return errs.ErrVersionMismatch
		}
	}
	if input.Kind == "CREATE" || input.Kind == "ROTATE" || input.Kind == "REVOKE" {
		active, found, err := repository.lockActiveRuntimeSecretMutation(ctx, tx, secret.id)
		if err != nil {
			return err
		}
		if found {
			if active.state != "PREPARED" || active.grantExpiresAt.After(now) {
				return errs.ErrConflict
			}
			if err := repository.failLockedRuntimeSecretOperation(ctx, tx, organizationID, active, "GRANT_EXPIRED"); err != nil {
				return err
			}
		}
	}
	if input.Kind == "REVOKE" {
		return repository.ensureRuntimeSecretUnreferenced(ctx, tx, organizationID, secret.ref)
	}
	return nil
}

func (repository *Repository) retryRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, operation lockedRuntimeSecretOperation, now time.Time) (platformrepo.RuntimeSecretPrepareResult, error) {
	result := platformrepo.RuntimeSecretPrepareResult{OperationRef: operation.ref, State: operation.state, ValueType: operation.valueType, FailureCode: operation.failureCode}
	switch operation.state {
	case "COMPLETED":
		if operation.kind == "REVEAL" {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
		}
		secret, err := decodeRuntimeSecretSnapshot(operation.terminalSnapshot)
		if err != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnavailable
		}
		result.TerminalSecret = &secret
		return result, nil
	case "FAILED":
		return result, nil
	case "CLAIMED":
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	case "PREPARED":
	default:
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	if !repository.runtimeSecretOperationMatchesCurrent(operation) {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	grant, tokenDigest, err := newRuntimeSecretGrant()
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	expiresAt := now.Add(runtimeSecretOperationTTL)
	tag, err := tx.Exec(ctx, queryRuntimeSecretOperationReissue, pgx.StrictNamedArgs{
		"operation_id": operation.id, "token_digest": tokenDigest, "grant_expires_at": expiresAt,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	result.State, result.OperationGrant, result.ExpiresAt = "PREPARED", grant, expiresAt
	return result, nil
}

func (repository *Repository) lockActiveRuntimeSecretMutation(ctx context.Context, tx pgx.Tx, secretID string) (lockedRuntimeSecretOperation, bool, error) {
	operation, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretHasActiveOperation, pgx.StrictNamedArgs{"secret_id": secretID}))
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecretOperation{}, false, nil
	}
	if err != nil {
		return lockedRuntimeSecretOperation{}, false, errs.ErrUnavailable
	}
	return operation, true, nil
}

func (repository *Repository) ConsumeRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretConsumeInput) (entity.RuntimeSecretOperation, error) {
	if principal.CallerWorkload != "secret-broker" || principal.Permission != "platform.runtime-secrets.operations.consume" ||
		len(input.OperationGrant) < 32 || len(input.OperationGrant) > 256 || strings.TrimSpace(input.OperationGrant) != input.OperationGrant || !validRuntimeSecretClaimant(input.ClaimantID) {
		return entity.RuntimeSecretOperation{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	digest := sha256.Sum256([]byte(input.OperationGrant))
	locked, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretOperationLockByDigest, pgx.StrictNamedArgs{
		"token_digest": hex.EncodeToString(digest[:]), "organization_id": current.organizationID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecretOperation{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrUnavailable
	}
	if locked.state != "PREPARED" || !time.Now().UTC().Before(locked.grantExpiresAt) || !repository.runtimeSecretOperationMatchesCurrent(locked) {
		return entity.RuntimeSecretOperation{}, errs.ErrConflict
	}
	descriptors, err := repository.runtimeSecretOperationDescriptors(ctx, tx, locked)
	if err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	leaseDeadline := time.Now().UTC().Add(runtimeSecretClaimLease)
	if err := tx.QueryRow(ctx, queryRuntimeSecretOperationConsume, pgx.StrictNamedArgs{
		"operation_id": locked.id, "claimant_id": input.ClaimantID, "claim_lease_deadline": leaseDeadline,
	}).Scan(&locked.claimGeneration, &leaseDeadline); errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecretOperation{}, errs.ErrConflict
	} else if err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrConflict
	}
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.SecretName)
	}
	return entity.RuntimeSecretOperation{
		Ref: locked.ref, Kind: locked.kind, ProjectRef: locked.projectRef, SecretRef: locked.secretRef,
		Name: locked.name, Description: locked.description, ValueType: locked.valueType,
		Namespace: locked.namespace, SecretKey: runtimeSecretKey, TargetRevision: locked.targetRevision,
		ExpectedContentSHA256: locked.expectedContentSHA256, ClaimGeneration: locked.claimGeneration,
		VersionedSecretNames: names, RevisionDescriptors: descriptors,
		ExpiresAt: locked.grantExpiresAt, LeaseDeadline: leaseDeadline,
	}, nil
}

func (repository *Repository) ListRuntimeSecretRecoveryWork(ctx context.Context, principal value.Principal, page platformrepo.RuntimeSecretRecoveryPage) ([]entity.RuntimeSecretRecoveryWork, string, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.recover") {
		return nil, "", errs.ErrForbidden
	}
	limit, err := boundedRuntimeSecretRecoveryPage(page.Size)
	if err != nil {
		return nil, "", err
	}
	cursorDeadline, cursorRef, err := decodeRuntimeSecretRecoveryCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryRuntimeSecretRecoveryWorkList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"cursor_deadline": cursorDeadline,
		"cursor_ref":      cursorRef,
		"page_size":       limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RuntimeSecretRecoveryWork, 0, limit+1)
	for rows.Next() {
		var item entity.RuntimeSecretRecoveryWork
		if err := rows.Scan(
			&item.OperationRef, &item.Kind, &item.ClaimantID, &item.ClaimGeneration,
			&item.Namespace, &item.SecretRef, &item.TargetRevision, &item.SecretKey,
			&item.ExpectedContentSHA256, &item.LeaseDeadline,
		); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeRuntimeSecretRecoveryCursor(last.LeaseDeadline, last.OperationRef)
	}
	return items, next, nil
}

func (repository *Repository) CompleteRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretCompleteInput) (entity.RuntimeSecret, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.complete") || input.OperationRef == "" || !validRuntimeSecretClaimant(input.ClaimantID) || input.ClaimGeneration < 1 {
		return entity.RuntimeSecret{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	if locked.state == "COMPLETED" {
		if locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration {
			return entity.RuntimeSecret{}, errs.ErrConflict
		}
		secret, decodeErr := decodeRuntimeSecretSnapshot(locked.terminalSnapshot)
		if decodeErr != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		if err := tx.Commit(ctx); err != nil {
			return entity.RuntimeSecret{}, errs.ErrConflict
		}
		return secret, nil
	}
	if locked.state != "CLAIMED" || locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration || locked.leaseDeadline == nil || !time.Now().UTC().Before(*locked.leaseDeadline) {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	secret, err := repository.completeLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, input.Materialization)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	return secret, nil
}

func (repository *Repository) FailRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretFailInput) (platformrepo.RuntimeSecretFailureResult, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.fail") || input.OperationRef == "" ||
		!validRuntimeSecretClaimant(input.ClaimantID) || input.ClaimGeneration < 1 || !validRuntimeSecretFailureCode(input.FailureCode) {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, err
	}
	if locked.state == "FAILED" {
		if locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration || locked.failureCode != input.FailureCode {
			return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
		}
		return platformrepo.RuntimeSecretFailureResult{OperationRef: locked.ref, State: "FAILED", FailureCode: locked.failureCode}, nil
	}
	if locked.state != "CLAIMED" || locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration || locked.leaseDeadline == nil {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
	}
	if err := repository.failLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, input.FailureCode); err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
	}
	return platformrepo.RuntimeSecretFailureResult{OperationRef: locked.ref, State: "FAILED", FailureCode: input.FailureCode}, nil
}

func (repository *Repository) RecoverRuntimeSecretMaterialization(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretRecoveryInput) (platformrepo.RuntimeSecretRecoveryResult, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.recover") || input.OperationRef == "" || !validRuntimeSecretMaterialization(&input.Materialization) {
		return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	result := platformrepo.RuntimeSecretRecoveryResult{Action: "DELETE", OperationState: locked.state}
	if locked.kind != "CREATE" && locked.kind != "ROTATE" || !repository.materializationMatchesOperation(locked, &input.Materialization) {
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if locked.state == "COMPLETED" {
		descriptor, descriptorErr := repository.runtimeSecretRevision(ctx, tx, locked.secretID, locked.targetRevision)
		if descriptorErr == nil && runtimeSecretMaterializationMatchesDescriptor(input.Materialization, descriptor) &&
			locked.secretVersion >= locked.expectedSecretVersion && locked.secretStateIsActiveRevision() {
			secret, decodeErr := decodeRuntimeSecretSnapshot(locked.terminalSnapshot)
			if decodeErr != nil {
				return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrUnavailable
			}
			result.Action, result.Secret = "KEEP", &secret
		}
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if locked.state != "CLAIMED" {
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if locked.leaseDeadline == nil || time.Now().UTC().Before(*locked.leaseDeadline) {
		result.Action = "KEEP"
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if !repository.runtimeSecretOperationMatchesCurrent(locked) {
		if err := repository.failLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, "STALE_SECRET_VERSION"); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, err
		}
		result.OperationState = "FAILED"
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	secret, err := repository.completeLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, &input.Materialization)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	result.Action, result.OperationState, result.Secret = "KEEP", "COMPLETED", &secret
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) completeLockedRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID string, locked lockedRuntimeSecretOperation, materialization *entity.RuntimeSecretMaterialization) (entity.RuntimeSecret, error) {
	if locked.state != "CLAIMED" || !repository.runtimeSecretOperationMatchesCurrent(locked) {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	secret := locked.runtimeSecret()
	switch locked.kind {
	case "CREATE", "ROTATE":
		if !validRuntimeSecretMaterialization(materialization) || !repository.materializationMatchesOperation(locked, materialization) {
			return entity.RuntimeSecret{}, errs.ErrInvalid
		}
		revisionRef, err := newRef("secr")
		if err != nil {
			return entity.RuntimeSecret{}, err
		}
		if _, err := tx.Exec(ctx, queryRuntimeSecretRevisionInsert, pgx.StrictNamedArgs{
			"ref": revisionRef, "secret_id": locked.secretID, "revision": locked.targetRevision,
			"namespace": materialization.Namespace, "secret_name": materialization.SecretName,
			"secret_key": materialization.SecretKey, "secret_uid": materialization.SecretUID,
			"secret_resource_version": materialization.SecretResourceVersion, "content_sha256": materialization.ContentSHA256,
		}); err != nil {
			return entity.RuntimeSecret{}, mapWriteError(err)
		}
		hintPrefix, hintSuffix := "", ""
		if materialization.DisplayHint != nil {
			hintPrefix, hintSuffix = materialization.DisplayHint.Prefix, materialization.DisplayHint.Suffix
			secret.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: hintPrefix, Suffix: hintSuffix}
		}
		expectedState := "ACTIVE"
		if locked.kind == "CREATE" {
			expectedState = "PROVISIONING"
		}
		if err := tx.QueryRow(ctx, queryRuntimeSecretActivate, pgx.StrictNamedArgs{
			"secret_id": locked.secretID, "revision": locked.targetRevision, "hint_prefix": hintPrefix, "hint_suffix": hintSuffix,
			"expected_version": locked.expectedSecretVersion, "expected_current_revision": locked.expectedCurrentRevision,
			"expected_state": expectedState,
		}).Scan(&secret.Version, &secret.State, &secret.CurrentRevision, &hintPrefix, &hintSuffix, &secret.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
			return entity.RuntimeSecret{}, errs.ErrConflict
		} else if err != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		secret.CurrentRevisionDescriptor = &entity.RuntimeSecretRevisionDescriptor{
			Revision: locked.targetRevision, Namespace: materialization.Namespace, SecretName: materialization.SecretName,
			SecretKey: materialization.SecretKey, SecretUID: materialization.SecretUID,
			SecretResourceVersion: materialization.SecretResourceVersion, ContentSHA256: materialization.ContentSHA256,
		}
	case "REVEAL":
		if materialization != nil || locked.secretState != "ACTIVE" {
			return entity.RuntimeSecret{}, errs.ErrInvalid
		}
		descriptor, err := repository.runtimeSecretRevision(ctx, tx, locked.secretID, locked.secretCurrentRevision)
		if err != nil {
			return entity.RuntimeSecret{}, err
		}
		secret.CurrentRevisionDescriptor = &descriptor
	case "REVOKE":
		if materialization != nil {
			return entity.RuntimeSecret{}, errs.ErrInvalid
		}
		if err := repository.ensureRuntimeSecretUnreferenced(ctx, tx, organizationID, locked.secretRef); err != nil {
			return entity.RuntimeSecret{}, err
		}
		if err := tx.QueryRow(ctx, queryRuntimeSecretRevoke, pgx.StrictNamedArgs{
			"secret_id": locked.secretID, "expected_version": locked.expectedSecretVersion,
			"expected_current_revision": locked.expectedCurrentRevision,
		}).Scan(&secret.Version, &secret.State, &secret.CurrentRevision, &secret.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
			return entity.RuntimeSecret{}, errs.ErrConflict
		} else if err != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		secret.DisplayHint = nil
		descriptor, err := repository.runtimeSecretRevision(ctx, tx, locked.secretID, locked.secretCurrentRevision)
		if err != nil {
			return entity.RuntimeSecret{}, err
		}
		secret.CurrentRevisionDescriptor = &descriptor
	default:
		return entity.RuntimeSecret{}, errs.ErrInvalid
	}
	snapshot, err := json.Marshal(secret)
	if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryRuntimeSecretOperationComplete, pgx.StrictNamedArgs{
		"operation_id": locked.id, "claimant_id": locked.claimantID, "claim_generation": locked.claimGeneration,
		"terminal_secret_snapshot": string(snapshot),
	})
	if err != nil || tag.RowsAffected() != 1 {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	if err := repository.insertRuntimeSecretAudit(ctx, tx, organizationID, locked, "SUCCEEDED", runtimeSecretAuditSummary(locked.kind)); err != nil {
		return entity.RuntimeSecret{}, err
	}
	return secret, nil
}

func (repository *Repository) failLockedRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID string, locked lockedRuntimeSecretOperation, failureCode string) error {
	tag, err := tx.Exec(ctx, queryRuntimeSecretOperationFail, pgx.StrictNamedArgs{
		"operation_id": locked.id, "claimant_id": locked.claimantID,
		"claim_generation": locked.claimGeneration, "failure_code": failureCode,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.insertRuntimeSecretAudit(ctx, tx, organizationID, locked, "FAILED", "i18n:RUNTIME_SECRET_OPERATION_FAILED")
}

func (repository *Repository) insertRuntimeSecretAudit(ctx context.Context, tx pgx.Tx, organizationID string, locked lockedRuntimeSecretOperation, outcome, summary string) error {
	auditRef, err := newRef("aud")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeSecretAudit, pgx.StrictNamedArgs{
		"ref": auditRef, "operation_id": locked.id, "organization_id": organizationID,
		"project_id": locked.projectID, "actor_id": locked.actorID,
		"action": "runtime-secret." + strings.ToLower(locked.kind), "secret_ref": locked.secretRef,
		"outcome": outcome, "summary": summary, "correlation_ref": locked.correlationRef,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) lockRuntimeSecret(ctx context.Context, tx pgx.Tx, organizationID, ref string) (lockedRuntimeSecret, error) {
	var result lockedRuntimeSecret
	err := tx.QueryRow(ctx, queryRuntimeSecretLock, pgx.StrictNamedArgs{"organization_id": organizationID, "secret_ref": ref}).Scan(
		&result.id, &result.ref, &result.version, &result.projectID, &result.projectRef, &result.name,
		&result.description, &result.valueType, &result.state, &result.currentRevision, &result.namespace,
		&result.createdAt, &result.updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeSecret{}, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) lockRuntimeSecretByName(ctx context.Context, tx pgx.Tx, organizationID, projectID, name string) (lockedRuntimeSecret, error) {
	var result lockedRuntimeSecret
	err := tx.QueryRow(ctx, queryRuntimeSecretLockByName, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "name": name,
	}).Scan(&result.id, &result.ref, &result.version, &result.projectID, &result.projectRef, &result.name,
		&result.description, &result.valueType, &result.state, &result.currentRevision, &result.namespace,
		&result.createdAt, &result.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeSecret{}, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) lockRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID, operationRef string) (lockedRuntimeSecretOperation, error) {
	operation, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretOperationLock, pgx.StrictNamedArgs{
		"organization_id": organizationID, "operation_ref": operationRef,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecretOperation{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeSecretOperation{}, errs.ErrUnavailable
	}
	return operation, nil
}

func (repository *Repository) lockRuntimeSecretOperationByIdempotency(ctx context.Context, tx pgx.Tx, organizationID, actorID, kind, key string) (lockedRuntimeSecretOperation, bool, error) {
	operation, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretOperationLockIdempotency, pgx.StrictNamedArgs{
		"organization_id": organizationID, "actor_id": actorID, "kind": kind, "idempotency_key": key,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecretOperation{}, false, nil
	}
	if err != nil {
		return lockedRuntimeSecretOperation{}, false, errs.ErrUnavailable
	}
	return operation, true, nil
}

func (repository *Repository) scanRuntimeSecretOperation(scanner rowScanner) (lockedRuntimeSecretOperation, error) {
	var operation lockedRuntimeSecretOperation
	err := scanner.Scan(
		&operation.id, &operation.ref, &operation.kind, &operation.state,
		&operation.projectID, &operation.secretID, &operation.actorID, &operation.correlationRef,
		&operation.targetRevision, &operation.expectedSecretVersion, &operation.expectedCurrentRevision,
		&operation.expectedContentSHA256, &operation.grantExpiresAt, &operation.claimantID,
		&operation.claimGeneration, &operation.leaseDeadline, &operation.failureCode,
		&operation.terminalSnapshot, &operation.intentDigest, &operation.projectRef, &operation.secretRef,
		&operation.secretVersion, &operation.secretState, &operation.secretCurrentRevision,
		&operation.name, &operation.description, &operation.valueType, &operation.namespace,
		&operation.secretCreatedAt, &operation.secretUpdatedAt,
	)
	return operation, err
}

func (repository *Repository) runtimeSecretOperationDescriptors(ctx context.Context, tx pgx.Tx, operation lockedRuntimeSecretOperation) ([]entity.RuntimeSecretRevisionDescriptor, error) {
	switch operation.kind {
	case "CREATE", "ROTATE":
		name, err := runtimesecret.VersionedKubernetesName(operation.secretRef, operation.targetRevision)
		if err != nil {
			return nil, errs.ErrInvalid
		}
		return []entity.RuntimeSecretRevisionDescriptor{{
			Revision: operation.targetRevision, Namespace: operation.namespace, SecretName: name,
			SecretKey: runtimeSecretKey, ContentSHA256: operation.expectedContentSHA256,
		}}, nil
	case "REVEAL":
		descriptor, err := repository.runtimeSecretRevision(ctx, tx, operation.secretID, operation.expectedCurrentRevision)
		if err != nil {
			return nil, err
		}
		return []entity.RuntimeSecretRevisionDescriptor{descriptor}, nil
	case "REVOKE":
		return repository.runtimeSecretRevisions(ctx, tx, operation.secretID)
	default:
		return nil, errs.ErrInvalid
	}
}

func (repository *Repository) runtimeSecretRevision(ctx context.Context, tx pgx.Tx, secretID string, revision int64) (entity.RuntimeSecretRevisionDescriptor, error) {
	var item entity.RuntimeSecretRevisionDescriptor
	err := tx.QueryRow(ctx, queryRuntimeSecretRevisionGet, pgx.StrictNamedArgs{"secret_id": secretID, "revision": revision}).Scan(
		&item.Revision, &item.Namespace, &item.SecretName, &item.SecretKey, &item.SecretUID,
		&item.SecretResourceVersion, &item.ContentSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecretRevisionDescriptor{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeSecretRevisionDescriptor{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) runtimeSecretRevisions(ctx context.Context, tx pgx.Tx, secretID string) ([]entity.RuntimeSecretRevisionDescriptor, error) {
	rows, err := tx.Query(ctx, queryRuntimeSecretRevisionList, pgx.StrictNamedArgs{"secret_id": secretID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var items []entity.RuntimeSecretRevisionDescriptor
	for rows.Next() {
		var item entity.RuntimeSecretRevisionDescriptor
		if err := rows.Scan(&item.Revision, &item.Namespace, &item.SecretName, &item.SecretKey, &item.SecretUID, &item.SecretResourceVersion, &item.ContentSHA256); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func (repository *Repository) ensureRuntimeSecretUnreferenced(ctx context.Context, tx pgx.Tx, organizationID, secretRef string) error {
	var referenced bool
	if err := tx.QueryRow(ctx, queryRuntimeSecretIsReferenced, pgx.StrictNamedArgs{
		"organization_id": organizationID, "secret_ref": secretRef,
	}).Scan(&referenced); err != nil {
		return errs.ErrUnavailable
	}
	if referenced {
		return errs.ErrConflict
	}
	return nil
}

func (repository *Repository) runtimeSecretOperationMatchesCurrent(operation lockedRuntimeSecretOperation) bool {
	if operation.namespace != repository.runtimeSecretNamespace || operation.secretVersion != operation.expectedSecretVersion || operation.secretCurrentRevision != operation.expectedCurrentRevision {
		return false
	}
	if operation.kind == "CREATE" {
		return operation.secretState == "PROVISIONING" && operation.targetRevision > operation.expectedCurrentRevision
	}
	if operation.kind == "ROTATE" {
		return operation.secretState == "ACTIVE" && operation.targetRevision > operation.expectedCurrentRevision
	}
	return operation.secretState == "ACTIVE" && operation.targetRevision == operation.expectedCurrentRevision
}

func (repository *Repository) materializationMatchesOperation(operation lockedRuntimeSecretOperation, materialization *entity.RuntimeSecretMaterialization) bool {
	if materialization == nil || operation.expectedContentSHA256 == "" || materialization.Namespace != repository.runtimeSecretNamespace ||
		materialization.Namespace != operation.namespace || materialization.SecretKey != runtimeSecretKey ||
		!runtimeSecretDigestsEqual(materialization.ContentSHA256, operation.expectedContentSHA256) {
		return false
	}
	name, err := runtimesecret.VersionedKubernetesName(operation.secretRef, operation.targetRevision)
	return err == nil && materialization.SecretName == name
}

func (operation lockedRuntimeSecretOperation) runtimeSecret() entity.RuntimeSecret {
	return entity.RuntimeSecret{
		Ref: operation.secretRef, ProjectRef: operation.projectRef, Name: operation.name,
		Description: operation.description, ValueType: operation.valueType, State: operation.secretState,
		Namespace: operation.namespace, Version: operation.secretVersion,
		CurrentRevision: operation.secretCurrentRevision, CreatedAt: operation.secretCreatedAt, UpdatedAt: operation.secretUpdatedAt,
	}
}

func (operation lockedRuntimeSecretOperation) secretStateIsActiveRevision() bool {
	return operation.secretState == "ACTIVE" && operation.secretCurrentRevision == operation.targetRevision
}

func scanRuntimeSecret(scanner rowScanner) (entity.RuntimeSecret, error) {
	var item entity.RuntimeSecret
	var prefix, suffix string
	var descriptor entity.RuntimeSecretRevisionDescriptor
	if err := scanner.Scan(&item.Ref, &item.Version, &item.ProjectRef, &item.Name, &item.Description,
		&item.ValueType, &item.State, &item.CurrentRevision, &prefix, &suffix,
		&item.CreatedAt, &item.UpdatedAt, &item.Namespace, &descriptor.Revision, &descriptor.Namespace,
		&descriptor.SecretName, &descriptor.SecretKey, &descriptor.SecretUID,
		&descriptor.SecretResourceVersion, &descriptor.ContentSHA256); errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	} else if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	if prefix != "" || suffix != "" {
		item.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: prefix, Suffix: suffix}
	}
	if descriptor.Revision > 0 {
		item.CurrentRevisionDescriptor = &descriptor
	}
	return item, nil
}

func validRuntimeSecretPrepare(input platformrepo.RuntimeSecretPrepareInput) bool {
	switch input.Kind {
	case "CREATE":
		return strings.HasPrefix(input.ProjectRef, "prj_") && input.SecretRef == "" && strings.TrimSpace(input.Name) == input.Name &&
			utf8.RuneCountInString(input.Name) > 0 && utf8.RuneCountInString(input.Name) <= 120 && len(input.Description) <= 1000 &&
			validRuntimeSecretValueType(input.ValueType) && input.Mutation.ExpectedVersion == nil && validRuntimeSecretSHA256(input.ExpectedContentSHA256)
	case "ROTATE":
		return input.ProjectRef == "" && strings.HasPrefix(input.SecretRef, "sec_") && input.Name == "" && input.Description == "" && validRuntimeSecretValueType(input.ValueType) && validRuntimeSecretSHA256(input.ExpectedContentSHA256)
	case "REVEAL", "REVOKE":
		return input.ProjectRef == "" && strings.HasPrefix(input.SecretRef, "sec_") && input.Name == "" && input.Description == "" && input.ValueType == "" && input.ExpectedContentSHA256 == ""
	default:
		return false
	}
}

func validRuntimeSecretValueType(value string) bool {
	return value == "STRING" || value == "BINARY" || value == "JSON"
}

func validRuntimeSecretMaterialization(value *entity.RuntimeSecretMaterialization) bool {
	if value == nil || value.Namespace == "" || value.SecretName == "" || value.SecretKey != runtimeSecretKey ||
		len(value.SecretUID) < 1 || len(value.SecretUID) > 128 || len(value.SecretResourceVersion) < 1 || len(value.SecretResourceVersion) > 128 ||
		!validRuntimeSecretSHA256(value.ContentSHA256) {
		return false
	}
	if value.DisplayHint != nil && (utf8.RuneCountInString(value.DisplayHint.Prefix) > 6 || utf8.RuneCountInString(value.DisplayHint.Suffix) > 6) {
		return false
	}
	return true
}

func validRuntimeSecretClaimant(value string) bool {
	if len(value) < 1 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validRuntimeSecretWorkPrincipal(principal value.Principal, permission string) bool {
	return principal.CallerWorkload == "secret-broker" && principal.Permission == permission
}

func validRuntimeSecretFailureCode(value string) bool {
	switch value {
	case "KUBERNETES_UNAVAILABLE", "MATERIALIZATION_CONFLICT", "MATERIALIZATION_INVALID", "STALE_SECRET_VERSION", "RECONCILIATION_FAILED":
		return true
	default:
		return false
	}
}

func boundedRuntimeSecretRecoveryPage(size int32) (int32, error) {
	if size < 0 {
		return 0, errs.ErrInvalid
	}
	if size == 0 {
		return runtimeSecretRecoveryPage, nil
	}
	if size > runtimeSecretRecoveryMax {
		return runtimeSecretRecoveryMax, nil
	}
	return size, nil
}

func encodeRuntimeSecretRecoveryCursor(deadline time.Time, operationRef string) string {
	payload := fmt.Sprintf("%s\n%d\n%s", runtimeSecretRecoveryCursorVersion, deadline.UTC().UnixMicro(), operationRef)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeRuntimeSecretRecoveryCursor(token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	if len(token) > 256 {
		return nil, "", errs.ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 3 || parts[0] != runtimeSecretRecoveryCursorVersion || !strings.HasPrefix(parts[2], "secop_") || len(parts[2]) > 96 {
		return nil, "", errs.ErrInvalid
	}
	microseconds, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || microseconds < 1 {
		return nil, "", errs.ErrInvalid
	}
	deadline := time.UnixMicro(microseconds).UTC()
	return &deadline, parts[2], nil
}

func validRuntimeSecretSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func runtimeSecretDigestsEqual(left, right string) bool {
	leftBytes, leftErr := hex.DecodeString(left)
	rightBytes, rightErr := hex.DecodeString(right)
	return leftErr == nil && rightErr == nil && len(leftBytes) == sha256.Size && len(rightBytes) == sha256.Size && subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

func runtimeSecretMaterializationMatchesDescriptor(materialization entity.RuntimeSecretMaterialization, descriptor entity.RuntimeSecretRevisionDescriptor) bool {
	return materialization.Namespace == descriptor.Namespace && materialization.SecretName == descriptor.SecretName &&
		materialization.SecretKey == descriptor.SecretKey && materialization.SecretUID == descriptor.SecretUID &&
		materialization.SecretResourceVersion == descriptor.SecretResourceVersion && runtimeSecretDigestsEqual(materialization.ContentSHA256, descriptor.ContentSHA256)
}

func decodeRuntimeSecretSnapshot(raw []byte) (entity.RuntimeSecret, error) {
	var result entity.RuntimeSecret
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil || result.Ref == "" || result.Namespace == "" {
		return entity.RuntimeSecret{}, errors.New("runtime secret terminal snapshot is invalid")
	}
	return result, nil
}

func newRuntimeSecretGrant() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", errs.ErrUnavailable
	}
	grant := base64.RawURLEncoding.EncodeToString(buffer)
	digest := sha256.Sum256([]byte(grant))
	return grant, hex.EncodeToString(digest[:]), nil
}

func runtimeSecretAuditSummary(kind string) string {
	return map[string]string{
		"CREATE": "i18n:RUNTIME_SECRET_CREATED", "ROTATE": "i18n:RUNTIME_SECRET_ROTATED",
		"REVEAL": "i18n:RUNTIME_SECRET_REVEALED", "REVOKE": "i18n:RUNTIME_SECRET_REVOKED",
	}[kind]
}
