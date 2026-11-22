package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesSetupPrivatePermit derives original tenant/actor from a stored permit-bound setup.
// It keeps inactive owners readable only for evidence recovery, never for issuing execution credit.
// queryKubernetesSetupPrivatePermit выводит original tenant/actor из stored permit-bound setup.
// Inactive owners доступны только для evidence recovery, не для выдачи execution credit.
//
//go:embed sql/kubernetes_setup__private_permit.sql
var queryKubernetesSetupPrivatePermit string

// queryKubernetesSetupMaterialRead reads immutable purpose pins without reading credential material.
// queryKubernetesSetupMaterialRead читает immutable purpose pins без credential material.
//
//go:embed sql/kubernetes_setup__material_read.sql
var queryKubernetesSetupMaterialRead string

// kubernetesUploadRow separates immutable original input from freshly resolved authorization.
// Its tenant and actor are read from owner rows; the broker's service identity cannot replace them.
// kubernetesUploadRow отделяет immutable original input от freshly resolved authorization.
// Tenant и actor берутся из owner rows; broker service identity не может их заменить.
type kubernetesUploadRow struct {
	input                                                           kubernetesSetupImmutableInput
	owner                                                           integrationBrokerIdentityWriteOwner
	id, organizationRef, actorRef, connectionRef, digest, lifecycle string
	state                                                           enum.KubernetesIdentitySetupState
	version                                                         int64
	now                                                             time.Time
	actorActive                                                     bool
}

// scanKubernetesUploadRow rejects corrupt immutable metadata before it can select private material.
// scanKubernetesUploadRow отклоняет corrupt immutable metadata до выбора private material.
func scanKubernetesUploadRow(row pgx.Row) (kubernetesUploadRow, error) {
	var result kubernetesUploadRow
	input, owner := &result.input, &result.owner
	err := row.Scan(&result.id, &input.OrganizationID, &result.organizationRef, &result.actorRef, &result.connectionRef,
		&input.IdentityID, &input.IdentityRef, &owner.subjectID, &owner.bindingID, &owner.bindingRef,
		&input.RegistryRevision, &input.TargetRef, &input.TargetRevision, &input.ConnectionVersion,
		&input.BindingVersion, &input.BindingGeneration, &input.DefinitionDigest, &input.ConfigurationDigest,
		&result.digest, &input.ContentDigest, &input.ContentBytes, &input.ExpiresAt, &result.now,
		&input.SetupRef, &input.ConnectionID, &input.IdentityRevision, &input.Policy, &result.state, &result.version,
		&input.ActorID, &result.actorActive, &result.lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	input.ExpiresAt, result.now = input.ExpiresAt.UTC(), result.now.UTC()
	owner.organizationID, owner.connectionID, owner.connectionRef = input.OrganizationID, input.ConnectionID, result.connectionRef
	owner.connectionVersion, owner.bindingVersion, owner.bindingGeneration = input.ConnectionVersion, input.BindingVersion, input.BindingGeneration
	owner.policy, owner.purpose, owner.targetRef = input.Policy, entity.IntegrationBrokerIdentityKubernetes, input.TargetRef
	canonical, err := json.Marshal(input)
	hash := sha256.Sum256(canonical)
	if err != nil || !owner.valid() || !managedkubernetes.ValidLocator(input.SetupRef) || !strings.HasPrefix(input.SetupRef, "ksu_") ||
		!managedkubernetes.ValidLocator(input.IdentityRef) || input.IdentityRevision < 1 || input.IdentityRevision > 9007199254740991 ||
		input.RegistryRevision < 1 || input.TargetRevision < 1 || input.ExpiresAt.IsZero() || result.now.IsZero() ||
		input.ContentBytes == 0 || input.ContentBytes > managedkubernetes.MaximumCredentialBytes ||
		!managedkubernetes.ValidDigest(input.DefinitionDigest) || !managedkubernetes.ValidDigest(input.ConfigurationDigest) ||
		!managedkubernetes.ValidDigest(input.ContentDigest) || result.digest != hex.EncodeToString(hash[:]) ||
		!result.state.Valid() || result.version < 1 ||
		(input.Policy == entity.IntegrationCredentialInitiatorRequired && input.ActorID != owner.subjectID) {
		return kubernetesUploadRow{}, errs.ErrUnavailable
	}
	return result, nil
}

// String prevents private original coordinates and content digests from entering diagnostics.
// String предотвращает попадание private original coordinates и content digests в диагностику.
func (kubernetesUploadRow) String() string { return "[Kubernetes private setup owner row]" }

// GoString applies the same redaction to detailed formatting of the private owner row.
// GoString сохраняет redaction при подробном форматировании private owner row.
func (row kubernetesUploadRow) GoString() string { return row.String() }

// kubernetesUploadActorScopeTx resolves the original actor through the canonical current scope query.
// No tenant or subject is accepted from the broker's payload or inherited from its system identity.
// kubernetesUploadActorScopeTx разрешает original actor через canonical current scope query.
// Tenant и subject не берутся из broker payload и не наследуются от system identity broker.
func kubernetesUploadActorScopeTx(ctx context.Context, tx pgx.Tx, row kubernetesUploadRow, correlation string) (scope, error) {
	var current scope
	if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive,
		row.actorRef, row.organizationRef).Scan(&current.organizationID, &current.organizationRef,
		&current.actorID, &current.actorRef, &current.actorName, &current.role); errors.Is(err, pgx.ErrNoRows) {
		return current, errs.ErrForbidden
	} else if err != nil {
		return current, errs.ErrUnavailable
	}
	if current.organizationID != row.input.OrganizationID || current.actorID != row.input.ActorID || !row.actorActive {
		return scope{}, errs.ErrForbidden
	}
	current.correlationRef = correlation
	return current, nil
}

