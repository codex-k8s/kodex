package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestInteractionSourceDoesNotBypassManagedReadGate(t *testing.T) {
	shipped, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := shipped["mattermost"]
	if !interactionSourceCapability(definition, "mattermost.inbound") || !interactionSourceCapability(definition, "mattermost.gate_decisions") {
		t.Fatal("shipped source contract became unavailable")
	}
	for index := range definition.Spec.Capabilities {
		if definition.Spec.Capabilities[index].Key == "mattermost.inbound" {
			definition.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
		}
	}
	if interactionSourceCapability(definition, "mattermost.inbound") || interactionSourceCapability(definition, "mattermost.notifications") {
		t.Fatal("automatic subscription bypassed gate or accepted an effect operation")
	}
	if !interactionSourceCapability(definition, "mattermost.gate_decisions") {
		t.Fatal("explicit owner decision was replaced by recursive approval")
	}
}

func TestInteractionIncidentUsesPinnedAttemptBudget(t *testing.T) {
	incident := projectInteractionIncident(entity.Incident{}, "FAILED", 1, 1)
	if incident.State != "OPEN" || incident.SafeNextStep != "i18n:INTERACTION_DELIVERY_RETRY_EXHAUSTED" {
		t.Fatal("exhausted package budget advertised hidden retry")
	}
}
