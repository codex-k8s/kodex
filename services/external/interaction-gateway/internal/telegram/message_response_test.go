package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestSendResponseKeepsChannelIdentityAndScalarPresence exercises channel sender semantics and nullable scalar failures through fresh provider preflight, one committed Begin and the production send consumer.
// TestSendResponseKeepsChannelIdentityAndScalarPresence проверяет channel sender semantics и nullable scalar failures через свежий provider preflight, один committed Begin и production send consumer.
func TestSendResponseKeepsChannelIdentityAndScalarPresence(t *testing.T) {
	for _, scenario := range []struct {
		name, chatType string
		topic          bool
		valid          bool
		change         func(map[string]any)
	}{
		{name: "channel_sender_chat_without_from", chatType: "channel", valid: true},
		{name: "channel_with_exact_from_and_sender", chatType: "channel", valid: true, change: func(m map[string]any) { m["from"] = map[string]any{"id": 123456, "is_bot": true} }},
		{name: "channel_with_exact_bot_from", chatType: "channel", valid: true, change: func(m map[string]any) {
			delete(m, "sender_chat")
			m["from"] = map[string]any{"id": 123456, "is_bot": true}
		}},
		{name: "supergroup_no_topic", chatType: "supergroup", valid: true},
		{name: "supergroup_exact_topic", chatType: "supergroup", topic: true, valid: true},
		{name: "channel_foreign_sender", chatType: "channel", change: func(m map[string]any) { m["sender_chat"].(map[string]any)["id"] = -1009999 }},
		{name: "channel_wrong_sender_type", chatType: "channel", change: func(m map[string]any) { m["sender_chat"].(map[string]any)["type"] = "supergroup" }},
		{name: "channel_missing_sender", chatType: "channel", change: func(m map[string]any) { delete(m, "sender_chat") }},
		{name: "channel_null_sender", chatType: "channel", change: func(m map[string]any) { m["sender_chat"] = nil }},
		{name: "channel_null_from", chatType: "channel", change: func(m map[string]any) { m["from"] = nil }},
		{name: "channel_foreign_from", chatType: "channel", change: func(m map[string]any) { m["from"] = map[string]any{"id": 999, "is_bot": true} }},
		{name: "non_channel_missing_from", chatType: "supergroup", change: func(m map[string]any) { delete(m, "from") }},
		{name: "non_channel_null_from", chatType: "supergroup", change: func(m map[string]any) { m["from"] = nil }},
		{name: "non_channel_sender_chat", chatType: "supergroup", change: func(m map[string]any) { m["sender_chat"] = map[string]any{"id": -1001234, "type": "supergroup"} }},
		{name: "no_topic_explicit_null", chatType: "supergroup", change: func(m map[string]any) { m["message_thread_id"] = nil }},
		{name: "no_topic_explicit_zero", chatType: "supergroup", change: func(m map[string]any) { m["message_thread_id"] = 0 }},
		{name: "topic_explicit_null", chatType: "supergroup", topic: true, change: func(m map[string]any) { m["message_thread_id"] = nil }},
		{name: "topic_missing", chatType: "supergroup", topic: true, change: func(m map[string]any) { delete(m, "message_thread_id") }},
		{name: "topic_string", chatType: "supergroup", topic: true, change: func(m map[string]any) { m["message_thread_id"] = "77" }},
		{name: "topic_foreign", chatType: "supergroup", topic: true, change: func(m map[string]any) { m["message_thread_id"] = 78 }},
		{name: "null_message_id", chatType: "supergroup", change: func(m map[string]any) { m["message_id"] = nil }},
		{name: "null_chat", chatType: "supergroup", change: func(m map[string]any) { m["chat"] = nil }},
		{name: "null_chat_id", chatType: "supergroup", change: func(m map[string]any) { m["chat"].(map[string]any)["id"] = nil }},
		{name: "null_chat_type", chatType: "supergroup", change: func(m map[string]any) { m["chat"].(map[string]any)["type"] = nil }},
		{name: "wrong_chat_type", chatType: "supergroup", change: func(m map[string]any) { m["chat"].(map[string]any)["type"] = "channel" }},
		{name: "null_text", chatType: "supergroup", change: func(m map[string]any) { m["text"] = nil }},
		{name: "null_from_id", chatType: "supergroup", change: func(m map[string]any) { m["from"].(map[string]any)["id"] = nil }},
		{name: "null_from_is_bot", chatType: "supergroup", change: func(m map[string]any) { m["from"].(map[string]any)["is_bot"] = nil }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter, claim := telegramAdapterBudgetFixture(t, 30*time.Second)
			if !scenario.topic {
				snapshot, err := claimedSnapshot(claim)
				if err != nil {
					t.Fatal("prepare no-topic snapshot")
				}
				snapshot.Configuration["topic"] = integrationpackage.TelegramNoTopic
				claim.Telegram.TopicId = 0
				claim.Telegram.InputDigest, err = integrationpackage.TelegramDeliveryDigest(snapshot)
				if err != nil {
					t.Fatal("prepare no-topic digest")
				}
			}
			sends, begun := 0, 0
			adapter.newTransport = func() http.RoundTripper {
				return fixtureTransport(func(request *http.Request) (*http.Response, error) {
					var result any
					switch strings.TrimPrefix(request.URL.Path, "/bot"+fixtureToken+"/") {
					case "getMe":
						result = map[string]any{"id": 123456, "is_bot": true}
					case "getChat":
						result = map[string]any{"id": -1001234, "type": scenario.chatType, "is_forum": scenario.topic, "permissions": map[string]any{"can_send_messages": true}}
					case "getChatMember":
						result = map[string]any{"status": "administrator", "can_post_messages": true, "user": map[string]any{"id": 123456, "is_bot": true}}
					case "sendMessage":
						sends++
						var input map[string]any
						if json.NewDecoder(request.Body).Decode(&input) != nil || input["chat_id"] != float64(-1001234) || begun != 1 {
							t.Fatal("send escaped exact target or committed Begin")
						}
						message := map[string]any{"message_id": 123, "chat": map[string]any{"id": -1001234, "type": scenario.chatType}, "text": input["text"]}
						if scenario.topic {
							message["message_thread_id"] = 77
						}
						if scenario.chatType == "channel" {
							message["sender_chat"] = map[string]any{"id": -1001234, "type": "channel"}
						} else {
							message["from"] = map[string]any{"id": 123456, "is_bot": true}
						}
						if scenario.change != nil {
							scenario.change(message)
						}
						result = message
					default:
						t.Fatal("unexpected provider method")
					}
					raw, _ := json.Marshal(map[string]any{"ok": true, "result": result})
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
				})
			}
			receipt, err := adapter.Deliver(t.Context(), claim, func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
				begun++
				return &cp.BeginInteractionDeliveryResponse{DeliveryRef: claim.GetDeliveryRef(), InputDigest: claim.GetTelegram().GetInputDigest(), Allowed: true}, nil
			})
			if begun != 1 || sends != 1 {
				t.Fatal("response validation repeated or skipped the fenced effect")
			}
			if scenario.valid {
				if err != nil || receipt.GetMessageId() != 123 || receipt.GetTopicId() != claim.GetTelegram().GetTopicId() {
					t.Fatalf("valid provider response rejected: %v", err)
				}
			} else if err == nil || receipt != nil || ConfirmedNoEffect(err) {
				t.Fatalf("unverified response became success or retryable no-effect: %v", err)
			}
		})
	}
}
