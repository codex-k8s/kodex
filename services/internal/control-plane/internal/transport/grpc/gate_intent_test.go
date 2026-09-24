package grpc

import (
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
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

func TestCastGatePreservesScopedApprovalPreview(t *testing.T) {
	value := entity.OwnerGate{IntegrationIntent: &entity.IntegrationIntent{
		EffectPreview: map[string]any{"approvalScope": map[string]any{
			"selected":     []integrationpackage.ApprovalScopeValue{{Path: "/body/marker", Type: "string", Value: "local-fixture"}},
			"mutablePaths": []string{"/body/description"},
		}},
	}}
	preview := castGate(value).GetIntegrationIntent().GetEffectPreview()
	if preview == nil {
		t.Fatal("scoped approval preview was lost in Proto projection")
	}
	scope, ok := preview.AsMap()["approvalScope"].(map[string]any)
	if !ok {
		t.Fatal("scoped approval details were lost")
	}
	selected, ok := scope["selected"].([]any)
	if !ok || len(selected) != 1 || selected[0].(map[string]any)["value"] != "local-fixture" {
		t.Fatal("selected typed parameter was lost")
	}
}
