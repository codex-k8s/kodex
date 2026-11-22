package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// querySSHInvocationIdentity resolves current custody and private material metadata under the originating Connection lock, without loading key bytes.
// querySSHInvocationIdentity разрешает current custody и private material metadata под originating Connection lock, не загружая key bytes.
//
//go:embed sql/ssh_invocation__identity.sql
var querySSHInvocationIdentity string

// querySSHInvocationHealth selects an applied actual SSH observation for the exact source, retaining its finite deadline rather than promoting a health check to resource authority.
// querySSHInvocationHealth выбирает applied actual SSH observation для exact source и сохраняет finite deadline, не повышая health check до resource authority.
//
//go:embed sql/ssh_invocation__health.sql
var querySSHInvocationHealth string

// querySSHInvocationPin records immutable originating identity lineage; the owner database independently checks every source coordinate at insertion.
// querySSHInvocationPin сохраняет immutable originating identity lineage; owner database независимо проверяет каждый source coordinate при insertion.
//
//go:embed sql/ssh_invocation__pin.sql
var querySSHInvocationPin string

// sshInvocationIdentity retains only owner-selected references and a bounded actual health fact; it contains neither private key bytes nor a transferable effect grant.
// sshInvocationIdentity хранит лишь owner-selected references и bounded actual health fact; private key bytes и transferable effect grant отсутствуют.
type sshInvocationIdentity struct {
	identityID, subjectID, healthID, subjectDigest, configurationDigest string
	identity                                                            entity.IntegrationBrokerIdentityRevision
	mode                                                                managedssh.TransportMode
	validUntil                                                          time.Time
}

// selectSSHInvocationIdentityTx reuses the sole credential selector for either custody mode, then requires durable known material and a fresh matching native observation; no caller coordinate selects a host or key.
// selectSSHInvocationIdentityTx использует единый credential selector для обоих custody modes, затем требует durable known material и fresh matching native observation; caller coordinate не выбирает host либо key.
func (repository *Repository) selectSSHInvocationIdentityTx(ctx context.Context, tx pgx.Tx, organizationID string, choice *personalInvocationCredentialChoice) (sshInvocationIdentity, error) {
	var result sshInvocationIdentity
	current := choice.context
	if !current.ready || !current.visible {
		return result, errs.ErrForbidden
	}
	definition, err := repository.integrationPackage(ctx, tx, organizationID, current.connectionRef, current.definitionKey, current.definitionVersion, current.definitionDigest)
	if err != nil {
		return result, err
	}
	result.mode, err = sshHealthMode(definition)
	if err != nil {
		return result, err
	}
	capability, exists := definition.Capability(current.capabilityKey)
	if !exists || integrationpackage.CredentialResourcePermissionProfile(definition, capability) != integrationpackage.CredentialResourcePermissionSSHScopedV1 {
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
	result.configurationDigest = sshHealthHash(raw)
	switch current.policy {
	case entity.IntegrationCredentialInitiatorRequired:
		selected := choice.selection
		if configuration["service_subject_ref"] != "" || selected.Kind != entity.IntegrationCredentialBrokerIdentity || selected.BrokerIdentity == nil || selected.Credential != nil ||
			selected.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionSSHScopedV1 || selected.PersonalBindingRef == "" || selected.BindingGeneration < 1 {
			return result, errs.ErrForbidden
		}
		result.identity = *selected.BrokerIdentity
	case entity.IntegrationCredentialProjectShared:
		selected, err := repository.readSharedHealthCredentialSourceTx(ctx, tx, organizationID, current.connectionID, current.connectionRef, definition)
		if err != nil {
			return result, err
		}
		if selected.identity == nil || selected.delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || selected.delivery.BrokerPurpose != managedssh.IdentityPurpose {
			return result, errs.ErrForbidden
		}
		result.identity = *selected.identity
	default:
		return result, errs.ErrForbidden
	}
	if !result.identity.ValidMetadata() || result.identity.Purpose != entity.IntegrationBrokerIdentitySSH || result.identity.TargetRef != configuration["target_ref"] {
		return result, errs.ErrForbidden
	}
	err = tx.QueryRow(ctx, querySSHInvocationIdentity, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_id": current.connectionID, "identity_ref": result.identity.Ref, "revision": result.identity.Revision,
		"binding_id": choice.bindingID, "generation": choice.selection.BindingGeneration, "root_actor_id": current.subjectID,
	}).Scan(&result.identityID, &result.subjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	err = tx.QueryRow(ctx, querySSHInvocationHealth, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_id": current.connectionID, "identity_id": result.identityID,
		"definition_digest": current.definitionDigest, "configuration_digest": result.configurationDigest, "configuration": current.configuration,
		"binding_id": choice.bindingID, "generation": choice.selection.BindingGeneration, "mode": string(result.mode),
	}).Scan(&result.healthID, &result.subjectDigest, &result.validUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil || !managedssh.Digest(result.subjectDigest) {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// pinSSHInvocationIdentityTx fixes exact current custody and observed target qualification in the original intent transaction; later claim and effect admission must reread these pins and cannot select another revision.
// pinSSHInvocationIdentityTx закрепляет exact current custody и observed target qualification в original intent transaction; последующие claim и effect admission перечитывают pins и не выбирают другую revision.
func (repository *Repository) pinSSHInvocationIdentityTx(ctx context.Context, tx pgx.Tx, organizationID string, choice *personalInvocationCredentialChoice) error {
	selected, err := repository.selectSSHInvocationIdentityTx(ctx, tx, organizationID, choice)
	if err != nil {
		return err
	}
	current := choice.context
	tag, err := tx.Exec(ctx, querySSHInvocationPin, pgx.StrictNamedArgs{
		"organization_id": organizationID, "invocation_id": current.invocationID, "connection_id": current.connectionID,
		"root_id": current.rootID, "root_actor_id": current.subjectID, "subject_id": selected.subjectID,
		"binding_id": choice.bindingID, "generation": choice.selection.BindingGeneration, "identity_id": selected.identityID,
		"health_id": selected.healthID, "subject_digest": selected.subjectDigest, "valid_until": selected.validUntil,
		"definition_digest": current.definitionDigest, "configuration_digest": selected.configurationDigest, "mode": string(selected.mode),
	})
	if err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
