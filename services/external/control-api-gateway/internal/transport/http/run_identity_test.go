package httptransport

import (
	"encoding/json"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// systemRunIdentityFixture builds a synthetic system-assistant run with explicit
// organization/conversation/assistant pins and no project/profile by default. It contains identity
// metadata only, without a live execution or grant.
// systemRunIdentityFixture собирает синтетический запуск системного помощника с явными
// organization/conversation/assistant pins и без project/profile по умолчанию. Это только identity
// metadata без живого исполнения и grant.
func systemRunIdentityFixture() *cp.Run {
	return &cp.Run{
		Ref: "run_fixture01", Source: cp.RunSource_RUN_SOURCE_SYSTEM_ASSISTANT,
		Target: &cp.RunTarget{Target: &cp.RunTarget_SystemAssistantRef{SystemAssistantRef: "agt_fixture01"}, DisplayName: "Kodex", TargetVersion: 3},
		AssistantPin: &cp.AssistantRunPin{Scope: cp.AssistantScope_ASSISTANT_SCOPE_SYSTEM,
			OrganizationRef: "org_fixture01", ConversationRef: "acon_fixture01", AssistantRef: "agt_fixture01"},
	}
}

// TestAssistantRunIdentityIsClosedAndPreserved checks system and project assistant runs retain
// exact target/version and context-pin fields through graph projection, with absent optional
// system project/profile omitted and valid public JSON.
// TestAssistantRunIdentityIsClosedAndPreserved проверяет сохранение точных target/version и
// context-pin полей system и project помощников через проекцию графа, отсутствие пустых optional
// system project/profile и корректный публичный JSON.
func TestAssistantRunIdentityIsClosedAndPreserved(t *testing.T) {
	t.Parallel()
	for _, scope := range []cp.AssistantScope{cp.AssistantScope_ASSISTANT_SCOPE_SYSTEM, cp.AssistantScope_ASSISTANT_SCOPE_PROJECT} {
		run := systemRunIdentityFixture()
		run.AssistantPin.Scope = scope
		if scope == cp.AssistantScope_ASSISTANT_SCOPE_PROJECT {
			run.ProjectRef, run.AssistantPin.ProjectRef, run.AssistantPin.ProfileRef = "prj_fixture01", "prj_fixture01", "pap_fixture01"
		}
		value, err := messageMap(&cp.GetRunGraphResponse{Run: run})
		if err != nil {
			t.Fatal(err)
		}
		body := value["run"].(map[string]any)
		pin := body["assistantPin"].(map[string]any)
		target := body["target"].(map[string]any)
		if target["type"] != "SYSTEM_ASSISTANT" || target["ref"] != "agt_fixture01" || target["version"] != float64(3) || pin["organizationRef"] != "org_fixture01" || pin["conversationRef"] != "acon_fixture01" {
			t.Fatalf("assistant identity was not preserved: %v", body)
		}
		if scope == cp.AssistantScope_ASSISTANT_SCOPE_SYSTEM {
			if _, present := body["projectRef"]; present {
				t.Fatal("empty SYSTEM project reference was emitted")
			}
			if _, present := pin["profileRef"]; present {
				t.Fatal("SYSTEM profile reference was emitted")
			}
		}
		encoded, err := json.Marshal(body)
		if err != nil || !json.Valid(encoded) {
			t.Fatal("invalid public identity JSON")
		}
	}
}

// TestAssistantRunIdentityRejectsMixedPins mutates missing/unknown assistant pins, foreign
// organization context, profile/source and target disguises and requires graph projection failure
// rather than accepting mixed run identity.
// TestAssistantRunIdentityRejectsMixedPins меняет отсутствующие и неизвестные assistant pins,
// чужой context, profile/source и маскировку target и требует отказа проекции графа вместо
// принятия смешанной identity запуска.
func TestAssistantRunIdentityRejectsMixedPins(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*cp.Run){
		"missing pin":          func(r *cp.Run) { r.AssistantPin = nil },
		"unknown scope":        func(r *cp.Run) { r.AssistantPin.Scope = cp.AssistantScope_ASSISTANT_SCOPE_UNSPECIFIED },
		"none scope":           func(r *cp.Run) { r.AssistantPin.Scope = cp.AssistantScope_ASSISTANT_SCOPE_NONE },
		"missing organization": func(r *cp.Run) { r.AssistantPin.OrganizationRef = "" },
		"missing conversation": func(r *cp.Run) { r.AssistantPin.ConversationRef = "" },
		"foreign agent":        func(r *cp.Run) { r.AssistantPin.AssistantRef = "agt_foreign01" },
		"foreign context":      func(r *cp.Run) { r.ProjectRef = "prj_foreign01" },
		"system profile":       func(r *cp.Run) { r.AssistantPin.ProfileRef = "pap_fixture01" },
		"project without pin":  func(r *cp.Run) { r.AssistantPin.Scope = cp.AssistantScope_ASSISTANT_SCOPE_PROJECT },
		"wrong source":         func(r *cp.Run) { r.Source = cp.RunSource_RUN_SOURCE_CONTROL_CENTER },
		"masked as agent":      func(r *cp.Run) { r.Target.Target = &cp.RunTarget_AgentRef{AgentRef: "agt_fixture01"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			run := proto.Clone(systemRunIdentityFixture()).(*cp.Run)
			mutate(run)
			if _, err := messageMap(&cp.GetRunGraphResponse{Run: run}); err == nil {
				t.Fatal("invalid pin accepted")
			}
		})
	}
}
