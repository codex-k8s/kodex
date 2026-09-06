package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
	"time"
)

func TestRoleImageImpactCommitmentPinsInputAcrossOutcomeReadback(t *testing.T) {
	plan := entity.RoleImageImpactPlan{Ref: "riip_fixture", ArtifactRef: "imgart_fixture", ArtifactDigest: "digest", Total: 2, State: "PREPARED", Version: 1}
	items := []entity.RoleImageImpactItem{{Ref: "b", EnvironmentRef: "env", SourceVersionRef: "old", EnvironmentVersion: 2, Outcome: "PENDING"}, {Ref: "a", EnvironmentRef: "env", SourceVersionRef: "old", EnvironmentVersion: 2, Consumer: entity.RuntimeEnvironmentConsumer{AgentRef: "agent", AgentVersion: 3, BindingRef: "binding", BindingVersion: 4}, Outcome: "PENDING"}}
	digest, err := roleImageImpactDigest(plan, "actor", items)
	if err != nil {
		t.Fatal(err)
	}
	plan.Digest, plan.State, plan.Version, plan.CreatedAt, plan.ExpiresAt = digest, "APPLIED", 2, time.Now(), time.Now().Add(time.Minute)
	items[0].Outcome, items[0].ResultEnvironmentVersionRef = "APPLIED", "new"
	if got, err := roleImageImpactDigest(plan, "actor", []entity.RoleImageImpactItem{items[1], items[0]}); err != nil || got != digest {
		t.Fatal("outcome or order changed immutable commitment")
	}
	items[1].Consumer.BindingVersion++
	if got, _ := roleImageImpactDigest(plan, "actor", items); got == digest {
		t.Fatal("changed binding pin retained commitment")
	}
	items[1].Consumer.BindingVersion--
	if got, _ := roleImageImpactDigest(plan, "foreign", items); got == digest {
		t.Fatal("actor changed without commitment")
	}
	plan.ArtifactRef = "foreign"
	if got, _ := roleImageImpactDigest(plan, "actor", items); got == digest {
		t.Fatal("artifact changed without commitment")
	}
}
