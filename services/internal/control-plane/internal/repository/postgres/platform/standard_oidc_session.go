package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// canonicalOIDCIdentifier accepts only nonzero canonical UUIDs already assigned by a trusted verifier or owner.
// It neither normalizes attacker-selected authority nor interprets raw provider subject strings.
// canonicalOIDCIdentifier принимает только ненулевые canonical UUIDs, назначенные trusted verifier или owner.
// Helper не нормализует выбранные атакующим authority и не интерпретирует raw provider subjects.
func canonicalOIDCIdentifier(raw string) bool {
	parsed, err := uuid.Parse(raw)
	return err == nil && parsed != uuid.Nil && parsed.String() == raw
}

// validateStandardOIDCIdentity bounds verified evidence before opening a transaction; missing auth_time never receives an invented fallback.
// The caller workload and operation must originate from authenticated transport, not an API request payload.
// validateStandardOIDCIdentity ограничивает verified evidence до транзакции; отсутствующий auth_time не заменяется выдуманным fallback.
// Caller workload и operation обязаны происходить из authenticated transport, а не API request payload.
func (repository *Repository) validateStandardOIDCIdentity(input platformrepo.StandardOIDCIdentity) error {
	policy := repository.standardOIDCPolicy
	now := time.Now().UTC()
	closure := platformrepo.StandardOIDCClosureOperationAllowed(input.Operation)
	if policy == nil || input.Issuer != policy.Issuer || input.CallerWorkload == "" || input.Operation == "" ||
		!canonicalOIDCIdentifier(input.Subject) || !canonicalOIDCIdentifier(input.SessionID) || !canonicalOIDCIdentifier(input.TokenID) ||
		input.AuthenticatedAt.IsZero() || input.AuthenticatedAt.Unix() <= 0 || input.IssuedAt.Before(input.AuthenticatedAt) ||
		input.IssuedAt.After(now.Add(30*time.Second)) || !input.ExpiresAt.After(now) || !input.ExpiresAt.After(input.IssuedAt) ||
		!closure && (!input.AuthenticatedAt.Add(time.Duration(policy.SessionLifetimeSeconds)*time.Second).After(now) ||
			!input.AuthenticatedAt.Add(time.Duration(policy.SessionLifetimeSeconds)*time.Second).After(input.IssuedAt)) ||
		utf8.RuneCountInString(strings.TrimSpace(input.DisplayName)) > 160 || len(input.EmailHint) > 200 ||
		strings.TrimSpace(input.EmailHint) != input.EmailHint || strings.ContainsAny(input.DisplayName+input.EmailHint, "\r\n\x00") {
		return errs.ErrForbidden
	}
	_, trusted := platformrepo.TrustedSTTAuthorityPermission(input.CallerWorkload, input.Operation)
	if input.CallerWorkload != "control-api-gateway" && !(trusted && repository.trustedCluster && input.RPCProfile == "trusted-cluster") {
		return errs.ErrForbidden
	}
	if closure && input.CallerWorkload != "control-api-gateway" {
		return errs.ErrForbidden
	}
	if _, err := normalizedOIDCGroups(platformrepo.ProofPrincipalInput{ExternalIssuer: input.Issuer, ExternalGroups: input.Groups}); err != nil {
		return err
	}
	return nil
}

// scanStandardOIDCSession reads the exact durable binding and its current subject-active flag without treating missing rows as authorization.
// scanStandardOIDCSession читает точный durable binding и текущий subject-active flag, не считая отсутствующую строку авторизацией.
func scanStandardOIDCSession(row pgx.Row) (platformrepo.StandardOIDCBinding, bool, error) {
	var binding platformrepo.StandardOIDCBinding
	var active bool
	err := row.Scan(&binding.Revision, &binding.OrganizationID, &binding.ActorID, &binding.Issuer, &binding.Subject, &binding.SessionID,
		&binding.AuthenticatedAt, &binding.AbsoluteExpiry, &binding.LastIssuedAt, &binding.PolicyRevision, &binding.PolicyDigest, &binding.State, &active)
	if err != nil {
		return platformrepo.StandardOIDCBinding{}, false, err
	}
	if !canonicalOIDCIdentifier(binding.OrganizationID) || !canonicalOIDCIdentifier(binding.ActorID) ||
		!canonicalOIDCIdentifier(binding.Subject) || !canonicalOIDCIdentifier(binding.SessionID) ||
		binding.Revision == 0 || binding.Revision > 1<<53-1 || binding.PolicyRevision == 0 ||
		binding.AuthenticatedAt.IsZero() || binding.LastIssuedAt.Before(binding.AuthenticatedAt) ||
		!binding.AbsoluteExpiry.After(binding.LastIssuedAt) || len(binding.PolicyDigest) != 64 ||
		(binding.State != "ACTIVE" && binding.State != "REVOKED") {
		return platformrepo.StandardOIDCBinding{}, false, errs.ErrUnavailable
	}
	return binding, active, nil
}

