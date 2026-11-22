package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// sshSetupBeginOperation fixes the technical route and audit action independently from any caller-provided mutation operation.
// sshSetupBeginOperation фиксирует technical route и audit action независимо от caller-provided mutation operation.
const sshSetupBeginOperation = "platform.integrations.ssh.setup.begin"

// querySSHSetupBeginReceipt reads one original actor-scoped intent before OCC and never returns the raw upload permit.
// querySSHSetupBeginReceipt читает original actor-scoped intent до OCC и не возвращает raw upload permit.
//
//go:embed sql/ssh_setup__begin_receipt.sql
var querySSHSetupBeginReceipt string

// querySSHSetupSerializeActor serializes Begin idempotency across this actor's Connections before acquiring the Connection and binding locks.
// querySSHSetupSerializeActor сериализует Begin idempotency между Connections одного actor до Connection и binding locks.
//
//go:embed sql/ssh_setup__serialize_actor.sql
var querySSHSetupSerializeActor string

// querySSHSetupCapacity bounds active or unresolved material obligations without interpreting expiry or a missing acknowledgement as absence.
// querySSHSetupCapacity ограничивает active либо unresolved material obligations без трактовки expiry либо missing acknowledgement как absence.
//
//go:embed sql/ssh_setup__capacity.sql
var querySSHSetupCapacity string

// querySSHSetupInsert stores owner-assigned immutable coordinates and a permit hash before any broker storage effect can be claimed.
// querySSHSetupInsert сохраняет owner-assigned immutable coordinates и permit hash до claim любого broker storage effect.
//
//go:embed sql/ssh_setup__insert.sql
var querySSHSetupInsert string

// querySSHSetupReplacePending closes previous grants of this exact selected credential owner before a new revision is admitted; late completion evidence is retained and never interpreted as joined remote work.
// querySSHSetupReplacePending закрывает прежние grants exact selected credential owner до допуска новой revision; late completion evidence сохраняется без утверждения joined remote work.
//
//go:embed sql/ssh_setup__replace_pending.sql
var querySSHSetupReplacePending string

// validSSHSetupKey checks the exact bounded idempotency coordinate without normalization or stripping control characters.
// validSSHSetupKey проверяет exact bounded idempotency coordinate без normalization либо удаления control characters.
func validSSHSetupKey(key string) bool {
	return len(key) >= 8 && len(key) <= 128 && utf8.ValidString(key) && strings.IndexFunc(key, unicode.IsControl) < 0
}

