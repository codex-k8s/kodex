package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querySSHHealthSharedSource is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthSharedSource является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__shared_source.sql
var querySSHHealthSharedSource string

// querySSHHealthMaterial is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthMaterial является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__material.sql
var querySSHHealthMaterial string

// querySSHHealthClock is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthClock является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__clock.sql
var querySSHHealthClock string

// querySSHHealthInsert is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthInsert является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__insert.sql
var querySSHHealthInsert string

// querySSHHealthRead is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthRead является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__read.sql
var querySSHHealthRead string

// querySSHHealthComplete is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthComplete является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__complete.sql
var querySSHHealthComplete string

// querySSHHealthPersonalState is the fixed owner query for the addressed health transition; callers cannot supply SQL or alternate material sources.
// querySSHHealthPersonalState является fixed owner query адресованного health transition; caller не передаёт SQL либо alternate material source.
//
//go:embed sql/ssh_health__personal_state.sql
var querySSHHealthPersonalState string

// sshHealthJob binds either existing personal or shared work to one Connection lock; representation is derived from stored owner state.
// sshHealthJob связывает existing personal либо shared work с Connection lock; representation выводится из stored owner state.
type sshHealthJob struct {
	id, ref, connectionRef, state, leaseRef, fenceDigest, instance, inputDigest, identityID string
	configurationDigest                                                                     string
	generation                                                                              int64
	expiresAt                                                                               *time.Time
	personal                                                                                *personalCredentialWorkJob
	identity                                                                                entity.IntegrationBrokerIdentityRevision
	definition                                                                              integrationpackage.Package
	resolved                                                                                personalCredentialTestInput
}

// sshHealthHash retains a canonical integrity digest only; hashes never confer owner authority.
// sshHealthHash сохраняет лишь canonical integrity digest; hashes не создают owner authority.
func sshHealthHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// lockSSHHealthJob follows Connection then binding/test ordering and rejects ambiguous test references across the two existing queues.
// lockSSHHealthJob соблюдает Connection затем binding/test lock order и отклоняет ambiguous test references двух existing queues.
func (repository *Repository) lockSSHHealthJob(ctx context.Context, tx pgx.Tx, organizationID, ref string) (sshHealthJob, error) {
	job := sshHealthJob{ref: ref}
	connectionRef, shared, err := locateBrokerIdentityHealthTestTx(ctx, tx, organizationID, ref)
	if err != nil {
		return job, err
	}
	job.connectionRef = connectionRef
	if _, err := repository.lockIntegrationConnection(ctx, tx, organizationID, job.connectionRef); err != nil {
		return job, err
	}
	if !shared {
		personal, err := repository.lockPersonalCredentialTest(ctx, tx, organizationID, ref)
		if err != nil {
			return job, err
		}
		raw, err := json.Marshal(personal.snapshot)
		if err != nil {
			return job, errs.ErrUnavailable
		}
		job.id, job.state, job.leaseRef, job.fenceDigest, job.instance = personal.id, personal.state, personal.leaseRef, personal.fenceDigest, personal.workloadInstance
		job.generation, job.expiresAt, job.inputDigest, job.personal = personal.generation, personal.expiresAt, sshHealthHash(raw), &personal
		return job, nil
	}
	sharedJob, err := repository.lockSharedBrokerIdentityHealthTx(ctx, tx, organizationID, ref, job.connectionRef, entity.IntegrationBrokerIdentitySSH)
	if err != nil {
		return job, err
	}
	job.id, job.state, job.leaseRef, job.fenceDigest, job.instance = sharedJob.id, sharedJob.state, sharedJob.leaseRef, sharedJob.fenceDigest, sharedJob.instance
	job.generation, job.expiresAt, job.inputDigest = sharedJob.generation, sharedJob.expiresAt, sharedJob.inputDigest
	job.identityID, job.identity = sharedJob.identityID, sharedJob.identity
	return job, nil
}

