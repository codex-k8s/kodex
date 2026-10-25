package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestInteractionSourceDoesNotBypassManagedReadGate checks shipped source admission obeys the
// actual approval policy, rejects notification effects and preserves explicit owner-decision input
// without recursive approval.
// TestInteractionSourceDoesNotBypassManagedReadGate проверяет соблюдение фактической политики
// подтверждения поставляемым источником, отклонение эффектов уведомлений и сохранение явного ввода
// решения владельца без рекурсивного подтверждения.
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

// TestInteractionIncidentUsesPinnedAttemptBudget projects an exhausted one-attempt delivery budget
// as an open incident with retry-exhausted guidance rather than a hidden retry.
// TestInteractionIncidentUsesPinnedAttemptBudget проецирует исчерпанный бюджет одной попытки
// доставки в открытый инцидент с указанием исчерпания повторов вместо скрытого повтора.
func TestInteractionIncidentUsesPinnedAttemptBudget(t *testing.T) {
	incident := projectInteractionIncident(entity.Incident{}, "FAILED", 1, 1)
	if incident.State != "OPEN" || incident.SafeNextStep != "i18n:INTERACTION_DELIVERY_RETRY_EXHAUSTED" {
		t.Fatal("exhausted package budget advertised hidden retry")
	}
}

// TestInteractionSourceUsesActualDecisionInputSchema mutates the shipped decision enum and checks
// only its actual allowed decision with a gate reference passes input validation.
// TestInteractionSourceUsesActualDecisionInputSchema изменяет enum решений поставляемого пакета и
// проверяет, что валидацию проходит только фактически разрешённое решение с ref подтверждения.
func TestInteractionSourceUsesActualDecisionInputSchema(t *testing.T) {
	shipped, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := shipped["mattermost"]
	if err := validateInteractionSourceInput(definition, "mattermost.inbound", "", ""); err != nil {
		t.Fatal(err)
	}
	for index := range definition.Spec.Capabilities {
		capability := &definition.Spec.Capabilities[index]
		if capability.Key == "mattermost.gate_decisions" {
			for fieldIndex := range capability.InputFields {
				if capability.InputFields[fieldIndex].Key == "decision" {
					capability.InputFields[fieldIndex].AllowedValues = []string{"APPROVE"}
				}
			}
		}
	}
	for _, decision := range []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "", "UNKNOWN"} {
		err := validateInteractionSourceInput(definition, "mattermost.gate_decisions", "gate_fixture", decision)
		if decision == "APPROVE" && err != nil || decision != "APPROVE" && !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("decision %s bypassed actual package input: %v", decision, err)
		}
	}
	if err := validateInteractionSourceInput(definition, "mattermost.gate_decisions", "", "APPROVE"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("missing gate reference bypassed actual input schema")
	}
}