// BeginSSHIdentitySetup allocates one immutable revision and intent in the owner transaction; replay reconciles metadata only and can never mint or reveal another raw permit.
// BeginSSHIdentitySetup выделяет immutable revision и intent в owner transaction; replay сверяет лишь metadata без выпуска либо раскрытия другого raw permit.
func (repository *Repository) BeginSSHIdentitySetup(ctx context.Context, principal value.Principal, mutation value.Mutation, input command.SSHIdentitySetupBegin) (result entity.SSHIdentitySetupAdmission, resultErr error) {
	if err := sshSetupAPICaller(principal, sshSetupBeginOperation); err != nil {
		return result, err
	}
	if !managedssh.OwnerReference(input.ConnectionRef) || input.ExpectedConnectionVersion < 1 || mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 0 || !validSSHSetupKey(mutation.IdempotencyKey) || !managedssh.Digest(input.ContentSHA256) || input.ContentBytes < 1 || input.ContentBytes > 16384 {
		return result, errs.ErrInvalid
	}
	body, err := json.Marshal(struct {
		Input           command.SSHIdentitySetupBegin
		ExpectedVersion int64
	}{input, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrInvalid
	}
	intent := sha256.Sum256(body)
	intentDigest := hex.EncodeToString(intent[:])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	viewUntil, err := repository.requireAccessUntil(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input.ConnectionRef})
	if err != nil {
		return result, sshSetupAccessError(err)
	}
	if _, err = tx.Exec(ctx, querySSHSetupSerializeActor, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID}); err != nil {
		return result, errs.ErrUnavailable
	}
	connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, input.ConnectionRef)
	if err != nil {
		return result, err
	}
	var setupRef, storedConnection, storedIntent string
	err = tx.QueryRow(ctx, querySSHSetupBeginReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "begin_key": mutation.IdempotencyKey}).Scan(&setupRef, &storedConnection, &storedIntent)
	if err == nil {
		if storedConnection != input.ConnectionRef || storedIntent != intentDigest {
			return result, errs.ErrIdempotencyReuse
		}
		result.View, err = readSSHSetupView(tx.QueryRow(ctx, querySSHSetupReadOwned, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef, "setup_ref": setupRef, "begin_key": ""}))
		if err != nil {
			return result, err
		}
		if err := commitIdentitySetupAdmission(ctx, tx, viewUntil, time.Time{}); err != nil {
			return entity.SSHIdentitySetupAdmission{}, err
		}
		result.ReconcileOnly = true
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if repository.sshIdentityUploadEndpoint == nil || !repository.sshIdentityUploadEndpoint.Valid() {
		return result, errs.ErrUnavailable
	}
	if connection.version != input.ExpectedConnectionVersion {
		return result, errs.ErrVersionMismatch
	}
	expectedBinding := *mutation.ExpectedVersion
	if connection.credentialPolicy == entity.IntegrationCredentialProjectShared {
		if expectedBinding != connection.version {
			return result, errs.ErrVersionMismatch
		}
		expectedBinding = 0
	}
	source, err := repository.sshSetupOwnerTx(ctx, tx, current, connection, input.ConnectionRef, expectedBinding)
	if err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, querySSHSetupReplacePending, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_id": connection.id, "subject_id": source.owner.subjectID, "policy": string(source.owner.policy)}); err != nil {
		return result, errs.ErrUnavailable
	}
	var capacity bool
	if tx.QueryRow(ctx, querySSHSetupCapacity, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID}).Scan(&capacity) != nil {
		return result, errs.ErrUnavailable
	}
	if !capacity {
		return result, errs.ErrConflict
	}
	identityID, identity, err := insertIntegrationBrokerIdentityRevisionTx(ctx, tx, source.owner)
	if err != nil {
		return result, err
	}
	setupRef, err = newRef("ssu")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	permit := make([]byte, 32)
	defer func() {
		if resultErr != nil {
			clear(permit)
		}
	}()
	if _, err = rand.Read(permit); err != nil {
		return result, errs.ErrUnavailable
	}
	permitHash := sha256.Sum256(permit)
	accessUntil := earlierAccessDeadline(viewUntil, source.accessUntil)
	expiresAt := earlierAccessDeadline(source.now.Add(90*time.Second), accessUntil).UTC()
	if !expiresAt.After(source.now) || accessDeadlineExpired(accessUntil) {
		return result, errs.ErrForbidden
	}
	snapshot := sshSetupImmutableInput{SetupRef: setupRef, OrganizationID: current.organizationID, ActorID: current.actorID, ConnectionID: connection.id, ConnectionRef: input.ConnectionRef, Namespace: sshIdentityMaterialNamespace, IdentityID: identityID, IdentityRef: identity.Ref,
		SubjectID: source.owner.subjectID, BindingID: source.owner.bindingID, BindingRef: source.owner.bindingRef, TargetRef: source.owner.targetRef, DefinitionDigest: source.definitionDigest, ConfigurationDigest: source.configurationDigest,
		IdentityRevision: identity.Revision, ConnectionVersion: source.owner.connectionVersion, BindingVersion: source.owner.bindingVersion, BindingGeneration: source.owner.bindingGeneration, Policy: source.owner.policy, ContentDigest: input.ContentSHA256, ContentBytes: input.ContentBytes, ExpiresAt: expiresAt}
	snapshotDigest, err := snapshot.digest()
	if err != nil {
		return result, err
	}
	snapshotBody, err := json.Marshal(snapshot)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, querySSHSetupInsert, pgx.StrictNamedArgs{"ref": setupRef, "organization_id": current.organizationID, "connection_id": connection.id, "actor_id": current.actorID, "identity_id": identityID, "material_namespace": snapshot.Namespace,
		"connection_version": snapshot.ConnectionVersion, "binding_version": snapshot.BindingVersion, "binding_generation": snapshot.BindingGeneration, "definition_digest": snapshot.DefinitionDigest, "configuration_digest": snapshot.ConfigurationDigest,
		"begin_key": mutation.IdempotencyKey, "intent_digest": intentDigest, "permit_digest": hex.EncodeToString(permitHash[:]), "snapshot_digest": snapshotDigest, "snapshot_body": string(snapshotBody), "content_digest": input.ContentSHA256, "content_bytes": input.ContentBytes, "created_at": source.now, "expires_at": snapshot.ExpiresAt}); err != nil {
		return result, mapWriteError(err)
	}
	result.View, err = readSSHSetupView(tx.QueryRow(ctx, querySSHSetupReadOwned, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef, "setup_ref": setupRef, "begin_key": ""}))
	if err != nil {
		return entity.SSHIdentitySetupAdmission{}, err
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.SSHIdentitySetupAdmission{}, errs.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil, current.actorID, sshSetupBeginOperation, "INTEGRATION_CONNECTION", input.ConnectionRef, "i18n:SSH_IDENTITY_SETUP_PREPARED", current.correlationRef); err != nil {
		return entity.SSHIdentitySetupAdmission{}, errs.ErrUnavailable
	}
	if err := commitIdentitySetupAdmission(ctx, tx, accessUntil, snapshot.ExpiresAt); err != nil {
		return entity.SSHIdentitySetupAdmission{}, err
	}
	result.Permit, result.Endpoint = permit, *repository.sshIdentityUploadEndpoint
	return result, nil
}
