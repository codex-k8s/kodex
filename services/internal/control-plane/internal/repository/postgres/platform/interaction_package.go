package platform

import (
	"context"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) interactionConnectionPackage(ctx context.Context, tx pgx.Tx, organizationID, connectionRef string) (lockedIntegrationConnection, integrationpackage.Package, error) {
	connection, err := repository.lockIntegrationConnection(ctx, tx, organizationID, connectionRef)
	if err != nil {
		return connection, integrationpackage.Package{}, err
	}
	if connection.definitionKey != "mattermost" || connection.lifecycleState != "ACTIVE" || !connection.enabled ||
		!contains([]string{"CONNECTED", "DEGRADED"}, connection.state) {
		return connection, integrationpackage.Package{}, errs.ErrForbidden
	}
	definition, err := repository.integrationPackage(ctx, tx, organizationID, connectionRef, connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
	return connection, definition, err
}

// Gate decision выполняет отдельное решение человека в owner lifecycle.
// Gated inbound нельзя автоматически превращать в непрерывную подписку.
func interactionSourceCapability(definition integrationpackage.Package, key string) bool {
	capability, ok := definition.Capability(key)
	if !ok || capability.Operation != key {
		return false
	}
	switch key {
	case "mattermost.inbound":
		return capability.Risk == "READ" && capability.ApprovalPolicy == "NONE"
	case "mattermost.gate_decisions":
		return capability.Risk == "SENSITIVE" && capability.ApprovalPolicy == "HUMAN_EACH_EFFECT"
	default:
		return false
	}
}

func validateInteractionSourceInput(definition integrationpackage.Package, key, gateRef, decision string) error {
	if !interactionSourceCapability(definition, key) {
		return errs.ErrForbidden
	}
	capability, _ := definition.Capability(key)
	input := map[string]string{}
	if key == "mattermost.gate_decisions" {
		input["decision_ref"], input["decision"] = gateRef, decision
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return errs.ErrInvalid
	}
	if _, err := capability.ValidateInput(raw); err != nil {
		return errs.ErrInvalid
	}
	return nil
}

func projectInteractionPackage(item map[string]any, connection lockedIntegrationConnection, definition integrationpackage.Package) error {
	raw, err := json.Marshal(definition)
	if err != nil || len(raw) == 0 || len(raw) > 256<<10 {
		return errs.ErrUnavailable
	}
	item["definitionKey"], item["definitionVersion"], item["definitionDigest"] = definition.Metadata.Key, definition.Metadata.Version, definition.Digest
	item["definitionPackage"], item["connectionVersion"] = raw, connection.version
	return nil
}
