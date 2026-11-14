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
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// personalCredentialWorkAuthority admits only the exact integration worker operation and keeps
// project-signed or browser identities out of the verification worker boundary.
// personalCredentialWorkAuthority допускает только точную integration worker operation и исключает
// project-signed либо browser identities из границы verification worker.
func personalCredentialWorkAuthority(principal value.Principal, current scope, operation string) error {
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != operation || current.authorityProjectID != "" {
		return errs.ErrForbidden
	}
	return nil
}

// ClaimPersonalIntegrationCredentialTests claims a bounded page without starting HTTP work.
// Expired or ineligible predecessors are terminally fenced in the same transaction; each binding
// remains self-owned and successful claims carry its exact immutable descriptor only.
// ClaimPersonalIntegrationCredentialTests захватывает bounded страницу без начала HTTP работы.
// Истёкшие либо недопустимые predecessors закрываются в той же транзакции; каждая привязка остаётся
// личной, успешный claim содержит только её точный immutable descriptor.
func (repository *Repository) ClaimPersonalIntegrationCredentialTests(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if limit < 1 || limit > 16 || strings.TrimSpace(instance) == "" || len(instance) > 256 || !utf8.ValidString(instance) || strings.ContainsAny(instance, "\x00\n\r") {
		return nil, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	if err := personalCredentialWorkAuthority(principal, current, "platform.personal-integration-tests.claim"); err != nil {
		return nil, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryPersonalCredentialTestCandidates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit * 2})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := make([]string, 0, limit*2)
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
	result := make([]map[string]any, 0, limit)
	for _, ref := range refs {
		job, err := repository.lockPersonalCredentialTest(ctx, tx, current.organizationID, ref)
		if errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if job.state == "CLAIMED" || job.state == "PROBING" {
			if job.expiresAt == nil || time.Now().Before(*job.expiresAt) {
				continue
			}
			if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, "FAILED", "UNAVAILABLE"); err != nil {
				return nil, err
			}
			if err := resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, job); err != nil {
				return nil, err
			}
			if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.expire", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_EXPIRED"); err != nil {
				return nil, err
			}
			continue
		}
		if job.state != "DUE" {
			continue
		}
		input, err := repository.currentPersonalCredentialTestInput(ctx, tx, current.organizationID, job)
		if err != nil {
			if errors.Is(err, errs.ErrUnavailable) {
				return nil, err
			}
			if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, "CANCELLED", "CANCELLED"); err != nil {
				return nil, err
			}
			if err := resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, job); err != nil {
				return nil, err
			}
			if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.cancel", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_CANCELLED"); err != nil {
				return nil, err
			}
			continue
		}
		lease, err := newRef("lea")
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		fence, err := newRef("fnc")
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		sum := sha256.Sum256([]byte(fence))
		expires := time.Now().UTC().Add(30 * time.Second)
		tag, err := tx.Exec(ctx, queryPersonalCredentialTestClaim, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "version": job.version,
			"lease_ref": lease, "fence_digest": hex.EncodeToString(sum[:]), "instance": instance, "expires_at": expires})
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.claim", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_CLAIMED"); err != nil {
			return nil, err
		}
		claim := map[string]any{"testRef": job.ref, "connectionRef": job.connectionRef, "definitionKey": input.definition.Metadata.Key,
			"definitionVersion": input.definition.Metadata.Version, "definitionDigest": input.definition.Digest, "definitionPackage": asJSON(input.definition),
			"configuration": input.configuration, "leaseRef": lease, "fence": fence, "generation": int64(1), "expiresAt": expires}
		claim["githubDiscoveryPage"] = job.snapshot.GitHubDiscoveryPage
		claim["personalReadProbeCapabilityKeys"] = append([]string(nil), job.snapshot.AllowedReadCapabilityKeys...)
		claim["personalObservationProfiles"] = append([]string(nil), job.snapshot.AllowedObservationProfiles...)
		claim["personalDeniedWriteCapabilityKeys"] = append([]string(nil), job.snapshot.AllowedDeniedWriteCapabilityKeys...)
		attachIntegrationClaimCredential(claim, true, input.binding.Credential, &input.binding.Credential.CreatedAt)
		result = append(result, claim)
		if len(result) == int(limit) {
			break
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// BeginPersonalIntegrationCredentialProbe consumes a claim exactly once immediately before the
// gateway's bounded READ. Revoke/configuration/ACL changes close admission. A lost ACK does not
// permit replaying PROBING, so uncertainty never becomes a second external request.
// BeginPersonalIntegrationCredentialProbe одноразово расходует claim непосредственно перед
// bounded READ шлюза. Revoke/configuration/ACL changes закрывают допуск. Потерянный ACK не разрешает
// повтор PROBING, поэтому неопределённость не превращается во второй внешний запрос.
func (repository *Repository) BeginPersonalIntegrationCredentialProbe(ctx context.Context, principal value.Principal, payload command.PersonalIntegrationCredentialTestCompletion) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if payload.Outcome != "" || !validPersonalCredentialTestLeaseInput(payload) {
		return errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return err
	}
	if err := personalCredentialWorkAuthority(principal, current, "platform.personal-integration-tests.begin"); err != nil {
		return err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	job, err := repository.lockPersonalCredentialTest(ctx, tx, current.organizationID, payload.TestRef)
	if err != nil {
		return err
	}
	if job.state != "CLAIMED" || !personalCredentialTestLease(job, payload, time.Now().UTC()) {
		return errs.ErrForbidden
	}
	if _, err := repository.currentPersonalCredentialTestInput(ctx, tx, current.organizationID, job); err != nil {
		if errors.Is(err, errs.ErrUnavailable) {
			return err
		}
		if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, "CANCELLED", "CANCELLED"); err != nil {
			return err
		}
		if err := resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, job); err != nil {
			return err
		}
		if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.cancel", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_CANCELLED"); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return errs.ErrUnavailable
		}
		return errs.ErrForbidden
	}
	tag, err := tx.Exec(ctx, queryPersonalCredentialTestBegin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "version": job.version})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.begin", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_PROBE_ADMITTED"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// authorizePersonalCredentialTestCompletion checks exact worker authority and tenant-owned target
