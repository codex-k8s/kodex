package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRuntimeRevisionDiff(t *testing.T) {
	previous := entity.RuntimeRevisionPublicProjection{Identity: entity.PublicRuntimeRevisionIdentity{Ref: "rrev_previous", SessionRef: "sess_same"},
		Model: entity.RuntimeRevisionDiffValue{Ref: "old-model"}, Environment: entity.RuntimeRevisionDiffValue{Ref: "env_same", Version: 1, Digest: "old"}}
	current := previous
	current.Identity.Ref = "rrev_current"
	current.Model.Ref = "new-model"
	current.Environment.Version = 2
	current.Environment.Digest = "new"
	diff := runtimeRevisionDiff(current, &previous)
	if diff.Previous == nil || diff.Previous.Ref != previous.Identity.Ref || diff.Current.Ref != current.Identity.Ref || len(diff.Changes) != 2 {
		t.Fatalf("unexpected identities or changes: %+v", diff)
	}
	if diff.Changes[0].Component != "MODEL" || diff.Changes[0].Previous.Ref != "old-model" || diff.Changes[0].Current.Ref != "new-model" ||
		diff.Changes[1].Component != "ENVIRONMENT" || diff.Changes[1].Previous.Version != 1 || diff.Changes[1].Current.Version != 2 {
		t.Fatalf("incorrect typed changes: %+v", diff.Changes)
	}
	if unchanged := runtimeRevisionDiff(current, &current); len(unchanged.Changes) != 0 {
		t.Fatal("unchanged projection has changes")
	}
	first := runtimeRevisionDiff(current, nil)
	if first.Previous != nil || len(first.Changes) != 11 {
		t.Fatal("first revision must contain additions without predecessor")
	}
	for _, change := range first.Changes {
		if change.Previous != nil || change.Current == nil {
			t.Fatal("invalid first revision presence")
		}
	}
}
