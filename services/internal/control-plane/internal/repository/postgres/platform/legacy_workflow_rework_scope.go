package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryLegacyReworkAuthority resolves current root initiator/project identity from the owned node, never caller payload.
// queryLegacyReworkAuthority разрешает текущие root initiator/project identity по owned node, а не caller payload.
//
//go:embed sql/legacy_workflow_rework__authority.sql
var queryLegacyReworkAuthority string

// validateLegacyReworkNodeAuthorityTx checks a fresh workflow permission before a new owned external READ or artifact write.
// validateLegacyReworkNodeAuthorityTx проверяет свежее workflow permission перед новым owned external READ либо записью материала.
func (repository *Repository) validateLegacyReworkNodeAuthorityTx(ctx context.Context, tx pgx.Tx, current scope, nodeID string) error {
	var origin legacyReworkContext
	err := tx.QueryRow(ctx, queryLegacyReworkAuthority, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": nodeID}).Scan(&origin.WorkflowRef, &origin.ProjectID, &origin.ProjectRef, &origin.ActorRef, &origin.OrganizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	return repository.validateLegacyReworkAuthorityTx(ctx, tx, current, &origin)
}

// validateLegacyReworkPublishedScopeTx admits only artifact production and exact current READ integration capabilities before allocating a physical retry.
// validateLegacyReworkPublishedScopeTx допускает лишь материалы и точные текущие READ integration capabilities до создания physical retry.
func (repository *Repository) validateLegacyReworkPublishedScopeTx(ctx context.Context, tx pgx.Tx, current scope, origin *legacyReworkContext) error {
	actor := current
	if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, origin.ActorRef, origin.OrganizationRef).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role) != nil {
		return errs.ErrForbidden
	}
	actor.authorityProjectID = origin.ProjectID
	platform, grants, err := repository.agentCapabilityAuthority(ctx, tx, actor, origin.ProjectRef, origin.AgentRef, origin.RequiredCapabilities)
	if err != nil {
		return err
	}
	if err := repository.validateLegacyReworkScopeTx(ctx, tx, current, origin.RequiredCapabilities, grants); err != nil {
		return err
	}
	for _, capability := range origin.RequiredCapabilities {
		if capability == runtimecontract.ArtifactCapability && !contains(platform, capability) {
			return errs.ErrCapabilityRequired
		}
	}
	return nil
}

// validateLegacyReworkScopeTx rechecks each exact package/version/digest and rejects delegation, workflow launch and non-READ effects without changing ordinary legacy execution.
// validateLegacyReworkScopeTx проверяет каждый exact package/version/digest и отклоняет delegate/launch/non-READ без изменения обычного legacy исполнения.
func (repository *Repository) validateLegacyReworkScopeTx(ctx context.Context, tx pgx.Tx, current scope, capabilities []string, grants []map[string]string) error {
	for _, capability := range capabilities {
		if capability == runtimecontract.ArtifactCapability {
			continue
		}
		if strings.HasPrefix(capability, "platform.") {
			return errs.ErrLegacyReworkScopeUnsupported
		}
		found := false
		for _, grant := range grants {
			if grant["capabilityKey"] != capability {
				continue
			}
			definition, err := repository.integrationPackage(ctx, tx, current.organizationID, grant["connectionRef"], grant["definitionKey"], grant["definitionVersion"], grant["definitionDigest"])
			if err != nil {
				return err
			}
			operation, exists := definition.Capability(capability)
			if !exists || operation.Risk != "READ" {
				return errs.ErrLegacyReworkScopeUnsupported
			}
			found = true
		}
		if !found {
			return errs.ErrCapabilityRequired
		}
	}
	return nil
}
