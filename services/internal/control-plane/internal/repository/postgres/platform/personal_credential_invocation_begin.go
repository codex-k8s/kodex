package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
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

// queryPersonalInvocationBeginLease compares a locked current selection with immutable intent pins.
// queryPersonalInvocationBeginLease сверяет текущий заблокированный selection с immutable intent pins.
//
//go:embed sql/personal_invocation__begin_lease.sql
var queryPersonalInvocationBeginLease string

// queryPersonalInvocationAdmit inserts one lease receipt and at most one EXECUTE receipt per intent.
// queryPersonalInvocationAdmit создаёт одну lease receipt и не более одной EXECUTE receipt намерения.
//
//go:embed sql/personal_invocation__admit.sql
var queryPersonalInvocationAdmit string

// queryPersonalInvocationDenyUnstarted closes only the exact lease with no admission history.
// Already started work retains its uncertainty, including after revoke or a lost Begin ACK.
// queryPersonalInvocationDenyUnstarted закрывает лишь exact lease без admission history.
// Начатая работа сохраняет неопределённость, в том числе после revoke либо потери Begin ACK.
//
//go:embed sql/personal_invocation__deny_unstarted.sql
var queryPersonalInvocationDenyUnstarted string

// BeginPersonalIntegrationInvocation commits a single fresh admission before returning metadata.
// It shares Connection→binding→invocation lock order with credential writers and uses the full
// canonical claim predicate, including historical workflows, approval and RuntimeRevision pins.
// Commit uncertainty or duplicate receipt returns no descriptor and never retries external work.
// BeginPersonalIntegrationInvocation фиксирует единственный свежий допуск до возврата metadata.
// Порядок Connection→binding→invocation согласован с credential writers; используется полный
// canonical claim predicate с historical workflows, approval и RuntimeRevision pins.
// Неизвестный commit и повтор receipt не выдают descriptor и не повторяют внешнюю работу.
func (repository *Repository) BeginPersonalIntegrationInvocation(ctx context.Context, principal value.Principal, input command.PersonalIntegrationInvocationAdmission) (entity.PersonalIntegrationInvocationAdmissionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var empty entity.PersonalIntegrationInvocationAdmissionResult
	if !validPersonalInvocationAdmission(input) {
		return empty, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return empty, err
	}
	if err := personalCredentialWorkAuthority(principal, current, "platform.personal-integration-calls.begin"); err != nil {
		return empty, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	choice, err := repository.selectPersonalInvocationCredentialTx(ctx, tx, current.organizationID, input.InvocationRef)
	if err != nil {
		return empty, denyUnstartedPersonalInvocationTx(ctx, tx, current, input, err)
	}
	if choice.context.policy != entity.IntegrationCredentialInitiatorRequired || choice.selection.Credential == nil {
		return empty, errs.ErrForbidden
	}
	var candidate integrationInvocationCandidate
	err = scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState,
		current.organizationID, 1, "integration-gateway", "MANAGED_MCP", false, input.InvocationRef), &candidate)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, denyUnstartedPersonalInvocationTx(ctx, tx, current, input, errs.ErrForbidden)
	}
	if err != nil {
		return empty, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if candidate.credentialPolicy != entity.IntegrationCredentialInitiatorRequired || candidate.credential.Ref != "" || candidate.ref != input.InvocationRef {
		return empty, errs.ErrForbidden
	}
	fence := sha256.Sum256([]byte(input.Fence))
	fenceDigest := hex.EncodeToString(fence[:])
	selection, selected := choice.selection, choice.context
	var recovery bool
	err = tx.QueryRow(ctx, queryPersonalInvocationBeginLease, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "invocation_ref": input.InvocationRef,
		"lease_ref": input.LeaseRef, "generation": input.Generation, "fence_digest": fenceDigest,
		"workload_instance": input.WorkloadInstance, "connection_id": selected.connectionID,
		"root_run_id": selected.rootID, "subject_id": selected.subjectID, "binding_id": choice.bindingID,
		"binding_generation": selection.BindingGeneration, "credential_ref": selection.Credential.Ref,
		"definition_digest": selected.definitionDigest, "configuration_digest": choice.configurationDigest,
		"resource_permission_profile": selection.ResourcePermissionProfile,
	}).Scan(&recovery)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, denyUnstartedPersonalInvocationTx(ctx, tx, current, input, errs.ErrForbidden)
	}
	if err != nil {
		return empty, serializableTransactionError(err, errs.ErrUnavailable)
	}
	tag, err := tx.Exec(ctx, queryPersonalInvocationAdmit, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "invocation_id": selected.invocationID,
		"generation": input.Generation, "lease_ref": input.LeaseRef, "fence_digest": fenceDigest,
		"workload_instance": input.WorkloadInstance, "recovery_read_only": recovery,
	})
	if err != nil {
		return empty, serializableTransactionError(err, mapWriteError(err))
	}
	if tag.RowsAffected() != 1 {
		return empty, errs.ErrConflict
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction,
		auditRef, current.organizationID, nil, current.actorID, "platform.personal-integration-calls.begin",
		"INTEGRATION_INVOCATION", input.InvocationRef, "i18n:PERSONAL_INTEGRATION_CALL_ADMITTED", current.correlationRef); err != nil {
		return empty, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, errs.ErrUnavailable
	}
	return entity.PersonalIntegrationInvocationAdmissionResult{Credential: selection.Credential, RecoveryReadOnly: recovery,
		ResourcePermissionProfile: selection.ResourcePermissionProfile}, nil
}