// matches compares the exact original worker instance, lease, fence, generation and opaque full-snapshot digest using the database clock.
// matches сравнивает exact original worker instance, lease, fence, generation и opaque full-snapshot digest по database clock.
func (job sshHealthJob) matches(binding managedssh.Binding, now time.Time) bool {
	return binding.Validate() == nil && binding.ConnectionTestRef == job.ref && binding.InputDigest == job.inputDigest && binding.WorkloadInstance == job.instance && binding.LeaseRef == job.leaseRef && binding.Generation == job.generation && job.generation == 1 && sshHealthHash([]byte(binding.Fence)) == job.fenceDigest && job.expiresAt != nil && now.Before(*job.expiresAt)
}

// sshHealthMode derives the only two SSH health operations from the exact validated package; no shell text, adapter name alone or caller field selects READ authority.
// sshHealthMode выводит две SSH health operations из exact validated package; shell text, одно adapter name либо caller field не выбирают READ authority.
func sshHealthMode(definition integrationpackage.Package) (managedssh.TransportMode, error) {
	delivery, known := definition.ConnectionCredentialDelivery()
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !known || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != "SSH_TARGET" || definition.Spec.Adapter != "SSH_TARGET" || definition.Spec.AdapterOwner != "integration-gateway" || definition.Spec.ExecutionRoute != "MANAGED_MCP" || definition.Spec.Readiness != "READY" || !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" {
		return "", errs.ErrForbidden
	}
	if definition.Metadata.Key == integrationpackage.SSHManagedDefinitionKey && health.Operation == "ssh.target.inspect" {
		return managedssh.ManagedTransport, nil
	}
	if definition.Metadata.Key == integrationpackage.SSHExecuteDefinitionKey && health.Operation == "ssh.identity.inspect" {
		return managedssh.ExecuteTransport, nil
	}
	return "", errs.ErrForbidden
}

// sshHealthSourceTx uses the existing authoritative selector and exact stored package; known material outlives upload grants but never rejection, quarantine or current owner revocation.
// sshHealthSourceTx использует existing authoritative selector и exact stored package; known material переживает upload grants, но не rejection, quarantine либо current owner revoke.
func (repository *Repository) sshHealthSourceTx(ctx context.Context, tx pgx.Tx, organizationID string, job *sshHealthJob) (entity.SSHIdentityHealthAdmission, error) {
	var result entity.SSHIdentityHealthAdmission
	if job.personal != nil {
		input, err := repository.currentPersonalCredentialTestInput(ctx, tx, organizationID, *job.personal)
		if err != nil {
			return result, err
		}
		if job.personal.snapshot.Kind != entity.IntegrationCredentialBrokerIdentity || input.binding.Kind != entity.IntegrationCredentialBrokerIdentity || input.binding.Credential != nil || input.binding.BrokerPurpose != entity.IntegrationBrokerIdentitySSH || input.binding.BrokerIdentity == nil {
			return result, errs.ErrForbidden
		}
		job.resolved, job.definition, job.identity = input, input.definition, *input.binding.BrokerIdentity
		job.configurationDigest = job.personal.snapshot.ConfigurationDigest
	} else {
		var fresh bool
		err := tx.QueryRow(ctx, querySSHHealthSharedSource, pgx.StrictNamedArgs{"organization_id": organizationID, "ref": job.ref}).Scan(&fresh)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if !fresh {
			return result, errs.ErrForbidden
		}
		connection, err := scanConnection(tx.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, organizationID, job.connectionRef))
		if err != nil {
			return result, err
		}
		definition, err := repository.integrationPackage(ctx, tx, organizationID, job.connectionRef, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
		if err != nil {
			return result, err
		}
		var connectionID string
		if err := tx.QueryRow(ctx, querySSHHealthConnectionID, organizationID, job.connectionRef).Scan(&connectionID); err != nil {
			return result, errs.ErrUnavailable
		}
		selected, err := repository.readSharedHealthCredentialSourceTx(ctx, tx, organizationID, connectionID, job.connectionRef, definition)
		if err != nil {
			return result, err
		}
		if selected.identity == nil || selected.identity.Ref != job.identity.Ref || selected.identity.Revision != job.identity.Revision || selected.identity.Purpose != entity.IntegrationBrokerIdentitySSH {
			return result, errs.ErrForbidden
		}
		job.definition, job.identity = definition, *selected.identity
		configuration, err := json.Marshal(connection.PublicConfiguration)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		job.configurationDigest = sshHealthHash(configuration)
	}
	mode, err := sshHealthMode(job.definition)
	if err != nil {
		return result, err
	}
	identity := managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: job.identity.Ref, TargetRef: job.identity.TargetRef, Revision: job.identity.Revision}
	err = tx.QueryRow(ctx, querySSHHealthMaterial, pgx.StrictNamedArgs{"organization_id": organizationID, "connection_ref": job.connectionRef, "identity_ref": job.identity.Ref, "revision": job.identity.Revision, "target_ref": job.identity.TargetRef}).Scan(&job.identityID, &identity.Namespace, &identity.SecretName, &identity.SecretUID, &identity.SecretResourceVersion, &identity.ContentSHA256, &identity.AuthenticationSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if !identity.Valid() {
		return result, errs.ErrUnavailable
	}
	result.Identity, result.Mode = identity, mode
	return result, nil
}

