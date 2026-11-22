package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesInvocationCandidates discovers bounded refs; it never supplies execution authority.
// queryKubernetesInvocationCandidates обнаруживает bounded refs без execution authority.
//
//go:embed sql/kubernetes_invocation__candidates.sql
var queryKubernetesInvocationCandidates string

// queryKubernetesInvocationCursor serializes fair discovery position before Connection locks.
// queryKubernetesInvocationCursor сериализует fair discovery position до Connection locks.
//
//go:embed sql/kubernetes_invocation__cursor.sql
var queryKubernetesInvocationCursor string

// queryKubernetesInvocationCursorAdvance persists only discovery position, never an admission.
// queryKubernetesInvocationCursorAdvance сохраняет лишь discovery position, не admission.
//
//go:embed sql/kubernetes_invocation__cursor_advance.sql
var queryKubernetesInvocationCursorAdvance string

// queryKubernetesInvocationDenyUnstarted closes only proven unstarted source denial without credit.
// queryKubernetesInvocationDenyUnstarted закрывает лишь proven unstarted source denial без credit.
//
//go:embed sql/kubernetes_invocation__deny_unstarted.sql
var queryKubernetesInvocationDenyUnstarted string

// queryKubernetesInvocationPinMatches refuses source replacement after an intent has been admitted.
// queryKubernetesInvocationPinMatches запрещает source replacement после admission intent.
//
//go:embed sql/kubernetes_invocation__pin_matches.sql
var queryKubernetesInvocationPinMatches string

// queryKubernetesInvocationLease checks exact original worker credit on the database clock.
// queryKubernetesInvocationLease проверяет exact original worker credit по database clock.
//
//go:embed sql/kubernetes_invocation__lease.sql
var queryKubernetesInvocationLease string

// queryKubernetesInvocationRenew extends only the still-live original credit, without changing its fence.
// queryKubernetesInvocationRenew продлевает лишь still-live original credit без смены fence.
//
//go:embed sql/kubernetes_invocation__renew.sql
var queryKubernetesInvocationRenew string

// queryKubernetesInvocationConnectionKindByInvocation closes ordinary completion for the typed lane.
// queryKubernetesInvocationConnectionKindByInvocation закрывает ordinary completion для typed lane.
//
//go:embed sql/kubernetes_invocation__connection_kind_by_invocation.sql
var queryKubernetesInvocationConnectionKindByInvocation string

// kubernetesInvocationPinArguments projects only owner-selected coordinates shared by insert and compare.
// It does not select a credential, apply a policy or perform any external request.
// kubernetesInvocationPinArguments проецирует лишь owner-selected coordinates для insert и compare.
// Не выбирает credential, не применяет policy и не выполняет external request.
func kubernetesInvocationPinArguments(organizationID string, choice *personalInvocationCredentialChoice, selected kubernetesInvocationIdentity) pgx.StrictNamedArgs {
	current := choice.context
	return pgx.StrictNamedArgs{"invocation_id": current.invocationID, "organization_id": organizationID,
		"connection_id": current.connectionID, "root_id": current.rootID, "root_actor_id": current.subjectID, "subject_id": selected.subjectID,
		"binding_id": selected.bindingID, "binding_generation": selected.bindingGeneration, "identity_id": selected.identityID, "health_id": selected.healthID,
		"policy": string(current.policy), "definition_digest": current.definitionDigest, "configuration_digest": selected.configurationDigest,
		"registry_revision": selected.source.registry.Revision, "target_ref": selected.target.Ref, "target_revision": selected.target.Revision, "subject_digest": selected.subjectDigest}
}

// currentKubernetesInvocationSourceTx compares fresh owner selection with the original immutable pin.
// The caller holds the operator source before Connection/binding locks to avoid reverse lock ordering.
// currentKubernetesInvocationSourceTx сверяет fresh owner selection с original immutable pin.
// Caller удерживает operator source до Connection/binding locks, исключая reverse lock ordering.
func (repository *Repository) currentKubernetesInvocationSourceTx(ctx context.Context, tx pgx.Tx, organizationID, invocationRef string) (*personalInvocationCredentialChoice, kubernetesInvocationIdentity, error) {
	choice, err := repository.selectPersonalInvocationCredentialTx(ctx, tx, organizationID, invocationRef)
	if err != nil {
		return nil, kubernetesInvocationIdentity{}, err
	}
	selected, err := repository.selectKubernetesInvocationIdentityTx(ctx, tx, organizationID, choice)
	if err != nil {
		return nil, selected, err
	}
	var matched bool
	if tx.QueryRow(ctx, queryKubernetesInvocationPinMatches, kubernetesInvocationPinArguments(organizationID, choice, selected)).Scan(&matched) != nil {
		return nil, selected, errs.ErrUnavailable
	}
	if !matched {
		return nil, selected, errs.ErrForbidden
	}
	return choice, selected, nil
}

