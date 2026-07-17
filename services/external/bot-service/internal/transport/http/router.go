package http

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	pathGitHubWebhook = "/github/webhook"
	pathAgentSessions = "/internal/agent-sessions/"
	pathMCPSessions   = "/mcp/sessions/"

	dialogCallbackResult = "agents_dialog_result"
)

type DialogOpener interface {
	OpenDialog(ctx context.Context, triggerID string, dialog statusservice.MattermostDialog) error
}

type RouterConfig struct {
	StatusService         *statusservice.StatusService
	SlashService          *statusservice.SlashCommandService
	SessionService        *statusservice.AgentSessionService
	DialogOpener          DialogOpener
	Localizer             *texti18n.Localizer
	SlashToken            string
	GitHubWebhookSecret   string
	MaxSlashFormBytes     int64
	MaxGitHubWebhookBytes int64
	PrometheusRegistry    *prometheus.Registry
	MattermostHTTPClient  *http.Client
	Logger                *slog.Logger
}

type Router struct {
	statusService         *statusservice.StatusService
	slashService          *statusservice.SlashCommandService
	sessionService        *statusservice.AgentSessionService
	dialogOpener          DialogOpener
	localizer             *texti18n.Localizer
	slashToken            string
	gitHubWebhookSecret   string
	maxSlashFormBytes     int64
	maxGitHubWebhookBytes int64
	mattermostHTTPClient  *http.Client
	logger                *slog.Logger
	mcpHandler            http.Handler
	mux                   *http.ServeMux
}

var _ http.Handler = (*Router)(nil)

func NewRouter(cfg RouterConfig) *Router {
	mattermostHTTPClient := cfg.MattermostHTTPClient
	if mattermostHTTPClient == nil {
		mattermostHTTPClient = http.DefaultClient
	}
	router := &Router{
		statusService:         cfg.StatusService,
		slashService:          cfg.SlashService,
		sessionService:        cfg.SessionService,
		dialogOpener:          cfg.DialogOpener,
		localizer:             cfg.Localizer,
		slashToken:            cfg.SlashToken,
		gitHubWebhookSecret:   cfg.GitHubWebhookSecret,
		maxSlashFormBytes:     cfg.MaxSlashFormBytes,
		maxGitHubWebhookBytes: cfg.MaxGitHubWebhookBytes,
		mattermostHTTPClient:  mattermostHTTPClient,
		logger:                cfg.Logger,
		mux:                   http.NewServeMux(),
	}
	if cfg.SessionService != nil {
		router.mcpHandler = newMCPHandler(cfg.SessionService)
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
	router.mux.HandleFunc(pathGitHubWebhook, router.handleGitHubWebhook)
	router.mux.HandleFunc(pathAgentSessions, router.handleAgentSessionInternal)
	if router.mcpHandler != nil {
		router.mux.Handle(pathMCPSessions, router.mcpHandler)
	}
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
	if contextString(request.Context, "kind") == "agent_turn" {
		router.handleAgentTurnAction(w, r, request)
		return
	}
	if router.slashService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "slash_service_not_configured"})
		return
	}
	command := statusservice.MenuActionCommand{
		View:      contextString(request.Context, "view"),
		Command:   contextString(request.Context, "command"),
		Dialog:    contextString(request.Context, "dialog"),
		Action:    contextString(request.Context, "action"),
		Resource:  contextString(request.Context, "resource_type"),
		ID:        contextString(request.Context, "resource_id"),
		Page:      contextInt(request.Context, "page"),
		UserID:    strings.TrimSpace(request.UserId),
		UserName:  strings.TrimSpace(request.UserName),
		ChannelID: strings.TrimSpace(request.ChannelId),
		PostID:    strings.TrimSpace(request.PostId),
	}
	if router.slashService.ShouldRunMenuActionAsync(command) {
		result := router.slashService.AsyncMenuActionAccepted(command)
		response := &mattermostmodel.PostActionIntegrationResponse{EphemeralText: result.EphemeralText}
		if result.Card != nil {
			response.Update = cardPost(*result.Card)
		}
		writeJSON(w, result.StatusCode, response)
		go router.runAsyncMenuAction(context.WithoutCancel(r.Context()), command)
		return
	}
	result := router.slashService.HandleMenuAction(r.Context(), command)
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

func (router *Router) runAsyncMenuAction(parent context.Context, command statusservice.MenuActionCommand) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	result := router.slashService.HandleMenuAction(ctx, command)
	if result.StatusCode >= http.StatusBadRequest {
		router.logWarn("asynchronous Mattermost menu action failed", "action", command.Action, "status", result.StatusCode)
	}
}