// BeginSSHIdentityHealthProbe consumes one original health claim after source checks; a lost Begin acknowledgement cannot repeat external authentication.
// BeginSSHIdentityHealthProbe расходует original health claim после source checks; lost Begin acknowledgement не разрешает повтор external authentication.
func (repository *Repository) BeginSSHIdentityHealthProbe(ctx context.Context, p value.Principal, binding managedssh.Binding) (entity.SSHIdentityHealthAdmission, error) {
	var result entity.SSHIdentityHealthAdmission
	if p.CallerWorkload != "secret-broker" || p.Permission != "platform.runtime.ssh.health.begin" || binding.Validate() != nil || binding.ConnectionTestRef == "" {
		return result, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	job, err := repository.lockSSHHealthJob(ctx, tx, current.organizationID, binding.ConnectionTestRef)
	if err != nil {
		return result, err
	}
	var now time.Time
	if tx.QueryRow(ctx, querySSHHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	if job.state != "CLAIMED" || !job.matches(binding, now) {
		return result, errs.ErrForbidden
	}
	result, err = repository.sshHealthSourceTx(ctx, tx, current.organizationID, &job)
	if err != nil {
		return result, err
	}
	result.AttemptRef, err = newRef("shp")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	result.ExpiresAt = job.expiresAt.UTC()
	// Redacted locator formatting is deliberately bypassed only in this private integrity tuple.
	// Redacted locator formatting обходится лишь в этом private integrity tuple.
	raw, err := json.Marshal(struct {
		Ref, Input, Lease, Fence, Instance, Identity, Target, Namespace, Name, UID, RV, Content, Authentication, Definition, Configuration string
		Generation, Revision                                                                                                               int64
		Mode                                                                                                                               managedssh.TransportMode
		Expires                                                                                                                            time.Time
	}{result.AttemptRef, binding.InputDigest, binding.LeaseRef, sshHealthHash([]byte(binding.Fence)), binding.WorkloadInstance, result.Identity.IdentityRevisionRef, result.Identity.TargetRef, result.Identity.Namespace, result.Identity.SecretName, result.Identity.SecretUID, result.Identity.SecretResourceVersion, result.Identity.ContentSHA256, result.Identity.AuthenticationSHA256, job.definition.Digest, job.configurationDigest, binding.Generation, result.Identity.Revision, result.Mode, result.ExpiresAt})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	result.SnapshotDigest = sshHealthHash(raw)
	personalID, sharedID := jobIDs(job)
	tag, err := tx.Exec(ctx, querySSHHealthInsert, pgx.StrictNamedArgs{"ref": result.AttemptRef, "organization_id": current.organizationID, "connection_ref": job.connectionRef, "identity_id": job.identityID, "personal_id": personalID, "shared_id": sharedID, "snapshot_digest": result.SnapshotDigest, "input_digest": binding.InputDigest, "lease_ref": binding.LeaseRef, "fence_digest": sshHealthHash([]byte(binding.Fence)), "instance": binding.WorkloadInstance, "generation": binding.Generation, "expires_at": result.ExpiresAt, "definition_digest": job.definition.Digest, "configuration_digest": job.configurationDigest, "mode": string(result.Mode)})
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" && (databaseError.ConstraintName == "ssh_health_personal_once" || databaseError.ConstraintName == "ssh_health_shared_once") {
			return entity.SSHIdentityHealthAdmission{}, errs.ErrConflict
		}
		return entity.SSHIdentityHealthAdmission{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return entity.SSHIdentityHealthAdmission{}, errs.ErrConflict
	}
	if job.personal != nil {
		tag, err = tx.Exec(ctx, queryPersonalCredentialTestBegin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "version": job.personal.version})
		if err != nil {
			return entity.SSHIdentityHealthAdmission{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return entity.SSHIdentityHealthAdmission{}, errs.ErrConflict
		}
	}
	if auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.runtime.ssh.health.begin", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_PROBE_ADMITTED") != nil || tx.Commit(ctx) != nil {
		return entity.SSHIdentityHealthAdmission{}, errs.ErrUnavailable
	}
	return result, nil
}

