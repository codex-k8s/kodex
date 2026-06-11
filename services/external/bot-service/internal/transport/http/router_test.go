package http

import (
	"context"
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
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
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

func TestSlashMenuReturnsAttachmentActions(t *testing.T) {
	router := testRouterWithSlashService()
	form := url.Values{}
	form.Set("token", "expected-token")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/slash/agents", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		ResponseType string `json:"response_type"`
		Text         string `json:"text"`
		Attachments  []struct {
			Title   string `json:"title"`
			Actions []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Integration struct {
					URL     string         `json:"url"`
					Context map[string]any `json:"context"`
				} `json:"integration"`
			} `json:"actions"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.ResponseType != "in_channel" || !strings.Contains(payload.Text, "control menu") {
		t.Fatalf("unexpected slash payload: %#v", payload)
	}
	if len(payload.Attachments) != 1 || payload.Attachments[0].Title != "Main menu" {
		t.Fatalf("unexpected attachments: %#v", payload.Attachments)
	}
	if len(payload.Attachments[0].Actions) == 0 {
		t.Fatal("menu actions are empty")
	}
	action := payload.Attachments[0].Actions[0]
	if action.ID != "menustartflow" {
		t.Fatalf("action id = %q", action.ID)
	}
	if action.Integration.URL != "http://bot-service/mattermost/actions/agents" {
		t.Fatalf("action url = %q", action.Integration.URL)
	}
	if action.Integration.Context["kind"] != "agents_menu" || action.Integration.Context["view"] != "start_flow" {
		t.Fatalf("action context = %#v", action.Integration.Context)
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

func TestAgentsActionReturnsUpdatedMenuCard(t *testing.T) {
	router := testRouterWithSlashService()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/agents", strings.NewReader(`{"user_id":"owner","user_name":"owner","channel_id":"channel-1","post_id":"post-1","context":{"kind":"agents_menu","view":"runtime"}}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		EphemeralText string `json:"ephemeral_text"`
		Update        struct {
			ID        string         `json:"id"`
			ChannelID string         `json:"channel_id"`
			Message   string         `json:"message"`
			Props     map[string]any `json:"props"`
		} `json:"update"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !strings.Contains(payload.EphemeralText, "opened") {
		t.Fatalf("ephemeral_text = %q", payload.EphemeralText)
	}
	if payload.Update.ID != "post-1" || payload.Update.ChannelID != "channel-1" {
		t.Fatalf("update = %#v", payload.Update)
	}
	attachments, ok := payload.Update.Props["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v", payload.Update.Props["attachments"])
	}
	attachment, ok := attachments[0].(map[string]any)
	if !ok || attachment["title"] != "Runtime" {
		t.Fatalf("attachment = %#v", attachments[0])
	}
	actions, ok := attachment["actions"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("actions = %#v", attachment["actions"])
	}
	if actionContext(actions, "cmdruntimesmoke")["command"] != "runtime smoke" {
		t.Fatalf("runtime smoke action is missing: %#v", actions)
	}
	if actionContext(actions, "menumain")["view"] != "main" {
		t.Fatalf("main menu action is missing: %#v", actions)
	}
}

func actionContext(actions []any, id string) map[string]any {
	for _, raw := range actions {
		action, ok := raw.(map[string]any)
		if !ok || action["id"] != id {
			continue
		}
		integration, ok := action["integration"].(map[string]any)
		if !ok {
			return nil
		}
		context, _ := integration["context"].(map[string]any)
		return context
	}
	return nil
}

func TestAgentsActionExecutesCommandButton(t *testing.T) {
	router := testRouterWithSlashService()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/agents", strings.NewReader(`{"user_id":"owner","user_name":"owner","channel_id":"channel-1","post_id":"post-1","context":{"kind":"agents_menu","view":"system","command":"status"}}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		EphemeralText string `json:"ephemeral_text"`
		Update        struct {
			Props map[string]any `json:"props"`
		} `json:"update"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !strings.Contains(payload.EphemeralText, "matter-codex: online") {
		t.Fatalf("ephemeral_text = %q", payload.EphemeralText)
	}
	attachments, ok := payload.Update.Props["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v", payload.Update.Props["attachments"])
	}
	attachment, ok := attachments[0].(map[string]any)
	if !ok || attachment["title"] != "Result - System" {
		t.Fatalf("attachment = %#v", attachments[0])
	}
	if text, _ := attachment["text"].(string); !strings.Contains(text, "matter-codex: online") {
		t.Fatalf("attachment text = %#v", attachment["text"])
	}
	if attachmentContainsSlashCommand(attachment) {
		t.Fatalf("result card exposes slash command: %#v", attachment)
	}
}

func TestAgentsActionOpensDialog(t *testing.T) {
	opener := &fakeDialogOpener{}
	router := testRouterWithDialogService(opener)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/agents", strings.NewReader(`{"user_id":"owner","user_name":"owner","channel_id":"channel-1","post_id":"post-1","trigger_id":"trigger-1","context":{"kind":"agents_menu","view":"repositories","dialog":"repo_add"}}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if opener.triggerID != "trigger-1" {
		t.Fatalf("triggerID = %q", opener.triggerID)
	}
	if opener.dialog.CallbackID != "agents_repo_add" || opener.dialog.SubmitURL != "http://bot-service/mattermost/dialogs/agents" {
		t.Fatalf("dialog = %#v", opener.dialog)
	}
	var payload struct {
		EphemeralText string `json:"ephemeral_text"`
		Update        any    `json:"update"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Update != nil {
		t.Fatalf("dialog action should not return post update: %#v", payload.Update)
	}
}

func TestAgentsActionRejectsDialogWithoutTriggerID(t *testing.T) {
	opener := &fakeDialogOpener{}
	router := testRouterWithDialogService(opener)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/agents", strings.NewReader(`{"user_id":"owner","user_name":"owner","channel_id":"channel-1","post_id":"post-1","context":{"kind":"agents_menu","view":"repositories","dialog":"repo_add"}}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if opener.triggerID != "" {
		t.Fatalf("dialog opener should not be called, triggerID = %q", opener.triggerID)
	}
	if !strings.Contains(recorder.Body.String(), "trigger id") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAgentsDialogReturnsFieldErrors(t *testing.T) {
	router := testRouterWithDialogService(&fakeDialogOpener{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/dialogs/agents", strings.NewReader(`{"callback_id":"agents_repo_add","state":"{\"view\":\"repositories\"}","submission":{"provider":"github","repository":"bad value","default_branch":"main"}}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Errors["repository"] == "" {
		t.Fatalf("repository field error is missing: %#v", payload.Errors)
	}
}

func attachmentContainsSlashCommand(attachment map[string]any) bool {
	for _, key := range []string{"title", "text"} {
		value, _ := attachment[key].(string)
		if strings.Contains(value, "/agents ") {
			return true
		}
	}
	fields, _ := attachment["fields"].([]any)
	for _, raw := range fields {
		field, _ := raw.(map[string]any)
		for _, key := range []string{"title", "value"} {
			value, _ := field[key].(string)
			if strings.Contains(value, "/agents ") {
				return true
			}
		}
	}
	return false
}

func TestFlowActionRejectsInvalidJSON(t *testing.T) {
	router := testRouter("expected-token", true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/flow", strings.NewReader(`{`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestFlowActionRejectsMissingService(t *testing.T) {
	router := testRouter("expected-token", true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/flow", strings.NewReader(`{"user_id":"owner","context":{"flow_id":"flow1","action":"approve","token":"token"}}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
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

func testRouterWithSlashService() *Router {
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		panic(err)
	}
	statusSvc := statusservice.NewStatusService(statusservice.Config{
		Localizer:            localizer,
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DatabaseConfigured:   true,
		StorageReady:         true,
		RuntimeConfigured:    true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control"},
	})
	slashSvc := statusservice.NewSlashCommandService(statusservice.SlashCommandServiceConfig{
		Localizer:         localizer,
		StatusService:     statusSvc,
		MenuActionURL:     "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL:   "http://bot-service/mattermost/dialogs/agents",
		StorageReady:      true,
		RuntimeConfigured: true,
	})
	return NewRouter(RouterConfig{
		StatusService:         statusSvc,
		SlashService:          slashSvc,
		Localizer:             localizer,
		SlashToken:            "expected-token",
		MaxSlashFormBytes:     65536,
		MaxGitHubWebhookBytes: 262144,
	})
}

func testRouterWithDialogService(opener *fakeDialogOpener) *Router {
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		panic(err)
	}
	statusSvc := statusservice.NewStatusService(statusservice.Config{
		Localizer:            localizer,
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DatabaseConfigured:   true,
		StorageReady:         true,
		RuntimeConfigured:    true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control"},
	})
	slashSvc := statusservice.NewSlashCommandService(statusservice.SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   statusSvc,
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})
	return NewRouter(RouterConfig{
		StatusService:         statusSvc,
		SlashService:          slashSvc,
		DialogOpener:          opener,
		Localizer:             localizer,
		SlashToken:            "expected-token",
		MaxSlashFormBytes:     65536,
		MaxGitHubWebhookBytes: 262144,
	})
}

type fakeDialogOpener struct {
	triggerID string
	dialog    statusservice.MattermostDialog
}

func (opener *fakeDialogOpener) OpenDialog(_ context.Context, triggerID string, dialog statusservice.MattermostDialog) error {
	opener.triggerID = triggerID
	opener.dialog = dialog
	return nil
}

func testRouterWithConfig(slashToken string, botTokenConfigured bool, gitHubWebhookSecret string) *Router {
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		panic(err)
	}
	statusSvc := statusservice.NewStatusService(statusservice.Config{
		Localizer:            localizer,
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   botTokenConfigured,
		SlashTokenConfigured: slashToken != "",
		RuntimeConfigured:    true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control", "agents-runs", "agent-alerts", "agents-audit"},
	})
	return NewRouter(RouterConfig{
		StatusService:         statusSvc,
		Localizer:             localizer,
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
