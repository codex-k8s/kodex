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
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
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
	if action.ID != "menuprojects" {
		t.Fatalf("action id = %q", action.ID)
	}
	if action.Integration.URL != "http://bot-service/mattermost/actions/agents" {
		t.Fatalf("action url = %q", action.Integration.URL)
	}
	if action.Integration.Context["kind"] != "agents_menu" || action.Integration.Context["view"] != "projects" {
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
	smokeContext := actionContext(actions, "runtimesmoke")
	if smokeContext["action"] != "runtime_smoke" || smokeContext["resource_type"] != "runtime" {
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

func TestAgentsDialogClosesAndPublishesResult(t *testing.T) {
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		t.Fatal(err)
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
	})
	store := &fakeRouterAdminStore{}
	slashSvc := statusservice.NewSlashCommandService(statusservice.SlashCommandServiceConfig{
		Localizer:       localizer,
		StatusService:   statusSvc,
		Store:           store,
		MenuActionURL:   "http://bot-service/mattermost/actions/agents",
		DialogSubmitURL: "http://bot-service/mattermost/dialogs/agents",
		StorageReady:    true,
	})
	router := NewRouter(RouterConfig{
		StatusService:         statusSvc,
		SlashService:          slashSvc,
		Localizer:             localizer,
		SlashToken:            "expected-token",
		MaxSlashFormBytes:     65536,
		MaxGitHubWebhookBytes: 262144,
	})
	type delayedResponse struct {
		ResponseType string `json:"response_type"`
		Text         string `json:"text"`
		Attachments  []struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"attachments"`
	}
	delivered := make(chan delayedResponse, 1)
	resultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("delayed response method = %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload delayedResponse
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("delayed response decode error = %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		delivered <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer resultServer.Close()
	recorder := httptest.NewRecorder()
	body := `{"callback_id":"agents_repo_add","url":"` + resultServer.URL + `","state":"{\"view\":\"repositories\",\"channel_id\":\"channel-1\",\"post_id\":\"post-1\",\"user_name\":\"owner\"}","user_id":"owner-id","submission":{"provider":"github","repository":"codex-k8s/kodex-package-store","default_branch":"main"}}`
	request := httptest.NewRequest(http.MethodPost, "/mattermost/dialogs/agents", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Error string `json:"error"`
		Type  string `json:"type"`
		Form  any    `json:"form"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Error != "" || payload.Type != "ok" || payload.Form != nil {
		t.Fatalf("payload = %#v", payload)
	}
	select {
	case response := <-delivered:
		if response.ResponseType != "ephemeral" {
			t.Fatalf("delayed response = %#v", response)
		}
		if len(response.Attachments) != 1 {
			t.Fatalf("delayed attachments = %#v", response.Attachments)
		}
		resultText := response.Text + "\n" + response.Attachments[0].Text
		if !strings.Contains(resultText, "codex-k8s/kodex-package-store") {
			t.Fatalf("delayed response text = %q", resultText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delayed dialog result")
	}
	if store.upsert.Owner != "codex-k8s" || store.upsert.Name != "kodex-package-store" || store.upsert.DefaultBranch != "main" {
		t.Fatalf("upsert = %#v", store.upsert)
	}
}

func TestMattermostDialogFormConvertsSelectOptions(t *testing.T) {
	form := mattermostDialogForm(statusservice.MattermostDialog{
		CallbackID:       "agents_repo_search_pick",
		Title:            "Choose repository",
		IntroductionText: "Pick one.",
		SubmitLabel:      "Choose",
		State:            `{"view":"repositories"}`,
		Elements: []statusservice.MattermostDialogElement{
			{
				DisplayName: "Repository",
				Name:        "repository_choice",
				Type:        "select",
				Options: []statusservice.MattermostDialogOption{
					{Text: "codex-k8s/matter-codex", Value: "repo-state"},
				},
			},
		},
	})

	if form.CallbackId != "agents_repo_search_pick" || form.Title != "Choose repository" || form.SubmitLabel != "Choose" {
		t.Fatalf("form = %#v", form)
	}
	if len(form.Elements) != 1 || form.Elements[0].Type != "select" || len(form.Elements[0].Options) != 1 {
		t.Fatalf("form elements = %#v", form.Elements)
	}
	if form.Elements[0].Options[0].Text != "codex-k8s/matter-codex" || form.Elements[0].Options[0].Value != "repo-state" {
		t.Fatalf("form options = %#v", form.Elements[0].Options)
	}
}

func TestAgentsDialogResultCallbackClosesResultForm(t *testing.T) {
	router := NewRouter(RouterConfig{MaxSlashFormBytes: 65536})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/dialogs/agents", strings.NewReader(`{"callback_id":"agents_dialog_result"}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Type != "ok" {
		t.Fatalf("payload = %#v", payload)
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
	actions, _ := attachment["actions"].([]any)
	for _, raw := range actions {
		action, _ := raw.(map[string]any)
		integration, _ := action["integration"].(map[string]any)
		context, _ := integration["context"].(map[string]any)
		if _, ok := context["command"]; ok {
			return true
		}
	}
	return false
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

func TestFlowActionEndpointIsNotRegistered(t *testing.T) {
	router := testRouter("expected-token", true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mattermost/actions/flow", strings.NewReader(`{}`))

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
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

type fakeRouterAdminStore struct {
	upsert        adminrepo.UpsertRepositoryInput
	auditRecorded bool
	repositories  map[string]entity.Repository
}

func (store *fakeRouterAdminStore) UpsertRepository(_ context.Context, input adminrepo.UpsertRepositoryInput) (entity.Repository, bool, error) {
	store.upsert = input
	store.ensureRepositories()
	key := input.Provider + ":" + input.Owner + "/" + input.Name
	_, exists := store.repositories[key]
	repo := entity.Repository{
		Provider:          input.Provider,
		Owner:             input.Owner,
		Name:              input.Name,
		DefaultBranch:     input.DefaultBranch,
		GitHubAccountName: input.GitHubAccountName,
		MattermostChannel: input.MattermostChannel,
		Status:            "active",
	}
	store.repositories[key] = repo
	return repo, !exists, nil
}

func (store *fakeRouterAdminStore) GetRepository(_ context.Context, provider string, owner string, name string) (entity.Repository, error) {
	store.ensureRepositories()
	repo, ok := store.repositories[provider+":"+owner+"/"+name]
	if !ok {
		return entity.Repository{}, adminrepo.ErrNotFound
	}
	return repo, nil
}

func (store *fakeRouterAdminStore) ListRepositories(context.Context, int) ([]entity.Repository, error) {
	store.ensureRepositories()
	items := make([]entity.Repository, 0, len(store.repositories))
	for _, repo := range store.repositories {
		items = append(items, repo)
	}
	return items, nil
}

func (store *fakeRouterAdminStore) DeleteRepository(_ context.Context, provider string, owner string, name string) (entity.Repository, error) {
	store.ensureRepositories()
	key := provider + ":" + owner + "/" + name
	repo, ok := store.repositories[key]
	if !ok {
		return entity.Repository{}, adminrepo.ErrNotFound
	}
	delete(store.repositories, key)
	return repo, nil
}

func (store *fakeRouterAdminStore) UpsertProject(_ context.Context, input adminrepo.UpsertProjectInput) (entity.Project, bool, error) {
	return entity.Project{
		Name:              input.Name,
		Slug:              input.Slug,
		MattermostTeamID:  input.MattermostTeamID,
		GitHubAccountName: input.GitHubAccountName,
		GitHubOwner:       input.GitHubOwner,
		GitHubOwnerType:   input.GitHubOwnerType,
	}, true, nil
}

func (store *fakeRouterAdminStore) GetProject(context.Context, int64) (entity.Project, error) {
	return entity.Project{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) GetProjectBySlug(context.Context, string) (entity.Project, error) {
	return entity.Project{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListProjects(context.Context, int) ([]entity.Project, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpsertProjectRepository(_ context.Context, input adminrepo.UpsertProjectRepositoryInput) (entity.ProjectRepository, bool, error) {
	return entity.ProjectRepository{ProjectID: input.ProjectID, RepositoryID: input.RepositoryID, IsDefault: input.IsDefault}, true, nil
}

func (store *fakeRouterAdminStore) ListProjectRepositories(context.Context, int64) ([]entity.ProjectRepository, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpsertProjectRuntimeVariable(_ context.Context, input adminrepo.UpsertProjectRuntimeVariableInput) (entity.ProjectRuntimeVariable, bool, error) {
	return entity.ProjectRuntimeVariable{
		ProjectID:   input.ProjectID,
		Name:        input.Name,
		Slug:        input.Slug,
		Description: input.Description,
		SecretRef:   input.SecretRef,
		SecretKey:   input.SecretKey,
		Sensitive:   input.Sensitive,
		Enabled:     input.Enabled,
	}, true, nil
}

func (store *fakeRouterAdminStore) GetProjectRuntimeVariable(context.Context, int64) (entity.ProjectRuntimeVariable, error) {
	return entity.ProjectRuntimeVariable{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListProjectRuntimeVariables(context.Context, int64) ([]entity.ProjectRuntimeVariable, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) DeleteProjectRuntimeVariable(context.Context, int64) (entity.ProjectRuntimeVariable, error) {
	return entity.ProjectRuntimeVariable{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpsertAgentRoleRuntimeVariable(context.Context, adminrepo.UpsertAgentRoleRuntimeVariableInput) (entity.AgentRoleRuntimeVariableBinding, bool, error) {
	return entity.AgentRoleRuntimeVariableBinding{}, true, nil
}

func (store *fakeRouterAdminStore) DeleteAgentRoleRuntimeVariable(context.Context, int64, int64) (entity.AgentRoleRuntimeVariableBinding, error) {
	return entity.AgentRoleRuntimeVariableBinding{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentRoleRuntimeVariables(context.Context, int64) ([]entity.AgentRoleRuntimeVariableBinding, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpsertAgentRole(_ context.Context, input adminrepo.UpsertAgentRoleInput) (entity.AgentRole, bool, error) {
	return entity.AgentRole{ProjectID: input.ProjectID, Name: input.Name, RoleType: input.RoleType, Enabled: input.Enabled}, true, nil
}

func (store *fakeRouterAdminStore) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return entity.AgentRole{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentRoles(context.Context, int64) ([]entity.AgentRole, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) CreateChat(_ context.Context, input adminrepo.CreateChatInput) (entity.Chat, bool, error) {
	return entity.Chat{ProjectID: input.ProjectID, MattermostChannelID: input.MattermostChannelID, Name: input.Name, Slug: input.Slug, ChatType: input.ChatType}, true, nil
}

func (store *fakeRouterAdminStore) GetChat(context.Context, int64) (entity.Chat, error) {
	return entity.Chat{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) GetChatByMattermostChannelID(context.Context, string) (entity.Chat, error) {
	return entity.Chat{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListChats(context.Context, int64) ([]entity.Chat, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListChatParticipants(context.Context, int64) ([]entity.ChatParticipant, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListChatRepositories(context.Context, int64) ([]entity.ChatRepositoryBinding, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) GetThreadContext(context.Context, int64, string) (entity.ThreadContext, error) {
	return entity.ThreadContext{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) GetThreadContextByID(context.Context, int64) (entity.ThreadContext, error) {
	return entity.ThreadContext{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpsertThreadContext(context.Context, adminrepo.UpsertThreadContextInput) (entity.ThreadContext, bool, error) {
	return entity.ThreadContext{}, true, nil
}

func (store *fakeRouterAdminStore) UpsertMattermostBotIdentity(context.Context, adminrepo.UpsertMattermostBotIdentityInput) (entity.MattermostBotIdentity, bool, error) {
	return entity.MattermostBotIdentity{}, true, nil
}

func (store *fakeRouterAdminStore) GetMattermostBotIdentityByRoleID(context.Context, int64) (entity.MattermostBotIdentity, error) {
	return entity.MattermostBotIdentity{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) GetMattermostBotIdentityByUserID(context.Context, string) (entity.MattermostBotIdentity, error) {
	return entity.MattermostBotIdentity{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListMattermostBotIdentitiesByProject(context.Context, int64) ([]entity.MattermostBotIdentity, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpsertAgentSession(context.Context, adminrepo.UpsertAgentSessionInput) (entity.AgentSession, bool, error) {
	return entity.AgentSession{}, true, nil
}

func (store *fakeRouterAdminStore) GetAgentSession(context.Context, string) (entity.AgentSession, error) {
	return entity.AgentSession{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) GetAgentSessionByID(context.Context, int64) (entity.AgentSession, error) {
	return entity.AgentSession{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentSessionsByThread(context.Context, int64, string) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListAgentSessionsByChat(context.Context, int64) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListAgentSessionsByRole(context.Context, int64) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) AcquireAgentSessionCapacityLock(context.Context) (func(), error) {
	return func() {}, nil
}

func (store *fakeRouterAdminStore) ListEvictableIdleAgentSessions(context.Context, int) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListQueuedIdleAgentSessions(context.Context, int) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListStaleActiveAgentSessions(context.Context, int) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListRunningActiveAgentSessions(context.Context, int) ([]entity.AgentSession, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpdateAgentSessionRuntime(context.Context, adminrepo.UpdateAgentSessionRuntimeInput) (entity.AgentSession, error) {
	return entity.AgentSession{}, nil
}

func (store *fakeRouterAdminStore) ResetAgentSessionRuntime(context.Context, string, string) (entity.AgentSession, error) {
	return entity.AgentSession{}, nil
}

func (store *fakeRouterAdminStore) ClearIdleAgentSessionPod(context.Context, string, string) (entity.AgentSession, error) {
	return entity.AgentSession{}, nil
}

func (store *fakeRouterAdminStore) UpdateAgentSessionSnapshot(context.Context, adminrepo.UpdateAgentSessionSnapshotInput) (entity.AgentSession, error) {
	return entity.AgentSession{}, nil
}

func (store *fakeRouterAdminStore) CreateAgentSessionTurn(context.Context, adminrepo.CreateAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, nil
}

func (store *fakeRouterAdminStore) GetAgentSessionTurn(context.Context, int64) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ClaimNextAgentSessionTurn(context.Context, string) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) CompleteAgentSessionTurn(context.Context, adminrepo.CompleteAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, nil
}

func (store *fakeRouterAdminStore) CancelAgentSessionTurn(context.Context, adminrepo.CancelAgentSessionTurnInput) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpdateAgentSessionTurnStatusPost(context.Context, adminrepo.UpdateAgentSessionTurnStatusPostInput) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, nil
}

func (store *fakeRouterAdminStore) UpdateAgentSessionTurnMessage(context.Context, adminrepo.UpdateAgentSessionTurnMessageInput) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{}, nil
}

func (store *fakeRouterAdminStore) ListQueuedAgentSessionTurns(context.Context, int64) ([]entity.AgentSessionTurn, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) CreateAgentDelegation(context.Context, adminrepo.CreateAgentDelegationInput) (entity.AgentDelegation, bool, error) {
	return entity.AgentDelegation{}, true, nil
}

func (store *fakeRouterAdminStore) GetAgentDelegationBySourceKey(context.Context, int64, string) (entity.AgentDelegation, error) {
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) GetAgentDelegationForCallback(context.Context, int64) (entity.AgentDelegation, error) {
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentDelegationsBySource(context.Context, int64, int) ([]entity.AgentDelegation, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) SetAgentDelegationRoot(context.Context, int64, string) (entity.AgentDelegation, error) {
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) SetAgentDelegationTarget(context.Context, int64, int64, int64, string) (entity.AgentDelegation, error) {
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) SetAgentDelegationFailed(context.Context, int64) (entity.AgentDelegation, error) {
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) SetAgentDelegationCallback(context.Context, int64, int64, string) (entity.AgentDelegation, error) {
	return entity.AgentDelegation{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentProfiles(context.Context) ([]entity.AgentProfile, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) GetAgentProfile(context.Context, string) (entity.AgentProfile, error) {
	return entity.AgentProfile{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpsertAgentProfile(context.Context, adminrepo.UpsertAgentProfileInput) (entity.AgentProfile, bool, error) {
	return entity.AgentProfile{}, true, nil
}

func (store *fakeRouterAdminStore) ListAgentPromptTemplates(context.Context, string) ([]entity.AgentPromptTemplate, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) GetAgentPromptTemplate(context.Context, string, string) (entity.AgentPromptTemplate, error) {
	return entity.AgentPromptTemplate{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpsertAgentPromptTemplate(context.Context, adminrepo.UpsertAgentPromptTemplateInput) (entity.AgentPromptTemplate, bool, error) {
	return entity.AgentPromptTemplate{}, true, nil
}

func (store *fakeRouterAdminStore) UpsertOpenAIAccount(context.Context, adminrepo.UpsertOpenAIAccountInput) (entity.OpenAIAccount, bool, error) {
	return entity.OpenAIAccount{}, true, nil
}

func (store *fakeRouterAdminStore) ListOpenAIAccounts(context.Context, int) ([]entity.OpenAIAccount, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) GetOpenAIAccount(context.Context, string) (entity.OpenAIAccount, error) {
	return entity.OpenAIAccount{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpdateOpenAIAccountStatus(context.Context, adminrepo.UpdateOpenAIAccountStatusInput) (entity.OpenAIAccount, error) {
	return entity.OpenAIAccount{}, nil
}

func (store *fakeRouterAdminStore) DeleteOpenAIAccount(context.Context, string) (entity.OpenAIAccount, error) {
	return entity.OpenAIAccount{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListGitHubAccounts(context.Context, int) ([]entity.GitHubAccount, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) GetGitHubAccount(context.Context, string) (entity.GitHubAccount, error) {
	return entity.GitHubAccount{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) UpsertGitHubAccount(context.Context, adminrepo.UpsertGitHubAccountInput) (entity.GitHubAccount, bool, error) {
	return entity.GitHubAccount{}, true, nil
}

func (store *fakeRouterAdminStore) DeleteGitHubAccount(context.Context, string) (entity.GitHubAccount, error) {
	return entity.GitHubAccount{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) CreateAgentFlow(context.Context, adminrepo.CreateAgentFlowInput) (entity.AgentFlow, bool, error) {
	return entity.AgentFlow{}, true, nil
}

func (store *fakeRouterAdminStore) GetAgentFlow(context.Context, string) (entity.AgentFlow, error) {
	return entity.AgentFlow{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentFlows(context.Context, string, int) ([]entity.AgentFlow, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpdateAgentFlow(context.Context, adminrepo.UpdateAgentFlowInput) (entity.AgentFlow, error) {
	return entity.AgentFlow{}, nil
}

func (store *fakeRouterAdminStore) CreateAgentRun(context.Context, adminrepo.CreateAgentRunInput) (entity.AgentRun, error) {
	return entity.AgentRun{}, nil
}

func (store *fakeRouterAdminStore) GetAgentRun(context.Context, string) (entity.AgentRun, error) {
	return entity.AgentRun{}, adminrepo.ErrNotFound
}

func (store *fakeRouterAdminStore) ListAgentRuns(context.Context, int) ([]entity.AgentRun, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) ListAgentRunsByFlowID(context.Context, string) ([]entity.AgentRun, error) {
	return nil, nil
}

func (store *fakeRouterAdminStore) UpdateAgentRunArtifacts(context.Context, adminrepo.UpdateAgentRunArtifactsInput) (entity.AgentRun, error) {
	return entity.AgentRun{}, nil
}

func (store *fakeRouterAdminStore) RecordAuditEvent(context.Context, adminrepo.AuditEventInput) error {
	store.auditRecorded = true
	return nil
}

func (store *fakeRouterAdminStore) ensureRepositories() {
	if store.repositories == nil {
		store.repositories = map[string]entity.Repository{}
	}
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
