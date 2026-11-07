package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// personalCredentialWorkJob holds one locked immutable owner snapshot and mutable lease state.
// No provider response or plaintext credential enters the row or command receipt.
// personalCredentialWorkJob содержит один заблокированный immutable owner snapshot и состояние lease.
// Provider response и plaintext credential не попадают в строку либо command receipt.
type personalCredentialWorkJob struct {
	id, ref, connectionRef, subjectID, bindingID, state, leaseRef, fenceDigest string
	version, generation                                                        int64
	expiresAt                                                                  *time.Time
	snapshot                                                                   entity.PersonalIntegrationCredentialTestSnapshot
}

// personalCredentialTestInput contains freshly resolved self metadata and a pinned published
// package for one READ health probe, excluding the shared Connection credential.
// personalCredentialTestInput содержит текущие собственные metadata и закреплённый опубликованный
// package одной READ health-проверки, исключая общую credential Connection.
type personalCredentialTestInput struct {
	binding       entity.PersonalIntegrationCredential
	definition    integrationpackage.Package
	configuration map[string]any
}

// cancelPersonalCredentialTests fences queued/claimed/probing work in the same transaction as
// key replacement or revoke. Already observed terminal results remain immutable history.
// cancelPersonalCredentialTests ограждает queued/claimed/probing работу в одной транзакции с
// заменой либо отзывом ключа. Наблюдённые terminal результаты сохраняются как immutable history.
func cancelPersonalCredentialTests(ctx context.Context, tx pgx.Tx, organizationID, bindingID string) error {
	_, err := tx.Exec(ctx, queryPersonalCredentialTestCancelBinding, pgx.StrictNamedArgs{"organization_id": organizationID, "binding_id": bindingID})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// persistPersonalCredentialVerification updates only the already locked current generation and
// its safe observation pins; the binding guard and OCC require exactly one affected row.
// persistPersonalCredentialVerification меняет только уже заблокированное текущее поколение и
// безопасные observation pins; binding guard и OCC требуют ровно одну изменённую строку.
func persistPersonalCredentialVerification(ctx context.Context, tx pgx.Tx, bindingID string, previousVersion int64, binding entity.PersonalIntegrationCredential) error {
	confirmed, denied := binding.VerifiedCapabilities, binding.DeniedCapabilities
	if confirmed == nil {
		confirmed = []string{}
	}
	if denied == nil {
		denied = []string{}
	}
	tag, err := tx.Exec(ctx, queryPersonalCredentialTestBindingState, pgx.StrictNamedArgs{
		"organization_id": binding.OrganizationID, "binding_id": bindingID, "state": binding.State, "version": binding.Version, "expected_version": previousVersion,
		"generation": binding.Generation, "definition_digest": binding.VerifiedDefinitionDigest, "configuration_digest": binding.VerifiedConfigurationDigest,
		"confirmed": confirmed, "denied": denied, "profiles": append([]string{}, binding.ObservedPermissionProfiles...), "expires_at": binding.ExpiresAt})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// queuePersonalCredentialTest derives all probe pins from a visible published Connection after
// self/OCC checks; it stores CHECKING, the immutable job and Execute's audit/receipt atomically.
// queuePersonalCredentialTest выводит все probe pins из видимого опубликованного Connection после
// self/OCC проверок; CHECKING, immutable job и audit/receipt Execute сохраняются атомарно.
func (repository *Repository) queuePersonalCredentialTest(ctx context.Context, tx pgx.Tx, current scope, input command.Command, bindingID string, binding entity.PersonalIntegrationCredential) (commandOutcome, error) {
	payload, ok := input.Payload.(command.PersonalIntegrationCredentialInput)
	if !ok || payload.Credential != nil || payload.MaterializationRef != "" || payload.ExpectedConnectionVersion < 1 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.ExpectedConnectionVersion != binding.ConnectionVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	connection, err := scanConnection(tx.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, current.organizationID, binding.ConnectionRef))
	if err != nil {
		return commandOutcome{}, err
	}
	if !connection.Enabled {
		return commandOutcome{}, errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connection.Ref, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return commandOutcome{}, err
	}
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" || !definition.RequiresConnectionCredential() ||
		!definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) {
		return commandOutcome{}, errs.ErrForbidden
	}
	configuration, valid := integrationStringConfiguration(connection.PublicConfiguration)
	if !valid || definition.ValidateConfiguration(configuration) != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	raw, err := json.Marshal(connection.PublicConfiguration)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	ref, err := newRef("pct")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	probeKeys := personalCredentialReadProbeKeys(definition)
	profiles, writeKeys := integrationpackage.CredentialObservationRequests(definition)
	started, snapshot, err := platformservice.BeginPersonalIntegrationCredentialVerification(binding,
		entity.IntegrationCredentialInitiator{OrganizationID: current.organizationID, SubjectID: current.actorID, SubjectKind: "USER"}, *input.Mutation.ExpectedVersion,
		entity.PersonalIntegrationCredentialTestSpec{Ref: ref, ConnectionVersion: connection.Version, DefinitionDigest: definition.Digest,
			ConfigurationDigest: hex.EncodeToString(sum[:]), HealthCapabilityKey: health.Key, AllowedReadCapabilityKeys: probeKeys, AllowedObservationProfiles: profiles, AllowedDeniedWriteCapabilityKeys: writeKeys})
	if err != nil {
		return commandOutcome{}, err
	}
	if err := cancelPersonalCredentialTests(ctx, tx, current.organizationID, bindingID); err != nil {
		return commandOutcome{}, err
	}
	if err := persistPersonalCredentialVerification(ctx, tx, bindingID, binding.Version, started); err != nil {
		return commandOutcome{}, err
	}
	snapshotBytes, err := json.Marshal(snapshot)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryPersonalCredentialTestInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID,
		"binding_id": bindingID, "subject_id": current.actorID, "version": started.Version, "snapshot": snapshotBytes})
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	return personalCredentialOutcome(started, "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_QUEUED"), nil
}