// jobIDs selects the stored queue oneof without accepting a caller-selected type or rewriting its immutable original test.
// jobIDs выбирает stored queue oneof без caller-selected type либо переписывания immutable original test.
func jobIDs(job sshHealthJob) (string, string) {
	if job.personal != nil {
		return job.id, ""
	}
	return "", job.id
}

// CompleteSSHIdentityHealthProbe preserves exact historical replay and applies only fresh matching owner authority; infrastructure failures roll back all state and retain the same receipt for recovery.
// CompleteSSHIdentityHealthProbe сохраняет exact historical replay и применяет лишь fresh matching owner authority; infrastructure failures откатывают state и сохраняют тот же receipt для recovery.
func (repository *Repository) CompleteSSHIdentityHealthProbe(ctx context.Context, p value.Principal, input command.SSHIdentityHealthComplete) (entity.SSHIdentityHealthReceipt, error) {
	var result entity.SSHIdentityHealthReceipt
	if p.CallerWorkload != "secret-broker" || p.Permission != "platform.runtime.ssh.health.complete" || !validSSHHealthOwnerCompletion(input) {
		return result, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	job, err := repository.lockSSHHealthJob(ctx, tx, current.organizationID, input.Binding.ConnectionTestRef)
	if err != nil {
		return result, err
	}
	var id, digest, inputDigest, lease, fence, instance, outcome, subject, state string
	var generation int64
	var expires time.Time
	var qualified *time.Time
	var applied bool
	personalID, sharedID := jobIDs(job)
	err = tx.QueryRow(ctx, querySSHHealthRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "attempt_ref": input.AttemptRef, "connection_ref": job.connectionRef, "personal_id": personalID, "shared_id": sharedID}).Scan(&id, &digest, &inputDigest, &lease, &fence, &instance, &generation, &expires, &outcome, &subject, &qualified, &state, &applied)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if digest != input.SnapshotDigest || inputDigest != input.Binding.InputDigest || lease != input.Binding.LeaseRef || fence != sshHealthHash([]byte(input.Binding.Fence)) || instance != input.Binding.WorkloadInstance || generation != input.Binding.Generation {
		return result, errs.ErrForbidden
	}
	if outcome != "" {
		sameTime := qualified == nil && input.QualifiedUntil.IsZero() || qualified != nil && qualified.Equal(input.QualifiedUntil)
		if outcome != string(input.Outcome) || subject != input.ObservedSubjectDigest || !sameTime {
			return result, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return entity.SSHIdentityHealthReceipt{TestRef: job.ref, State: state, Applied: applied}, nil
	}
	_, sourceErr := repository.sshHealthSourceTx(ctx, tx, current.organizationID, &job)
	if errors.Is(sourceErr, errs.ErrUnavailable) {
		return result, sourceErr
	}
	var now time.Time
	if tx.QueryRow(ctx, querySSHHealthClock).Scan(&now) != nil {
		return result, errs.ErrUnavailable
	}
	requiredState := "CLAIMED"
	if job.personal != nil {
		requiredState = "PROBING"
	}
	fresh := sourceErr == nil && job.state == requiredState && job.matches(input.Binding, now) && now.Before(expires)
	if input.Outcome == entity.PersonalIntegrationCredentialAuthenticated {
		if input.QualifiedUntil.After(now.Add(24 * time.Hour)) {
			return result, errs.ErrInvalid
		}
		fresh = fresh && input.QualifiedUntil.After(now)
	}
	sharedConnectionChanged := false
	result = entity.SSHIdentityHealthReceipt{TestRef: job.ref, State: "CANCELLED"}
	if fresh {
		result.State = "FAILED"
		if input.Outcome == entity.PersonalIntegrationCredentialAuthenticated {
			result.State = "SUCCEEDED"
		}
		if job.personal != nil {
			observation := entity.PersonalIntegrationCredentialTestObservation{Outcome: input.Outcome}
			if result.State == "SUCCEEDED" {
				expiry := now.Add(5 * time.Minute)
				if input.QualifiedUntil.Before(expiry) {
					expiry = input.QualifiedUntil
				}
				observation.ExpiresAt = &expiry
				observation.ConfirmedCapabilityKeys = []string{job.personal.snapshot.HealthCapabilityKey}
			}
			verified, err := platformservice.CompletePersonalIntegrationCredentialVerification(job.resolved.binding, job.personal.snapshot, job.personal.snapshot.DefinitionDigest, job.personal.snapshot.ConfigurationDigest, observation)
			if err != nil {
				return result, err
			}
			result.Applied, err = persistSSHHealthVerification(ctx, tx, job, verified, expires)
			if err != nil {
				return result, err
			}
		} else {
			var updated string
			connectionState := "DEGRADED"
			if result.State == "SUCCEEDED" {
				connectionState = "CONNECTED"
			}
			errorCode := "INTEGRATION_UNAVAILABLE"
			if result.State == "SUCCEEDED" {
				errorCode = ""
			}
			if _, err := tx.Exec(ctx, queryBrokerHealthSharedFinish, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "state": result.State, "summary": "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", "error_code": errorCode}); err != nil {
				return result, errs.ErrUnavailable
			}
			err = tx.QueryRow(ctx, queryBrokerHealthSharedConnection, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": job.connectionRef, "identity_id": job.identityID, "deadline": expires, "state": connectionState, "authenticated": result.State == "SUCCEEDED", "summary": "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED"}).Scan(&updated)
			if err != nil {
				return result, errs.ErrUnavailable
			}
			result.Applied = true
			sharedConnectionChanged = true
			job.state = result.State
		}
	}
	if !result.Applied {
		result.State = "CANCELLED"
	}
	if job.personal != nil && (job.state == "CLAIMED" || job.state == "PROBING") {
		finishOutcome := string(input.Outcome)
		if !result.Applied {
			finishOutcome = "CANCELLED"
			if resetPersonalCredentialTestBinding(ctx, tx, current.organizationID, *job.personal) != nil {
				return result, errs.ErrUnavailable
			}
		}
		if err := finishPersonalCredentialTest(ctx, tx, current.organizationID, *job.personal, result.State, finishOutcome); err != nil {
			return result, err
		}
	} else if job.personal == nil && job.state == "CLAIMED" {
		errorCode := "INTEGRATION_UNAVAILABLE"
		if result.State == "SUCCEEDED" {
			errorCode = ""
		}
		if result.State == "CANCELLED" {
			errorCode = "INTEGRATION_CREDENTIAL_UNAVAILABLE"
		}
		if _, err := tx.Exec(ctx, queryBrokerHealthSharedFinish, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id, "state": result.State, "summary": "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", "error_code": errorCode}); err != nil {
			return result, errs.ErrUnavailable
		}
		if result.State == "CANCELLED" {
			var reset string
			err := tx.QueryRow(ctx, queryBrokerHealthSharedReset, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": job.id}).Scan(&reset)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return result, errs.ErrUnavailable
			}
			sharedConnectionChanged = err == nil
		}
	}
	var qualifiedUntil *time.Time
	if !input.QualifiedUntil.IsZero() {
		date := input.QualifiedUntil.UTC()
		qualifiedUntil = &date
	}
	tag, err := tx.Exec(ctx, querySSHHealthComplete, pgx.StrictNamedArgs{"id": id, "outcome": string(input.Outcome), "subject_digest": input.ObservedSubjectDigest, "qualified_until": qualifiedUntil, "state": result.State, "applied": result.Applied})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	if sharedConnectionChanged {
		connection, err := scanConnection(tx.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, current.organizationID, job.connectionRef))
		if err != nil {
			return result, err
		}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", job.connectionRef, "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", connection.Version, connection.State); err != nil {
			return result, err
		}
	}
	if auditPersonalCredentialWork(ctx, tx, current, job.ref, "platform.runtime.ssh.health.complete", "i18n:PERSONAL_INTEGRATION_CREDENTIAL_TEST_COMPLETED") != nil || tx.Commit(ctx) != nil {
		return entity.SSHIdentityHealthReceipt{}, errs.ErrUnavailable
	}
	return result, nil
}