// denyUnstartedPersonalInvocationTx persists a known pre-effect refusal without inventing an
// external cancellation. Only owned exact lease/no-admission rows can change; all started or
// uncertain work stays untouched. Terminal observation is the existing GetIntegrationInvocation
// owner read, with an atomic audit and no new domain event. Infrastructure errors never trigger it.
// denyUnstartedPersonalInvocationTx сохраняет известный отказ до effect, не выдумывая внешнюю
// отмену. Меняется лишь exact owned lease без admissions; начатая и неопределённая работа не
// затрагивается. Terminal читается существующим owner GetIntegrationInvocation с atomic audit
// без нового доменного события. Инфраструктурные ошибки этот переход не вызывают.
func denyUnstartedPersonalInvocationTx(ctx context.Context, tx pgx.Tx, current scope, input command.PersonalIntegrationInvocationAdmission, cause error) error {
	var preflight *platformservice.IntegrationCredentialPreflightError
	if !errors.Is(cause, errs.ErrForbidden) && !errors.Is(cause, errs.ErrNotFound) && !errors.As(cause, &preflight) {
		return cause
	}
	fence := sha256.Sum256([]byte(input.Fence))
	tag, err := tx.Exec(ctx, queryPersonalInvocationDenyUnstarted, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "invocation_ref": input.InvocationRef,
		"generation": input.Generation, "lease_ref": input.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]), "workload_instance": input.WorkloadInstance,
	})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if tag.RowsAffected() == 0 {
		return cause
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction,
		auditRef, current.organizationID, nil, current.actorID, "platform.personal-integration-calls.deny",
		"INTEGRATION_INVOCATION", input.InvocationRef, "i18n:PERSONAL_INTEGRATION_CALL_BLOCKED", current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrUnavailable
	}
	return cause
}

// validPersonalInvocationAdmission accepts bounded owner-issued lease coordinates, not selectors.
// validPersonalInvocationAdmission принимает bounded owner-issued lease coordinates, не selectors.
func validPersonalInvocationAdmission(input command.PersonalIntegrationInvocationAdmission) bool {
	return personalInvocationLeaseCoordinate(input.InvocationRef) && personalInvocationLeaseCoordinate(input.LeaseRef) &&
		personalInvocationLeaseCoordinate(input.Fence) && input.Generation > 0 && strings.TrimSpace(input.WorkloadInstance) != "" &&
		len(input.WorkloadInstance) <= 256 && utf8.ValidString(input.WorkloadInstance) && !strings.ContainsAny(input.WorkloadInstance, "\x00\n\r")
}

// personalInvocationLeaseCoordinate admits bounded opaque identifiers with no transport authority.
// personalInvocationLeaseCoordinate допускает bounded opaque identifiers без transport authority.
func personalInvocationLeaseCoordinate(raw string) bool {
	if len(raw) < 8 || len(raw) > 96 {
		return false
	}
	for _, character := range raw {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}