// kubernetesInvocationDiscoveryLimit bounds canonical rechecks within the existing ten-second claim.
// Durable keyset progress continues across polls; this number is not an eligibility or lease limit.
// kubernetesInvocationDiscoveryLimit ограничивает canonical rechecks в существующем десятисекундном claim.
// Durable keyset продолжает progress между polls; число не является eligibility либо lease limit.
const kubernetesInvocationDiscoveryLimit = 32

// ClaimManagedKubernetesInvocations uses full existing grant/root/RuntimeRevision/approval eligibility.
// Candidate discovery grants nothing; each ref is re-read after source→Connection→binding locks.
// Only first attempts enter this lane, and generic worker claims explicitly exclude Kubernetes.
// ClaimManagedKubernetesInvocations использует полный existing grant/root/RuntimeRevision/approval predicate.
// Candidate discovery не даёт authority; каждый ref перечитывается после source→Connection→binding locks.
// Lane принимает лишь first attempts; generic worker claims явно исключают Kubernetes.
func (repository *Repository) ClaimManagedKubernetesInvocations(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != "platform.runtime.kubernetes.claim" || principal.ProjectRef != "" ||
		!managedkubernetes.ValidLocator(instance) || limit < 1 || limit > 4 {
		return nil, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	current, tx, route, err := repository.beginIntegrationClaimTransaction(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	claims, err := repository.claimManagedKubernetesInvocationsTx(ctx, tx, current, principal, instance, limit, route)
	if err != nil {
		return nil, err
	}
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	return claims, nil
}

// claimManagedKubernetesInvocationsTx preserves canonical fair discovery inside the single Runtime owner transaction.
// It never remints principal permissions or commits a partial claim independently of another delivery class.
// claimManagedKubernetesInvocationsTx сохраняет canonical fair discovery в единой Runtime owner transaction.
// Не меняет principal permissions и не фиксирует partial claim отдельно от другого delivery class.
func (repository *Repository) claimManagedKubernetesInvocationsTx(ctx context.Context, tx pgx.Tx, current scope, principal value.Principal, instance string, limit int32, route string) ([]map[string]any, error) {
	if _, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare)); err != nil {
		return nil, err
	}
	var afterCreatedAt time.Time
	var afterRef string
	if tx.QueryRow(ctx, queryKubernetesInvocationCursor, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&afterCreatedAt, &afterRef) != nil {
		return nil, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases, current.organizationID, principal.CallerWorkload); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryKubernetesInvocationCandidates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": kubernetesInvocationDiscoveryLimit, "after_created_at": afterCreatedAt, "after_ref": afterRef})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// discoveredInvocation retains the keyset position without supplying any eligibility fact.
	// discoveredInvocation хранит keyset position без eligibility facts.
	type discoveredInvocation struct {
		ref       string
		createdAt time.Time
	}
	refs := make([]discoveredInvocation, 0, kubernetesInvocationDiscoveryLimit)
	for rows.Next() {
		var ref discoveredInvocation
		if rows.Scan(&ref.ref, &ref.createdAt) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	claims := make([]map[string]any, 0, limit)
	processed := 0
	for _, discovered := range refs {
		ref := discovered.ref
		afterCreatedAt, afterRef = discovered.createdAt, ref
		processed++
		_, selected, sourceErr := repository.currentKubernetesInvocationSourceTx(ctx, tx, current.organizationID, ref)
		if err := sourceErr; err != nil {
			if accessInfrastructureFailure(err) {
				return nil, err
			}
			if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
				if err := repository.denyUnstartedKubernetesInvocationTx(ctx, tx, current, ref); err != nil {
					return nil, err
				}
				continue
			}
			var preflight *platformservice.IntegrationCredentialPreflightError
			if errors.As(err, &preflight) {
				continue
			}
			return nil, err
		}
		var candidate integrationInvocationCandidate
		err := scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState,
			current.organizationID, 1, principal.CallerWorkload, route, false, ref, true, true, false), &candidate)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		claim, err := repository.claimIntegrationInvocationCandidateTx(ctx, tx, current, principal, instance, false, candidate)
		if err != nil {
			return nil, err
		}
		if claim != nil {
			claim["credentialDeliveryKind"] = "BROKER_IDENTITY"
			claim["brokerIdentityPurpose"] = "KUBERNETES_TARGET"
			claim["brokerIdentityRef"], claim["brokerIdentityRevision"] = selected.identity.Ref, selected.identity.Revision
			claims = append(claims, claim)
		}
		if len(claims) == int(limit) {
			break
		}
	}
	if len(refs) < kubernetesInvocationDiscoveryLimit && processed == len(refs) {
		afterCreatedAt, afterRef = time.Unix(0, 0).UTC(), ""
	}
	if _, err := tx.Exec(ctx, queryKubernetesInvocationCursorAdvance, pgx.StrictNamedArgs{"organization_id": current.organizationID, "after_created_at": afterCreatedAt, "after_ref": afterRef}); err != nil {
		return nil, errs.ErrUnavailable
	}
	return claims, nil
}

