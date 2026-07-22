package mattermost

import (
	"context"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type capturingChatPostHandler struct {
	command statusservice.ChatPostCommand
}

func (handler *capturingChatPostHandler) HandleChatPost(_ context.Context, command statusservice.ChatPostCommand) statusservice.ChatRunResult {
	handler.command = command
	return statusservice.ChatRunResult{Ignored: true}
}

type fakeMattermostUserNameResolver struct {
	userName string
	calls    int
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

func TestChatListenerCarriesServerOwnedPostCreateAt(t *testing.T) {
	handler := &capturingChatPostHandler{}
	listener := &ChatListener{handler: handler, botUserID: "bot-user"}
	event := mattermostmodel.NewWebSocketEvent(mattermostmodel.WebsocketEventPosted, "", "channel-1", "owner-id", nil, "")
	event = event.SetData(map[string]any{
		"post": `{"id":"reply-1","channel_id":"channel-1","root_id":"root-1","user_id":"owner-id","create_at":2001,"message":"Продолжить"}`,
	})

	listener.handleEvent(context.Background(), event)

	if handler.command.PostID != "reply-1" || handler.command.CreateAt != 2_001 {
		t.Fatalf("transport command=%#v", handler.command)
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
