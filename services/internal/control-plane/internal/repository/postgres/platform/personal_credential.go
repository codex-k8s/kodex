package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// GetPersonalIntegrationCredential reads only the verified principal's binding under current
// Connection visibility; it never accepts another subject selector or reads plaintext material.
// GetPersonalIntegrationCredential читает только привязку проверенного principal с текущей
// видимостью Connection; selector другого subject и чтение plaintext не поддерживаются.
func (repository *Repository) GetPersonalIntegrationCredential(ctx context.Context, principal value.Principal, connectionRef string) (entity.PersonalIntegrationCredential, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}); err != nil {
		return entity.PersonalIntegrationCredential{}, errs.ErrNotFound
	}
	binding, err := readPersonalCredentialTx(ctx, tx, current, connectionRef)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	return binding, nil
}

// ReadPersonalIntegrationCredentialReceipt reconciles only the self actor's exact configure intent
// after current authority checks. A mismatch is not a missing receipt and never permits a new write.
// ReadPersonalIntegrationCredentialReceipt сверяет только точное configure intent собственного actor
// после текущих authority checks. Несовпадение не означает отсутствие receipt и не разрешает новую запись.
func (repository *Repository) ReadPersonalIntegrationCredentialReceipt(ctx context.Context, principal value.Principal, mutation value.Mutation, payload command.PersonalIntegrationCredentialInput) (entity.PersonalIntegrationCredential, bool, error) {
	if len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 {
		return entity.PersonalIntegrationCredential{}, false, errs.ErrInvalid
	}
	intentDigest, err := platformservice.PersonalIntegrationCredentialConfigureIntentDigest(payload)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, false, err
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, false, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.PersonalIntegrationCredential{}, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	input := command.Command{Kind: command.ConfigurePersonalIntegrationCredential, Principal: principal, Mutation: mutation, Payload: payload}
	if err := repository.authorizePersonalCredentialCommand(ctx, tx, current, input); err != nil {
		return entity.PersonalIntegrationCredential{}, false, err
	}
	operation := "controlplane." + strings.ToLower(string(input.Kind))
	var storedDigest string
	var raw []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, operation, mutation.IdempotencyKey).Scan(&storedDigest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.PersonalIntegrationCredential{}, false, nil
	}
	if err != nil {
		return entity.PersonalIntegrationCredential{}, false, errs.ErrUnavailable
	}
	if storedDigest != intentDigest {
		return entity.PersonalIntegrationCredential{}, false, errs.ErrIdempotencyReuse
	}
	var result command.Result
	if json.Unmarshal(raw, &result) != nil || result.PersonalIntegrationCredential == nil ||
		result.PersonalIntegrationCredential.OrganizationID != current.organizationID ||
		result.PersonalIntegrationCredential.SubjectID != current.actorID || result.PersonalIntegrationCredential.ConnectionRef != payload.ConnectionRef {
		return entity.PersonalIntegrationCredential{}, false, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.PersonalIntegrationCredential{}, false, errs.ErrUnavailable
	}
	return *result.PersonalIntegrationCredential, true, nil
}