// before generic receipt replay; current lease/binding checks occur only for a new completion.
// authorizePersonalCredentialTestCompletion проверяет exact worker authority и tenant-owned target
// до generic receipt replay; текущие lease/binding проверяются только для нового completion.
func (repository *Repository) authorizePersonalCredentialTestCompletion(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	if err := personalCredentialWorkAuthority(input.Principal, current, "platform.personal-integration-tests.complete"); err != nil {
		return err
	}
	payload, ok := input.Payload.(command.PersonalIntegrationCredentialTestCompletion)
	if !ok || !validPersonalCredentialTestLeaseInput(payload) || input.Mutation.ExpectedVersion != nil {
		return errs.ErrInvalid
	}
	switch payload.Outcome {
	case entity.PersonalIntegrationCredentialAuthenticated, entity.PersonalIntegrationCredentialAuthenticationRejected, entity.PersonalIntegrationCredentialVerificationUnavailable:
	default:
		return errs.ErrInvalid
	}
	var connection, subject, binding string
	if err := tx.QueryRow(ctx, queryPersonalCredentialTestLocator, pgx.StrictNamedArgs{"organization_id": current.organizationID, "ref": payload.TestRef}).Scan(&connection, &subject, &binding); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// completePersonalCredentialTest records an exact begun READ observation or safely discards it
// after eligibility/configuration drift. Only the tested user's binding can become READY;
// Connection readiness and every other user's verification remain unchanged.
// completePersonalCredentialTest сохраняет точное начатое READ observation либо безопасно отбрасывает
// его после eligibility/configuration drift. READY получает только проверяемая собственная привязка;
// Connection readiness и проверки остальных пользователей не меняются.
func (repository *Repository) completePersonalCredentialTest(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.PersonalIntegrationCredentialTestCompletion)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	job, err := repository.lockPersonalCredentialTest(ctx, tx, current.organizationID, payload.TestRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if job.state != "PROBING" || !personalCredentialTestLease(job, payload, time.Now().UTC()) {
		return commandOutcome{}, errs.ErrForbidden
	}
	resolved, err := repository.currentPersonalCredentialTestInput(ctx, tx, current.organizationID, job)
	if err != nil {
		if errors.Is(err, errs.ErrUnavailable) {
			return commandOutcome{}, err
		}
		if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, "CANCELLED", "CANCELLED"); err != nil {
			return commandOutcome{}, err
		}
		if err := resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, job); err != nil {
			return commandOutcome{}, err
		}
		return personalCredentialTestOutcome(job.ref, "CANCELLED", false), nil
	}
	observation := entity.PersonalIntegrationCredentialTestObservation{Outcome: payload.Outcome, ConfirmedCapabilityKeys: append([]string(nil), payload.ConfirmedCapabilityKeys...), DeniedCapabilityKeys: append([]string(nil), payload.DeniedCapabilityKeys...), ObservedPermissionProfiles: append([]string(nil), payload.ObservedPermissionProfiles...), ExpiresAt: payload.ExpiresAt, GitHubDiscovery: payload.GitHubDiscovery}
	if payload.Outcome == entity.PersonalIntegrationCredentialAuthenticated && len(observation.ConfirmedCapabilityKeys) == 0 && len(observation.DeniedCapabilityKeys) == 0 {
		// Совместимость с прежним worker: он мог подтвердить исключительно health.
		observation.ConfirmedCapabilityKeys = []string{job.snapshot.HealthCapabilityKey}
	}
	verified, err := platformservice.CompletePersonalIntegrationCredentialVerification(resolved.binding, job.snapshot, job.snapshot.DefinitionDigest, job.snapshot.ConfigurationDigest, observation)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := persistPersonalCredentialVerification(ctx, tx, job.bindingID, resolved.binding.Version, verified); err != nil {
		return commandOutcome{}, err
	}
	state := "FAILED"
	if payload.Outcome == entity.PersonalIntegrationCredentialAuthenticated {
		state = "SUCCEEDED"
	}
	job.githubDiscovery = verified.GitHubDiscovery
	if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, state, string(payload.Outcome)); err != nil {
		return commandOutcome{}, err
	}
	return personalCredentialTestOutcome(job.ref, state, true), nil
}

