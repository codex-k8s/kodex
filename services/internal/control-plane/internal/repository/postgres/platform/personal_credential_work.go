package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		expires := time.Now().Add(30 * time.Second)
		tag, err := tx.Exec(ctx, queryPersonalCredentialTestClaim, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "version": job.version,
			"lease_ref": lease, "fence_digest": hex.EncodeToString(sum[:]), "instance": instance, "expires_at": expires.UTC()})
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.claim", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_CLAIMED"); err != nil {
			return nil, err
		}
		dispatchUntil := expires.UTC()
		if input.binding.Kind == entity.IntegrationCredentialToken {
			dispatchUntil = expires
			if !input.accessUntil.IsZero() && input.accessUntil.Before(dispatchUntil) {
				dispatchUntil = input.accessUntil
			}
		}
		claim := map[string]any{"testRef": job.ref, "connectionRef": job.connectionRef, "definitionKey": input.definition.Metadata.Key,
			"definitionVersion": input.definition.Metadata.Version, "definitionDigest": input.definition.Digest, "definitionPackage": asJSON(input.definition),
			"configuration": input.configuration, "leaseRef": lease, "fence": fence, "generation": int64(1), "expiresAt": dispatchUntil}
		claim["githubDiscoveryPage"] = job.snapshot.GitHubDiscoveryPage
		claim["personalReadProbeCapabilityKeys"] = append([]string(nil), job.snapshot.AllowedReadCapabilityKeys...)
		claim["personalObservationProfiles"] = append([]string(nil), job.snapshot.AllowedObservationProfiles...)
		claim["personalDeniedWriteCapabilityKeys"] = append([]string(nil), job.snapshot.AllowedDeniedWriteCapabilityKeys...)
		snapshotBytes, err := json.Marshal(job.snapshot)
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		snapshotHash := sha256.Sum256(snapshotBytes)
		claim["inputDigest"] = hex.EncodeToString(snapshotHash[:])
		switch input.binding.Kind {
		case entity.IntegrationCredentialToken:
			if input.binding.Credential == nil || input.binding.BrokerIdentity != nil {
				return nil, errs.ErrUnavailable
			}
			claim["credentialDeliveryKind"] = "TOKEN"
			attachIntegrationClaimCredential(claim, true, input.binding.Credential, &input.binding.Credential.CreatedAt)
		case entity.IntegrationCredentialBrokerIdentity:
			if input.binding.Credential != nil || input.binding.BrokerIdentity == nil || !input.binding.BrokerIdentity.ValidMetadata() || (input.binding.BrokerPurpose != entity.IntegrationBrokerIdentityKubernetes && input.binding.BrokerPurpose != entity.IntegrationBrokerIdentitySSH) {
				return nil, errs.ErrUnavailable
			}
			claim["credentialDeliveryKind"] = "BROKER_IDENTITY"
			claim["brokerIdentityPurpose"] = string(input.binding.BrokerPurpose)
			claim["brokerIdentityRef"] = input.binding.BrokerIdentity.Ref
			claim["brokerIdentityRevision"] = input.binding.BrokerIdentity.Revision
		default:
			return nil, errs.ErrUnavailable
		}
		result = append(result, claim)
		if len(result) == int(limit) {
			break
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrUnavailable
	}
	// A late claim ACK never discloses an expired allowance; the durable lease remains available for ordinary owner expiry.
	// Поздний claim ACK не выдаёт expired allowance; durable lease остаётся для обычного owner expiry.
	currentClaims := result[:0]
	for _, claim := range result {
		if claim["credentialDeliveryKind"] == "BROKER_IDENTITY" {
			currentClaims = append(currentClaims, claim)
			continue
		}
		until, ok := claim["expiresAt"].(time.Time)
		if ok && time.Now().Before(until) {
			currentClaims = append(currentClaims, claim)
		}
	}
	return currentClaims, nil
}

// BeginPersonalIntegrationCredentialProbe consumes a claim exactly once immediately before the
// gateway's bounded READ. Revoke/configuration/ACL changes close admission. A lost ACK does not
// permit replaying PROBING, so uncertainty never becomes a second external request.
// BeginPersonalIntegrationCredentialProbe одноразово расходует claim непосредственно перед
// bounded READ шлюза. Revoke/configuration/ACL changes закрывают допуск. Потерянный ACK не разрешает
// повтор PROBING, поэтому неопределённость не превращается во второй внешний запрос.
func (repository *Repository) BeginPersonalIntegrationCredentialProbe(ctx context.Context, principal value.Principal, input command.PersonalIntegrationCredentialProbeAdmission) (entity.PersonalIntegrationCredentialProbeAdmissionResult, error) {
	var empty entity.PersonalIntegrationCredentialProbeAdmissionResult
	if input.ContractVersion != command.PersonalIntegrationCredentialProbeContractVersion {
		return empty, errs.ErrForbidden
	}
	payload := command.PersonalIntegrationCredentialTestCompletion{TestRef: input.TestRef, LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if payload.Outcome != "" || !validPersonalCredentialTestLeaseInput(payload) {
		return empty, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return empty, err
	}
	if err := personalCredentialWorkAuthority(principal, current, "platform.personal-integration-tests.begin"); err != nil {
		return empty, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	job, err := repository.lockPersonalCredentialTest(ctx, tx, current.organizationID, payload.TestRef)
	if err != nil {
		return empty, err
	}
	if job.snapshot.Kind != entity.IntegrationCredentialToken {
		return empty, errs.ErrForbidden
	}
	if job.state != "CLAIMED" || !personalCredentialTestLease(job, payload, time.Now().UTC()) {
		return empty, errs.ErrForbidden
	}
	selected, err := repository.currentPersonalCredentialTestInput(ctx, tx, current.organizationID, job)
	if err != nil {
		if errors.Is(err, errs.ErrUnavailable) {
			return empty, err
		}
		if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, job, "CANCELLED", "CANCELLED"); err != nil {
			return empty, err
		}
		if err := resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, job); err != nil {
			return empty, err
		}
		if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.cancel", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_CANCELLED"); err != nil {
			return empty, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, errs.ErrUnavailable
		}
		return empty, errs.ErrForbidden
	}
	until := job.leaseUntil
	if !selected.accessUntil.IsZero() && selected.accessUntil.Before(until) {
		until = selected.accessUntil
	}
	if until.IsZero() || !time.Now().Before(until) {
		return empty, errs.ErrForbidden
	}
	tag, err := tx.Exec(ctx, queryPersonalCredentialTokenProbeBegin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "version": job.version, "authorized_until": until})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return empty, errs.ErrConflict
	}
	if err := auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.personal-integration-tests.begin", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_PROBE_ADMITTED"); err != nil {
		return empty, err
	}
	if !time.Now().Before(until) {
		return empty, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, errs.ErrUnavailable
	}
	if !time.Now().Before(until) {
		return empty, errs.ErrForbidden
	}
	return entity.PersonalIntegrationCredentialProbeAdmissionResult{AuthorizedUntil: until}, nil
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
	if job.snapshot.Kind != entity.IntegrationCredentialToken {
		// Broker identity facts are admitted and completed only by the purpose-specific private owner path.
		// Broker identity facts допускает и завершает лишь purpose-specific private owner path.
		return commandOutcome{}, errs.ErrForbidden
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
