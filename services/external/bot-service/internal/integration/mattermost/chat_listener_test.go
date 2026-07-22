package mattermost

import (
	"context"
	"encoding/json"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type fakeMattermostUserNameResolver struct {
	userName string
	calls    int
}

type captureChatPostHandler struct {
	command statusservice.ChatPostCommand
}

func (handler *captureChatPostHandler) HandleChatPost(_ context.Context, command statusservice.ChatPostCommand) statusservice.ChatRunResult {
	handler.command = command
	return statusservice.ChatRunResult{Ignored: true}
}

func (resolver *fakeMattermostUserNameResolver) ResolveMattermostUserName(_ context.Context, _ string) (string, error) {
	resolver.calls++
	return resolver.userName, nil
}

func TestMattermostWebSocketURL(t *testing.T) {
	tests := map[string]string{
		"http://mattermost:8065":  "ws://mattermost:8065",
		"https://mattermost.test": "wss://mattermost.test",
		"ws://mattermost/ws":      "ws://mattermost/ws",
	}
	for input, want := range tests {
		got, err := mattermostWebSocketURL(input)
		if err != nil {
			t.Fatalf("mattermostWebSocketURL(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("mattermostWebSocketURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWebsocketEventPostParsesStringPayload(t *testing.T) {
	data := map[string]any{
		"post": `{"id":"post-1","channel_id":"channel-1","user_id":"owner","message":"hello"}`,
	}

	post, ok := websocketEventPost(data)

	if !ok || post.Id != "post-1" || post.ChannelId != "channel-1" || post.Message != "hello" {
		t.Fatalf("post = %#v ok=%v", post, ok)
	}
}

func TestWebsocketEventPostIgnoresSystemPosts(t *testing.T) {
	data := map[string]any{
		"post": &mattermostmodel.Post{Id: "post-1", ChannelId: "channel-1", Type: mattermostmodel.PostTypeJoinChannel},
	}

	_, ok := websocketEventPost(data)

	if ok {
		t.Fatal("system post should be ignored")
	}
}

func TestChatListenerResolvesAndCachesPostSenderName(t *testing.T) {
	resolver := &fakeMattermostUserNameResolver{userName: "owner"}
	listener := &ChatListener{userNameResolver: resolver}

	first := listener.resolveUserName(context.Background(), "user-1", nil)
	second := listener.resolveUserName(context.Background(), "user-1", nil)

	if first != "owner" || second != "owner" {
		t.Fatalf("resolved names = %q, %q", first, second)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
}

func TestChatListenerUsesSenderNameFromWebSocketEvent(t *testing.T) {
	resolver := &fakeMattermostUserNameResolver{userName: "fallback"}
	listener := &ChatListener{userNameResolver: resolver}

	got := listener.resolveUserName(context.Background(), "user-1", map[string]any{"sender_name": "owner"})

	if got != "owner" {
		t.Fatalf("resolved name = %q, want owner", got)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
}

func TestChatListenerCarriesServerOwnedPostCreateAt(t *testing.T) {
	handler := &captureChatPostHandler{}
	listener := &ChatListener{handler: handler, botUserID: "bot-user"}
	post := &mattermostmodel.Post{
		Id: "owner-response", ChannelId: "channel-1", RootId: "root-1", UserId: "owner-user",
		Message: "Продолжить", CreateAt: 2_000,
	}
	raw, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	event := mattermostmodel.NewWebSocketEvent(mattermostmodel.WebsocketEventPosted, "team-1", "channel-1", "owner-user", nil, "")
	event.Add("post", string(raw))
	event.Add("sender_name", "owner")

	listener.handleEvent(context.Background(), event)

	if handler.command.PostID != "owner-response" || handler.command.MattermostCreateAt != 2_000 || handler.command.ChannelID != "channel-1" || handler.command.RootPostID != "root-1" {
		t.Fatalf("команда потеряла server-owned ordering proof: %#v", handler.command)
	}
}