// validateKubernetesUploadOwnerTx rechecks current owner, exact package/config and selected SERVICE.
// Callers retain root-source then Connection locks before entering the binding/subject boundary.
// validateKubernetesUploadOwnerTx заново проверяет current owner, exact package/config и selected SERVICE.
// Caller удерживает root-source, затем Connection locks до binding/subject boundary.
func (repository *Repository) validateKubernetesUploadOwnerTx(ctx context.Context, tx pgx.Tx, row kubernetesUploadRow, source kubernetesOperatorStored, correlation string) (scope, value.ManagedKubernetesTarget, time.Time, error) {
	current, err := kubernetesUploadActorScopeTx(ctx, tx, row, correlation)
	if err != nil {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, err
	}
	viewUntil, err := repository.requireAccessUntil(ctx, tx, current, "integration.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: row.connectionRef,
	})
	if err != nil {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, row.connectionRef)
	if err != nil {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, err
	}
	if connection.version != row.input.ConnectionVersion || connection.definitionDigest != row.input.DefinitionDigest ||
		connection.credentialPolicy != row.input.Policy || row.lifecycle != "ACTIVE" ||
		source.registry.Revision != row.input.RegistryRevision || repository.kubernetesOperator == nil || repository.kubernetesOperatorDigest != source.digest {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, row.connectionRef,
		connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
	if err != nil {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, err
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid || definition.Spec.Adapter != "KUBERNETES" || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != string(row.owner.purpose) {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrConflict
	}
	var raw []byte
	var now time.Time
	if tx.QueryRow(ctx, queryKubernetesSetupBeginContext, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_id": connection.id,
	}).Scan(&raw, &now) != nil || len(raw) > 16384 {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrUnavailable
	}
	var configuration map[string]string
	if json.Unmarshal(raw, &configuration) != nil || definition.ValidateConfiguration(configuration) != nil {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrConflict
	}
	canonical, err := json.Marshal(configuration)
	hash := sha256.Sum256(canonical)
	if err != nil || hex.EncodeToString(hash[:]) != row.input.ConfigurationDigest || !now.Before(row.input.ExpiresAt) {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrConflict
	}
	owner, sharedUntil, err := repository.beginKubernetesSetupOwnerTx(ctx, tx, current, connection, row.connectionRef, row.input.BindingVersion, configuration)
	if err != nil {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, err
	}
	if owner != row.owner {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrConflict
	}
	accessUntil := earlierAccessDeadline(viewUntil, sharedUntil)
	if accessDeadlineExpired(accessUntil) {
		return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrForbidden
	}
	for _, entry := range source.registry.Targets {
		if entry.Target.Ref == row.input.TargetRef && entry.Target.Revision == row.input.TargetRevision {
			return current, entry.Target, accessUntil, nil
		}
	}
	return current, value.ManagedKubernetesTarget{}, time.Time{}, errs.ErrConflict
}

