package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/gorilla/websocket"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	testTeamID    = "tttttttttttttttttttttttttt"
	testChannelID = "cccccccccccccccccccccccccc"
	testPostID    = "pppppppppppppppppppppppppp"
	testUserID    = "uuuuuuuuuuuuuuuuuuuuuuuuuu"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: -1}
}

func testAPI(transport http.RoundTripper) *model.Client4 {
	base, _ := url.Parse("https://chat.example.test")
	client := model.NewAPIv4Client(base.String())
	client.HTTPClient = scopedHTTPClient(base, transport, time.Second)
	client.SetToken("synthetic-local-fixture-token")
	return client
}

func TestCreatePostDistinguishesNoEffectAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		body              string
		transportErr      error
		noEffect, success bool
	}{
		{name: "created", status: 201, body: `{"id":"` + testPostID + `","channel_id":"` + testChannelID + `"}`, success: true},
		{name: "forbidden", status: 403, body: `{"status_code":403}`, noEffect: true},
		{name: "rate_limited", status: 429, body: `{"status_code":429}`, noEffect: true},
		{name: "server_failure", status: 503, body: `{"status_code":503}`},
		{name: "timeout_after_acceptance", transportErr: context.DeadlineExceeded},
		{name: "malformed_success", status: 201, body: `{"id":`},
		{name: "wrong_channel", status: 201, body: `{"id":"` + testPostID + `","channel_id":"foreign"}`},
		{name: "unexpected_status", status: 200, body: `{"id":"` + testPostID + `","channel_id":"` + testChannelID + `"}`},
		{name: "unexpected_thread", status: 201, body: `{"id":"` + testPostID + `","channel_id":"` + testChannelID + `","root_id":"` + testPostID + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := testAPI(roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodPost || request.URL.Path != "/api/v4/posts" {
					t.Errorf("unexpected operation: %s %s", request.Method, request.URL.Path)
				}
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return testResponse(tc.status, tc.body), nil
			}))
			post, thread, err := createPost(t.Context(), client, testChannelID, "", "fixture message", nil)
			if (err == nil) != tc.success || ConfirmedNoEffect(err) != tc.noEffect || calls != 1 {
				t.Fatalf("result = %q, %q, %v; no effect=%v; calls=%d", post, thread, err, ConfirmedNoEffect(err), calls)
			}
			if tc.success && (post != testPostID || thread != testPostID) {
				t.Fatal("missing exact post/thread receipt")
			}
		})
	}
}

func TestDeliveryPreflightConfirmsNoEffect(t *testing.T) {
	adapter, err := New(Config{CredentialDirectory: t.TempDir(), ProxyURL: "http://" + egressProxyHost, Timeout: time.Second}, localizerForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	claim := systemDeliveryFixture(t, adapter)
	claim.BaseUrl = "https://unapproved.example.test"
	_, err = adapter.Deliver(t.Context(), claim)
	if err == nil || !ConfirmedNoEffect(err) {
		t.Fatalf("preflight failure not classified: %v", err)
	}
}

func TestAcknowledgementRequiresExactAcceptedThread(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*controlplanev1.InteractionDeliveryClaim)
		valid  bool
	}{
		{name: "accepted", valid: true, mutate: func(*controlplanev1.InteractionDeliveryClaim) {}},
		{name: "foreign_team", mutate: func(claim *controlplanev1.InteractionDeliveryClaim) { claim.ExternalTeamRef = testUserID }},
		{name: "foreign_channel", mutate: func(claim *controlplanev1.InteractionDeliveryClaim) { claim.ExternalChannelRef = testUserID }},
		{name: "missing_receipt", mutate: func(claim *controlplanev1.InteractionDeliveryClaim) { claim.AcceptanceReceiptRef = "" }},
		{name: "missing_root", mutate: func(claim *controlplanev1.InteractionDeliveryClaim) { claim.ExternalRootPostRef = "" }},
		{name: "ordinary_delivery_with_thread", mutate: func(claim *controlplanev1.InteractionDeliveryClaim) { claim.CapabilityKey = "mattermost.notifications" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := &controlplanev1.InteractionDeliveryClaim{CapabilityKey: "mattermost.acknowledgements", AcceptanceReceiptRef: "receipt", ExternalTeamRef: testTeamID, ExternalChannelRef: testChannelID, ExternalRootPostRef: testPostID}
			tc.mutate(claim)
			client := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
				if request.Method != http.MethodGet {
					t.Fatal("ack preflight created an effect")
				}
				return nil, false
			})
			root, err := deliveryRoot(t.Context(), client, testChannel(), claim)
			if (err == nil) != tc.valid || tc.valid && root != testPostID {
				t.Fatalf("root=%q err=%v", root, err)
			}
		})
	}
}

