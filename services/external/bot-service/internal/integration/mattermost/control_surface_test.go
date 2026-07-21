package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestControlSurfaceUsesBoundedHTTPTransportForEveryToken(t *testing.T) {
	config := HTTPClientConfig{
		Timeout:               7 * time.Second,
		DialTimeout:           2 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		IdleConnTimeout:       45 * time.Second,
	}
	surface := NewControlSurfaceWithHTTPConfig("https://mattermost.example", "bot-token", "admin-token", config)
	if surface.client.HTTPClient.Timeout != config.Timeout || surface.adminClient.HTTPClient.Timeout != config.Timeout {
		t.Fatal("основной или административный Client4 не получил общий HTTP timeout")
	}
	transport, ok := surface.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("HTTP transport = %T", surface.httpClient.Transport)
	}
	if transport.TLSHandshakeTimeout != config.TLSHandshakeTimeout ||
		transport.ResponseHeaderTimeout != config.ResponseHeaderTimeout ||
		transport.IdleConnTimeout != config.IdleConnTimeout || transport.MaxConnsPerHost <= 0 {
		t.Fatalf("неполные transport bounds: %#v", transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Mattermost transport не должен передавать bearer token через environment proxy")
	}
	tokenClient := surface.clientWithToken("role-token")
	if tokenClient.HTTPClient != surface.httpClient || tokenClient.HTTPClient.Timeout == 0 {
		t.Fatal("token-specific Client4 обошёл общий bounded HTTP client")
	}
}

