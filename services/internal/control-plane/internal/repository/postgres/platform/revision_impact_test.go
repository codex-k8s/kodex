package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRevisionImpactCommitmentPreservesIntent(t *testing.T) {
	plan := entity.RevisionImpactPlan{Ref: "rvip_test00001", Kind: "RUNTIME_ENVIRONMENT", Version: 1, State: "PREPARED", DraftRef: "draft", DraftVersion: 3, SourceRef: "env", SourceVersion: 2, TargetDigest: "target", Total: 2}
	items := []entity.RevisionImpactItem{{Ref: "rvit_a0000001", ConsumerRef: "agent1", ConsumerVersion: 4, BindingRef: "binding1", BindingVersion: 2}, {Ref: "rvit_b0000001", ConsumerRef: "agent2", ConsumerVersion: 5, BindingRef: "binding2", BindingVersion: 3}}
	want, err := revisionImpactDigest(plan, "actor", items)
	if err != nil {
		t.Fatal(err)
	}
	items[0].Outcome = "APPLIED"
	items[0].ResultRevisionRef = "new"
	items[0].ResultConsumerVersion = 5
	plan.State, plan.Version, plan.PublishedRevisionRef = "APPLIED", 2, "new"
	got, err := revisionImpactDigest(plan, "actor", []entity.RevisionImpactItem{items[1], items[0]})
	if err != nil || got != want {
		t.Fatal("terminal receipts changed immutable intent")
	}
	for _, change := range []string{"actor", "draft", "binding"} {
		changed := plan
		copyItems := append([]entity.RevisionImpactItem{}, items...)
		actor := "actor"
		switch change {
		case "actor":
			actor = "other"
		case "draft":
			changed.DraftVersion++
		case "binding":
			copyItems[0].BindingVersion++
		}
		got, err = revisionImpactDigest(changed, actor, copyItems)
		if err != nil || got == want {
			t.Fatalf("changed %s retained commitment", change)
		}
	}
}
