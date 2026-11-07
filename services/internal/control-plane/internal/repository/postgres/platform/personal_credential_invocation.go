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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPersonalInvocationContext resolves stored root identity and locks its exact Connection.
// It is called only after normal invocation grant/runtime admission and does not replace it.
// queryPersonalInvocationContext разрешает сохранённую root identity и блокирует точный Connection.
// Вызывается после обычного grant/runtime admission invocation и не заменяет его.
//
//go:embed sql/personal_invocation__context.sql
var queryPersonalInvocationContext string

// queryPersonalInvocationPin appends an immutable personal descriptor lineage without reading bytes.
// Existing invocation pins cannot be changed by an idempotent intent replay or credential rotation.
// queryPersonalInvocationPin добавляет неизменяемую lineage личного descriptor без чтения байтов.
// Replay намерения и ротация credential не могут изменить прежнюю привязку invocation.
//
//go:embed sql/personal_invocation__pin.sql
var queryPersonalInvocationPin string

// personalInvocationCredentialContext contains owner-resolved coordinates only, never prompt fields.
// Its policy and configuration belong to the locked Connection; subject belongs to the stored root.
// personalInvocationCredentialContext содержит лишь разрешённые владельцем координаты, не prompt.
// Policy/configuration принадлежат блокированному Connection, subject — сохранённому корню.
type personalInvocationCredentialContext struct {
	invocationID, connectionID, connectionRef, definitionKey, definitionVersion, definitionDigest string
	rootID, subjectID, subjectKind, source, capabilityKey                                         string
	policy                                                                                        entity.IntegrationCredentialPolicy
	configuration                                                                                 []byte
	ready, visible                                                                                bool
}

// personalInvocationCredentialChoice keeps a freshly locked owner selection inside its transaction.
// It cannot be serialized as worker authority; begin must compare it with the immutable intent pin.
// personalInvocationCredentialChoice хранит свежий заблокированный owner selection внутри транзакции.
// Она не сериализуется как authority worker; begin сверяет её с неизменяемым pin намерения.
type personalInvocationCredentialChoice struct {
	context                        personalInvocationCredentialContext
	bindingID, configurationDigest string
	selection                      entity.IntegrationCredentialSelection
}