func (router *Router) handleAgentTurnAction(w http.ResponseWriter, r *http.Request, request mattermostmodel.PostActionIntegrationRequest) {
	if router.sessionService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "agent_session_service_not_configured"})
		return
	}
	switch contextString(request.Context, "action") {
	case "stop_turn":
		result, err := router.sessionService.StopAgentSessionTurns(r.Context(), statusservice.StopAgentSessionTurnsCommand{
			TurnIDs:   contextInt64List(request.Context, "turn_ids"),
			UserID:    strings.TrimSpace(request.UserId),
			UserName:  strings.TrimSpace(request.UserName),
			ChannelID: strings.TrimSpace(request.ChannelId),
			PostID:    strings.TrimSpace(request.PostId),
		})
		if err != nil {
			router.logWarn("agent turn stop failed", "error", err)
			writeJSON(w, http.StatusBadGateway, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("chat.session.turn.stop.failed", nil)})
			return
		}
		response := &mattermostmodel.PostActionIntegrationResponse{EphemeralText: result.Message}
		if result.Card != nil {
			response.Update = cardPost(*result.Card)
		}
		writeJSON(w, http.StatusOK, response)
	case "retry_turn":
		turnIDs := contextInt64List(request.Context, "turn_ids")
		if len(turnIDs) != 1 {
			writeJSON(w, http.StatusBadRequest, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("chat.session.turn.retry.failed", nil)})
			return
		}
		result, err := router.sessionService.RetryFailedTurn(r.Context(), statusservice.RetryAgentSessionTurnCommand{
			TurnID:    turnIDs[0],
			UserID:    strings.TrimSpace(request.UserId),
			UserName:  strings.TrimSpace(request.UserName),
			ChannelID: strings.TrimSpace(request.ChannelId),
			PostID:    strings.TrimSpace(request.PostId),
		})
		if err != nil {
			router.logWarn("agent turn retry failed", "error", err)
			writeJSON(w, http.StatusBadGateway, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("chat.session.turn.retry.failed", nil)})
			return
		}
		response := &mattermostmodel.PostActionIntegrationResponse{EphemeralText: result.Message}
		if result.Card != nil {
			response.Update = cardPost(*result.Card)
		}
		writeJSON(w, http.StatusOK, response)
	default:
		writeJSON(w, http.StatusBadRequest, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("menu.action.unknown", nil)})
	}
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
	if strings.TrimSpace(request.CallbackId) == dialogCallbackResult {
		writeJSON(w, http.StatusOK, mattermostmodel.SubmitDialogResponse{Type: string(mattermostmodel.SubmitDialogResponseTypeOK)})
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
	if result.Dialog != nil {
		writeJSON(w, status, mattermostmodel.SubmitDialogResponse{
			Type: string(mattermostmodel.SubmitDialogResponseTypeForm),
			Form: mattermostDialogForm(*result.Dialog),
		})
		return
	}
	if result.Card != nil {
		router.publishDialogResult(r.Context(), request.URL, *result.Card)
		writeJSON(w, status, mattermostmodel.SubmitDialogResponse{Type: string(mattermostmodel.SubmitDialogResponseTypeOK)})
		return
	}
	writeJSON(w, status, mattermostmodel.SubmitDialogResponse{Type: string(mattermostmodel.SubmitDialogResponseTypeOK)})
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

func (router *Router) handleAgentSessionInternal(w http.ResponseWriter, r *http.Request) {
	if router.sessionService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "agent_session_service_not_configured"})
		return
	}
	sessionKey, action, ok := parseAgentSessionInternalPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusNotFound, transportmodels.ErrorResponse{Error: "not_found"})
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, transportmodels.ErrorResponse{Error: "missing_bearer_token"})
		return
	}
	switch action {
	case "snapshot":
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
			return
		}
		result, err := router.sessionService.Snapshot(r.Context(), sessionKey, token)
		if err != nil {
			router.logWarn("agent session snapshot failed", "error", err)
			writeJSON(w, http.StatusUnauthorized, transportmodels.ErrorResponse{Error: "agent_session_snapshot_failed"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	case "turns/claim":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
			return
		}
		result, err := router.sessionService.ClaimNextTurn(r.Context(), sessionKey, token)
		if err != nil {
			router.logWarn("agent session claim failed", "error", err)
			writeJSON(w, http.StatusUnauthorized, transportmodels.ErrorResponse{Error: "agent_session_claim_failed"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	case "turns/complete":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
			return
		}
		var command statusservice.CompleteAgentSessionTurnCommand
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_complete_payload"})
			return
		}
		if err := router.sessionService.CompleteTurn(r.Context(), sessionKey, token, command); err != nil {
			router.logWarn("agent session complete failed", "error", err)
			writeJSON(w, http.StatusBadGateway, transportmodels.ErrorResponse{Error: "agent_session_complete_failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "turns/status":
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, transportmodels.ErrorResponse{Error: "method_not_allowed"})
			return
		}
		var command statusservice.UpdateAgentSessionTurnStatusCommand
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_status_payload"})
			return
		}
		result, err := router.sessionService.UpdateTurnSystemStatus(r.Context(), sessionKey, token, command)
		if err != nil {
			router.logWarn("agent session status update failed", "error", err)
			writeJSON(w, http.StatusBadGateway, transportmodels.ErrorResponse{Error: "agent_session_status_failed"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeJSON(w, http.StatusNotFound, transportmodels.ErrorResponse{Error: "not_found"})
	}
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
		RootId:    card.RootPostID,
		Message:   card.Message,
	}
	props := mattermostmodel.StringInterface{}
	for key, value := range card.Props {
		props[key] = value
	}
	props["attachments"] = []*mattermostmodel.MessageAttachment{cardAttachment(card)}
	post.SetProps(props)
	return post
}

func mattermostDialogForm(dialog statusservice.MattermostDialog) *mattermostmodel.Dialog {
	elements := make([]mattermostmodel.DialogElement, 0, len(dialog.Elements))
	for _, element := range dialog.Elements {
		options := make([]*mattermostmodel.PostActionOptions, 0, len(element.Options))
		for _, option := range element.Options {
			options = append(options, &mattermostmodel.PostActionOptions{
				Text:  option.Text,
				Value: option.Value,
			})
		}
		elements = append(elements, mattermostmodel.DialogElement{
			DisplayName: element.DisplayName,
			Name:        element.Name,
			Type:        element.Type,
			SubType:     element.SubType,
			Default:     element.Default,
			Placeholder: element.Placeholder,
			HelpText:    element.HelpText,
			Optional:    element.Optional,
			MinLength:   element.MinLength,
			MaxLength:   element.MaxLength,
			Options:     options,
		})
	}
	return &mattermostmodel.Dialog{
		CallbackId:       dialog.CallbackID,
		Title:            dialog.Title,
		IntroductionText: dialog.IntroductionText,
		Elements:         elements,
		SubmitLabel:      dialog.SubmitLabel,
		State:            dialog.State,
	}
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

func (router *Router) publishDialogResult(ctx context.Context, responseURL string, card statusservice.MattermostCard) {
	responseURL = strings.TrimSpace(responseURL)
	if responseURL == "" {
		return
	}
	go router.postDialogResult(ctx, responseURL, card)
}

func (router *Router) postDialogResult(ctx context.Context, responseURL string, card statusservice.MattermostCard) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	payload := dialogResultResponse(card)
	body, err := json.Marshal(payload)
	if err != nil {
		router.logWarn("marshal Mattermost dialog result failed", "error", err)
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, responseURL, bytes.NewReader(body))
	if err != nil {
		router.logWarn("create Mattermost dialog result request failed", "error", err)
		return
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := router.mattermostHTTPClient.Do(request)
	if err != nil {
		router.logWarn("post Mattermost dialog result failed", "error", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		router.logWarn("post Mattermost dialog result returned non-success status", "status", response.StatusCode)
	}
}

func dialogResultResponse(card statusservice.MattermostCard) *mattermostmodel.CommandResponse {
	text := strings.TrimSpace(card.Message)
	if text == "" {
		text = strings.TrimSpace(card.Text)
	}
	if text == "" {
		text = strings.TrimSpace(card.Title)
	}
	response := ephemeral(text)
	response.Attachments = []*mattermostmodel.MessageAttachment{
		cardAttachment(card),
	}
	return response
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

func contextInt(context map[string]any, key string) int {
	value := contextString(context, key)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func contextInt64List(context map[string]any, key string) []int64 {
	value := contextString(context, key)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]int64, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && parsed > 0 {
			items = append(items, parsed)
		}
	}
	return items
}

func parseAgentSessionInternalPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, pathAgentSessions)
	if rest == path || rest == "" {
		return "", "", false
	}
	sessionKey, action, ok := strings.Cut(rest, "/")
	if !ok || strings.TrimSpace(sessionKey) == "" || strings.TrimSpace(action) == "" {
		return "", "", false
	}
	return strings.TrimSpace(sessionKey), strings.TrimSpace(action), true
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	prefix := "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
