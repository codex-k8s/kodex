package mattermost

import (
	"testing"

	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

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