// denyUnstartedKubernetesInvocationTx retains a closed no-effect failure and schedules the existing
// continuation/event path. It cannot consume a Gate or reinterpret an already attempted effect.
// denyUnstartedKubernetesInvocationTx сохраняет closed no-effect failure и использует existing
// continuation/event path. Не расходует Gate и не переопределяет already attempted effect.
func (repository *Repository) denyUnstartedKubernetesInvocationTx(ctx context.Context, tx pgx.Tx, current scope, ref string) error {
	var id, project, root, node string
	err := tx.QueryRow(ctx, queryKubernetesInvocationDenyUnstarted, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": ref}).Scan(&id, &project, &root, &node)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, id, project); err != nil {
		return err
	}
	_, err = repository.emitIntegrationActionEvent(ctx, tx, current, project, root, ref, node, "FAILED")
	return err
}

// RenewManagedKubernetesInvocation renews the same lease after complete fresh eligibility and source checks.
// It cannot revive expired credit, rotate a fence or authorize a different identity during a stream.
// RenewManagedKubernetesInvocation продлевает тот же lease после full fresh eligibility и source checks.
// Не восстанавливает expired credit, не меняет fence и не разрешает другую identity во время stream.
func (repository *Repository) RenewManagedKubernetesInvocation(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding) (result entity.ManagedKubernetesInvocationLease, resultErr error) {
	if principal.CallerWorkload != "integration-gateway" || principal.Permission != "platform.runtime.kubernetes.renew" || principal.ProjectRef != "" || binding.Validate() != nil || binding.InvocationRef == "" {
		return result, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, route, err := repository.beginIntegrationClaimTransaction(ctx, principal)
	if err != nil {
		return result, err
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare)); err != nil {
		return result, err
	}
	if _, _, err := repository.currentKubernetesInvocationSourceTx(ctx, tx, current.organizationID, binding.InvocationRef); err != nil {
		return result, err
	}
	var candidate integrationInvocationCandidate
	if err := scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState,
		current.organizationID, 1, "integration-gateway", route, false, binding.InvocationRef, true, false, false), &candidate); errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	} else if err != nil {
		return result, errs.ErrUnavailable
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef,
		"instance": binding.WorkloadInstance, "lease_ref": binding.LeaseRef, "fence_digest": kubernetesHealthFenceDigest(binding.Fence),
		"generation": binding.Generation, "input_digest": binding.InputDigest}
	err = tx.QueryRow(ctx, queryKubernetesInvocationLease, args).Scan(&result.Ref, &result.Generation, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrUnavailable
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, candidate.runID)
	if err != nil {
		return result, err
	}
	if expired {
		return result, errs.ErrForbidden
	}
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	expires := now.Add(30 * time.Second)
	if deadline != nil && deadline.EffectiveDeadlineAt.Before(expires) {
		expires = deadline.EffectiveDeadlineAt
	}
	args["expires_at"] = expires
	err = tx.QueryRow(ctx, queryKubernetesInvocationRenew, args).Scan(&result.Ref, &result.Generation, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrUnavailable
	}
	return result, nil
}