// ResolveStandardOIDCIdentity atomically resolves server-owned tenant/actor and an allocated session revision after real identity verification.
// Only known aborted database transactions retry; uncertain commits return failure for a later exact identity readback, never external token replay.
// ResolveStandardOIDCIdentity атомарно разрешает server-owned tenant/actor и выделенную session revision после реальной проверки identity.
// Повторяются лишь известные aborted DB transactions; uncertain commit требует последующего exact identity readback без replay внешнего token exchange.
func (repository *Repository) ResolveStandardOIDCIdentity(ctx context.Context, input platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	if err := repository.validateStandardOIDCIdentity(input); err != nil {
		return platformrepo.StandardOIDCBinding{}, err
	}
	if platformrepo.StandardOIDCClosureOperationAllowed(input.Operation) {
		return repository.resolveStandardOIDCClosureIdentity(ctx, input)
	}
	return retrySerializableTransaction(ctx, func() (platformrepo.StandardOIDCBinding, error) {
		return repository.resolveStandardOIDCIdentityOnce(ctx, input)
	})
}

// resolveStandardOIDCClosureIdentity reads one existing signed identity tuple without creating users/sessions, changing observations or granting application membership.
// Revoked, disabled or application-expired sessions remain closable while the real provider credential is still valid.
// resolveStandardOIDCClosureIdentity читает один существующий signed identity tuple без создания users/sessions, изменения observations и выдачи application membership.
// Revoked, disabled и application-expired sessions допускают лишь закрытие, пока реальный provider credential остаётся действительным.
func (repository *Repository) resolveStandardOIDCClosureIdentity(ctx context.Context, input platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	binding, _, err := scanStandardOIDCSession(repository.pool.QueryRow(ctx, queryStandardOIDCSessionRead, pgx.StrictNamedArgs{
		"issuer": input.Issuer, "subject": input.Subject, "session_id": input.SessionID, "authenticated_at": input.AuthenticatedAt,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.StandardOIDCBinding{}, errs.ErrForbidden
	}
	if err != nil {
		return platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
	}
	return binding, nil
}

// ReadStandardOIDCSession returns one owner-resolved tuple, distinguishing ordinary active access from the explicitly selected self-closure read.
// The closure flag originates only in the exact domain command, never in a browser or RPC payload.
// ReadStandardOIDCSession возвращает один owner-resolved tuple, разделяя обычный active access и явно выбранное self-closure чтение.
// Closure flag происходит лишь из точной domain command, а не browser или RPC payload.
func (repository *Repository) ReadStandardOIDCSession(ctx context.Context, organizationID, actorID string, revision uint64, closure bool) (platformrepo.StandardOIDCBinding, error) {
	var zero platformrepo.StandardOIDCBinding
	if repository.standardOIDCPolicy == nil || !canonicalOIDCIdentifier(organizationID) || !canonicalOIDCIdentifier(actorID) || revision == 0 || revision > 1<<53-1 {
		return zero, errs.ErrForbidden
	}
	binding, active, err := scanStandardOIDCSession(repository.pool.QueryRow(ctx, queryStandardOIDCSessionRevisionRead, pgx.StrictNamedArgs{
		"organization_id": organizationID, "subject_id": actorID, "revision": revision,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, errs.ErrForbidden
	}
	if err != nil {
		return zero, errs.ErrUnavailable
	}
	if binding.Issuer != repository.standardOIDCPolicy.Issuer {
		return zero, errs.ErrForbidden
	}
	if closure {
		return binding, nil
	}
	if !active || binding.State != "ACTIVE" || !binding.AbsoluteExpiry.After(time.Now()) {
		return zero, errs.ErrForbidden
	}
	if err := repository.standardOIDCPolicyReady(ctx); err != nil {
		return zero, errs.ErrUnavailable
	}
	digest, err := repository.standardOIDCPolicy.Digest()
	if err != nil || binding.PolicyRevision != repository.standardOIDCPolicy.Revision || binding.PolicyDigest != digest {
		return zero, errs.ErrForbidden
	}
	var allowed bool
	if err := repository.pool.QueryRow(ctx, queryProofSubjectHasActiveBinding, organizationID, actorID).Scan(&allowed); err != nil {
		return zero, errs.ErrUnavailable
	}
	if !allowed {
		return zero, errs.ErrForbidden
	}
	return binding, nil
}

// resolveStandardOIDCIdentityOnce owns the whole serializable database-only state transition and completes cleanup before any retry marker escapes.
// Explicit owner mapping applies only to the pending claim; active application bindings remain the eligibility authority for every session.
// resolveStandardOIDCIdentityOnce владеет всей serializable DB-only транзакцией и завершает cleanup до возврата retry marker.
// Explicit owner mapping применяется лишь к pending claim; активные application bindings определяют eligibility каждой сессии.
func (repository *Repository) resolveStandardOIDCIdentityOnce(ctx context.Context, input platformrepo.StandardOIDCIdentity) (result platformrepo.StandardOIDCBinding, resultErr error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() {
		if cleanupErr := tx.Rollback(ctx); cleanupErr != nil && !errors.Is(cleanupErr, pgx.ErrTxClosed) {
			result, resultErr = platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
		}
	}()
	organizationID, policy, digest, err := scanStandardOIDCPolicy(tx.QueryRow(ctx, queryStandardOIDCPolicyRead))
	if err != nil {
		return result, serializableTransactionError(err, errs.ErrUnavailable)
	}
	expected, err := repository.standardOIDCPolicy.Digest()
	if err != nil || policy.Revision != repository.standardOIDCPolicy.Revision || digest != expected || policy.Issuer != input.Issuer {
		return result, errs.ErrForbidden
	}
	binding, active, err := scanStandardOIDCSession(tx.QueryRow(ctx, queryStandardOIDCSessionRead, pgx.StrictNamedArgs{
		"issuer": input.Issuer, "subject": input.Subject, "session_id": input.SessionID, "authenticated_at": input.AuthenticatedAt,
	}))
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if found {
		if !active || binding.State != "ACTIVE" || binding.OrganizationID != organizationID || binding.PolicyRevision != policy.Revision ||
			binding.PolicyDigest != digest || !binding.AbsoluteExpiry.After(time.Now()) {
			return result, errs.ErrForbidden
		}
	} else {
		// Только gateway может впервые зарегистрировать сессию; STT не получает bootstrap path.
		if input.CallerWorkload != "control-api-gateway" {
			return result, errs.ErrForbidden
		}
		binding = platformrepo.StandardOIDCBinding{OrganizationID: organizationID, Issuer: input.Issuer, Subject: input.Subject,
			SessionID: input.SessionID, AuthenticatedAt: input.AuthenticatedAt, LastIssuedAt: input.IssuedAt, State: "ACTIVE",
			AbsoluteExpiry: input.AuthenticatedAt.Add(time.Duration(policy.SessionLifetimeSeconds) * time.Second), PolicyRevision: policy.Revision, PolicyDigest: digest}
		if err := tx.QueryRow(ctx, queryStandardOIDCSessionNextRevision).Scan(&binding.Revision); err != nil {
			return result, serializableTransactionError(err, errs.ErrUnavailable)
		}
		if binding.Revision == 0 || binding.Revision > 1<<53-1 {
			return result, errs.ErrUnavailable
		}
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = "i18n:OIDC_USER_NAME"
	}
	principal := platformrepo.ProofPrincipalInput{CallerWorkload: input.CallerWorkload, Operation: input.Operation, RPCProfile: input.RPCProfile,
		ExternalTenantID: organizationID, ExternalActorID: input.Subject, ExternalDisplayName: displayName, ExternalEmailHint: input.EmailHint,
		ExternalIssuer: input.Issuer, ExternalGroups: input.Groups, ExternalSessionRevision: binding.Revision,
		ExternalAuthenticatedAt: input.AuthenticatedAt, ExternalIssuedAt: input.IssuedAt, PlatformManagedOIDC: true,
		OwnerClaim: policy.BootstrapOwner(input.Subject)}
	actorDigest := sha256.Sum256([]byte(organizationID + "\x00" + input.Subject))
	authority, err := repository.resolveHumanIdentityTx(ctx, tx, principal, hex.EncodeToString(actorDigest[:]), displayName)
	if err != nil {
		return result, err
	}
	if authority.OrganizationID != organizationID || found && authority.ActorID != binding.ActorID {
		return result, errs.ErrForbidden
	}
	binding.ActorID = authority.ActorID
	// An unchanged or older signed issuance still rechecks current authority,
	// but must not rewrite the session and contend with parallel revalidation.
	// Равное или старое подписанное время выпуска повторно проверяет полномочия,
	// но не переписывает сессию и не конфликтует с параллельной проверкой.
	if found && input.IssuedAt.After(binding.LastIssuedAt) {
		updated, err := tx.Exec(ctx, queryStandardOIDCSessionAdvance, pgx.StrictNamedArgs{
			"revision": binding.Revision, "organization_id": organizationID, "subject_id": binding.ActorID, "issued_at": input.IssuedAt,
		})
		if err != nil {
			return result, serializableTransactionError(err, errs.ErrUnavailable)
		}
		if updated.RowsAffected() != 1 {
			return result, errs.ErrForbidden
		}
		binding.LastIssuedAt = input.IssuedAt
	} else if !found {
		_, err := tx.Exec(ctx, queryStandardOIDCSessionInsert, pgx.StrictNamedArgs{
			"revision": binding.Revision, "organization_id": organizationID, "subject_id": binding.ActorID, "issuer": input.Issuer,
			"external_subject": input.Subject, "session_id": input.SessionID, "authenticated_at": input.AuthenticatedAt,
			"absolute_expires_at": binding.AbsoluteExpiry, "issued_at": input.IssuedAt, "policy_revision": policy.Revision, "policy_digest": digest,
		})
		if err != nil {
			return result, serializableTransactionError(err, errs.ErrUnavailable)
		}
	}
	var allowed bool
	if err := tx.QueryRow(ctx, queryProofSubjectHasActiveBinding, organizationID, binding.ActorID).Scan(&allowed); err != nil {
		return result, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if !allowed {
		// Наблюдения сохраняются для настройки admin, но session authority и browser cookie не выдаются.
		return result, errs.ErrForbidden
	}
	return binding, nil
}

// RevokeStandardOIDCSession irreversibly revokes only the exact owner-resolved actor/tenant/revision tuple.
// It is naturally idempotent; after a lost response the same tuple reads the tombstone without creating a replacement session.
// RevokeStandardOIDCSession необратимо отзывает лишь точный owner-resolved actor/tenant/revision tuple.
// Операция идемпотентна; после потерянного ответа тот же tuple читает tombstone без создания замещающей сессии.
func (repository *Repository) RevokeStandardOIDCSession(ctx context.Context, organizationID, actorID string, revision uint64) error {
	if !canonicalOIDCIdentifier(organizationID) || !canonicalOIDCIdentifier(actorID) || revision == 0 || revision > 1<<53-1 {
		return errs.ErrForbidden
	}
	var revoked bool
	err := repository.pool.QueryRow(ctx, queryStandardOIDCSessionRevoke, pgx.StrictNamedArgs{
		"organization_id": organizationID, "subject_id": actorID, "revision": revision,
	}).Scan(&revoked)
	if err != nil {
		return errs.ErrUnavailable
	}
	if !revoked {
		return errs.ErrForbidden
	}
	return nil
}

// resolveStandardOIDCClosureProof binds the exact self-lifecycle operation to an existing verified identity session even after revoke or loss of application eligibility.
// It returns no project and cannot serve ordinary application operations; missing or foreign tuples never create authority.
// resolveStandardOIDCClosureProof связывает точную self-lifecycle operation с существующей verified identity session даже после revoke и утраты application eligibility.
// Project не возвращается, обычные application operations не обслуживаются; отсутствующий или чужой tuple не создаёт authority.
func (repository *Repository) resolveStandardOIDCClosureProof(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	var zero platformrepo.ProofAuthority
	if !platformrepo.StandardOIDCClosureOperationAllowed(input.Operation) || !input.PlatformManagedOIDC || input.CallerWorkload != "control-api-gateway" || input.ProjectRef != "" ||
		repository.standardOIDCPolicy == nil || input.ExternalIssuer != repository.standardOIDCPolicy.Issuer ||
		!canonicalOIDCIdentifier(input.ExternalActorID) || !canonicalOIDCIdentifier(input.ExternalTenantID) || !canonicalOIDCIdentifier(input.ExternalSessionID) ||
		input.ExternalSessionRevision == 0 || input.ExternalSessionRevision > 1<<53-1 || input.ExternalIssuedAt.IsZero() {
		return zero, errs.ErrForbidden
	}
	binding, _, err := scanStandardOIDCSession(repository.pool.QueryRow(ctx, queryStandardOIDCSessionSubjectRead, pgx.StrictNamedArgs{
		"revision": input.ExternalSessionRevision, "issuer": input.ExternalIssuer, "external_subject": input.ExternalActorID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, errs.ErrForbidden
	}
	if err != nil {
		return zero, errs.ErrUnavailable
	}
	if binding.OrganizationID != input.ExternalTenantID || binding.SessionID != input.ExternalSessionID || !binding.AuthenticatedAt.Equal(input.ExternalAuthenticatedAt) || input.ExternalIssuedAt.Before(binding.AuthenticatedAt) {
		return zero, errs.ErrForbidden
	}
	authority := platformrepo.ProofAuthority{ActorID: binding.ActorID, OrganizationID: binding.OrganizationID, ActorVersion: 1}
	if err := repository.pool.QueryRow(ctx, queryStandardOIDCOrganizationVersion, pgx.StrictNamedArgs{"organization_id": binding.OrganizationID}).Scan(&authority.OrganizationVersion); err != nil {
		return zero, errs.ErrUnavailable
	}
	if authority.OrganizationVersion == 0 {
		return zero, errs.ErrForbidden
	}
	return authority, nil
}

// standardOIDCProofBinding rechecks the durable session and current policy before an already mapped principal enters the application-proof path.
// Neither a platform-mode marker nor a previously successful login substitutes for current revocation, identity and lifetime checks.
// standardOIDCProofBinding повторно проверяет durable session и current policy до application-proof path уже mapped principal.
// Ни platform-mode marker, ни прежний успешный login не заменяют текущие revocation, identity и lifetime checks.
func (repository *Repository) standardOIDCProofBinding(ctx context.Context, input platformrepo.ProofPrincipalInput) (platformrepo.StandardOIDCBinding, error) {
	var zero platformrepo.StandardOIDCBinding
	if repository.standardOIDCPolicy == nil || input.ExternalIssuer != repository.standardOIDCPolicy.Issuer ||
		input.ExternalIssuedAt.IsZero() || input.ExternalSessionRevision == 0 || input.ExternalSessionRevision > 1<<53-1 ||
		!canonicalOIDCIdentifier(input.ExternalSessionID) {
		return zero, errs.ErrForbidden
	}
	organization, policy, digest, err := scanStandardOIDCPolicy(repository.pool.QueryRow(ctx, queryStandardOIDCPolicyRead))
	if err != nil {
		return zero, errs.ErrUnavailable
	}
	expected, err := repository.standardOIDCPolicy.Digest()
	if err != nil || digest != expected || policy.Revision != repository.standardOIDCPolicy.Revision || organization != input.ExternalTenantID {
		return zero, errs.ErrForbidden
	}
	binding, active, err := scanStandardOIDCSession(repository.pool.QueryRow(ctx, queryStandardOIDCSessionSubjectRead, pgx.StrictNamedArgs{
		"revision": input.ExternalSessionRevision, "issuer": input.ExternalIssuer, "external_subject": input.ExternalActorID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, errs.ErrForbidden
	}
	if err != nil {
		return zero, errs.ErrUnavailable
	}
	if !active || binding.OrganizationID != organization || binding.PolicyRevision != policy.Revision || binding.PolicyDigest != digest ||
		binding.State != "ACTIVE" || binding.SessionID != input.ExternalSessionID || !binding.AuthenticatedAt.Equal(input.ExternalAuthenticatedAt) ||
		input.ExternalIssuedAt.Before(binding.AuthenticatedAt) || !binding.AbsoluteExpiry.After(input.ExternalIssuedAt) || !binding.AbsoluteExpiry.After(time.Now()) {
		return zero, errs.ErrForbidden
	}
	return binding, nil
}
