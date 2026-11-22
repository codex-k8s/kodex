package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySSHSetupPrivatePermit derives the original owner from a permit-bound immutable ledger row; inactive actors remain readable for evidence retention only, never for execution credit.
// querySSHSetupPrivatePermit выводит original owner из permit-bound immutable ledger row; inactive actors доступны лишь для evidence retention, не для execution credit.
//
//go:embed sql/ssh_setup__private_permit.sql
var querySSHSetupPrivatePermit string

// sshUploadRow separates immutable input from mutable authority and clock; no namespace, tenant, subject or target is taken from a broker-supplied locator.
// sshUploadRow отделяет immutable input от mutable authority и clock; namespace, tenant, subject и target не берутся из broker-supplied locator.
type sshUploadRow struct {
	input                                                                  sshSetupImmutableInput
	owner                                                                  integrationBrokerIdentityWriteOwner
	id, digest, organizationRef, actorRef, lifecycle, authenticationDigest string
	state                                                                  enum.SSHIdentitySetupState
	version                                                                int64
	now, accessUntil                                                       time.Time
	actorActive                                                            bool
}

// String keeps private source and material-integrity pins out of logs even when an entire row is formatted.
// String скрывает private source и material-integrity pins из logs при форматировании всей row.
func (sshUploadRow) String() string { return "[SSH private setup owner row]" }

// GoString enforces the same redaction for detailed Go diagnostics.
// GoString сохраняет ту же redaction для подробных Go diagnostics.
func (row sshUploadRow) GoString() string { return row.String() }

