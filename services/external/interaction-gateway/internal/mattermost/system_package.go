package mattermost

import (
	"encoding/json"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// systemPackageSource supplies exact executable-package pins and connection configuration shared by source and delivery claims.
// RU: systemPackageSource передаёт точные координаты исполняемого пакета и настройки подключения, общие для claim источника и доставки.
type systemPackageSource interface {
	// GetDefinitionPackage returns the bounded managed package bytes for executable-baseline validation.
	// RU: GetDefinitionPackage возвращает ограниченные байты управляемого пакета для проверки исполняемой основы.
	GetDefinitionPackage() []byte
	// GetDefinitionKey returns the expected package kind, which must remain Mattermost at this executor.
	// RU: GetDefinitionKey возвращает ожидаемый вид пакета, который у этого исполнителя должен оставаться Mattermost.
	GetDefinitionKey() string
	// GetDefinitionVersion returns the exact managed version that must match the decoded package.
	// RU: GetDefinitionVersion возвращает точную управляемую версию, обязанную совпасть с декодированным пакетом.
	GetDefinitionVersion() string
	// GetDefinitionDigest returns the package digest checked against the decoded executable revision.
	// RU: GetDefinitionDigest возвращает дайджест пакета, сверяемый с декодированной исполняемой ревизией.
	GetDefinitionDigest() string
	// GetConnectionVersion returns a positive owner snapshot pin required before system operations.
	// RU: GetConnectionVersion возвращает положительную координату снимка владельца, обязательную до системных операций.
	GetConnectionVersion() int64
	// GetBaseUrl supplies the configured origin for package validation and later exact-host transport checks.
	// RU: GetBaseUrl задаёт адрес подключения для проверки пакета и последующих проверок точного узла транспорта.
	GetBaseUrl() string
	// GetTeamName supplies the configured team name used by package validation and exact provider resolution.
	// RU: GetTeamName задаёт имя команды для проверки пакета и точного разрешения у провайдера.
	GetTeamName() string
	// GetChannelName supplies the configured channel name without granting authority to external message identifiers.
	// RU: GetChannelName задаёт имя канала, не выдавая полномочий внешним идентификаторам сообщений.
	GetChannelName() string
}

// systemPackage verifies exact managed package pins, a positive connection version and the supplied public connection configuration.
// RU: systemPackage проверяет точные координаты управляемого пакета, положительную версию подключения и его публичную конфигурацию.
func (adapter *Adapter) systemPackage(source systemPackageSource) (integrationpackage.Package, error) {
	definition, err := adapter.claimDefinition(source.GetDefinitionPackage(), source.GetDefinitionKey(), source.GetDefinitionVersion(), source.GetDefinitionDigest())
	if err != nil || source.GetConnectionVersion() < 1 || definition.ValidateConfiguration(map[string]string{
		"base_url": source.GetBaseUrl(), "team_name": source.GetTeamName(), "channel_name": source.GetChannelName(),
	}) != nil {
		return integrationpackage.Package{}, errConfiguration
	}
	return definition, nil
}

// sourceCapability permits only the exact inbound READ/NONE or gate-decision SENSITIVE/HUMAN_EACH_EFFECT system capability profile.
// RU: sourceCapability допускает только точный системный профиль входящих READ/NONE либо решений gate SENSITIVE/HUMAN_EACH_EFFECT.
func sourceCapability(definition integrationpackage.Package, key string) (integrationpackage.Capability, bool) {
	capability, ok := definition.Capability(key)
	if !ok || capability.Operation != key {
		return integrationpackage.Capability{}, false
	}
	valid := (key == "mattermost.inbound" && capability.Risk == "READ" && capability.ApprovalPolicy == "NONE") ||
		(key == "mattermost.gate_decisions" && capability.Risk == "SENSITIVE" && capability.ApprovalPolicy == "HUMAN_EACH_EFFECT")
	return capability, valid
}

// sourceBudget validates one or two distinct enabled source capabilities and chooses the smallest adapter/capability operation timeout.
// RU: sourceBudget проверяет одну или две различные включённые capability источника и выбирает наименьший timeout адаптера и capabilities.
func (adapter *Adapter) sourceBudget(source *cp.InteractionSource) (time.Duration, error) {
	definition, err := adapter.systemPackage(source)
	if err != nil || len(source.GetEnabledCapabilities()) == 0 || len(source.GetEnabledCapabilities()) > 2 {
		return 0, errConfiguration
	}
	seen := map[string]bool{}
	budget := adapter.timeout
	for _, key := range source.GetEnabledCapabilities() {
		capability, valid := sourceCapability(definition, key)
		if !valid || seen[key] {
			return 0, errConfiguration
		}
		seen[key] = true
		budget = min(budget, time.Duration(capability.Execution.TimeoutSeconds)*time.Second)
	}
	return budget, nil
}

// validateSourceInput validates the system capability's exact input schema, including gate reference and decision text when required.
// RU: validateSourceInput проверяет точную схему ввода системной capability, включая ссылку gate и текст решения при необходимости.
// Schema-valid correlation is not an owner approval; authoritative message acceptance remains a separate RPC.
// RU: Соответствие корреляции схеме не является одобрением владельца; авторитетный приём сообщения остаётся отдельным RPC.
func (adapter *Adapter) validateSourceInput(source *cp.InteractionSource, key, gateRef string, decision cp.OwnerGateDecision) error {
	definition, err := adapter.systemPackage(source)
	if err != nil {
		return err
	}
	capability, valid := sourceCapability(definition, key)
	if !valid {
		return errInvocation
	}
	input := map[string]string{}
	if key == "mattermost.gate_decisions" {
		input["decision_ref"] = gateRef
		input["decision"] = strings.TrimPrefix(decision.String(), "OWNER_GATE_DECISION_")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return errInvocation
	}
	if _, err := capability.ValidateInput(raw); err != nil {
		return errInvocation
	}
	return nil
}

// deliveryCapability distinguishes system acknowledgements/gate messages from notification/mirror writes and checks each profile's exact source and gate pins.
// RU: deliveryCapability различает системные подтверждения/сообщения gate и записи уведомления/зеркала, проверяя точные source и gate координаты каждого профиля.
// It rejects detached or unresolved approval fields before provider I/O without creating or approving a human decision itself.
// RU: Функция отклоняет несвязанные или неразрешённые поля подтверждения до I/O провайдера, сама не создавая и не одобряя человеческое решение.
func (adapter *Adapter) deliveryCapability(claim *cp.InteractionDeliveryClaim) (integrationpackage.Capability, error) {
	definition, err := adapter.systemPackage(claim)
	if err != nil {
		return integrationpackage.Capability{}, err
	}
	key, sourceKey := claim.GetCapabilityKey(), claim.GetSourceCapabilityKey()
	if key == "mattermost.acknowledgements" || key == "mattermost.gate_decisions" {
		capability, valid := sourceCapability(definition, sourceKey)
		if !valid || (key == "mattermost.gate_decisions" && sourceKey != key) ||
			claim.GetApprovalGateRef() != "" || claim.GetApprovalGateVersion() != 0 {
			return integrationpackage.Capability{}, errInvocation
		}
		return capability, nil
	}
	if key != "mattermost.notifications" && key != "mattermost.result_mirror" {
		return integrationpackage.Capability{}, errInvocation
	}
	capability, valid := definition.Capability(key)
	if !valid || sourceKey != key || capability.Operation != key || capability.Risk != "WRITE" ||
		capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" || !boundedReference(claim.GetApprovalGateRef()) || claim.GetApprovalGateVersion() < 2 {
		return integrationpackage.Capability{}, errInvocation
	}
	return capability, nil
}