// personalCredentialTestOutcome returns only a safe fenced worker receipt, never credential
// metadata or provider data; the generic owner transaction persists its audit and idempotency record.
// personalCredentialTestOutcome возвращает только безопасный ограждённый worker receipt без
// credential metadata и provider data; owner-транзакция сохраняет audit и idempotency record.
func personalCredentialTestOutcome(ref, state string, applied bool) commandOutcome {
	summary := "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_COMPLETED"
	if state == "CANCELLED" {
		summary = "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_CANCELLED"
	}
	return commandOutcome{result: command.Result{PersonalIntegrationCredentialTest: &entity.PersonalIntegrationCredentialTestReceipt{TestRef: ref, State: state, Applied: applied}},
		resourceKind: "PERSONAL_INTEGRATION_CREDENTIAL_TEST", resourceRef: ref, summary: summary}
}

// auditPersonalCredentialWork appends one safe owner work transition in the same transaction;
// neither empty polls, credential descriptors, fences nor provider payloads are recorded here.
// auditPersonalCredentialWork добавляет один безопасный owner work transition в той же транзакции;
// пустые polls, credential descriptors, fences и provider payloads здесь не записываются.
func auditPersonalCredentialWork(ctx context.Context, tx pgx.Tx, current scope, ref, action, summary string) error {
	auditRef, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil, current.actorID,
		action, "PERSONAL_INTEGRATION_CREDENTIAL_TEST", ref, summary, current.correlationRef)
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