// scanSSHUploadRow compares a canonical immutable body with independently constrained relational coordinates before using any decoded owner field; duplicate, aliased, unknown or malformed JSON cannot acquire meaning.
// scanSSHUploadRow сверяет canonical immutable body с отдельно constrained relational coordinates до использования decoded owner field; duplicate, aliased, unknown либо malformed JSON не получают смысла.
func scanSSHUploadRow(row pgx.Row) (sshUploadRow, error) {
	var result sshUploadRow
	var raw string
	var expected sshSetupImmutableInput
	err := row.Scan(&result.id, &raw, &result.digest, &result.state, &result.version, &expected.ExpiresAt, &result.now, &result.authenticationDigest, &result.organizationRef, &result.actorRef, &result.actorActive,
		&result.lifecycle, &expected.ConnectionID, &expected.ConnectionRef, &expected.OrganizationID, &expected.ActorID,
		&expected.IdentityID, &expected.IdentityRef, &expected.SubjectID, &expected.BindingID, &expected.BindingRef, &expected.TargetRef, &expected.IdentityRevision, &expected.Policy, &expected.Namespace,
		&expected.ConnectionVersion, &expected.BindingVersion, &expected.BindingGeneration, &expected.DefinitionDigest, &expected.ConfigurationDigest, &expected.ContentDigest, &expected.ContentBytes, &expected.SetupRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil || len(raw) < 1 || len(raw) > 8192 {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	var input sshSetupImmutableInput
	if json.Unmarshal([]byte(raw), &input) != nil {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	canonical, err := json.Marshal(input)
	if err != nil || !bytes.Equal(canonical, []byte(raw)) {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	digest, err := input.digest()
	if err != nil || digest != result.digest {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	expected.ExpiresAt = expected.ExpiresAt.UTC()
	input.ExpiresAt = input.ExpiresAt.UTC()
	if input != expected || !result.state.Valid() || result.version < 1 || result.now.IsZero() || result.organizationRef == "" || len(result.organizationRef) > 256 || !utf8.ValidString(result.organizationRef) || result.actorRef == "" || len(result.actorRef) > 256 || !utf8.ValidString(result.actorRef) {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	result.input = input
	result.now = result.now.UTC()
	result.owner = integrationBrokerIdentityWriteOwner{organizationID: input.OrganizationID, connectionID: input.ConnectionID, connectionRef: input.ConnectionRef, subjectID: input.SubjectID, bindingID: input.BindingID, bindingRef: input.BindingRef, targetRef: input.TargetRef, connectionVersion: input.ConnectionVersion, bindingVersion: input.BindingVersion, bindingGeneration: input.BindingGeneration, policy: input.Policy, purpose: "SSH_TARGET"}
	if !result.owner.valid() {
		return sshUploadRow{}, errs.ErrUnavailable
	}
	return result, nil
}

// sshUploadActorScopeTx reuses the canonical current membership query for the stored original USER; the broker's service principal cannot replace that actor or tenant.
// sshUploadActorScopeTx использует canonical current membership query stored original USER; service principal broker не заменяет actor либо tenant.
func sshUploadActorScopeTx(ctx context.Context, tx pgx.Tx, row sshUploadRow, correlation string) (scope, error) {
	var current scope
	err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, row.actorRef, row.organizationRef).Scan(&current.organizationID, &current.organizationRef, &current.actorID, &current.actorRef, &current.actorName, &current.role)
	if errors.Is(err, pgx.ErrNoRows) {
		return current, errs.ErrForbidden
	}
	if err != nil {
		return current, errs.ErrUnavailable
	}
	if current.organizationID != row.input.OrganizationID || current.actorID != row.input.ActorID || !row.actorActive {
		return scope{}, errs.ErrForbidden
	}
	current.correlationRef = correlation
	return current, nil
}

// validateSSHUploadOwnerTx checks current original owner, exact package/configuration and binding generation under Connection→binding lock order before issuing any private storage admission.
// validateSSHUploadOwnerTx проверяет current original owner, exact package/configuration и binding generation под Connection→binding lock order до выдачи private storage admission.
func (repository *Repository) validateSSHUploadOwnerTx(ctx context.Context, tx pgx.Tx, row sshUploadRow, correlation string) (scope, time.Time, error) {
	current, err := sshUploadActorScopeTx(ctx, tx, row, correlation)
	if err != nil {
		return current, time.Time{}, err
	}
	connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, row.input.ConnectionRef)
	if err != nil {
		return current, time.Time{}, err
	}
	if row.lifecycle != "ACTIVE" || connection.version != row.input.ConnectionVersion || connection.definitionDigest != row.input.DefinitionDigest || connection.credentialPolicy != row.input.Policy || !row.now.Before(row.input.ExpiresAt) {
		return current, time.Time{}, errs.ErrConflict
	}
	source, err := repository.sshSetupOwnerTx(ctx, tx, current, connection, row.input.ConnectionRef, row.input.BindingVersion)
	if err != nil {
		return current, time.Time{}, err
	}
	if source.owner != row.owner || source.definitionDigest != row.input.DefinitionDigest || source.configurationDigest != row.input.ConfigurationDigest || !source.now.Before(row.input.ExpiresAt) {
		return current, time.Time{}, errs.ErrConflict
	}
	return current, source.accessUntil, nil
}

// querySSHSetupRecoveryEligibility requires a durable classified WRITE receipt and excludes known/rejected material; lease expiry and absence of completion never create observation authority.
// querySSHSetupRecoveryEligibility требует durable classified WRITE receipt и исключает known/rejected material; lease expiry и missing completion не создают observation authority.
//
//go:embed sql/ssh_setup__recovery_eligibility.sql
var querySSHSetupRecoveryEligibility string

// sshRecoveryEligibility contains only authoritative receipt facts for one immutable setup and has no external transport outcome inference.
// sshRecoveryEligibility содержит лишь authoritative receipt facts одного immutable setup без вывода external transport outcome.
type sshRecoveryEligibility struct {
	spent, unknown, rejected, known, active bool
	generation                              int64
}

// readSSHRecoveryEligibility reads all recovery facts from the owner transaction rather than handler memory or a client-selected action.
// readSSHRecoveryEligibility читает recovery facts из owner transaction, а не handler memory либо client-selected action.
func readSSHRecoveryEligibility(ctx context.Context, tx pgx.Tx, row sshUploadRow) (sshRecoveryEligibility, error) {
	var result sshRecoveryEligibility
	if tx.QueryRow(ctx, querySSHSetupRecoveryEligibility, pgx.StrictNamedArgs{"setup_id": row.id, "identity_id": row.input.IdentityID}).Scan(&result.spent, &result.unknown, &result.rejected, &result.known, &result.active, &result.generation) != nil || result.generation < 0 || result.generation > 8 {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// mode denies every terminal/unreported setup and an active attempt; a new observation requires the latest attempt's durable UNKNOWN as well as the original WRITE receipt, never ATTEMPTING plus lease expiry.
// mode отклоняет terminal/unreported setup и active attempt; новое observation требует durable UNKNOWN последней attempt и original WRITE receipt, а не ATTEMPTING плюс lease expiry.
func (info sshRecoveryEligibility) mode(row sshUploadRow) (enum.SSHIdentityUploadMode, error) {
	if row.state.Terminal() || !row.now.Before(row.input.ExpiresAt) || info.active || info.known || info.rejected || info.generation >= 8 {
		return "", errs.ErrConflict
	}
	if !info.spent && row.state == enum.SSHIdentitySetupPrepared && row.authenticationDigest == "" {
		return enum.SSHIdentityUploadWrite, nil
	}
	if info.spent && info.unknown && row.state == enum.SSHIdentitySetupUnknown && managedssh.Digest(row.authenticationDigest) {
		return enum.SSHIdentityUploadObserve, nil
	}
	return "", errs.ErrConflict
}

// sshSetupBrokerCaller confines private setup effects to the authenticated secret-broker operation; its service actor never becomes the original credential owner.
// sshSetupBrokerCaller ограничивает private setup effects authenticated secret-broker operation; service actor не становится original credential owner.
func sshSetupBrokerCaller(principal value.Principal, permission string) error {
	if principal.Validate() != nil || principal.CallerWorkload != "secret-broker" || principal.Permission != permission {
		return errs.ErrForbidden
	}
	return nil
}

// ResolveSSHIdentityUpload resolves only fresh original ownership and current receipt-backed eligibility; a valid permit digest cannot select another purpose, namespace or a second material write.
// ResolveSSHIdentityUpload разрешает лишь fresh original ownership и current receipt-backed eligibility; valid permit digest не выбирает другой purpose, namespace либо второй material write.
func (repository *Repository) ResolveSSHIdentityUpload(ctx context.Context, principal value.Principal, input command.SSHIdentityUploadResolve) (entity.SSHIdentityUploadSnapshot, error) {
	var result entity.SSHIdentityUploadSnapshot
	if err := sshSetupBrokerCaller(principal, "platform.runtime.ssh.setup.resolve"); err != nil {
		return result, err
	}
	if !managedssh.OwnerReference(input.SetupRef) || !managedssh.Digest(input.PermitSHA256) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := scanSSHUploadRow(tx.QueryRow(ctx, querySSHSetupPrivatePermit, pgx.StrictNamedArgs{"setup_ref": input.SetupRef, "permit_digest": input.PermitSHA256}))
	if err != nil {
		return result, err
	}
	_, accessUntil, err := repository.validateSSHUploadOwnerTx(ctx, tx, row, principal.CorrelationRef)
	if err != nil {
		return result, err
	}
	info, err := readSSHRecoveryEligibility(ctx, tx, row)
	if err != nil {
		return result, err
	}
	mode, err := info.mode(row)
	if err != nil {
		return result, err
	}
	result = entity.SSHIdentityUploadSnapshot{SetupRef: row.input.SetupRef, Digest: row.digest, Preparation: managedssh.IdentityPreparation{IdentityRevisionRef: row.input.IdentityRef, TargetRef: row.input.TargetRef, Revision: row.input.IdentityRevision, Namespace: row.input.Namespace, ContentSHA256: row.input.ContentDigest}, ContentBytes: row.input.ContentBytes, ExpiresAt: earlierAccessDeadline(row.input.ExpiresAt, accessUntil).UTC(), Mode: mode}
	if mode == enum.SSHIdentityUploadObserve {
		result.AuthenticationSHA256 = row.authenticationDigest
	}
	if err := commitIdentitySetupAdmission(ctx, tx, accessUntil, result.ExpiresAt); err != nil {
		return entity.SSHIdentityUploadSnapshot{}, err
	}
	return result, nil
}
