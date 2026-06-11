package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	transportmodels "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http/models"
	githubapi "github.com/google/go-github/v88/github"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	pathHealthz       = "/healthz"
	pathHealthLivez   = "/health/livez"
	pathHealthReady   = "/health/readyz"
	pathReadyz        = "/readyz"
	pathMetrics       = "/metrics"
	pathAgentsSlash   = "/mattermost/slash/agents"
	pathAgentsAction  = "/mattermost/actions/agents"
	pathAgentsDialog  = "/mattermost/dialogs/agents"
	pathFlowAction    = "/mattermost/actions/flow"
	pathGitHubWebhook = "/github/webhook"
)

type DialogOpener interface {
	OpenDialog(ctx context.Context, triggerID string, dialog statusservice.MattermostDialog) error
}

type RouterConfig struct {
	StatusService          *statusservice.StatusService
	SlashService           *statusservice.SlashCommandService
	DialogOpener           DialogOpener
	EphemeralCardPublisher statusservice.EphemeralCardPublisher
	Localizer              *texti18n.Localizer
	SlashToken             string
	GitHubWebhookSecret    string
	MaxSlashFormBytes      int64
	MaxGitHubWebhookBytes  int64
	PrometheusRegistry     *prometheus.Registry
	Logger                 *slog.Logger
}

type Router struct {
	statusService          *statusservice.StatusService
	slashService           *statusservice.SlashCommandService
	dialogOpener           DialogOpener
	ephemeralCardPublisher statusservice.EphemeralCardPublisher
	localizer              *texti18n.Localizer
	slashToken             string
	gitHubWebhookSecret    string
	maxSlashFormBytes      int64
	maxGitHubWebhookBytes  int64
	logger                 *slog.Logger
	mux                    *http.ServeMux
}

var _ http.Handler = (*Router)(nil)

func NewRouter(cfg RouterConfig) *Router {
	router := &Router{
		statusService:          cfg.StatusService,
		slashService:           cfg.SlashService,
		dialogOpener:           cfg.DialogOpener,
		ephemeralCardPublisher: cfg.EphemeralCardPublisher,
		localizer:              cfg.Localizer,
		slashToken:             cfg.SlashToken,
		gitHubWebhookSecret:    cfg.GitHubWebhookSecret,
		maxSlashFormBytes:      cfg.MaxSlashFormBytes,
		maxGitHubWebhookBytes:  cfg.MaxGitHubWebhookBytes,
		logger:                 cfg.Logger,
		mux:                    http.NewServeMux(),
	}
	registry := cfg.PrometheusRegistry
	if registry == nil {
		registry = prometheus.NewRegistry()
	}
	router.mux.HandleFunc(pathHealthz, router.handleHealth)
	router.mux.HandleFunc(pathHealthLivez, router.handleLivez)
	router.mux.HandleFunc(pathHealthReady, router.handleReady)
	router.mux.HandleFunc(pathReadyz, router.handleReady)
	router.mux.Handle(pathMetrics, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry, EnableOpenMetrics: true}))
	router.mux.HandleFunc(pathAgentsSlash, router.handleAgentsSlash)
	router.mux.HandleFunc(pathAgentsAction, router.handleAgentsAction)
	router.mux.HandleFunc(pathAgentsDialog, router.handleAgentsDialog)
	router.mux.HandleFunc(pathFlowAction, router.handleFlowAction)
	router.mux.HandleFunc(pathGitHubWebhook, router.handleGitHubWebhook)
	return router
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	router.mux.ServeHTTP(w, r)
}

func (router *Router) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse(router.statusService.Snapshot()))
}

func (router *Router) handleLivez(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	writeJSON(w, http.StatusOK, transportmodels.ReadyResponse{Status: "live", Service: "matter-codex-bot-service"})
}

func (router *Router) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	writeJSON(w, http.StatusOK, transportmodels.ReadyResponse{Status: "ready", Service: "matter-codex-bot-service"})
}

func (router *Router) handleAgentsSlash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, router.maxSlashFormBytes)
	if err := r.ParseForm(); err != nil {
		router.logWarn("invalid slash form", "error", err)
		writeCommandResponse(w, http.StatusBadRequest, ephemeral(router.t("router.slash.invalid_request", nil)))
		return
	}
	if strings.TrimSpace(router.slashToken) == "" {
		writeCommandResponse(w, http.StatusServiceUnavailable, ephemeral(router.t("router.slash.token_missing", nil)))
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(router.slashToken)) != 1 {
		writeCommandResponse(w, http.StatusUnauthorized, ephemeral(router.t("router.slash.token_invalid", nil)))
		return
	}

	text := strings.TrimSpace(r.PostForm.Get("text"))
	if router.slashService == nil {
		writeCommandResponse(w, http.StatusOK, ephemeral(router.statusService.SlashStatusText()))
		return
	}
	result := router.slashService.HandleResponse(r.Context(), statusservice.SlashCommand{
		Text:        text,
		UserID:      strings.TrimSpace(r.PostForm.Get("user_id")),
		UserName:    strings.TrimSpace(r.PostForm.Get("user_name")),
		ChannelID:   strings.TrimSpace(r.PostForm.Get("channel_id")),
		ChannelName: strings.TrimSpace(r.PostForm.Get("channel_name")),
		TeamID:      strings.TrimSpace(r.PostForm.Get("team_id")),
		TeamDomain:  strings.TrimSpace(r.PostForm.Get("team_domain")),
	})
	writeCommandResponse(w, http.StatusOK, slashResponse(result))
}

