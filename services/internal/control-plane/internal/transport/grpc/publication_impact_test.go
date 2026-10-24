package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strings"
	"testing"
)

// TestPublicationImpactResultPins checks both supported publication result kinds and rejects
// missing plans or mismatched digest, kind, published revision, state and source pins.
// TestPublicationImpactResultPins проверяет оба поддерживаемых вида результата публикации и отказ
// при отсутствии плана либо несовпадении digest, kind, опубликованной ревизии, состояния и
// источника.
func TestPublicationImpactResultPins(t *testing.T) {

	for _, kind := range []string{"AGENT_INSTRUCTIONS", "PROMPT_TEMPLATE"} {
		if err := validatePublishedImpactResult(publicationImpactResultFixture(kind), kind, "rvip_fixture", "source_fixture"); err != nil {
			t.Fatal(err)
		}
		for _, mutate := range []func(*command.Result){func(r *command.Result) { r.RevisionImpactPlan = nil }, func(r *command.Result) { r.RevisionImpactPlan.Digest = "bad" }, func(r *command.Result) { r.RevisionImpactPlan.Kind = "RUNTIME_ENVIRONMENT" }, func(r *command.Result) { r.RevisionImpactPlan.PublishedRevisionRef = "wrong" }, func(r *command.Result) { r.RevisionImpactPlan.State = "PREPARED" }, func(r *command.Result) { r.RevisionImpactPlan.SourceRef = "wrong" }} {
			r := publicationImpactResultFixture(kind)
			mutate(&r)
			if err := validatePublishedImpactResult(r, kind, "rvip_fixture", "source_fixture"); err == nil {
				t.Fatal("invalid publication receipt accepted")
			}
		}
	}
}

// publicationImpactResultFixture supplies consistent instruction/template publication pins for
// negative receipt checks and shared digest-boundary tests using only synthetic identifiers.
// publicationImpactResultFixture задаёт согласованные привязки публикации инструкции и шаблона
// для negative receipt и общих digest-проверок только с синтетическими идентификаторами.
func publicationImpactResultFixture(kind string) command.Result {
	plan := &entity.RevisionImpactPlan{Ref: "rvip_fixture", Kind: kind, State: "APPLIED", Version: 2, SourceRef: "source_fixture", SourceVersion: 3, DraftRef: "revision_fixture", DraftVersion: 2, PublishedRevisionRef: "revision_fixture", Digest: strings.Repeat("a", 64), TargetDigest: strings.Repeat("b", 64)}
	return command.Result{RevisionImpactPlan: plan, Agent: &entity.Agent{Ref: plan.SourceRef, Version: 4}, ManagedConfiguration: &entity.ManagedConfigurationSet{Ref: plan.SourceRef, Version: 5}, ManagedRevision: &entity.ManagedConfigurationRevision{Ref: plan.DraftRef, State: "PUBLISHED", Digest: plan.TargetDigest}}
}
