package mattermost

import (
	"encoding/json"
	"net/http"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/mattermost/mattermost/server/public/model"
)

func TestGateDeliveryCarriesExactTupleWithoutNumericPrecisionLoss(t *testing.T) {
	gate := &gateContext{ref: "gate_fixture", runRef: "run_fixture", version: 9007199254740993}
	client := testAPI(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var post model.Post
		if err := json.NewDecoder(request.Body).Decode(&post); err != nil {
			t.Fatal(err)
		}
		if post.GetProp(gateVersionProperty) != "9007199254740993" || post.GetProp(gateRefProperty) != gate.ref || post.GetProp(gateRunProperty) != gate.runRef {
			t.Fatal("gate tuple changed in HTTP request")
		}
		post.Id = testPostID
		body, err := json.Marshal(&post)
		if err != nil {
			t.Fatal(err)
		}
		return testResponse(201, string(body)), nil
	}))
	if _, _, err := createPost(t.Context(), client, testChannelID, "", "gate message", gate); err != nil {
		t.Fatal(err)
	}
}

func TestGateReadbackMustMatchDelivery(t *testing.T) {
	gate := &gateContext{ref: "gate_fixture", runRef: "run_fixture", version: 1}
	for _, mutate := range []func(*model.Post){
		func(post *model.Post) { post.AddProp(gateVersionProperty, "2") },
		func(post *model.Post) { post.AddProp(gateRunProperty, "run_other") },
		func(post *model.Post) { post.AddProp(gateRefProperty, "gate_other") },
		func(post *model.Post) { post.AddProp(gateVersionProperty, float64(1)) },
	} {
		client := testAPI(roundTripFunc(func(*http.Request) (*http.Response, error) {
			post := &model.Post{Id: testPostID, ChannelId: testChannelID}
			gate.addToPost(post)
			mutate(post)
			body, err := json.Marshal(post)
			if err != nil {
				t.Fatal(err)
			}
			return testResponse(201, string(body)), nil
		}))
		_, _, err := createPost(t.Context(), client, testChannelID, "", "gate message", gate)
		if err == nil || ConfirmedNoEffect(err) {
			t.Fatal("mismatched gate delivery treated as successful or retryable")
		}
	}
}

func TestGateReplyReadsRootAndRequiresBotAuthor(t *testing.T) {
	gate := &gateContext{ref: "gate_fixture", runRef: "run_fixture", version: 7}
	for _, tc := range []struct {
		name, author, channel string
		version               any
		wantGate, wantErr     bool
	}{
		{name: "bot_exact", author: testUserID, channel: testChannelID, version: "7", wantGate: true},
		{name: "user_forged_props", author: testTeamID, channel: testChannelID, version: "7"},
		{name: "foreign_channel", author: testUserID, channel: testTeamID, version: "7", wantErr: true},
		{name: "numeric_version", author: testUserID, channel: testChannelID, version: float64(7), wantErr: true},
		{name: "negative_version", author: testUserID, channel: testChannelID, version: "-1", wantErr: true},
		{name: "alternate_encoding", author: testUserID, channel: testChannelID, version: "07", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := testAPI(roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != "/api/v4/posts/"+testPostID {
					t.Fatal("root not resolved through exact read path")
				}
				root := &model.Post{Id: testPostID, UserId: tc.author, ChannelId: tc.channel}
				gate.addToPost(root)
				root.AddProp(gateVersionProperty, tc.version)
				body, err := json.Marshal(root)
				if err != nil {
					t.Fatal(err)
				}
				return testResponse(200, string(body)), nil
			}))
			actual, err := readGateContext(t.Context(), client, &model.Post{RootId: testPostID}, testChannelID, testUserID)
			if (err != nil) != tc.wantErr || (actual != nil) != tc.wantGate || calls != 1 {
				t.Fatalf("gate=%v err=%v calls=%d", actual, err, calls)
			}
			if actual != nil && *actual != *gate {
				t.Fatal("wrong gate tuple returned")
			}
		})
	}
}

func TestGateClaimsRejectIncompleteAndUnexpectedTuple(t *testing.T) {
	for _, claim := range []*controlplanev1.InteractionDeliveryClaim{
		{CapabilityKey: "mattermost.gate_decisions"},
		{CapabilityKey: "mattermost.gate_decisions", GateRef: "gate_fixture", GateVersion: 1},
		{CapabilityKey: "mattermost.gate_decisions", GateRef: "gate/fixture", GateVersion: 1, RunRef: "run_fixture"},
		{CapabilityKey: "mattermost.notifications", GateRef: "gate_fixture", GateVersion: 1, RunRef: "run_fixture"},
	} {
		if _, err := gateFromClaim(claim); err == nil {
			t.Fatal("invalid gate claim accepted")
		}
	}
	if gate, err := gateFromClaim(&controlplanev1.InteractionDeliveryClaim{CapabilityKey: "mattermost.notifications", RunRef: "run_fixture"}); gate != nil || err != nil {
		t.Fatal("ordinary notification requires gate")
	}
}