// readPersonalCredentialTx maps one owner-qualified metadata row and refuses a partial descriptor.
// Callers enforce current Connection access before this addressed read or hold the same owner locks.
// readPersonalCredentialTx преобразует одну owner-qualified metadata row и отклоняет частичный descriptor.
// Вызывающий заранее проверяет текущий доступ к Connection либо удерживает те же owner locks.
func readPersonalCredentialTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) (entity.PersonalIntegrationCredential, error) {
	var row personalCredentialRow
	err := tx.QueryRow(ctx, queryPersonalCredentialReadOwned, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "subject_id": current.actorID, "connection_ref": connectionRef,
	}).Scan(row.targets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.PersonalIntegrationCredential{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	return row.decode()
}

// personalCredentialRow shares the exact descriptor projection between addressed and catalog
// reads. It holds metadata only and never supplies an alternate owner selector or a plaintext read.
// personalCredentialRow объединяет точную descriptor-проекцию адресного и каталожного чтения.
// Содержит только metadata, не вводит иной owner selector либо чтение plaintext.
type personalCredentialRow struct {
	value           entity.PersonalIntegrationCredential
	revision        entity.IntegrationCredentialRevision
	createdAt       *time.Time
	githubDiscovery []byte
}

// targets returns stable scan addresses in the same order as the two named SQL projections.
// targets возвращает устойчивые scan-адреса в порядке двух именованных SQL-проекций.
func (row *personalCredentialRow) targets() []any {
	binding, credential := &row.value, &row.revision
	return []any{&binding.Ref, &binding.OrganizationID, &binding.SubjectID, &binding.ConnectionRef, &binding.CredentialPolicy, &binding.ConnectionVersion,
		&binding.Version, &binding.Generation, &binding.State, &credential.Ref, &credential.Revision,
		&credential.SecretRef, &credential.SecretUID, &credential.SecretResourceVersion, &credential.ContentSHA256, &row.createdAt,
		&binding.VerifiedCredentialRef, &binding.VerifiedDefinitionDigest, &binding.VerifiedConfigurationDigest,
		&binding.VerifiedCapabilities, &binding.DeniedCapabilities, &binding.ObservedPermissionProfiles, &binding.ExpiresAt, &row.githubDiscovery}
}

// decode rejects partial or malformed immutable descriptors before attaching metadata to the
// internal owner result. A missing descriptor is allowed only when every descriptor field is absent.
// decode отклоняет частичные либо неверные immutable descriptors до добавления metadata во внутренний
// результат владельца. Отсутствие descriptor допустимо лишь при отсутствии всех его полей.
func (row *personalCredentialRow) decode() (entity.PersonalIntegrationCredential, error) {
	binding, credential := row.value, row.revision
	discovery, parseErr := integrationpackage.ParseGitHubDiscoveryPage(row.githubDiscovery)
	if parseErr != nil {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	binding.GitHubDiscovery = discovery
	if credential.Ref != "" {
		if row.createdAt == nil || !credential.ValidDescriptor() || credential.Revision < 1 {
			return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
		}
		credential.CreatedAt = *row.createdAt
		binding.Credential = &credential
	} else if row.createdAt != nil || credential.Revision != 0 || credential.SecretRef != "" || credential.SecretUID != "" ||
		credential.SecretResourceVersion != "" || credential.ContentSHA256 != "" {
		return entity.PersonalIntegrationCredential{}, errs.ErrUnavailable
	}
	return binding, nil
}

// personalCredentialCommand identifies only the three dedicated self-binding commands.
// personalCredentialCommand определяет только три специализированные команды собственной привязки.
func personalCredentialCommand(kind command.Kind) bool {
	return kind == command.TestPersonalIntegrationCredential || kind == command.CreatePersonalIntegrationCredential || kind == command.ConfigurePersonalIntegrationCredential ||
		kind == command.RevokePersonalIntegrationCredential
}

// authorizePersonalCredentialCommand checks exact browser workload/operation, active USER identity
// and current Connection visibility before generic idempotency receipt replay or version checks.
// authorizePersonalCredentialCommand проверяет точные browser workload/operation, активную USER identity
// и текущую видимость Connection до generic replay receipt либо проверки версии.
func (repository *Repository) authorizePersonalCredentialCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.PersonalIntegrationCredentialInput)
	if !ok || payload.ConnectionRef == "" || len(payload.ConnectionRef) > 96 {
		return errs.ErrInvalid
	}
	operation := personalCredentialCommandOperation(input.Kind)
	if operation == "" || input.Principal.CallerWorkload != "control-api-gateway" || input.Principal.Permission != operation {
		return errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: payload.ConnectionRef}); err != nil {
		return errs.ErrNotFound
	}
	var connectionID string
	var policy entity.IntegrationCredentialPolicy
	var version int64
	err := tx.QueryRow(ctx, queryPersonalCredentialConnection, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "subject_id": current.actorID, "connection_ref": payload.ConnectionRef,
	}).Scan(&connectionID, &policy, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if input.Kind != command.RevokePersonalIntegrationCredential && policy != entity.IntegrationCredentialInitiatorRequired {
		return errs.ErrConflict
	}
	return nil
}

// personalCredentialCommandOperation maps dedicated self commands to their exact proof operations.
// It provides no alternate route through generic Connection management or project administration.
// personalCredentialCommandOperation сопоставляет собственные команды точным proof operations.
// Альтернативного пути через generic управление Connection или администратора проекта нет.
func personalCredentialCommandOperation(kind command.Kind) string {
	switch kind {
	case command.TestPersonalIntegrationCredential:
		return "platform.personal-integrations.test"
	case command.CreatePersonalIntegrationCredential:
		return "platform.personal-integrations.create"
	case command.ConfigurePersonalIntegrationCredential:
		return "platform.personal-integrations.configure"
	case command.RevokePersonalIntegrationCredential:
		return "platform.personal-integrations.revoke"
	default:
		return ""
	}
}

