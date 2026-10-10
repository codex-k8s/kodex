package controlplaneapi

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func encodeTaskSessionTestCursor(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func taskSessionFixture() *controlplanev1.AssistantTaskSessionPage {
	return &controlplanev1.AssistantTaskSessionPage{RunRef: "run_selected123", ProjectRef: "prj_owned123", SessionRef: "ses_selected123", Title: "Проверить прежнюю задачу", State: controlplanev1.RunState_RUN_STATE_FAILED, RunVersion: 4, ResultSummary: "Публичный результат", SafeErrorCode: "SYNTHETIC_ERROR", SessionStorageState: "ARCHIVED", SourceSha256: strings.Repeat("a", 64), Messages: []*controlplanev1.AssistantTaskPublishedMessage{
		{EventRef: "evt_message123", MessageRef: "msg_final123", Phase: controlplanev1.AssistantTaskMessagePhase_ASSISTANT_TASK_MESSAGE_PHASE_FINAL, Text: "Опубликованный итог", Origin: controlplanev1.AssistantTaskMessageOrigin_ASSISTANT_TASK_MESSAGE_ORIGIN_ORDINARY, SourceRunRef: "run_selected123", SourceRunVersion: 4, SessionRef: "ses_selected123", NodeRef: "nod_source123", TurnRef: "trn_source123", TurnNumber: 1, Attempt: 1, EventSequence: 48, MessageRevision: 1},
	}}
}

func TestTaskSessionTypedProjectionDigestAndBudget(t *testing.T) {
	t.Parallel()
	page := taskSessionFixture()
	if err := SealTaskSessionPage(page); err != nil {
		t.Fatal(err)
	}
	value, err := ProjectTaskSessionPage(page)
	if err != nil || value.State != "FAILED" || value.Coverage != "SELECTED_SESSION_PUBLIC_MESSAGES" || value.Messages[0].Phase != "FINAL" {
		t.Fatal("typed failed result lost public message")
	}
	raw, _ := json.Marshal(value)
	if len(raw) > TaskSessionMaximumProjectionBytes || !strings.Contains(string(raw), `"run_version":"4"`) {
		t.Fatal("typed projection lost exact decimal or budget")
	}
	before := page.ProjectionSha256
	if SealTaskSessionPage(page) != nil || page.ProjectionSha256 != before {
		t.Fatal("same source produced different projection")
	}
	page.Messages[0].Text = "Изменено"
	if _, err := ProjectTaskSessionPage(page); err == nil {
		t.Fatal("tampered public text passed commitment")
	}
}

func TestTaskSessionTypedProjectionRejectsUnknownAndPrivateShapes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*controlplanev1.AssistantTaskSessionPage)
	}{
		{"state unknown", func(p *controlplanev1.AssistantTaskSessionPage) { p.State = 999 }},
		{"state zero", func(p *controlplanev1.AssistantTaskSessionPage) { p.State = 0 }},
		{"phase unknown", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].Phase = 999 }},
		{"origin unknown", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].Origin = 999 }},
		{"storage unknown", func(p *controlplanev1.AssistantTaskSessionPage) { p.SessionStorageState = "UNKNOWN" }},
		{"foreign session", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].SessionRef = "ses_foreign123" }},
		{"invalid revision", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].MessageRevision = 2 }},
		{"invalid source version", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].SourceRunVersion = 0 }},
		{"invalid UTF8", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].Text = "bad\xff" }},
		{"unknown top field", func(p *controlplanev1.AssistantTaskSessionPage) {
			p.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		}},
		{"unknown message field", func(p *controlplanev1.AssistantTaskSessionPage) {
			p.Messages[0].ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		}},
		{"raw callback input", func(p *controlplanev1.AssistantTaskSessionPage) {
			p.Messages[0].Phase = 1
			p.Messages[0].MessageRef = p.Messages[0].TurnRef
			p.Messages[0].Origin = 2
			p.Messages[0].Text = "PRIVATE_CALLBACK_PAYLOAD"
		}},
		{"truncated without cursor", func(p *controlplanev1.AssistantTaskSessionPage) { p.Truncated = true }},
		{"cursor without truncated", func(p *controlplanev1.AssistantTaskSessionPage) { p.NextCursor = "bad" }},
		{"duplicate event", func(p *controlplanev1.AssistantTaskSessionPage) {
			p.Messages = append(p.Messages, proto.Clone(p.Messages[0]).(*controlplanev1.AssistantTaskPublishedMessage))
		}},
		{"message cardinality", func(p *controlplanev1.AssistantTaskSessionPage) {
			p.Messages = make([]*controlplanev1.AssistantTaskPublishedMessage, 11)
		}},
		{"encoded overflow", func(p *controlplanev1.AssistantTaskSessionPage) { p.Messages[0].Text = strings.Repeat("\x01", 100000) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := taskSessionFixture()
			test.mutate(page)
			if SealTaskSessionPage(page) == nil {
				t.Fatal("unsafe typed projection accepted")
			}
		})
	}
}

func TestTaskSessionCursorAndCanonicalCallback(t *testing.T) {
	page := taskSessionFixture()
	page.Messages[0].Phase = 1
	page.Messages[0].MessageRef = page.Messages[0].TurnRef
	page.Messages[0].Origin = 2
	page.Messages[0].Text = "i18n:CALLBACK_CONTINUATION_PUBLIC"
	raw, _ := json.Marshal(TaskSessionCursor{1, strings.Repeat("b", 64), page.SourceSha256, 1})
	page.NextCursor = encodeTaskSessionTestCursor(raw)
	page.Truncated = true
	if SealTaskSessionPage(page) != nil {
		t.Fatal("canonical callback/cursor rejected")
	}
	for _, token := range []string{"bad", encodeTaskSessionTestCursor([]byte(`{"Version":999,"Binding":"a","Source":"b","Offset":1}`)), encodeTaskSessionTestCursor(append(raw, []byte("{}")...)), encodeTaskSessionTestCursor(append([]byte(" "), raw...)), encodeTaskSessionTestCursor([]byte(strings.Replace(string(raw), `"Offset":1`, `"Offset":1,"Offset":1`, 1)))} {
		if _, err := ReadTaskSessionCursor(token); err == nil {
			t.Fatal("malformed cursor accepted")
		}
	}
}
