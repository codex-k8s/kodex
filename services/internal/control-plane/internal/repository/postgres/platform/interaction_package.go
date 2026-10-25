package platform

import (
	"context"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// interactionConnectionPackage locks the exact organization connection, requires active enabled
// Mattermost in CONNECTED/DEGRADED state and resolves its pinned definition version/digest. It
// returns package metadata without issuing a delivery grant or substituting a different definition
// after denial.
// interactionConnectionPackage блокирует точную connection организации, требует active enabled
// Mattermost в CONNECTED/DEGRADED и разрешает pinned definition version/digest. Возвращает package
// metadata без выдачи delivery grant и подстановки иной definition после отказа.
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

// interactionSourceCapability accepts only exact operation-key matches for inbound READ/NONE or
// gate-decisions SENSITIVE/HUMAN_EACH_EFFECT. A gated inbound decision remains a separate owner
// lifecycle action and cannot become a continuous subscription; capability classification is not
// actor authorization.
// interactionSourceCapability принимает лишь точное совпадение operation/key для inbound READ/NONE
// либо gate-decisions SENSITIVE/HUMAN_EACH_EFFECT. Решение gated inbound остаётся отдельным
// действием owner lifecycle и не становится непрерывной подпиской; классификация capability не
// является авторизацией субъекта.
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

// validateInteractionSourceInput requires the closed source capability and validates either an
// empty inbound object or decision_ref/decision against the package input contract. Capability
// denial is Forbidden and encoding/schema errors Invalid; gate existence and human authority are
// checked separately.
// validateInteractionSourceInput требует закрытую source capability и проверяет пустой inbound
// object либо decision_ref/decision по package input contract. Отказ capability даёт Forbidden,
// encoding/schema errors — Invalid; существование gate и полномочия человека проверяются отдельно.
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

// projectInteractionPackage serializes the already resolved definition within256KiB before
// attaching definition pins, package bytes and connection version to the item map. Failure leaves
// the map unchanged and returns Unavailable; the map must be nonnil and this projection grants no
// invocation authority.
// projectInteractionPackage сериализует уже разрешённую definition в пределах256KiB до добавления
// definition pins, package bytes и connection version в map элемента. Ошибка сохраняет map и даёт
// Unavailable; map должна быть nonnil, проекция не выдаёт права invocation.
func projectInteractionPackage(item map[string]any, connection lockedIntegrationConnection, definition integrationpackage.Package) error {
	raw, err := json.Marshal(definition)
	if err != nil || len(raw) == 0 || len(raw) > 256<<10 {
		return errs.ErrUnavailable
	}
	item["definitionKey"], item["definitionVersion"], item["definitionDigest"] = definition.Metadata.Key, definition.Metadata.Version, definition.Digest
	item["definitionPackage"], item["connectionVersion"] = raw, connection.version
	return nil
}
