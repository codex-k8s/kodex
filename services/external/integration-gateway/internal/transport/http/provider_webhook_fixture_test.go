package httptransport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
)

const (
	githubBootstrapMergedFixturePath     = "../../../../../../fixtures/provider-webhooks/github_pull_request_bootstrap_merged.json"
	githubAdoptionMergedFixturePath      = "../../../../../../fixtures/provider-webhooks/github_pull_request_adoption_merged.json"
	githubIssueOpenedFixturePath         = "../../../../../../fixtures/provider-webhooks/github_issues_opened.json"
	githubPullRequestOpenedFixturePath   = "../../../../../../fixtures/provider-webhooks/github_pull_request_opened.json"
	githubIssueCommentCreatedFixturePath = "../../../../../../fixtures/provider-webhooks/github_issue_comment_created.json"
	githubPullRequestReviewFixturePath   = "../../../../../../fixtures/provider-webhooks/github_pull_request_review_submitted.json"
	githubPushProviderNativeFixturePath  = "../../../../../../fixtures/provider-webhooks/github_push_provider_native_change.json"
)

func TestProviderWebhookForwardsGitHubPullRequestMergedFixtures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fixture    string
		deliveryID string
	}{
		{name: "bootstrap", fixture: githubBootstrapMergedFixturePath, deliveryID: "smoke-bootstrap-merged"},
		{name: "adoption", fixture: githubAdoptionMergedFixturePath, deliveryID: "smoke-adoption-merged"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := os.ReadFile(tc.fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			payload := string(fixture)
			providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-smoke-1"}}
			router := newTestRouterWithVerifier(t, enabledTestConfig(4096), providerHub, newGitHubVerifier(t, testWebhookSecret))

			req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-GitHub-Delivery", tc.deliveryID)
			req.Header.Set("X-GitHub-Event", "pull_request")
			req.Header.Set("X-Hub-Signature-256", githubSignature(testWebhookSecret, payload))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
			}
			if providerHub.event.ProviderSlug != "github" ||
				providerHub.event.DeliveryID != tc.deliveryID ||
				providerHub.event.EventName != "pull_request" {
				t.Fatalf("providerHub event = %+v, want GitHub pull_request fixture envelope", providerHub.event)
			}
			if providerHub.event.PayloadJSON != payload {
				t.Fatalf("providerHub payload changed before owner service")
			}
			if providerHub.event.RequestID == "" || providerHub.event.CorrelationID == "" {
				t.Fatalf("providerHub event lacks correlation metadata: %+v", providerHub.event)
			}
		})
	}
}

func TestProviderWebhookForwardsGitHubProviderNativeFixtures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		fixture    string
		eventName  string
		deliveryID string
	}{
		{name: "issue", fixture: githubIssueOpenedFixturePath, eventName: "issues", deliveryID: "provider-native-issue-opened"},
		{name: "pull_request", fixture: githubPullRequestOpenedFixturePath, eventName: "pull_request", deliveryID: "provider-native-pull-request-opened"},
		{name: "issue_comment", fixture: githubIssueCommentCreatedFixturePath, eventName: "issue_comment", deliveryID: "provider-native-issue-comment-created"},
		{name: "pull_request_review", fixture: githubPullRequestReviewFixturePath, eventName: "pull_request_review", deliveryID: "provider-native-pull-request-review"},
		{name: "push", fixture: githubPushProviderNativeFixturePath, eventName: "push", deliveryID: "provider-native-push-main"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := os.ReadFile(tc.fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			payload := string(fixture)
			providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-provider-native-1"}}
			router := newTestRouterWithVerifier(t, enabledTestConfig(16*1024), providerHub, newGitHubVerifier(t, testWebhookSecret))

			req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-GitHub-Delivery", tc.deliveryID)
			req.Header.Set("X-GitHub-Event", tc.eventName)
			req.Header.Set("X-Hub-Signature-256", githubSignature(testWebhookSecret, payload))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
			}
			if providerHub.event.ProviderSlug != "github" ||
				providerHub.event.DeliveryID != tc.deliveryID ||
				providerHub.event.EventName != tc.eventName {
				t.Fatalf("providerHub event = %+v, want GitHub %s fixture envelope", providerHub.event, tc.eventName)
			}
			if providerHub.event.PayloadJSON != payload {
				t.Fatalf("providerHub payload changed before owner service")
			}
			if providerHub.event.RequestID == "" || providerHub.event.CorrelationID == "" {
				t.Fatalf("providerHub event lacks correlation metadata: %+v", providerHub.event)
			}
		})
	}
}
