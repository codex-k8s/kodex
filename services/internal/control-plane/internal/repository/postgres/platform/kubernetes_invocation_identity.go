package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesInvocationSharedIdentity requires the exact explicitly selected active SERVICE.
// queryKubernetesInvocationSharedIdentity требует exact явно выбранную active SERVICE.
//
//go:embed sql/kubernetes_invocation__shared_identity.sql
var queryKubernetesInvocationSharedIdentity string

// queryKubernetesInvocationPersonalIdentity matches current personal metadata without a Secret descriptor.
// queryKubernetesInvocationPersonalIdentity сверяет current personal metadata без Secret descriptor.
//
//go:embed sql/kubernetes_invocation__personal_identity.sql
var queryKubernetesInvocationPersonalIdentity string

// queryKubernetesInvocationHealth selects a completed actual identity observation, never a global RBAC proof.
// queryKubernetesInvocationHealth выбирает completed actual identity observation, не global RBAC proof.
//
//go:embed sql/kubernetes_invocation__health.sql
var queryKubernetesInvocationHealth string

// queryKubernetesInvocationPin binds the originating root to one immutable source in the intent transaction.
// queryKubernetesInvocationPin связывает originating root с immutable source в intent transaction.
//
//go:embed sql/kubernetes_invocation__pin.sql
var queryKubernetesInvocationPin string

// queryKubernetesInvocationConnectionKind reads only a tenant-owned discriminator before source locks.
// queryKubernetesInvocationConnectionKind читает только tenant-owned discriminator до source locks.
//
//go:embed sql/kubernetes_invocation__connection_kind.sql
var queryKubernetesInvocationConnectionKind string

// lockKubernetesInvocationSourceTx preserves source→Connection lock order before invocation resolution.
// The discriminator grants no access and cannot release registry metadata to its caller.
// lockKubernetesInvocationSourceTx сохраняет source→Connection lock order до invocation resolution.
// Discriminator не выдаёт access и не раскрывает registry metadata caller.
func lockKubernetesInvocationSourceTx(ctx context.Context, tx pgx.Tx, organizationID, connectionRef string) error {
	var selected bool
	if tx.QueryRow(ctx, queryKubernetesInvocationConnectionKind, pgx.StrictNamedArgs{"organization_id": organizationID, "connection_ref": connectionRef}).Scan(&selected) != nil {
		return errs.ErrUnavailable
	}
	if !selected {
		return nil
	}
	_, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	return err
}

// kubernetesInvocationIdentity keeps private owner selection under the originating Connection lock.
// A source change creates no fallback and cannot replace an already-pinned invocation identity.
// kubernetesInvocationIdentity сохраняет private owner selection под originating Connection lock.
// Source change не создаёт fallback и не заменяет уже pinned invocation identity.
type kubernetesInvocationIdentity struct {
	identity                                                                       entity.IntegrationBrokerIdentityRevision
	identityID, subjectID, bindingID, healthID, subjectDigest, configurationDigest string
	bindingGeneration                                                              int64
	source                                                                         kubernetesOperatorStored
	target                                                                         value.ManagedKubernetesTarget
}

