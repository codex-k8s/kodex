package http

import (
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

func testRouter(slashToken string, botTokenConfigured bool) *Router {
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
		StatusService:     statusSvc,
		SlashToken:        slashToken,
		MaxSlashFormBytes: 65536,
	})
}