// kubernetesKnownMaterialTx returns only exact immutable private pins after the current owner gate.
// Once a UID is known, subsequent observation cannot silently adopt an object replacement.
// kubernetesKnownMaterialTx возвращает exact immutable private pins после current owner gate.
// После первого known UID observation не может незаметно принять replacement объекта.
func kubernetesKnownMaterialTx(ctx context.Context, tx pgx.Tx, row kubernetesUploadRow, namespace string) (*value.ManagedKubernetesIdentityLocator, error) {
	result := value.ManagedKubernetesIdentityLocator{IdentityRef: row.input.IdentityRef, TargetRef: row.input.TargetRef, Revision: row.input.IdentityRevision}
	err := tx.QueryRow(ctx, queryKubernetesSetupMaterialRead, pgx.StrictNamedArgs{
		"organization_id": row.input.OrganizationID, "identity_id": row.input.IdentityID,
	}).Scan(&result.Namespace, &result.Name, &result.UID, &result.ResourceVersion, &result.ContentSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || result.Validate(namespace) != nil || result.ContentSHA256 != row.input.ContentDigest {
		return nil, errs.ErrUnavailable
	}
	return &result, nil
}

// ResolveKubernetesIdentityUpload verifies the technical broker and original current owner separately.
// A valid permit resolves metadata only; a distinct durable Claim must precede storage effects.
// ResolveKubernetesIdentityUpload отдельно проверяет technical broker и original current owner.
// Valid permit разрешает только metadata; storage effects требуют отдельного durable Claim.
func (repository *Repository) ResolveKubernetesIdentityUpload(ctx context.Context, principal value.Principal, input command.KubernetesIdentityUploadResolve) (result entity.KubernetesIdentityUploadSnapshot, resultErr error) {
	if err := kubernetesSetupBrokerCaller(principal, "platform.runtime.kubernetes.setup.resolve"); err != nil {
		return result, err
	}
	if !managedkubernetes.ValidLocator(input.SetupRef) || !strings.HasPrefix(input.SetupRef, "ksu_") || len(input.SetupRef) > 96 || !managedkubernetes.ValidDigest(input.PermitSHA256) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	source, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	if err != nil {
		return result, err
	}
	row, err := scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, pgx.StrictNamedArgs{
		"setup_ref": input.SetupRef, "permit_digest": input.PermitSHA256,
	}))
	if err != nil {
		return result, err
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, row.input.OrganizationID, row.connectionRef); err != nil {
		return result, err
	}
	row, err = scanKubernetesUploadRow(tx.QueryRow(ctx, queryKubernetesSetupPrivatePermit, pgx.StrictNamedArgs{
		"setup_ref": input.SetupRef, "permit_digest": input.PermitSHA256,
	}))
	if err != nil {
		return result, err
	}
	if row.state == enum.KubernetesIdentitySetupCompleted {
		current, err := kubernetesUploadActorScopeTx(ctx, tx, row, principal.CorrelationRef)
		if err != nil {
			return result, err
		}
		if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{
			Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: row.connectionRef,
		}); err != nil {
			return result, concealAccessDenial(err, errs.ErrNotFound)
		}
		view, err := readKubernetesSetupView(tx.QueryRow(ctx, queryKubernetesSetupReadOwned, pgx.StrictNamedArgs{
			"organization_id": row.input.OrganizationID, "actor_id": row.input.ActorID,
			"connection_ref": row.connectionRef, "setup_ref": row.input.SetupRef, "begin_key": "",
		}))
		if err != nil {
			return result, err
		}
		if view.ReasonCode == kubernetesSetupAcknowledgementConflict || view.ReasonCode == kubernetesSetupAcknowledgementRejected {
			return result, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return entity.KubernetesIdentityUploadSnapshot{SetupRef: row.input.SetupRef, Digest: row.digest,
			ExpiresAt: row.input.ExpiresAt, ReconcileOnly: true, Completed: &view}, nil
	}
	if row.state.Terminal() || !row.now.Before(row.input.ExpiresAt) {
		return result, errs.ErrConflict
	}
	_, target, accessUntil, err := repository.validateKubernetesUploadOwnerTx(ctx, tx, row, source, principal.CorrelationRef)
	if err != nil {
		return result, err
	}
	known, err := kubernetesKnownMaterialTx(ctx, tx, row, source.registry.IdentityNamespace)
	if err != nil {
		return result, err
	}
	until := earlierAccessDeadline(row.input.ExpiresAt, accessUntil)
	if err := commitIdentitySetupAdmission(ctx, tx, accessUntil, until); err != nil {
		return result, err
	}
	return entity.KubernetesIdentityUploadSnapshot{SetupRef: row.input.SetupRef, Digest: row.digest, Target: target,
		Provision: value.ManagedKubernetesIdentityProvision{IdentityRef: row.input.IdentityRef, TargetRef: row.input.TargetRef,
			Namespace: source.registry.IdentityNamespace, Revision: row.input.IdentityRevision, ContentSHA256: row.input.ContentDigest},
		ContentBytes: row.input.ContentBytes, ExpiresAt: until.UTC(), ReconcileOnly: row.state != enum.KubernetesIdentitySetupPrepared,
		KnownIdentity: known}, nil
}

// kubernetesSetupBrokerCaller checks the exact verified workload before private owner lookup.
// It supplies no user privileges and accepts no project-delegated authority on the setup lane.
// kubernetesSetupBrokerCaller проверяет exact verified workload до private owner lookup.
// Не выдаёт user privileges и не принимает project-delegated authority в setup lane.
func kubernetesSetupBrokerCaller(principal value.Principal, permission string) error {
	if principal.Validate() != nil || principal.CallerWorkload != "secret-broker" || principal.Permission != permission || principal.ProjectRef != "" {
		return errs.ErrForbidden
	}
	return nil
}
