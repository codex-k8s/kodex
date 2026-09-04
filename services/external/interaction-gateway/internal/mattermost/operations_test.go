package mattermost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/mattermost/mattermost/server/public/model"
)

const testFileID = "ffffffffffffffffffffffffff"

func jsonResponse(t *testing.T, status int, value any) *http.Response {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return testResponse(status, string(body))
}

func operationFixture(t *testing.T, intercept func(*http.Request) (*http.Response, bool)) *model.Client4 {
	t.Helper()
	post := &model.Post{Id: testPostID, ChannelId: testChannelID, UserId: testUserID, Message: "lead", UpdateAt: 1, FileIds: []string{testFileID}}
	file := &model.FileInfo{Id: testFileID, PostId: testPostID, ChannelId: testChannelID, Name: "fixture.txt", Size: 5, MimeType: "text/plain"}
	return testAPI(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if intercept != nil {
			if response, ok := intercept(request); ok {
				return response, nil
			}
		}
		path := request.URL.Path
		switch {
		case path == "/api/v4/teams/"+testTeamID:
			return jsonResponse(t, 200, &model.Team{Id: testTeamID, Name: "team", DisplayName: "Team"}), nil
		case path == "/api/v4/channels/"+testChannelID+"/members":
			return jsonResponse(t, 200, model.ChannelMembers{{ChannelId: testChannelID, UserId: testUserID, Roles: "channel_user"}}), nil
		case path == "/api/v4/channels/"+testChannelID+"/posts", path == "/api/v4/posts/"+testPostID+"/thread", path == "/api/v4/teams/"+testTeamID+"/posts/search":
			if strings.HasSuffix(path, "/search") {
				var params model.SearchParameter
				if json.NewDecoder(request.Body).Decode(&params) != nil || params.Terms == nil || *params.Terms != "in:channel \"lead\"" || params.IsOrSearch == nil || *params.IsOrSearch || params.IncludeDeletedChannels == nil || *params.IncludeDeletedChannels {
					t.Error("search escaped channel scope")
				}
			}
			return jsonResponse(t, 200, &model.PostList{Order: []string{testPostID}, Posts: map[string]*model.Post{testPostID: post}}), nil
		case path == "/api/v4/posts/"+testPostID:
			return jsonResponse(t, 200, post), nil
		case path == "/api/v4/posts/"+testPostID+"/files/info":
			return jsonResponse(t, 200, []*model.FileInfo{file}), nil
		case path == "/api/v4/files/"+testFileID+"/info":
			return jsonResponse(t, 200, file), nil
		case path == "/api/v4/files/"+testFileID:
			if request.Header.Get("Range") != "bytes=1-3" {
				t.Error("file read did not request exact range")
			}
			response := testResponse(206, "ell")
			response.Header.Set("Content-Range", "bytes 1-3/5")
			return response, nil
		case path == "/api/v4/posts/"+testPostID+"/reactions":
			return jsonResponse(t, 200, []*model.Reaction{{PostId: testPostID, UserId: testUserID, EmojiName: "like"}}), nil
		case path == "/api/v4/users/me":
			return jsonResponse(t, 200, &model.User{Id: testUserID}), nil
		case path == "/api/v4/posts" && request.Method == http.MethodPost:
			var input model.Post
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.ChannelId != testChannelID || input.Message != "new message" {
				t.Error("wrong post request")
			}
			input.Id, input.UserId, input.UpdateAt = testPostID, testUserID, 1
			return jsonResponse(t, 201, &input), nil
		case path == "/api/v4/posts/"+testPostID+"/patch" && request.Method == http.MethodPut:
			var input model.PostPatch
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.Message == nil || *input.Message != "new message" {
				t.Fatal("wrong update request")
			}
			updated := post.Clone()
			updated.Message = *input.Message
			return jsonResponse(t, 200, updated), nil
		case path == "/api/v4/reactions" && request.Method == http.MethodPost:
			var input model.Reaction
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.UserId != testUserID || input.PostId != testPostID || input.EmojiName != "like" {
				t.Error("wrong reaction request")
			}
			return jsonResponse(t, 201, &input), nil
		case path == "/api/v4/users/"+testUserID+"/posts/"+testPostID+"/reactions/like" && request.Method == http.MethodDelete:
			return testResponse(200, `{"status":"OK"}`), nil
		default:
			t.Errorf("unexpected SDK operation: %s %s", request.Method, path)
			return testResponse(404, `{"status_code":404}`), nil
		}
	}))
}

