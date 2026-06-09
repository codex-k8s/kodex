package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
)

func TestHealthDoesNotExposeTokenValues(t *testing.T) {
	router := testRouter("secret-slash-token", true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "secret-slash-token") {
		t.Fatal("health response exposes slash token")
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["bot_token_configured"] != true || payload["slash_token_configured"] != true {
		t.Fatalf("unexpected health payload: %#v", payload)
	}
}

func TestSlashStatus(t *testing.T) {
	router := testRouter("expected-token", true)
	form := url.Values{}
	form.Set("token", "expected-token")
	form.Set("text", "status")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/slash/agents", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "matter-codex: online") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestSlashRejectsWrongToken(t *testing.T) {
	router := testRouter("expected-token", true)
	form := url.Values{}
	form.Set("token", "wrong-token")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/slash/agents", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestGitHubWebhookRejectsMissingSecret(t *testing.T) {
	router := testRouter("expected-token", true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(`{}`))

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestGitHubWebhookRejectsInvalidSignature(t *testing.T) {
	router := testRouterWithGitHubWebhook("webhook-secret")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", "ping")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestGitHubWebhookAcceptsSignedPayload(t *testing.T) {
	secret := "webhook-secret"
	body := `{"zen":"keep it logically awesome","hook_id":1}`
	router := testRouterWithGitHubWebhook(secret)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/github/webhook", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", "ping")
	request.Header.Set("X-Hub-Signature-256", signGitHubPayload(secret, body))

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"event":"ping"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func testRouter(slashToken string, botTokenConfigured bool) *Router {
	return testRouterWithConfig(slashToken, botTokenConfigured, "")
}

func testRouterWithGitHubWebhook(secret string) *Router {
	return testRouterWithConfig("expected-token", true, secret)
}

func testRouterWithConfig(slashToken string, botTokenConfigured bool, gitHubWebhookSecret string) *Router {
	statusSvc := statusservice.NewStatusService(statusservice.Config{
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   botTokenConfigured,
		SlashTokenConfigured: slashToken != "",
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control", "agents-runs", "agent-alerts", "agents-audit"},
	})
	return NewRouter(RouterConfig{
		StatusService:         statusSvc,
		SlashToken:            slashToken,
		GitHubWebhookSecret:   gitHubWebhookSecret,
		MaxSlashFormBytes:     65536,
		MaxGitHubWebhookBytes: 262144,
	})
}

func signGitHubPayload(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