// selectKubernetesInvocationIdentityTx derives both personal and shared variants from exact owner state.
// Shared never reads a personal binding, and neither variant can return a generic TOKEN credential.
// selectKubernetesInvocationIdentityTx выводит personal и shared variants из exact owner state.
// Shared не читает personal binding; ни один variant не возвращает generic TOKEN credential.
func (repository *Repository) selectKubernetesInvocationIdentityTx(ctx context.Context, tx pgx.Tx, organizationID string, choice *personalInvocationCredentialChoice) (result kubernetesInvocationIdentity, resultErr error) {
	current := choice.context
	if !current.ready || !current.visible {
		return result, errs.ErrForbidden
	}
	definition, err := repository.integrationPackage(ctx, tx, organizationID, current.connectionRef, current.definitionKey, current.definitionVersion, current.definitionDigest)
	if err != nil {
		return result, err
	}
	delivery, known := definition.ConnectionCredentialDelivery()
	if !known || current.definitionKey != integrationpackage.KubernetesDefinitionKey || definition.Spec.Adapter != "KUBERNETES" || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != "KUBERNETES_TARGET" {
		return result, errs.ErrForbidden
	}
	var configuration map[string]string
	if json.Unmarshal(current.configuration, &configuration) != nil || definition.ValidateConfiguration(configuration) != nil {
		return result, errs.ErrForbidden
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	result.configurationDigest = hex.EncodeToString(digest[:])
	if current.policy == entity.IntegrationCredentialInitiatorRequired {
		if configuration["service_subject_ref"] != "" || choice.selection.Kind != entity.IntegrationCredentialBrokerIdentity || choice.selection.BrokerIdentity == nil || choice.selection.Credential != nil || choice.selection.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionKubernetesScopedV1 {
			return result, errs.ErrForbidden
		}
		result.identity, result.subjectID, result.bindingID, result.bindingGeneration = *choice.selection.BrokerIdentity, current.subjectID, choice.bindingID, choice.selection.BindingGeneration
		err = tx.QueryRow(ctx, queryKubernetesInvocationPersonalIdentity, pgx.StrictNamedArgs{"organization_id": organizationID, "binding_id": result.bindingID,
			"generation": result.bindingGeneration, "identity_ref": result.identity.Ref, "revision": result.identity.Revision, "target_ref": configuration["target_ref"]}).Scan(&result.identityID)
	} else if current.policy == entity.IntegrationCredentialProjectShared {
		err = tx.QueryRow(ctx, queryKubernetesInvocationSharedIdentity, pgx.StrictNamedArgs{"organization_id": organizationID, "connection_id": current.connectionID}).Scan(
			&result.identityID, &result.subjectID, &result.identity.Ref, &result.identity.TargetRef, &result.identity.Purpose, &result.identity.Revision, &result.identity.CreatedAt)
	} else {
		return result, errs.ErrForbidden
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil || !result.identity.ValidMetadata() || result.identity.TargetRef != configuration["target_ref"] || result.identity.Purpose != entity.IntegrationBrokerIdentityKubernetes {
		return result, errs.ErrUnavailable
	}
	result.source, err = scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare))
	if err != nil {
		return result, err
	}
	for _, entry := range result.source.registry.Targets {
		if entry.Target.Ref == result.identity.TargetRef {
			result.target = entry.Target
			break
		}
	}
	if result.target.Ref == "" {
		return result, errs.ErrForbidden
	}
	err = tx.QueryRow(ctx, queryKubernetesInvocationHealth, pgx.StrictNamedArgs{"organization_id": organizationID, "identity_id": result.identityID,
		"registry_revision": result.source.registry.Revision, "target_ref": result.target.Ref, "target_revision": result.target.Revision,
		"definition_digest": current.definitionDigest, "configuration_digest": result.configurationDigest, "configuration": string(raw)}).Scan(&result.healthID, &result.subjectDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil || !managedkubernetes.ValidDigest(result.subjectDigest) {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// pinKubernetesInvocationIdentityTx fixes exact BI lineage before the original intent transaction commits.
// The existing root/grant/RuntimeRevision admission remains mandatory; this pin grants no API request.
// pinKubernetesInvocationIdentityTx закрепляет exact BI lineage до original intent transaction commit.
// Existing root/grant/RuntimeRevision admission обязателен; pin не разрешает API request.
func (repository *Repository) pinKubernetesInvocationIdentityTx(ctx context.Context, tx pgx.Tx, organizationID string, choice *personalInvocationCredentialChoice) error {
	selected, err := repository.selectKubernetesInvocationIdentityTx(ctx, tx, organizationID, choice)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, queryKubernetesInvocationPin, kubernetesInvocationPinArguments(organizationID, choice, selected))
	if err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	return nil
}
