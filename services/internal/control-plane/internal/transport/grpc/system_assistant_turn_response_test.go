package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

func TestAssistantTurnResponsePreservesScopeAndOptionalSystemAssistant(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		scope     string
		project   string
		profile   string
		assistant *entity.SystemAssistant
	}{
		{name: "PROJECT без системного профиля", scope: "PROJECT", project: "prj_fixture", profile: "asstp_fixture"},
		{name: "SYSTEM с системным профилем", scope: "SYSTEM", assistant: &entity.SystemAssistant{Ref: "agt_fixture", Version: 7, Name: "Системный помощник", Ready: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conversation := entity.AssistantConversation{
				Ref: "cnv_fixture", Version: 3, Title: "Проверка ответа", State: "ACTIVE",
				AssistantScope: test.scope, AssistantRef: "agt_fixture", ProjectRef: test.project, AssistantProfileRef: test.profile,
				Turns: []entity.AssistantTurn{{Ref: "trn_fixture", Sequence: 1, Actor: "USER", Content: "Проверка", State: "QUEUED", RunRef: "run_fixture", RunVersion: 1}},
			}
			response := castAssistantTurnResult(command.Result{Conversation: &conversation, Assistant: test.assistant})
			if !proto.Equal(response.GetConversation(), castConversation(conversation)) {
				t.Fatal("accepted conversation scope, pins or turn were lost")
			}
			if test.assistant == nil {
				if response.GetAssistant() != nil {
					t.Fatal("PROJECT response contains a synthetic SYSTEM assistant")
				}
			} else if !proto.Equal(response.GetAssistant(), castAssistant(*test.assistant)) {
				t.Fatal("SYSTEM response lost the authoritative assistant")
			}
			wire, err := proto.Marshal(response)
			if err != nil {
				t.Fatalf("marshal turn response: %v", err)
			}
			received := &controlplanev1.AddAssistantTurnResponse{}
			if err := proto.Unmarshal(wire, received); err != nil {
				t.Fatalf("unmarshal turn response: %v", err)
			}
			if !proto.Equal(response, received) {
				t.Fatal("turn response or optional assistant presence changed during protobuf transmission")
			}
		})
	}
}
