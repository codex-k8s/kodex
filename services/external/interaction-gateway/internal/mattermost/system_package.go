package mattermost

import (
	"encoding/json"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

type systemPackageSource interface {
	GetDefinitionPackage() []byte
	GetDefinitionKey() string
	GetDefinitionVersion() string
	GetDefinitionDigest() string
	GetConnectionVersion() int64
	GetBaseUrl() string
	GetTeamName() string
	GetChannelName() string
}

func (adapter *Adapter) systemPackage(source systemPackageSource) (integrationpackage.Package, error) {
	definition, err := adapter.claimDefinition(source.GetDefinitionPackage(), source.GetDefinitionKey(), source.GetDefinitionVersion(), source.GetDefinitionDigest())
	if err != nil || source.GetConnectionVersion() < 1 || definition.ValidateConfiguration(map[string]string{
		"base_url": source.GetBaseUrl(), "team_name": source.GetTeamName(), "channel_name": source.GetChannelName(),
	}) != nil {
		return integrationpackage.Package{}, errConfiguration
	}
	return definition, nil
}

func sourceCapability(definition integrationpackage.Package, key string) (integrationpackage.Capability, bool) {
	capability, ok := definition.Capability(key)
	if !ok || capability.Operation != key {
		return integrationpackage.Capability{}, false
	}
	valid := (key == "mattermost.inbound" && capability.Risk == "READ" && capability.ApprovalPolicy == "NONE") ||
		(key == "mattermost.gate_decisions" && capability.Risk == "SENSITIVE" && capability.ApprovalPolicy == "HUMAN_EACH_EFFECT")
	return capability, valid
}

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
