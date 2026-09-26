package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestMembershipSelfActions(t *testing.T) {
	owner := scope{actorRef: "user_owner", role: "OWNER"}
	self := entity.Membership{User: entity.User{Ref: owner.actorRef}, Role: "OWNER", Active: true}
	other := entity.Membership{User: entity.User{Ref: "user_other"}, Role: "MEMBER", Active: true}

	if actions := projectMembershipActions(owner, self); len(actions) != 0 {
		t.Fatalf("owner received self project membership actions: %v", actions)
	}
	if actions := platformMembershipActions(owner, self); len(actions) != 0 {
		t.Fatalf("owner received self platform membership actions: %v", actions)
	}
	if actions := projectMembershipActions(owner, other); len(actions) != 2 || actions[0] != "EDIT" || actions[1] != "REVOKE" {
		t.Fatalf("owner did not receive other member actions: %v", actions)
	}
}