func (router *Router) handleAgentsAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, router.maxSlashFormBytes)
	var request mattermostmodel.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		router.logWarn("invalid Mattermost agents action", "error", err)
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_agents_action"})
		return
	}
	if router.slashService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "slash_service_not_configured"})
		return
	}
	result := router.slashService.HandleMenuAction(r.Context(), statusservice.MenuActionCommand{
		View:      contextString(request.Context, "view"),
		Command:   contextString(request.Context, "command"),
		Dialog:    contextString(request.Context, "dialog"),
		UserID:    strings.TrimSpace(request.UserId),
		UserName:  strings.TrimSpace(request.UserName),
		ChannelID: strings.TrimSpace(request.ChannelId),
		PostID:    strings.TrimSpace(request.PostId),
	})
	status := result.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	response := &mattermostmodel.PostActionIntegrationResponse{
		EphemeralText: result.EphemeralText,
	}
	if result.Dialog != nil {
		triggerID := strings.TrimSpace(request.TriggerId)
		if router.logger != nil {
			router.logger.Info("opening Mattermost dialog", "view", contextString(request.Context, "view"), "dialog", contextString(request.Context, "dialog"), "trigger_present", triggerID != "")
		}
		if triggerID == "" {
			response.EphemeralText = router.t("router.dialog.trigger_missing", nil)
			writeJSON(w, http.StatusBadRequest, response)
			return
		}
		if router.dialogOpener == nil {
			response.EphemeralText = router.t("router.dialog.opener_missing", nil)
			writeJSON(w, http.StatusServiceUnavailable, response)
			return
		}
		if err := router.dialogOpener.OpenDialog(r.Context(), triggerID, *result.Dialog); err != nil {
			router.logWarn("open Mattermost dialog failed", "error", err)
			response.EphemeralText = router.t("router.dialog.open_failed", nil)
			writeJSON(w, http.StatusBadGateway, response)
			return
		}
		writeJSON(w, status, response)
		return
	}
	if result.Card != nil {
		response.Update = cardPost(*result.Card)
	}
	writeJSON(w, status, response)
}

func (router *Router) handleAgentsDialog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, router.maxSlashFormBytes)
	var request mattermostmodel.SubmitDialogRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		router.logWarn("invalid Mattermost agents dialog", "error", err)
		writeJSON(w, http.StatusBadRequest, mattermostmodel.SubmitDialogResponse{Error: router.t("router.dialog.invalid_request", nil)})
		return
	}
	if router.slashService == nil {
		writeJSON(w, http.StatusServiceUnavailable, mattermostmodel.SubmitDialogResponse{Error: router.t("router.dialog.service_missing", nil)})
		return
	}
	result := router.slashService.HandleDialogSubmission(r.Context(), statusservice.DialogSubmissionCommand{
		CallbackID: strings.TrimSpace(request.CallbackId),
		State:      strings.TrimSpace(request.State),
		UserID:     strings.TrimSpace(request.UserId),
		ChannelID:  strings.TrimSpace(request.ChannelId),
		TeamID:     strings.TrimSpace(request.TeamId),
		Submission: request.Submission,
		Cancelled:  request.Cancelled,
	})
	status := result.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	if result.Error != "" || len(result.Errors) > 0 {
		writeJSON(w, status, mattermostmodel.SubmitDialogResponse{
			Error:  result.Error,
			Errors: result.Errors,
		})
		return
	}
	if result.Card != nil {
		if router.ephemeralCardPublisher != nil && strings.TrimSpace(request.UserId) != "" {
			if err := router.ephemeralCardPublisher.PostEphemeralCard(r.Context(), strings.TrimSpace(request.UserId), *result.Card); err != nil {
				router.logWarn("post Mattermost ephemeral dialog result failed", "error", err)
			}
		} else {
			router.logWarn("Mattermost ephemeral dialog result skipped", "publisher_configured", router.ephemeralCardPublisher != nil, "user_id_present", strings.TrimSpace(request.UserId) != "")
		}
	}
	writeJSON(w, status, mattermostmodel.SubmitDialogResponse{Type: string(mattermostmodel.SubmitDialogResponseTypeOK)})
}

func (router *Router) handleFlowAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, router.maxSlashFormBytes)
	var request mattermostmodel.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		router.logWarn("invalid Mattermost flow action", "error", err)
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_flow_action"})
		return
	}
	if router.slashService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "slash_service_not_configured"})
		return
	}
	result := router.slashService.HandleFlowAction(r.Context(), statusservice.FlowActionCommand{
		FlowID:    contextString(request.Context, "flow_id"),
		Action:    contextString(request.Context, "action"),
		Token:     contextString(request.Context, "token"),
		UserID:    strings.TrimSpace(request.UserId),
		UserName:  strings.TrimSpace(request.UserName),
		ChannelID: strings.TrimSpace(request.ChannelId),
		PostID:    strings.TrimSpace(request.PostId),
	})
	status := result.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, &mattermostmodel.PostActionIntegrationResponse{
		EphemeralText: result.EphemeralText,
	})
}

