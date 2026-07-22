package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
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

func TestControlSurfaceReconcilesBothAmbiguousUpdateOutcomes(t *testing.T) {
	card := statusservice.MattermostCard{
		ChannelID: "channel-1", RootPostID: "root-1", PostID: "status-post-1",
		Message: "matter-codex agent turn status #notrigger",
		Props:   map[string]any{"matter_codex_event": "agent_status", "session_key": "session-1", "turn_id": int64(2)},
		Actions: []statusservice.MattermostCardAction{{
			ID: "stopturn", Name: "Завершить остановку", Context: map[string]any{
				"kind": "agent_turn", "action": "recover_stop_turn", "capability": "synthetic-capability",
			},
		}},
	}
	for _, test := range []struct {
		name        string
		applyUpdate bool
	}{
		{name: "update not applied", applyUpdate: false},
		{name: "update applied response lost", applyUpdate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := &mattermostmodel.Post{
				Id: card.PostID, ChannelId: card.ChannelID, RootId: card.RootPostID,
				Message: "previous status card", Props: map[string]any{"matter_codex_event": "agent_status"},
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				switch request.Method {
				case http.MethodPut:
					var attempted mattermostmodel.Post
					if err := json.NewDecoder(request.Body).Decode(&attempted); err != nil {
						t.Errorf("decode Mattermost update request: %v", err)
					}
					if test.applyUpdate {
						actual = &attempted
						actual.Id = card.PostID
						actual.ChannelId = card.ChannelID
						actual.RootId = card.RootPostID
						actual.GetProps()[mattermostmodel.PostPropsFromBot] = "true"
					}
					writer.WriteHeader(http.StatusBadGateway)
					_, _ = writer.Write([]byte(`{"id":"synthetic","message":"ambiguous update","status_code":502}`))
				case http.MethodGet:
					if err := json.NewEncoder(writer).Encode(actual); err != nil {
						t.Errorf("encode Mattermost post response: %v", err)
					}
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			surface := NewControlSurface(server.URL, "synthetic-token", "")
			if _, err := surface.UpdateThreadCard(context.Background(), card); err == nil {
				t.Fatal("ambiguous UpdateThreadCard() error = nil")
			}
			ref, applied, err := surface.ReconcileThreadCardUpdate(context.Background(), card)
			if err != nil || applied != test.applyUpdate {
				t.Fatalf("reconcile ref=%#v applied=%t/%t error=%v", ref, applied, test.applyUpdate, err)
			}
			if applied && (ref.ChannelID != card.ChannelID || ref.PostID != card.PostID) {
				t.Fatalf("reconciled binding=%#v", ref)
			}
		})
	}
}

func TestControlSurfaceInexactUpdateNeverActivatesCapability(t *testing.T) {
	mutateAttachments := func(post *mattermostmodel.Post, mutate func([]map[string]any)) {
		t.Helper()
		encoded, err := json.Marshal(post.GetProps()["attachments"])
		if err != nil {
			t.Fatalf("encode attachments: %v", err)
		}
		var attachments []map[string]any
		if err := json.Unmarshal(encoded, &attachments); err != nil {
			t.Fatalf("decode attachments: %v", err)
		}
		mutate(attachments)
		post.GetProps()["attachments"] = attachments
	}
	for _, test := range []struct {
		name   string
		mutate func(*mattermostmodel.Post)
	}{
		{name: "unexpected matter_codex prop", mutate: func(post *mattermostmodel.Post) {
			post.GetProps()["matter_codex_unexpected"] = "foreign"
		}},
		{name: "missing prop", mutate: func(post *mattermostmodel.Post) {
			delete(post.GetProps(), "status")
		}},
		{name: "changed prop", mutate: func(post *mattermostmodel.Post) {
			post.GetProps()["session_key"] = "foreign-session"
		}},
		{name: "changed attachment", mutate: func(post *mattermostmodel.Post) {
			mutateAttachments(post, func(attachments []map[string]any) { attachments[0]["text"] = "foreign text" })
		}},
		{name: "changed action", mutate: func(post *mattermostmodel.Post) {
			mutateAttachments(post, func(attachments []map[string]any) {
				actions := attachments[0]["actions"].([]any)
				actions[0].(map[string]any)["name"] = "Foreign action"
			})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var actual *mattermostmodel.Post
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				switch request.Method {
				case http.MethodPut:
					var attempted mattermostmodel.Post
					if err := json.NewDecoder(request.Body).Decode(&attempted); err != nil {
						t.Errorf("decode Mattermost update request: %v", err)
					}
					actual = attempted.Clone()
					actual.Id = "status-post-1"
					actual.ChannelId = "channel-1"
					actual.RootId = "root-1"
					test.mutate(actual)
					writer.WriteHeader(http.StatusBadGateway)
					_, _ = writer.Write([]byte(`{"id":"synthetic","message":"ambiguous update","status_code":502}`))
				case http.MethodGet:
					if actual == nil {
						t.Error("GetPost called before UpdatePost")
						writer.WriteHeader(http.StatusNotFound)
						return
					}
					if err := json.NewEncoder(writer).Encode(actual); err != nil {
						t.Errorf("encode Mattermost post response: %v", err)
					}
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			repository := newControlSurfaceCapabilityRepository()
			security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
				Repository: repository, Admission: controlSurfaceAllowedAdmission{},
			})
			publisher := statusservice.NewSecuredMattermostThreadPublisher(NewControlSurface(server.URL, "synthetic-token", ""), security)
			card := statusservice.MattermostCard{
				ChannelID: "channel-1", RootPostID: "root-1", PostID: "status-post-1",
				ActionURL: "https://bot.example/actions", Message: "matter-codex agent turn status #notrigger",
				Props: map[string]any{
					"matter_codex_event": "agent_status", "session_key": "session-1", "turn_id": int64(2), "status": "canceled",
				},
				Color: "#9aa4b2", Title: "Agent", Text: "Canceled",
				Interaction: statusservice.MattermostCardInteraction{
					Actor: statusservice.AuthenticatedActor{UserID: "actor-1", UserName: "owner"},
					Scope: statusservice.InteractionScope{Workspace: "1", Session: "session-1"},
				},
				Actions: []statusservice.MattermostCardAction{{
					ID: "stopturn", Name: "Завершить остановку", Style: "danger",
					Context: map[string]any{
						"kind": "agent_turn", "action": "recover_stop_turn", "turn_ids": "2",
						"resource_type": "agent_session_turn", "resource_id": "2",
					},
				}},
			}
			if _, err := publisher.UpdateThreadCard(context.Background(), card); err == nil {
				t.Fatal("inexact UpdateThreadCard() error = nil")
			}
			if state := repository.onlyState(); state != securityrepo.CapabilityStateRevoked {
				t.Fatalf("inexact capability state=%q", state)
			}
		})
	}
}