// pinPersonalInvocationCredentialTx fixes the credential of a newly created personal intent.
// The caller already owns invocation admission and calls this only for its first insert; replay
// retains the prior pin. Connection→binding locks match configure/revoke. This pin is not call-start
// authority and exposes no secret to a worker: a separate one-time admission remains required.
// pinPersonalInvocationCredentialTx закрепляет credential нового личного намерения invocation.
// Caller уже выполнил invocation admission и вызывает метод лишь для первой вставки; replay
// сохраняет прежний pin. Порядок Connection→binding совпадает с configure/revoke. Pin не разрешает
// начало вызова и не выдаёт worker секрет: нужен отдельный одноразовый допуск непосредственно перед effect.
func (repository *Repository) pinPersonalInvocationCredentialTx(ctx context.Context, tx pgx.Tx, organizationID, invocationRef string) error {
	choice, err := repository.selectPersonalInvocationCredentialTx(ctx, tx, organizationID, invocationRef)
	if err != nil {
		return err
	}
	current := choice.context
	if current.policy == entity.IntegrationCredentialProjectShared {
		return nil
	}
	selection := choice.selection
	tag, err := tx.Exec(ctx, queryPersonalInvocationPin, pgx.StrictNamedArgs{
		"invocation_id": current.invocationID, "organization_id": organizationID,
		"connection_id": current.connectionID, "root_run_id": current.rootID, "subject_id": current.subjectID,
		"binding_id": choice.bindingID, "binding_ref": selection.PersonalBindingRef, "binding_generation": selection.BindingGeneration,
		"credential_ref": selection.Credential.Ref, "definition_digest": current.definitionDigest, "configuration_digest": choice.configurationDigest,
		"resource_permission_profile": selection.ResourcePermissionProfile,
	})
	if err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// selectPersonalInvocationCredentialTx rechecks stored root identity and current personal eligibility.
// Connection→binding locks remain held by the caller through pin creation or one-time call admission.
// Shared policy returns no personal selection and never reads a personal binding.
// selectPersonalInvocationCredentialTx повторно проверяет сохранённую root identity и личную eligibility.
// Caller удерживает Connection→binding locks до создания pin либо одноразового допуска вызова.
// Shared policy не выбирает и не читает личную привязку.
func (repository *Repository) selectPersonalInvocationCredentialTx(ctx context.Context, tx pgx.Tx, organizationID, invocationRef string) (*personalInvocationCredentialChoice, error) {
	var current personalInvocationCredentialContext
	err := tx.QueryRow(ctx, queryPersonalInvocationContext, pgx.StrictNamedArgs{
		"organization_id": organizationID, "invocation_ref": invocationRef,
	}).Scan(&current.invocationID, &current.connectionID, &current.connectionRef, &current.policy,
		&current.definitionKey, &current.definitionVersion, &current.definitionDigest, &current.configuration,
		&current.capabilityKey, &current.rootID, &current.subjectID, &current.subjectKind, &current.source,
		&current.ready, &current.visible)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrForbidden
	}
	if err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if current.policy == entity.IntegrationCredentialProjectShared {
		return &personalInvocationCredentialChoice{context: current}, nil
	}
	if current.policy != entity.IntegrationCredentialInitiatorRequired || !current.ready || !current.visible {
		return nil, errs.ErrForbidden
	}
	if current.subjectKind != "USER" || current.source == "SCHEDULE" || current.source == "INTEGRATION" {
		return nil, personalInvocationCredentialProblem(current, entity.IntegrationCredentialInitiatorMissing)
	}
	definition, err := repository.integrationPackage(ctx, tx, organizationID, current.connectionRef,
		current.definitionKey, current.definitionVersion, current.definitionDigest)
	if err != nil {
		return nil, err
	}
	capability, exists := definition.Capability(current.capabilityKey)
	if !exists || !definition.RequiresConnectionCredential() || capability.Operation == "mattermost.inbound" || capability.Operation == "mattermost.gate_decisions" {
		return nil, errs.ErrForbidden
	}
	var configuration map[string]any
	if json.Unmarshal(current.configuration, &configuration) != nil {
		return nil, errs.ErrUnavailable
	}
	strings, valid := integrationStringConfiguration(configuration)
	if !valid || definition.ValidateConfiguration(strings) != nil {
		return nil, errs.ErrForbidden
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	digest := sha256.Sum256(encoded)
	configurationDigest := hex.EncodeToString(digest[:])
	arguments := pgx.StrictNamedArgs{"organization_id": organizationID, "subject_id": current.subjectID, "connection_ref": current.connectionRef}
	var bindingID string
	var lockedPolicy entity.IntegrationCredentialPolicy
	var connectionVersion int64
	err = tx.QueryRow(ctx, queryPersonalCredentialLockOwned, arguments).Scan(&bindingID, &lockedPolicy, &connectionVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, personalInvocationCredentialProblem(current, entity.IntegrationCredentialMissing)
	}
	if err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if lockedPolicy != entity.IntegrationCredentialInitiatorRequired || connectionVersion < 1 {
		return nil, errs.ErrForbidden
	}
	binding, err := readPersonalCredentialTx(ctx, tx, scope{organizationID: organizationID, actorID: current.subjectID}, current.connectionRef)
	if err != nil {
		return nil, err
	}
	selection, err := platformservice.ResolveIntegrationCredential(entity.IntegrationCredentialRequirement{
		ConnectionRef: current.connectionRef, CapabilityKey: current.capabilityKey,
		DefinitionDigest: current.definitionDigest, ConfigurationDigest: configurationDigest,
		Policy: current.policy, CredentialRequired: true,
		DefinitionKey: definition.Metadata.Key, ResourcePermissionProfile: integrationpackage.CredentialResourcePermissionProfile(definition, capability),
	}, entity.IntegrationCredentialInitiator{OrganizationID: organizationID, SubjectID: current.subjectID, SubjectKind: current.subjectKind}, nil, &binding, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if selection.Problem != "" {
		return nil, personalInvocationCredentialProblem(current, selection.Problem)
	}
	if selection.Credential == nil || selection.PersonalBindingRef == "" || selection.BindingGeneration < 1 {
		return nil, errs.ErrUnavailable
	}
	return &personalInvocationCredentialChoice{context: current, bindingID: bindingID, configurationDigest: configurationDigest, selection: selection}, nil
}

// personalInvocationCredentialProblem emits only a closed owner-selected requirement diagnostic.
// It carries no subject, root, binding, Secret descriptor or provider text across the transport.
// personalInvocationCredentialProblem выдаёт лишь закрытую диагностику требования от владельца.
// Subject, root, binding, Secret descriptor и текст провайдера не передаются через transport.
func personalInvocationCredentialProblem(current personalInvocationCredentialContext, problem entity.IntegrationCredentialProblem) error {
	return &platformservice.IntegrationCredentialPreflightError{Issues: []entity.IntegrationCredentialPreflightIssue{
		{ConnectionRef: current.connectionRef, CapabilityKey: current.capabilityKey, Problem: problem},
	}}
}