func TestEveryCallableMattermostOperationHasTypedExecution(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["mattermost"]
	count := 0
	for _, capability := range definition.Spec.Capabilities {
		if !capability.CallableByAgent() {
			continue
		}
		count++
		t.Run(capability.Operation, func(t *testing.T) {
			input := operationInput{PostID: testPostID, FileID: testFileID, Message: "new message", Query: "lead", Emoji: "like"}
			if capability.Operation == "mattermost.file.read" {
				input.Offset, input.Limit = 1, 3
			}
			output, reference, attempted, err := executeOperation(t.Context(), operationFixture(t, nil), &model.Channel{Id: testChannelID, TeamId: testTeamID, Name: "channel", DisplayName: "Channel"}, capability.Operation, input)
			if err != nil || reference == "" || attempted != (capability.Risk != "READ") {
				t.Fatalf("execution = %v, %q, %v, %v", output, reference, attempted, err)
			}
			raw, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := capability.ValidateOutput(raw); err != nil {
				t.Fatalf("output violates package contract: %v; body=%s", err, raw)
			}
		})
	}
	if count != 16 {
		t.Fatalf("tested %d callable operations, want 16", count)
	}
}

func TestOperationsNeverMutateForeignPost(t *testing.T) {
	for _, operation := range []string{"mattermost.post.update", "mattermost.reaction.add", "mattermost.reaction.remove", "mattermost.file.read", "mattermost.thread.read"} {
		t.Run(operation, func(t *testing.T) {
			mutations := 0
			client := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
				if request.Method != http.MethodGet {
					mutations++
				}
				if request.URL.Path == "/api/v4/posts/"+testPostID {
					return jsonResponse(t, 200, &model.Post{Id: testPostID, ChannelId: testTeamID, UserId: testUserID, Message: "foreign"}), true
				}
				return nil, false
			})
			_, _, attempted, err := executeOperation(t.Context(), client, &model.Channel{Id: testChannelID, TeamId: testTeamID, Name: "channel"}, operation, operationInput{PostID: testPostID, FileID: testFileID, Message: "new message", Emoji: "like"})
			if err == nil || attempted || mutations != 0 {
				t.Fatalf("foreign post reached effect: %v %v %d", err, attempted, mutations)
			}
		})
	}
}

func TestSearchCannotOverrideChannelAndRejectsForeignResults(t *testing.T) {
	channel := &model.Channel{Id: testChannelID, TeamId: testTeamID, Name: "channel"}
	for _, query := range []string{"in:other", "lead\" OR in:other", "from:user", "lead\n-in:channel"} {
		client := operationFixture(t, func(*http.Request) (*http.Response, bool) {
			t.Fatal("unsafe search reached network")
			return nil, false
		})
		if _, _, _, err := executeOperation(t.Context(), client, channel, "mattermost.post.search", operationInput{Query: query}); err == nil {
			t.Fatal("unsafe search accepted")
		}
	}
	client := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
		if strings.HasSuffix(request.URL.Path, "/search") {
			return jsonResponse(t, 200, &model.PostList{Order: []string{testPostID}, Posts: map[string]*model.Post{testPostID: {Id: testPostID, ChannelId: testTeamID, UserId: testUserID}}}), true
		}
		return nil, false
	})
	if _, _, _, err := executeOperation(t.Context(), client, channel, "mattermost.post.search", operationInput{Query: "lead"}); err == nil {
		t.Fatal("foreign search projection accepted")
	}
}

func TestFileRangeMustMatchRequestedAttachment(t *testing.T) {
	for _, contentRange := range []string{"", "bytes 0-2/5", "bytes 1-3/500"} {
		t.Run(fmt.Sprintf("range_%s", contentRange), func(t *testing.T) {
			client := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
				if request.URL.Path == "/api/v4/files/"+testFileID {
					response := testResponse(206, "ell")
					response.Header.Set("Content-Range", contentRange)
					return response, true
				}
				return nil, false
			})
			if _, _, _, err := executeOperation(t.Context(), client, &model.Channel{Id: testChannelID, TeamId: testTeamID}, "mattermost.file.read", operationInput{PostID: testPostID, FileID: testFileID, Offset: 1, Limit: 3}); err == nil {
				t.Fatal("wrong file range accepted")
			}
		})
	}
}

func TestAgentCannotModifyHumanGateMessage(t *testing.T) {
	client := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
		if request.URL.Path == "/api/v4/posts/"+testPostID {
			post := &model.Post{Id: testPostID, ChannelId: testChannelID, UserId: testUserID}
			post.AddProp(gateRefProperty, "gate_fixture")
			return jsonResponse(t, 200, post), true
		}
		if request.Method != http.MethodGet {
			t.Fatal("gate post mutation reached network")
		}
		return nil, false
	})
	if _, _, attempted, err := executeOperation(t.Context(), client, &model.Channel{Id: testChannelID, TeamId: testTeamID}, "mattermost.post.update", operationInput{PostID: testPostID, Message: "new message"}); err == nil || attempted {
		t.Fatal("agent modified Human Gate message")
	}
}