func (router *Router) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
		return
	}
	if strings.TrimSpace(router.gitHubWebhookSecret) == "" {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "github_webhook_secret_not_configured"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, router.maxGitHubWebhookBytes)
	payload, err := githubapi.ValidatePayload(r, []byte(router.gitHubWebhookSecret))
	if err != nil {
		router.logWarn("invalid github webhook signature", "error", err)
		writeJSON(w, http.StatusUnauthorized, transportmodels.ErrorResponse{Error: "invalid_github_webhook_signature"})
		return
	}
	eventType := githubapi.WebHookType(r)
	if strings.TrimSpace(eventType) == "" {
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "missing_github_webhook_event"})
		return
	}
	if _, err := githubapi.ParseWebHook(eventType, payload); err != nil {
		router.logWarn("invalid github webhook payload", "event", eventType, "error", err)
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_github_webhook_payload"})
		return
	}
	if router.logger != nil {
		router.logger.Info("github webhook accepted", "event", eventType)
	}
	writeJSON(w, http.StatusAccepted, transportmodels.GitHubWebhookResponse{
		Status: "accepted",
		Event:  eventType,
	})
}

func (router *Router) logWarn(message string, args ...any) {
	if router.logger != nil {
		router.logger.Warn(message, args...)
	}
}

func (router *Router) t(messageID string, data map[string]any) string {
	return router.localizer.T(messageID, data)
}

func healthResponse(snapshot value.StatusSnapshot) transportmodels.HealthResponse {
	return transportmodels.HealthResponse{
		Status:               snapshot.Status,
		Service:              snapshot.ServiceName,
		Version:              string(snapshot.ServiceVersion),
		MattermostConfigured: snapshot.MattermostConfigured,
		BotTokenConfigured:   snapshot.BotTokenConfigured,
		SlashTokenConfigured: snapshot.SlashTokenConfigured,
		DatabaseConfigured:   snapshot.DatabaseConfigured,
		StorageReady:         snapshot.StorageReady,
		RuntimeConfigured:    snapshot.RuntimeConfigured,
		DefaultTeam:          snapshot.DefaultTeamName,
		DefaultChannels:      snapshot.DefaultChannels,
	}
}

func ephemeral(text string) *mattermostmodel.CommandResponse {
	return &mattermostmodel.CommandResponse{
		ResponseType: mattermostmodel.CommandResponseTypeEphemeral,
		Text:         text,
	}
}

func slashResponse(result statusservice.SlashResponse) *mattermostmodel.CommandResponse {
	response := ephemeral(result.Text)
	if result.ChannelVisible {
		response.ResponseType = mattermostmodel.CommandResponseTypeInChannel
	}
	if result.Card != nil {
		response.Attachments = []*mattermostmodel.MessageAttachment{
			cardAttachment(*result.Card),
		}
	}
	return response
}

func cardPost(card statusservice.MattermostCard) *mattermostmodel.Post {
	post := &mattermostmodel.Post{
		Id:        card.PostID,
		ChannelId: card.ChannelID,
		Message:   card.Message,
	}
	post.SetProps(mattermostmodel.StringInterface{
		"attachments": []*mattermostmodel.MessageAttachment{
			cardAttachment(card),
		},
	})
	return post
}

func cardAttachment(card statusservice.MattermostCard) *mattermostmodel.MessageAttachment {
	fields := make([]*mattermostmodel.MessageAttachmentField, 0, len(card.Fields))
	for _, field := range card.Fields {
		fields = append(fields, &mattermostmodel.MessageAttachmentField{
			Title: field.Title,
			Value: field.Value,
			Short: mattermostmodel.SlackCompatibleBool(field.Short),
		})
	}
	actions := make([]*mattermostmodel.PostAction, 0, len(card.Actions))
	for _, action := range card.Actions {
		actions = append(actions, &mattermostmodel.PostAction{
			Id:       action.ID,
			Type:     mattermostmodel.PostActionTypeButton,
			Name:     action.Name,
			Tooltip:  action.Tooltip,
			Style:    action.Style,
			Disabled: action.Disabled,
			Integration: &mattermostmodel.PostActionIntegration{
				URL:     card.ActionURL,
				Context: action.Context,
			},
		})
	}
	return &mattermostmodel.MessageAttachment{
		Fallback: card.Title,
		Color:    card.Color,
		Title:    card.Title,
		Text:     card.Text,
		Fields:   fields,
		Actions:  actions,
	}
}

func writeCommandResponse(w http.ResponseWriter, status int, response *mattermostmodel.CommandResponse) {
	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func contextString(context map[string]any, key string) string {
	value, ok := context[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