// changePersonalCredential runs only inside Execute's owner/idempotency transaction. All reads,
// revision insertion, fence advance, immutable command result and audit share that transaction.
// changePersonalCredential работает только внутри owner/idempotency transaction Execute. Чтения,
// добавление ревизии, ограждение, immutable результат команды и audit принадлежат одной транзакции.
func (repository *Repository) changePersonalCredential(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.PersonalIntegrationCredentialInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_id": current.actorID, "connection_ref": payload.ConnectionRef}
	if input.Kind == command.CreatePersonalIntegrationCredential {
		if input.Mutation.ExpectedVersion != nil || payload.ExpectedConnectionVersion < 1 || payload.Credential != nil || payload.MaterializationRef != "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, err := newRef("ipc")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var createdRef string
		err = tx.QueryRow(ctx, queryPersonalCredentialCreate, pgx.StrictNamedArgs{
			"ref": ref, "organization_id": current.organizationID, "subject_id": current.actorID,
			"connection_ref": payload.ConnectionRef, "connection_version": payload.ExpectedConnectionVersion,
		}).Scan(&createdRef)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		binding, err := readPersonalCredentialTx(ctx, tx, current, payload.ConnectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return personalCredentialOutcome(binding, "i18n:PERSONAL_INTEGRATION_CREDENTIAL_CREATED"), nil
	}
	if input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var bindingID string
	var policy entity.IntegrationCredentialPolicy
	var connectionVersion int64
	err := tx.QueryRow(ctx, queryPersonalCredentialLockOwned, args).Scan(&bindingID, &policy, &connectionVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	binding, err := readPersonalCredentialTx(ctx, tx, current, payload.ConnectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	initiator := entity.IntegrationCredentialInitiator{OrganizationID: current.organizationID, SubjectID: current.actorID, SubjectKind: "USER"}
	if input.Kind == command.TestPersonalIntegrationCredential {
		return repository.queuePersonalCredentialTest(ctx, tx, current, input, bindingID, binding)
	}
	if input.Kind == command.RevokePersonalIntegrationCredential {
		if payload.Credential != nil || payload.MaterializationRef != "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		revoked, err := platformservice.RevokePersonalIntegrationCredential(binding, initiator, *input.Mutation.ExpectedVersion)
		if err != nil {
			return commandOutcome{}, err
		}
		if revoked.Version != binding.Version {
			if err := repository.cancelRepositoryBundleDependency(ctx, tx, current, "PERSONAL_CREDENTIAL", binding.Ref); err != nil {
				return commandOutcome{}, err
			}
			if err := cancelPersonalCredentialTests(ctx, tx, current.organizationID, bindingID); err != nil {
				return commandOutcome{}, err
			}
			tag, err := tx.Exec(ctx, queryPersonalCredentialRevoke, pgx.StrictNamedArgs{
				"organization_id": current.organizationID, "subject_id": current.actorID, "binding_id": bindingID,
				"version": revoked.Version, "generation": revoked.Generation, "expected_version": binding.Version,
			})
			if err != nil || tag.RowsAffected() != 1 {
				return commandOutcome{}, errs.ErrConflict
			}
		}
		return personalCredentialOutcome(revoked, "i18n:PERSONAL_INTEGRATION_CREDENTIAL_REVOKED"), nil
	}
	if input.Kind != command.ConfigurePersonalIntegrationCredential || policy != entity.IntegrationCredentialInitiatorRequired ||
		connectionVersion != payload.ExpectedConnectionVersion || binding.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err := repository.cancelRepositoryBundleDependency(ctx, tx, current, "PERSONAL_CREDENTIAL", binding.Ref); err != nil {
		return commandOutcome{}, err
	}
	if err := cancelPersonalCredentialTests(ctx, tx, current.organizationID, bindingID); err != nil {
		return commandOutcome{}, err
	}
	if !validIntegrationCredentialInput(true, payload.Credential) {
		return commandOutcome{}, errs.ErrInvalid
	}
	expectedMaterialization, err := platformservice.PersonalIntegrationCredentialMaterializationRef(initiator, payload.ConnectionRef, input.Mutation.IdempotencyKey)
	if err != nil || payload.MaterializationRef != expectedMaterialization ||
		payload.Credential.SecretRef != "kodex-system/kodex-integration-credentials#"+expectedMaterialization {
		return commandOutcome{}, errs.ErrInvalid
	}
	ref, err := newRef("ipcr")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var revisionID string
	var stored entity.IntegrationCredentialRevision
	credential := payload.Credential
	err = tx.QueryRow(ctx, queryPersonalCredentialInsertRevision, pgx.StrictNamedArgs{
		"ref": ref, "organization_id": current.organizationID, "binding_id": bindingID, "secret_ref": credential.SecretRef,
		"secret_uid": credential.SecretUID, "secret_resource_version": credential.SecretResourceVersion, "content_sha256": credential.ContentSHA256,
	}).Scan(&revisionID, &stored.Ref, &stored.Revision, &stored.SecretRef, &stored.SecretUID, &stored.SecretResourceVersion, &stored.ContentSHA256, &stored.CreatedAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	replacement, err := platformservice.ReplacePersonalIntegrationCredential(binding, initiator, *input.Mutation.ExpectedVersion, stored)
	if err != nil {
		return commandOutcome{}, err
	}
	tag, err := tx.Exec(ctx, queryPersonalCredentialReplace, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "subject_id": current.actorID, "binding_id": bindingID,
		"revision_id": revisionID, "version": replacement.Version, "generation": replacement.Generation, "expected_version": binding.Version,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	return personalCredentialOutcome(replacement, "i18n:PERSONAL_INTEGRATION_CREDENTIAL_CONFIGURED"), nil
}

// personalCredentialOutcome emits an internal receipt and audit target without a platform-wide
// event: this private self state has an authoritative addressed reader and no other-user consumer.
// personalCredentialOutcome формирует внутренний receipt и audit target без общего platform event:
// у собственного закрытого состояния есть адресное чтение, consumers другого пользователя нет.
func personalCredentialOutcome(binding entity.PersonalIntegrationCredential, summary string) commandOutcome {
	return commandOutcome{result: command.Result{PersonalIntegrationCredential: &binding},
		resourceKind: "PERSONAL_INTEGRATION_CREDENTIAL", resourceRef: binding.Ref, summary: summary}
}
