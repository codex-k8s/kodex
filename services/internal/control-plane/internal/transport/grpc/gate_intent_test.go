package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

func TestCastGatePreservesIntentConsequencesAndSource(t *testing.T) {
	value := entity.OwnerGate{SourceAttachmentSetRef: "ats_source", DecisionConsequences: []entity.OwnerGateDecisionConsequence{{Decision: "APPROVE", SafeSummary: "Разрешить", ExecutesExternalEffect: true}}, IntegrationIntent: &entity.IntegrationIntent{ConnectionRef: "connection", ConnectionName: "Соединение", DefinitionKey: "synthetic", CapabilityKey: "synthetic.journal.write", Operation: "synthetic.journal.write", EffectKey: "effect", ResourceKind: "SYNTHETIC", ResourceScope: map[string]string{"journal": "main"}, ResourceScopeDigest: "digest", EffectPreview: map[string]any{"contentComplete": false}}}
	got := castGate(value)
	if got.SourceAttachmentSetRef != value.SourceAttachmentSetRef || len(got.DecisionConsequences) != 1 || !got.DecisionConsequences[0].ExecutesExternalEffect || got.IntegrationIntent == nil || got.IntegrationIntent.EffectKey != "effect" || got.IntegrationIntent.EffectPreview == nil {
		t.Fatal("gate projection lost")
	}
	if castGate(entity.OwnerGate{}).IntegrationIntent != nil {
		t.Fatal("ordinary gate received integration intent")
	}
}
