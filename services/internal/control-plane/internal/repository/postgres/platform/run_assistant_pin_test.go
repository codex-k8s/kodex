package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
)

func TestRunAssistantPinRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()
	run := entity.Run{Target: entity.RunTarget{Ref: "agt_fixture01"}}
	pin := entity.AssistantRunPin{Scope: "SYSTEM", OrganizationRef: "org_fixture01", ConversationRef: "acon_fixture01", AssistantRef: "agt_fixture01"}
	if !validRunAssistantPin(run, pin) {
		t.Fatal("valid system pin rejected")
	}
	for _, scope := range []string{"", "NONE", "UNKNOWN", "PROJECT"} {
		p := pin
		p.Scope = scope
		if validRunAssistantPin(run, p) {
			t.Fatalf("invalid scope %s accepted", scope)
		}
	}
	pin.Scope, pin.ProjectRef, pin.ProfileRef = "PROJECT", "prj_fixture01", "pap_fixture01"
	if validRunAssistantPin(run, pin) {
		t.Fatal("foreign project accepted")
	}
	run.ProjectRef = pin.ProjectRef
	if !validRunAssistantPin(run, pin) {
		t.Fatal("valid project pin rejected")
	}
	pin.AssistantRef = "agt_foreign01"
	if validRunAssistantPin(run, pin) {
		t.Fatal("foreign agent accepted")
	}
}
