package mattermost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	"github.com/mattermost/mattermost/server/public/model"
)

func configureProviderFixture(t *testing.T, adapter *Adapter, intercept func(*http.Request) (*http.Response, bool)) *controlplanev1.IntegrationCredentialRevision {
	t.Helper()
	root := t.TempDir()
	value := []byte("synthetic-token")
	if err := os.WriteFile(filepath.Join(root, "test-token"), value, 0400); err != nil {
		t.Fatal(err)
	}
	var err error
	adapter.credentials, err = credentialfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	fixture := operationFixture(t, func(request *http.Request) (*http.Response, bool) {
		if request.URL.Host != "chat.example.test" || request.Header.Get("Authorization") != model.HeaderBearer+" synthetic-token" {
			t.Fatal("provider request did not use exact origin and credential")
		}
		if intercept != nil {
			return intercept(request)
		}
		return nil, false
	})
	adapter.newTransport = func(*url.URL) http.RoundTripper { return fixture.HTTPClient.Transport }
	digest := sha256.Sum256(value)
	return &controlplanev1.IntegrationCredentialRevision{Ref: "credential", Revision: 1, SecretRef: credentialSecretPrefix + "test-token", SecretUid: "00000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSha256: hex.EncodeToString(digest[:])}
}

func TestClaimCredentialProviderAndReceiptTogether(t *testing.T) {
	for _, tc := range []struct {
		operation string
		input     map[string]any
	}{
		{operation: "mattermost.post.send", input: map[string]any{"message": "new message"}},
		{operation: "mattermost.file.read", input: map[string]any{"post_id": testPostID, "file_id": testFileID, "offset": 1, "limit": 3}},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			adapter, claim := claimFixture(t, tc.operation, tc.input)
			calls := 0
			claim.CredentialRevision = configureProviderFixture(t, adapter, func(*http.Request) (*http.Response, bool) { calls++; return nil, false })
			receipt, err := adapter.Execute(t.Context(), claim)
			if err != nil || receipt.GetEffectKey() != claim.GetEffectKey() || receipt.GetInputDigest() != claim.GetInputDigest() || calls < 3 {
				t.Fatalf("complete execution path failed: %v", err)
			}
			calls = 0
			claim.CredentialRevision.ContentSha256 = hex.EncodeToString(make([]byte, 32))
			if _, err := adapter.Execute(t.Context(), claim); err == nil || calls != 0 {
				t.Fatal("credential generation mismatch reached provider")
			}
		})
	}
}

func TestConnectionReadinessUsesWorkingCredentialAndChannel(t *testing.T) {
	adapter, invocation := claimFixture(t, "mattermost.team.read", map[string]any{})
	credential := configureProviderFixture(t, adapter, nil)
	claim := &controlplanev1.IntegrationConnectionTestClaim{DefinitionPackage: invocation.GetDefinitionPackage(), DefinitionKey: invocation.GetDefinitionKey(), DefinitionVersion: invocation.GetDefinitionVersion(), DefinitionDigest: invocation.GetDefinitionDigest(), PublicConfiguration: invocation.GetPublicConfiguration(), CredentialRevision: credential}
	if _, err := adapter.TestConnection(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	adapter.newTransport = func(*url.URL) http.RoundTripper {
		return roundTripFunc(func(*http.Request) (*http.Response, error) { return testResponse(401, `{"status_code":401}`), nil })
	}
	if _, err := adapter.TestConnection(t.Context(), claim); err == nil {
		t.Fatal("readiness ignored provider authorization failure")
	}
}

func TestAcknowledgementDeliveryUsesOwnerThreadAndExactReadback(t *testing.T) {
	adapter, _ := claimFixture(t, "mattermost.post.send", map[string]any{"message": "new message"})
	posts := 0
	credential := configureProviderFixture(t, adapter, func(request *http.Request) (*http.Response, bool) {
		if request.Method != http.MethodPost {
			return nil, false
		}
		posts++
		var post model.Post
		if json.NewDecoder(request.Body).Decode(&post) != nil || post.ChannelId != testChannelID || post.RootId != testPostID || post.Message != "Ready" {
			t.Fatal("ACK did not use accepted thread")
		}
		post.Id = testFileID
		return jsonResponse(t, 201, &post), true
	})
	claim := &controlplanev1.InteractionDeliveryClaim{DeliveryRef: "delivery", ConnectionRef: "connection", BaseUrl: "https://chat.example.test", TeamName: "team", ChannelName: "channel", CredentialDescriptor: credential, CapabilityKey: "mattermost.acknowledgements", AcceptanceReceiptRef: "receipt", ExternalTeamRef: testTeamID, ExternalChannelRef: testChannelID, ExternalRootPostRef: testPostID, MessageKey: "READY", Locale: "en"}
	result, err := adapter.Deliver(t.Context(), claim)
	if err != nil || result.PostRef != testFileID || result.ThreadRef != testPostID || result.TeamRef != testTeamID || result.ChannelRef != testChannelID || posts != 1 {
		t.Fatalf("ACK result=%+v err=%v", result, err)
	}
	claim.ExternalChannelRef = testUserID
	if _, err := adapter.Deliver(t.Context(), claim); err == nil || !ConfirmedNoEffect(err) || posts != 1 {
		t.Fatal("foreign accepted channel reached CreatePost")
	}
}
