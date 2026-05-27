package httptransport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
)

const githubBootstrapMergedFixturePath = "../../../../../../fixtures/provider-webhooks/github_pull_request_bootstrap_merged.json"

func TestProviderWebhookForwardsGitHubPullRequestMergedFixture(t *testing.T) {
	fixture, err := os.ReadFile(githubBootstrapMergedFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	payload := string(fixture)
	providerHub := &fakeProviderHub{result: providerhubclient.WebhookResult{WebhookEventID: "webhook-smoke-1"}}
	router := newTestRouterWithVerifier(t, enabledTestConfig(4096), providerHub, newGitHubVerifier(t, testWebhookSecret))

	req := httptest.NewRequest(http.MethodPost, "/v1/provider-webhooks/github", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", "smoke-bootstrap-merged")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", githubSignature(testWebhookSecret, payload))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if providerHub.event.ProviderSlug != "github" ||
		providerHub.event.DeliveryID != "smoke-bootstrap-merged" ||
		providerHub.event.EventName != "pull_request" {
		t.Fatalf("providerHub event = %+v, want GitHub pull_request fixture envelope", providerHub.event)
	}
	if providerHub.event.PayloadJSON != payload {
		t.Fatalf("providerHub payload changed before owner service")
	}
	if providerHub.event.RequestID == "" || providerHub.event.CorrelationID == "" {
		t.Fatalf("providerHub event lacks correlation metadata: %+v", providerHub.event)
	}
}
