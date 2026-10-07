package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestPublishedRunMessagePreservesLiteralText(t *testing.T) {
	for _, text := range []string{"RUN_STATE_CANCELLED", "i18n:PRIVATE_TEST", "Проверяю текущие инструменты."} {
		projection, err := ProtoMap(&cp.RunMessage{Ref: "msg_fixture01", Phase: cp.RunMessagePhase_RUN_MESSAGE_PHASE_COMMENTARY, Revision: 1, Text: text, Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_ORDINARY}})
		if err != nil {
			t.Fatal(err)
		}
		LocalizeSafeErrors(projection, func(string) string { return "must-not-localize" })
		if projection["text"] != text || projection["phase"] != "COMMENTARY" || projection["revision"] != float64(1) {
			t.Fatalf("published message changed: %#v", projection)
		}
	}
}

func TestMessageSourceProjectionRejectsUnknownAndMissing(t *testing.T) {
	for _, origin := range []cp.MessageOrigin{cp.MessageOrigin_MESSAGE_ORIGIN_ORDINARY, cp.MessageOrigin_MESSAGE_ORIGIN_CALLBACK_CONTINUATION} {
		message := &cp.RunMessage{Ref: "trn_fixture01", Phase: cp.RunMessagePhase_RUN_MESSAGE_PHASE_USER, Text: "Safe public text", Revision: 1, Source: &cp.MessageSource{Origin: origin}}
		projection, err := ProtoMap(message)
		if err != nil || projection["source"].(map[string]any)["origin"] == nil {
			t.Fatal("message source was not projected", err)
		}
	}
	for _, source := range []*cp.MessageSource{nil, {}, {Origin: 99}} {
		if _, err := ProtoMap(&cp.RunMessage{Source: source}); err == nil {
			t.Fatal("unknown or missing source accepted")
		}
		if _, err := ProtoMap(&cp.AssistantTurn{Source: source}); err == nil {
			t.Fatal("unknown or missing turn source accepted")
		}
	}
}

func TestCallbackMessageSourceRequiresExactPublicBinding(t *testing.T) {
	valid := func() *cp.RunEvent {
		return &cp.RunEvent{
			Actor:     &cp.RunEventActor{Kind: cp.RunEventActorKind_RUN_EVENT_ACTOR_KIND_AGENT},
			Execution: &cp.RunEventExecution{RunRef: "run_callback01", NodeRef: "nod_callback01", SessionRef: "ses_callback01", TurnRef: "trn_callback01", TurnNumber: 3, Attempt: 2},
			Message: &cp.RunMessage{Ref: "trn_callback01", Phase: cp.RunMessagePhase_RUN_MESSAGE_PHASE_USER,
				Text: "i18n:CALLBACK_CONTINUATION_PUBLIC", Revision: 1,
				Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_CALLBACK_CONTINUATION}},
		}
	}
	if _, err := ProtoMap(valid()); err != nil {
		t.Fatal("owner callback projection rejected", err)
	}
	for _, mutate := range []func(*cp.RunEvent){
		func(event *cp.RunEvent) { event.Actor.Kind = cp.RunEventActorKind_RUN_EVENT_ACTOR_KIND_USER },
		func(event *cp.RunEvent) { event.Execution = nil },
		func(event *cp.RunEvent) { event.Execution.TurnRef = "trn_other01" },
		func(event *cp.RunEvent) { event.Message.Phase = cp.RunMessagePhase_RUN_MESSAGE_PHASE_FINAL },
	} {
		event := valid()
		mutate(event)
		if _, err := ProtoMap(event); err == nil {
			t.Fatal("invalid callback binding accepted")
		}
	}
	if _, err := ProtoMap(&cp.AssistantTurn{Role: "USER", Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_CALLBACK_CONTINUATION}}); err == nil {
		t.Fatal("callback source accepted as genuine owner turn")
	}
}
