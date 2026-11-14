package telegram

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestCurrentScopeUsesFreshIdentityAndOwnMembership exercises one exact group/chat/topic and proves revoked membership is re-read on the next call instead of cached as permission.
// TestCurrentScopeUsesFreshIdentityAndOwnMembership проверяет точные group/chat/topic и доказывает повторное чтение отозванного членства вместо permission cache.
func TestCurrentScopeUsesFreshIdentityAndOwnMembership(t *testing.T) {
	calls := []string{}
	revoked := false
	scope := integrationpackage.TelegramConfiguration{BotID: 123456, ChatID: -1001234, TopicID: 77}
	client, _ := newAPIClient(fixtureToken, fixtureTransport(func(request *http.Request) (*http.Response, error) {
		method := strings.TrimPrefix(request.URL.Path, "/bot"+fixtureToken+"/")
		calls = append(calls, method)
		raw, _ := io.ReadAll(request.Body)
		var input map[string]int64
		if json.Unmarshal(raw, &input) != nil {
			t.Fatal("invalid typed provider input")
		}
		result := ""
		switch method {
		case "getMe":
			if len(input) != 0 {
				t.Fatal("identity accepted a payload actor")
			}
			result = `{"id":123456,"is_bot":true,"username":"FixtureBot"}`
		case "getChat":
			if len(input) != 1 || input["chat_id"] != scope.ChatID {
				t.Fatal("chat lookup escaped scope")
			}
			result = `{"id":-1001234,"type":"supergroup","is_forum":true,"permissions":{"can_send_messages":true}}`
		case "getChatMember":
			if len(input) != 2 || input["chat_id"] != scope.ChatID || input["user_id"] != scope.BotID {
				t.Fatal("membership lookup accepted another subject")
			}
			result = `{"status":"member","user":{"id":123456,"is_bot":true}}`
			if revoked {
				result = `{"status":"left","user":{"id":123456,"is_bot":true}}`
			}
		default:
			t.Fatal("scope preflight attempted an unselected provider operation")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":` + result + `}`))}, nil
	}), time.Second)
	if _, err := client.currentScope(t.Context(), scope); err != nil {
		t.Fatal("exact synthetic scope rejected")
	}
	revoked = true
	if _, err := client.currentScope(t.Context(), scope); err == nil || !ConfirmedNoEffect(err) {
		t.Fatal("revoked membership remained authorized")
	}
	if strings.Join(calls, ",") != "getMe,getChat,getChatMember,getMe,getChat,getChatMember" {
		t.Fatal("fresh permission path changed")
	}
}

// TestCurrentScopeRejectsForeignIdentityChatAndTopic prevents canonical bot/chat mismatches and unsupported topic selection before any send.
// TestCurrentScopeRejectsForeignIdentityChatAndTopic отклоняет подмену bot/chat и неподдержанный topic до любой отправки.
func TestCurrentScopeRejectsForeignIdentityChatAndTopic(t *testing.T) {
	for _, fixture := range []struct{ name, identity, chat string }{
		{name: "foreign_bot", identity: `{"id":123457,"is_bot":true}`, chat: `{"id":-1001234,"type":"supergroup","is_forum":true}`},
		{name: "human", identity: `{"id":123456,"is_bot":false}`, chat: `{"id":-1001234,"type":"supergroup","is_forum":true}`},
		{name: "foreign_chat", identity: `{"id":123456,"is_bot":true}`, chat: `{"id":-1009999,"type":"supergroup","is_forum":true}`},
		{name: "non_forum", identity: `{"id":123456,"is_bot":true}`, chat: `{"id":-1001234,"type":"supergroup","is_forum":false}`},
		{name: "unsupported_direct_messages", identity: `{"id":123456,"is_bot":true}`, chat: `{"id":-1001234,"type":"supergroup","is_forum":true,"is_direct_messages":true}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			calls := 0
			client, _ := newAPIClient(fixtureToken, fixtureTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				result := fixture.chat
				if strings.HasSuffix(request.URL.Path, "/getMe") {
					result = fixture.identity
				}
				if strings.HasSuffix(request.URL.Path, "/sendMessage") {
					t.Fatal("invalid scope dispatched a message")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":` + result + `}`))}, nil
			}), time.Second)
			scope := integrationpackage.TelegramConfiguration{BotID: 123456, ChatID: -1001234, TopicID: 77}
			if _, err := client.currentScope(t.Context(), scope); err == nil || !ConfirmedNoEffect(err) || calls > 2 {
				t.Fatal("invalid scope was not closed before dispatch")
			}
		})
	}
}

// TestMemberPermissionMappingKeepsChannelWritesExplicit separates ordinary text membership from channel administrator posting permission and rejects unknown roles.
// TestMemberPermissionMappingKeepsChannelWritesExplicit разделяет обычное text membership и channel administrator posting permission, отклоняя неизвестные роли.
func TestMemberPermissionMappingKeepsChannelWritesExplicit(t *testing.T) {
	for _, fixture := range []struct {
		chat   scopedChat
		member scopedMember
		want   bool
	}{
		{chat: scopedChat{Type: "channel"}, member: scopedMember{Status: "member"}},
		{chat: scopedChat{Type: "channel"}, member: scopedMember{Status: "administrator"}},
		{chat: scopedChat{Type: "channel"}, member: scopedMember{Status: "administrator", CanPostMessages: true}, want: true},
		{chat: scopedChat{Type: "supergroup"}, member: scopedMember{Status: "restricted", IsMember: true, CanSendMessages: true}, want: true},
		{chat: scopedChat{Type: "supergroup"}, member: scopedMember{Status: "restricted", CanSendMessages: true}},
		{chat: scopedChat{Type: "supergroup"}, member: scopedMember{Status: "member"}},
		{chat: scopedChat{Type: "supergroup"}, member: scopedMember{Status: "administrator"}, want: true},
		{chat: scopedChat{Type: "supergroup"}, member: scopedMember{Status: "future_role", CanSendMessages: true}},
	} {
		if memberCanSend(fixture.chat, fixture.member) != fixture.want {
			t.Fatal("closed provider permission mapping changed")
		}
	}
}