func TestScopedTransportRejectsRedirectsAndForeignOrigins(t *testing.T) {
	for _, destination := range []string{"https://chat.example.test/api/v4/posts", "https://other.example.test/api/v4/posts", "http://chat.example.test/api/v4/posts", "https://chat.example.test:444/api/v4/posts"} {
		t.Run(destination, func(t *testing.T) {
			calls := 0
			base, _ := url.Parse("https://chat.example.test")
			client := scopedHTTPClient(base, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				response := testResponse(http.StatusTemporaryRedirect, "")
				response.Header.Set("Location", "https://other.example.test/credential-receiver")
				return response, nil
			}), time.Second)
			request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, destination, nil)
			request.Header.Set("Authorization", "Bearer fixture")
			response, err := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || calls > 1 {
				t.Fatalf("redirect or origin accepted: calls=%d err=%v", calls, err)
			}
			if destination != base.String()+"/api/v4/posts" && calls != 0 {
				t.Fatal("foreign request reached network")
			}
		})
	}
}

func TestScopedTransportBoundsChunkedResponses(t *testing.T) {
	client := testAPI(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(200, strings.Repeat("x", maximumResponseBytes+1)), nil
	}))
	_, _, err := client.GetTeamByName(t.Context(), "team", "")
	if err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestResolveChannelChecksExactTeamAndChannel(t *testing.T) {
	for _, tc := range []struct {
		name, teamName, channelName, channelTeam string
		archived                                 bool
		wantErr                                  bool
	}{
		{name: "exact", teamName: "team", channelName: "channel", channelTeam: testTeamID},
		{name: "team_alias", teamName: "other", channelName: "channel", channelTeam: testTeamID, wantErr: true},
		{name: "channel_alias", teamName: "team", channelName: "other", channelTeam: testTeamID, wantErr: true},
		{name: "foreign_team", teamName: "team", channelName: "channel", channelTeam: testUserID, wantErr: true},
		{name: "archived", teamName: "team", channelName: "channel", channelTeam: testTeamID, archived: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testAPI(roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var value any = &model.Team{Id: testTeamID, Name: tc.teamName}
				if strings.Contains(request.URL.Path, "/channels/") {
					channel := &model.Channel{Id: testChannelID, TeamId: tc.channelTeam, Name: tc.channelName}
					if tc.archived {
						channel.DeleteAt = 1
					}
					value = channel
				}
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return testResponse(200, string(body)), nil
			}))
			_, err := resolveChannel(t.Context(), client, &controlplanev1.InteractionSource{TeamName: "team", ChannelName: "channel"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("resolveChannel = %v", err)
			}
		})
	}
}

func TestInboundReadbackRejectsEditedForeignAndDeletedPosts(t *testing.T) {
	event := &model.Post{Id: testPostID, ChannelId: testChannelID, UserId: testUserID, Message: "approve", UpdateAt: 10}
	if !sameInboundPost(event, event, testChannelID, "bot") {
		t.Fatal("exact readback rejected")
	}
	for _, mutate := range []func(*model.Post){
		func(post *model.Post) { post.ChannelId = "foreign" },
		func(post *model.Post) { post.UserId = "bot" },
		func(post *model.Post) { post.Message = "reject" },
		func(post *model.Post) { post.UpdateAt++ },
		func(post *model.Post) { post.DeleteAt = 1 },
		func(post *model.Post) { post.RootId = testPostID },
	} {
		post := event.Clone()
		mutate(post)
		if sameInboundPost(event, post, testChannelID, "bot") {
			t.Fatal("mismatched inbound post accepted")
		}
	}
}

func TestHostnameAndProxyHaveNoFallback(t *testing.T) {
	for _, host := range []string{"", "localhost", "127.0.0.1", "chat.example.test.", "*.example.test", "chat..test", "chat_example.test", "chat.example.test:443", "chat.example.test/path", "chat.example.test\n"} {
		if validHostname(host) {
			t.Errorf("invalid hostname accepted: %q", host)
		}
	}
	_, err := New(Config{CredentialDirectory: t.TempDir(), ProxyURL: "http://other.example.test:8080", Timeout: time.Second}, localizerForTest(t))
	if !errors.Is(err, errConfiguration) {
		t.Fatalf("alternate proxy accepted: %v", err)
	}
}

func TestCloseSocketDrainsBlockedSDKReader(t *testing.T) {
	written := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		for index := 0; index < 8; index++ {
			if err := connection.WriteMessage(websocket.TextMessage, []byte(`{"event":"posted","data":{"post":"{}"},"broadcast":{"channel_id":"channel"},"seq":1}`)); err != nil {
				break
			}
		}
		close(written)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	socket, err := model.NewWebSocketClient4("ws"+strings.TrimPrefix(server.URL, "http"), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	socket.EventChannel = make(chan *model.WebSocketEvent, 1)
	socket.Listen()
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("fixture events not written")
	}
	done := make(chan struct{})
	go func() { closeSocket(socket); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SDK reader not joined after cancellation")
	}
	if _, ok := <-socket.EventChannel; ok {
		t.Fatal("SDK event channel still open")
	}
	if _, ok := <-socket.ResponseChannel; ok {
		t.Fatal("SDK response channel still open")
	}
}
