package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesSetupExpiryPending distinguishes denied expired authority from a completed DB closure.
// kubernetesSetupExpiryPending отличает denied expired authority от завершённого DB closure.
const kubernetesSetupExpiryPending = "SETUP_EXPIRED_PENDING_CLOSURE"

// kubernetesSetupCompletionRequired reports missing durable outcomes without granting observation.
// kubernetesSetupCompletionRequired сообщает missing durable outcomes без допуска observation.
const kubernetesSetupCompletionRequired = "SETUP_COMPLETION_REQUIRED"

// kubernetesSetupAcknowledgementRejected exposes only the safe terminal classification, never its witness.
// kubernetesSetupAcknowledgementRejected показывает лишь safe terminal classification, не witness.
const kubernetesSetupAcknowledgementRejected = "SETUP_ACK_REJECTED"

// kubernetesSetupAcknowledgementConflict distinguishes retained completion history from usable authority.
// kubernetesSetupAcknowledgementConflict отличает retained completion history от usable authority.
const kubernetesSetupAcknowledgementConflict = "SETUP_ACK_CONFLICT"

// queryKubernetesSetupReadOwned selects only safe metadata under one exact actor/Connection boundary.
// queryKubernetesSetupReadOwned выбирает только safe metadata в exact actor/Connection boundary.
//
//go:embed sql/kubernetes_setup__read_owned.sql
var queryKubernetesSetupReadOwned string

// kubernetesSetupAPICaller rejects another workload and project-delegated authority before owner reads.
// Its inputs are verified transport context, never caller-supplied subject or tenant selectors.
// kubernetesSetupAPICaller отклоняет иной workload и project-delegated authority до owner reads.
// Inputs — verified transport context, не subject либо tenant selectors из payload.
func kubernetesSetupAPICaller(principal value.Principal, permission string) error {
	if principal.Validate() != nil || principal.CallerWorkload != "control-api-gateway" || principal.Permission != permission || principal.ProjectRef != "" {
		return errs.ErrForbidden
	}
	return nil
}

// readKubernetesSetupView maps a metadata-only row and refuses incomplete or unknown stored state.
// Expiry is a display reason until an owner transaction durably closes every attempt grant.
// readKubernetesSetupView преобразует metadata-only row и отклоняет incomplete либо unknown stored state.
// Expiry остаётся display reason, пока owner transaction устойчиво не закроет все attempt grants.
func readKubernetesSetupView(row pgx.Row) (entity.KubernetesIdentitySetupView, error) {
	var result entity.KubernetesIdentitySetupView
	var expired, unreported, rejected, conflicting bool
	err := row.Scan(&result.Ref, &result.ConnectionRef, &result.IdentityRef, &result.TargetRef, &result.Version,
		&result.ConnectionVersion, &result.BindingVersion, &result.BindingGeneration, &result.IdentityRevision,
		&result.Policy, &result.State, &result.ExpiresAt, &expired, &unreported, &rejected, &conflicting, &result.CanonicalContext)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.KubernetesIdentitySetupView{}, errs.ErrNotFound
	}
	if err != nil || !managedkubernetes.ValidLocator(result.Ref) || !managedkubernetes.ValidLocator(result.ConnectionRef) ||
		!managedkubernetes.ValidLocator(result.IdentityRef) || !managedkubernetes.ValidLocator(result.TargetRef) ||
		result.Version < 1 || result.ConnectionVersion < 1 || result.IdentityRevision < 1 || result.IdentityRevision > 9007199254740991 ||
		!managedkubernetes.ValidLocator(result.CanonicalContext) || result.ExpiresAt.IsZero() || !result.State.Valid() ||
		(result.Policy != entity.IntegrationCredentialInitiatorRequired && result.Policy != entity.IntegrationCredentialProjectShared) ||
		(result.State == enum.KubernetesIdentitySetupAcknowledgementRejected && !rejected) ||
		(result.State == enum.KubernetesIdentitySetupCompleted && (rejected && !conflicting || unreported)) ||
		(result.Policy == entity.IntegrationCredentialInitiatorRequired && (result.BindingVersion < 1 || result.BindingGeneration < 1)) ||
		(result.Policy == entity.IntegrationCredentialProjectShared && (result.BindingVersion != 0 || result.BindingGeneration != 0)) {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	if expired && !result.State.Terminal() {
		result.ReasonCode = kubernetesSetupExpiryPending
	}
	if unreported {
		result.ReasonCode = kubernetesSetupCompletionRequired
	}
	if rejected {
		result.ReasonCode = kubernetesSetupAcknowledgementRejected
	}
	if conflicting {
		result.ReasonCode = kubernetesSetupAcknowledgementConflict
	}
	return result, nil
}

// GetKubernetesIdentitySetup reads the original actor's safe setup metadata with fresh visibility.
// The Begin-key selector reconciles a lost first response without recovering its one-shot permit.
// No operator file, private storage locator, content hash or credential material enters this result.
// GetKubernetesIdentitySetup читает safe setup metadata исходного actor со fresh visibility.
// Begin-key selector восстанавливает lost first response без возврата одноразового permit.
// Result не содержит operator file, private storage locator, content hash либо credential material.
func (repository *Repository) GetKubernetesIdentitySetup(ctx context.Context, principal value.Principal, input command.KubernetesIdentitySetupRead) (result entity.KubernetesIdentitySetupView, resultErr error) {
	if err := kubernetesSetupAPICaller(principal, "platform.integrations.kubernetes.setup.get"); err != nil {
		return result, err
	}
	if len(input.ConnectionRef) > 96 || !managedkubernetes.ValidLocator(input.ConnectionRef) ||
		(input.SetupRef == "") == (input.BeginIdempotencyKey == "") ||
		(input.SetupRef != "" && (len(input.SetupRef) > 96 || !strings.HasPrefix(input.SetupRef, "ksu_") || !managedkubernetes.ValidLocator(input.SetupRef))) ||
		(input.BeginIdempotencyKey != "" && (len(input.BeginIdempotencyKey) < 8 || len(input.BeginIdempotencyKey) > 128 ||
			!utf8.ValidString(input.BeginIdempotencyKey) || strings.ContainsAny(input.BeginIdempotencyKey, "\r\n\x00"))) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input.ConnectionRef,
	}); err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	result, err = readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupReadOwned, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef,
		"setup_ref": input.SetupRef, "begin_key": input.BeginIdempotencyKey,
	}))
	if err != nil {
		return entity.KubernetesIdentitySetupView{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesIdentitySetupView{}, errs.ErrUnavailable
	}
	return result, nil
}