// lockPersonalCredentialTest uses Connection→binding→job ordering, including when actor access
// was revoked. Cleanup can therefore fence work without granting access to its credential.
// lockPersonalCredentialTest использует порядок Connection→binding→job, включая отозванный actor
// access. Cleanup ограждает работу, не выдавая доступ к её credential.
func (repository *Repository) lockPersonalCredentialTest(ctx context.Context, tx pgx.Tx, organizationID, ref string) (personalCredentialWorkJob, error) {
	job := personalCredentialWorkJob{ref: ref}
	args := pgx.StrictNamedArgs{"organization_id": organizationID, "ref": ref}
	err := tx.QueryRow(ctx, queryPersonalCredentialTestLocator, args).Scan(&job.connectionRef, &job.subjectID, &job.bindingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, errs.ErrNotFound
	}
	if err != nil {
		return job, errs.ErrUnavailable
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, organizationID, job.connectionRef); err != nil {
		return job, err
	}
	var lockedID string
	if err := tx.QueryRow(ctx, queryPersonalCredentialTestLockBinding, pgx.StrictNamedArgs{"organization_id": organizationID, "binding_id": job.bindingID}).Scan(&lockedID); err != nil {
		return job, errs.ErrUnavailable
	}
	var raw []byte
	if err := tx.QueryRow(ctx, queryPersonalCredentialTestLock, args).Scan(&job.id, &raw, &job.state, &job.version, &job.generation, &job.leaseRef, &job.fenceDigest, &job.expiresAt); err != nil {
		return job, errs.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 8192 || decoder.Decode(&job.snapshot) != nil {
		return job, errs.ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return job, errs.ErrUnavailable
	}
	if job.snapshot.Ref != ref || job.snapshot.OrganizationID != organizationID || job.snapshot.SubjectID != job.subjectID || job.snapshot.ConnectionRef != job.connectionRef {
		return job, errs.ErrUnavailable
	}
	return job, nil
}

// currentPersonalCredentialTestInput rechecks the stored user's active visibility and current
// credential/configuration immediately before claim/admission/completion. It never reads shared material.
// currentPersonalCredentialTestInput повторно проверяет active visibility сохранённого пользователя
// и текущие credential/configuration перед claim/admission/completion. Общий ключ не читается.
func (repository *Repository) currentPersonalCredentialTestInput(ctx context.Context, tx pgx.Tx, organizationID string, job personalCredentialWorkJob) (personalCredentialTestInput, error) {
	var result personalCredentialTestInput
	owner := scope{organizationID: organizationID, actorID: job.subjectID}
	binding, err := readPersonalCredentialTx(ctx, tx, owner, job.connectionRef)
	if err != nil {
		return result, err
	}
	connection, err := scanConnection(tx.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, organizationID, job.connectionRef))
	if err != nil {
		return result, err
	}
	if !connection.Enabled {
		return result, errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, organizationID, connection.Ref, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return result, err
	}
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" || health.Key != job.snapshot.HealthCapabilityKey || !slices.Equal(personalCredentialReadProbeKeys(definition), job.snapshot.AllowedReadCapabilityKeys) || !definition.RequiresConnectionCredential() || !definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) {
		return result, errs.ErrForbidden
	}
	profiles, writeKeys := integrationpackage.CredentialObservationRequests(definition)
	if (len(job.snapshot.AllowedObservationProfiles) != 0 || len(job.snapshot.AllowedDeniedWriteCapabilityKeys) != 0) &&
		(!slices.Equal(profiles, job.snapshot.AllowedObservationProfiles) || !slices.Equal(writeKeys, job.snapshot.AllowedDeniedWriteCapabilityKeys)) {
		return result, errs.ErrForbidden
	}
	configuration, valid := integrationStringConfiguration(connection.PublicConfiguration)
	if !valid || definition.ValidateConfiguration(configuration) != nil {
		return result, errs.ErrInvalid
	}
	raw, err := json.Marshal(connection.PublicConfiguration)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	if err := platformservice.ValidatePersonalIntegrationCredentialVerificationSnapshot(binding, job.snapshot, definition.Digest, hex.EncodeToString(sum[:])); err != nil {
		return result, err
	}
	return personalCredentialTestInput{binding: binding, definition: definition, configuration: connection.PublicConfiguration}, nil
}