// validSSHHealthOwnerCompletion keeps the repository boundary closed even when an internal caller bypasses the service validator.
// validSSHHealthOwnerCompletion сохраняет repository boundary закрытой, даже если internal caller обходит service validator.
func validSSHHealthOwnerCompletion(input command.SSHIdentityHealthComplete) bool {
	if input.Binding.Validate() != nil || input.Binding.ConnectionTestRef == "" || !managedssh.OwnerReference(input.AttemptRef) || !managedssh.Digest(input.SnapshotDigest) {
		return false
	}
	switch input.Outcome {
	case entity.PersonalIntegrationCredentialAuthenticated:
		return managedssh.Digest(input.ObservedSubjectDigest) && !input.QualifiedUntil.IsZero() && input.QualifiedUntil.Nanosecond()%1000 == 0
	case entity.PersonalIntegrationCredentialAuthenticationRejected, entity.PersonalIntegrationCredentialVerificationUnavailable:
		return input.ObservedSubjectDigest == "" && input.QualifiedUntil.IsZero()
	default:
		return false
	}
}

// persistSSHHealthVerification fences the final binding update with the original lease deadline after every dependency read.
// persistSSHHealthVerification ограждает final binding update исходным lease deadline после каждого dependency read.
func persistSSHHealthVerification(ctx context.Context, tx pgx.Tx, job sshHealthJob, binding entity.PersonalIntegrationCredential, deadline time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, querySSHHealthPersonalState, pgx.StrictNamedArgs{"organization_id": binding.OrganizationID, "binding_id": job.personal.bindingID, "state": binding.State, "version": binding.Version, "expected_version": job.resolved.binding.Version, "generation": binding.Generation, "definition_digest": binding.VerifiedDefinitionDigest, "configuration_digest": binding.VerifiedConfigurationDigest, "confirmed": append([]string{}, binding.VerifiedCapabilities...), "denied": append([]string{}, binding.DeniedCapabilities...), "profiles": append([]string{}, binding.ObservedPermissionProfiles...), "expires_at": binding.ExpiresAt, "github_discovery": asJSON(binding.GitHubDiscovery), "deadline": deadline})
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return tag.RowsAffected() == 1, nil
}

// querySSHHealthConnectionID resolves the already locked Connection UUID within the same tenant.
// querySSHHealthConnectionID разрешает UUID уже locked Connection в том же tenant.
//
//go:embed sql/ssh_health__connection_id.sql
var querySSHHealthConnectionID string