func TestControlSurfaceReconcilesStatusCardAfterAmbiguousCreateAndRestart(t *testing.T) {
	card := statusservice.MattermostCard{
		ChannelID: "channel-1", RootPostID: "root-1", Message: "matter-codex agent turn status #notrigger",
		Props: map[string]any{
			"matter_codex_event": "agent_status", "matter_codex_status_delivery_id": "cccccccccccccccccccccccccc",
			"session_key": "session-1", "role_id": int64(1), "turn_id": int64(2), "run_id": "run-1", "status": "queued",
		},
	}
	var getCalls atomic.Int64
	var postCalls atomic.Int64
	var created *mattermostmodel.Post
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/posts/root-1/thread"):
			call := getCalls.Add(1)
			list := &mattermostmodel.PostList{Posts: map[string]*mattermostmodel.Post{}, Order: []string{}}
			if call >= 3 && created != nil {
				list.Posts[created.Id] = created
				list.Order = append(list.Order, created.Id)
			}
			if err := json.NewEncoder(writer).Encode(list); err != nil {
				t.Errorf("encode Mattermost thread response: %v", err)
			}
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/posts"):
			postCalls.Add(1)
			var post mattermostmodel.Post
			if err := json.NewDecoder(request.Body).Decode(&post); err != nil {
				t.Errorf("decode Mattermost status create request: %v", err)
			}
			post.Id = "durable-status-post"
			created = &post
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte(`{"id":"synthetic","message":"ambiguous create","status_code":502}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	first := NewControlSurface(server.URL, "synthetic-token", "")
	if _, err := first.ReconcileOrPostThreadCard(context.Background(), card); err == nil {
		t.Fatal("первая неоднозначная публикация не вернула ошибку")
	}
	if created == nil || created.PendingPostId != "cccccccccccccccccccccccccc" || postCalls.Load() != 1 {
		t.Fatalf("created=%#v post_calls=%d", created, postCalls.Load())
	}
	restarted := NewControlSurface(server.URL, "synthetic-token", "")
	ref, err := restarted.ReconcileOrPostThreadCard(context.Background(), card)
	if err != nil || ref.PostID != "durable-status-post" || postCalls.Load() != 1 {
		t.Fatalf("restart ref=%#v error=%v post_calls=%d", ref, err, postCalls.Load())
	}
}

func TestControlSurfaceReconcilesDeterministicCallbackPublication(t *testing.T) {
	input := statusservice.MattermostThreadPostInput{
		ChannelID: "channel-1", RootPostID: "root-1", Message: "immutable callback\n\n#notrigger",
		IdempotencyID: "aaaaaaaaaaaaaaaaaaaaaaaaaa",
		Props: map[string]any{
			"matter_codex_event":                   "agent_cross_chat_callback",
			"matter_codex_callback_delivery_id":    "aaaaaaaaaaaaaaaaaaaaaaaaaa",
			"matter_codex_callback_payload_sha256": strings.Repeat("b", 64),
		},
	}
	exactPost := &mattermostmodel.Post{
		Id: "post-1", ChannelId: input.ChannelID, RootId: input.RootPostID,
		Message: input.Message, Props: callbackServerProps(input.Props), PendingPostId: input.IdempotencyID,
	}

	for name, mutate := range map[string]func(*statusservice.MattermostThreadPostInput){
		"missing client identity": func(candidate *statusservice.MattermostThreadPostInput) {
			delete(candidate.Props, "matter_codex_callback_delivery_id")
		},
		"foreign client identity": func(candidate *statusservice.MattermostThreadPostInput) {
			candidate.Props["matter_codex_callback_delivery_id"] = "bbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			candidate.Props = callbackServerProps(input.Props)
			delete(candidate.Props, mattermostmodel.PostPropsFromBot)
			mutate(&candidate)
			server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post { return nil }, false)
			defer server.Close()
			surface := NewControlSurface(server.URL, "synthetic-token", "")
			if _, err := surface.ReconcileOrPostThreadMessage(context.Background(), candidate); err == nil || postCalls.Load() != 0 {
				t.Fatalf("invalid client identity error=%v create_calls=%d", err, postCalls.Load())
			}
		})
	}

	t.Run("existing exact post", func(t *testing.T) {
		server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post { return []*mattermostmodel.Post{exactPost} }, false)
		defer server.Close()
		surface := NewControlSurface(server.URL, "synthetic-token", "")
		ref, err := surface.ReconcileOrPostThreadMessage(context.Background(), input)
		if err != nil || ref.PostID != exactPost.Id || postCalls.Load() != 0 {
			t.Fatalf("reconcile ref=%#v error=%v create_calls=%d", ref, err, postCalls.Load())
		}
	})

	t.Run("foreign payload fails closed", func(t *testing.T) {
		foreign := exactPost.Clone()
		foreign.Message = "foreign callback"
		server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post { return []*mattermostmodel.Post{foreign} }, false)
		defer server.Close()
		surface := NewControlSurface(server.URL, "synthetic-token", "")
		if _, err := surface.ReconcileOrPostThreadMessage(context.Background(), input); err == nil || postCalls.Load() != 0 {
			t.Fatalf("foreign reconcile error=%v create_calls=%d", err, postCalls.Load())
		}
	})

	t.Run("server-owned preview props are ignored", func(t *testing.T) {
		withPreview := exactPost.Clone()
		withPreview.Props = map[string]any{}
		for key, value := range input.Props {
			withPreview.Props[key] = value
		}
		withPreview.Props[mattermostmodel.PostPropsFromBot] = "true"
		withPreview.Props["previewed_post"] = "server generated preview"
		server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post { return []*mattermostmodel.Post{withPreview} }, false)
		defer server.Close()
		surface := NewControlSurface(server.URL, "synthetic-token", "")
		if _, err := surface.ReconcileOrPostThreadMessage(context.Background(), input); err != nil || postCalls.Load() != 0 {
			t.Fatalf("server props reconcile error=%v create_calls=%d", err, postCalls.Load())
		}
	})

	for name, mutate := range map[string]func(map[string]any){
		"missing server-owned from_bot": func(props map[string]any) { delete(props, mattermostmodel.PostPropsFromBot) },
		"wrong server-owned value":      func(props map[string]any) { props[mattermostmodel.PostPropsFromBot] = "false" },
		"wrong server-owned type":       func(props map[string]any) { props[mattermostmodel.PostPropsFromBot] = true },
		"unexpected client prop":        func(props map[string]any) { props["matter_codex_foreign"] = "value" },
	} {
		t.Run(name, func(t *testing.T) {
			foreign := exactPost.Clone()
			foreign.Props = callbackServerProps(exactPost.GetProps())
			mutate(foreign.Props)
			server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post { return []*mattermostmodel.Post{foreign} }, false)
			defer server.Close()
			surface := NewControlSurface(server.URL, "synthetic-token", "")
			if _, err := surface.ReconcileOrPostThreadMessage(context.Background(), input); err == nil || postCalls.Load() != 0 {
				t.Fatalf("invalid props reconcile error=%v create_calls=%d", err, postCalls.Load())
			}
		})
	}

	t.Run("duplicate identity fails closed", func(t *testing.T) {
		duplicate := exactPost.Clone()
		duplicate.Id = "post-2"
		server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post {
			return []*mattermostmodel.Post{exactPost, duplicate}
		}, false)
		defer server.Close()
		surface := NewControlSurface(server.URL, "synthetic-token", "")
		if _, err := surface.ReconcileOrPostThreadMessage(context.Background(), input); err == nil || postCalls.Load() != 0 {
			t.Fatalf("duplicate reconcile error=%v create_calls=%d", err, postCalls.Load())
		}
	})

	t.Run("network ambiguity reconciles without duplicate", func(t *testing.T) {
		server, postCalls := callbackMattermostServer(t, func(getCall int) []*mattermostmodel.Post {
			if getCall == 1 {
				return nil
			}
			return []*mattermostmodel.Post{exactPost}
		}, true)
		defer server.Close()
		surface := NewControlSurface(server.URL, "synthetic-token", "")
		ref, err := surface.ReconcileOrPostThreadMessage(context.Background(), input)
		if err != nil || ref.PostID != exactPost.Id || postCalls.Load() != 1 {
			t.Fatalf("ambiguous reconcile ref=%#v error=%v create_calls=%d", ref, err, postCalls.Load())
		}
	})

	t.Run("create carries deterministic pending id and exact payload", func(t *testing.T) {
		server, postCalls := callbackMattermostServer(t, func(int) []*mattermostmodel.Post { return nil }, false)
		defer server.Close()
		surface := NewControlSurface(server.URL, "synthetic-token", "")
		ref, err := surface.ReconcileOrPostThreadMessage(context.Background(), input)
		if err != nil || ref.ChannelID != input.ChannelID || postCalls.Load() != 1 {
			t.Fatalf("create ref=%#v error=%v create_calls=%d", ref, err, postCalls.Load())
		}
	})
}

func callbackMattermostServer(t *testing.T, threadPosts func(int) []*mattermostmodel.Post, failCreate bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var getCalls atomic.Int64
	var postCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/posts/root-1/thread"):
			posts := threadPosts(int(getCalls.Add(1)))
			postList := &mattermostmodel.PostList{Posts: map[string]*mattermostmodel.Post{}, Order: []string{}}
			for _, post := range posts {
				postList.Posts[post.Id] = post
				postList.Order = append(postList.Order, post.Id)
			}
			if err := json.NewEncoder(writer).Encode(postList); err != nil {
				t.Errorf("encode Mattermost thread response: %v", err)
			}
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/posts"):
			postCalls.Add(1)
			var post mattermostmodel.Post
			if err := json.NewDecoder(request.Body).Decode(&post); err != nil {
				t.Errorf("decode Mattermost create request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			if post.PendingPostId != "aaaaaaaaaaaaaaaaaaaaaaaaaa" || post.ChannelId != "channel-1" || post.RootId != "root-1" || post.Message != "immutable callback\n\n#notrigger" {
				t.Errorf("create payload не совпадает с immutable plan: %#v", &post)
			}
			if failCreate {
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte(`{"id":"synthetic","message":"synthetic failure","status_code":500}`))
				return
			}
			post.Id = "created-post"
			post.Props = callbackServerProps(post.GetProps())
			if err := json.NewEncoder(writer).Encode(&post); err != nil {
				t.Errorf("encode Mattermost create response: %v", err)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	return server, &postCalls
}

func callbackServerProps(clientProps map[string]any) map[string]any {
	props := make(map[string]any, len(clientProps)+1)
	for key, value := range clientProps {
		props[key] = value
	}
	props[mattermostmodel.PostPropsFromBot] = "true"
	return props
}