// personalCredentialReadProbeKeys binds the same closed probe registry used by the gateway to the
// owner snapshot; no worker response can enlarge this set after verification begins.
// personalCredentialReadProbeKeys закрепляет общий закрытый реестр probes в owner snapshot;
// ответ worker не может расширить этот набор после начала проверки.
func personalCredentialReadProbeKeys(definition integrationpackage.Package) []string {
	probes := integrationpackage.CredentialReadProbes(definition)
	keys := make([]string, len(probes))
	for index, probe := range probes {
		keys[index] = probe.Key
	}
	return keys
}

// finishPersonalCredentialTest fences exactly one locked nonterminal job and retains its snapshot.
// finishPersonalCredentialTest ограждает ровно одно заблокированное незавершённое задание, сохраняя snapshot.
func finishPersonalCredentialTest(ctx context.Context, tx pgx.Tx, organizationID string, job personalCredentialWorkJob, state, outcome string) error {
	tag, err := tx.Exec(ctx, queryPersonalCredentialTestFinish, pgx.StrictNamedArgs{"organization_id": organizationID, "id": job.id, "version": job.version, "state": state, "outcome": outcome})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// resetPersonalCredentialTestBinding clears only the exact still-checking snapshot, preserving a
// replacement/revoked/newer binding even during expiry or eligibility-loss cleanup.
// resetPersonalCredentialTestBinding сбрасывает только точный всё ещё проверяемый snapshot,
// сохраняя replacement/revoked/newer binding при expiry либо потере eligibility.
func resetPersonalCredentialTestBinding(ctx context.Context, tx pgx.Tx, organizationID string, job personalCredentialWorkJob) error {
	_, err := tx.Exec(ctx, queryPersonalCredentialTestResetBinding, pgx.StrictNamedArgs{"organization_id": organizationID, "binding_id": job.bindingID,
		"version": job.snapshot.BindingVersion, "generation": job.snapshot.BindingGeneration})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// personalCredentialTestLease validates the full one-time lease envelope and owner clock.
// personalCredentialTestLease проверяет полный одноразовый lease envelope и часы владельца.
func personalCredentialTestLease(job personalCredentialWorkJob, payload command.PersonalIntegrationCredentialTestCompletion, now time.Time) bool {
	if !validPersonalCredentialTestLeaseInput(payload) {
		return false
	}
	sum := sha256.Sum256([]byte(payload.Fence))
	return payload.TestRef == job.ref && payload.LeaseRef != "" && payload.LeaseRef == job.leaseRef && len(payload.Fence) <= 96 && payload.Generation == job.generation &&
		job.generation == 1 && hex.EncodeToString(sum[:]) == job.fenceDigest && job.expiresAt != nil && now.Before(*job.expiresAt)
}

// validPersonalCredentialTestLeaseInput bounds opaque owner-issued coordinates before lookup or
// hashing; both admission and completion reject empty, oversized or noncanonical generations.
// validPersonalCredentialTestLeaseInput ограничивает непрозрачные координаты владельца до чтения
// либо хеширования; admission и completion отклоняют пустые, длинные поля и неверное поколение.
func validPersonalCredentialTestLeaseInput(payload command.PersonalIntegrationCredentialTestCompletion) bool {
	return payload.TestRef != "" && len(payload.TestRef) <= 96 && payload.LeaseRef != "" && len(payload.LeaseRef) <= 96 &&
		payload.Fence != "" && len(payload.Fence) <= 96 && payload.Generation == 1
}