type controlSurfaceCapabilityRepository struct {
	mu     sync.Mutex
	states map[string]securityrepo.CapabilityState
}

func newControlSurfaceCapabilityRepository() *controlSurfaceCapabilityRepository {
	return &controlSurfaceCapabilityRepository{states: map[string]securityrepo.CapabilityState{}}
}

func (repository *controlSurfaceCapabilityRepository) IssueInteractionCapability(_ context.Context, input securityrepo.IssueCapabilityInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.states[string(input.TokenHash)] = input.State
	return nil
}

func (*controlSurfaceCapabilityRepository) CheckInteractionCapability(context.Context, securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	return securityrepo.Capability{}, securityrepo.ErrCapabilityInactive
}

func (*controlSurfaceCapabilityRepository) ConsumeInteractionCapability(context.Context, securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	return securityrepo.Capability{}, securityrepo.ErrCapabilityInactive
}

func (repository *controlSurfaceCapabilityRepository) TransitionInteractionCapabilities(_ context.Context, input securityrepo.TransitionCapabilitiesInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, tokenHash := range input.TokenHashes {
		if repository.states[string(tokenHash)] != input.From {
			return securityrepo.ErrCapabilityInactive
		}
	}
	for _, tokenHash := range input.TokenHashes {
		repository.states[string(tokenHash)] = input.To
	}
	return nil
}

func (*controlSurfaceCapabilityRepository) AdmitExistingClusterAdmin(context.Context, securityrepo.ClusterAdminAdmissionInput) (bool, error) {
	return false, nil
}

func (repository *controlSurfaceCapabilityRepository) onlyState() securityrepo.CapabilityState {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, state := range repository.states {
		return state
	}
	return ""
}

type controlSurfaceAllowedAdmission struct{}

func (controlSurfaceAllowedAdmission) Admit(context.Context, statusservice.InteractionAdmissionRequest) statusservice.InteractionAdmissionDecision {
	return statusservice.InteractionAdmissionDecision{Status: statusservice.AdmissionAllowed}
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

func TestControlSurfaceReconcilesAutomationOwnerAttentionIdentity(t *testing.T) {
	input := statusservice.MattermostThreadPostInput{
		ChannelID:     "channel-1",
		RootPostID:    "root-1",
		Message:       "immutable callback\n\n#notrigger",
		IdempotencyID: "aaaaaaaaaaaaaaaaaaaaaaaaaa",
		Props: map[string]any{
			"matter_codex_event":                 "automation_owner_attention",
			"matter_codex_callback_delivery_id":  "aaaaaaaaaaaaaaaaaaaaaaaaaa",
			"matter_codex_automation_run_id":     "scheduled-run-11111111111111111111111111111111",
			"matter_codex_process_run_id":        "process-1",
			"matter_codex_human_decision_status": "pending",
		},
	}
	exactPost := &mattermostmodel.Post{
		Id: "attention-post-1", ChannelId: input.ChannelID, RootId: input.RootPostID,
		Message: input.Message, Props: callbackServerProps(input.Props), PendingPostId: input.IdempotencyID,
	}
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
		t.Fatalf("owner attention reconcile ref=%#v error=%v create_calls=%d", ref, err, postCalls.Load())
	}
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
