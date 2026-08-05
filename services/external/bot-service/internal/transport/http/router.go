package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	transportmodels "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http/models"
	githubapi "github.com/google/go-github/v88/github"
	"github.com/google/uuid"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	pathHealthz           = "/healthz"
	pathHealthLivez       = "/health/livez"
	pathHealthReady       = "/health/readyz"
	pathReadyz            = "/readyz"
	pathMetrics           = "/metrics"
	pathAgentsSlash       = "/mattermost/slash/agents"
	pathAgentsAction      = "/mattermost/actions/agents"
	pathAgentsDialog      = "/mattermost/dialogs/agents"
	pathGitHubWebhook     = "/github/webhook"
	pathRuntimeMCPBinding = "/internal/runtime-mcp-bindings"
	pathMCPSessions       = "/mcp/sessions/"
	pathControlCenter     = "/control-center/"
	dialogCallbackResult  = "agents_dialog_result"
)

type RouteBoundary string

const (
	RouteBoundaryPublic  RouteBoundary = "public"
	RouteBoundaryCluster RouteBoundary = "cluster"
)

type RegisteredRoute struct {
	Path     string        `json:"path"`
	Boundary RouteBoundary `json:"boundary"`
}

type DialogOpener interface {
	OpenDialog(ctx context.Context, triggerID string, dialog statusservice.MattermostDialog) error
}

type RouterConfig struct {
	StatusService                   *statusservice.StatusService
	SlashService                    *statusservice.SlashCommandService
	SessionService                  *statusservice.AgentSessionService
	DialogOpener                    DialogOpener
	InteractionSecurity             *statusservice.InteractionSecurityService
	Localizer                       *texti18n.Localizer
	SlashToken                      string
	GitHubWebhookSecret             string
	MaxSlashFormBytes               int64
	MaxGitHubWebhookBytes           int64
	MaxMCPRequestBodyBytes          int64
	PrometheusRegistry              *prometheus.Registry
	MattermostSiteURL               string
	MattermostInternalURL           string
	ThreadPublisher                 statusservice.MattermostThreadPublisher
	ControlCenterAssetsDir          string
	MattermostResolver              mattermostDNSResolver
	MattermostDialer                mattermostContextDialer
	Logger                          *slog.Logger
	RuntimeMCPBindingClientSPIFFEID string
}

type Router struct {
	statusService                   *statusservice.StatusService
	slashService                    *statusservice.SlashCommandService
	sessionService                  *statusservice.AgentSessionService
	dialogOpener                    DialogOpener
	localizer                       *texti18n.Localizer
	slashToken                      string
	gitHubWebhookSecret             string
	maxSlashFormBytes               int64
	maxGitHubWebhookBytes           int64
	interactionSecurity             *statusservice.InteractionSecurityService
	mattermostResponses             *mattermostResponseClient
	threadPublisher                 statusservice.MattermostThreadPublisher
	logger                          *slog.Logger
	runtimeMCPBindingClientSPIFFEID string
	mcpHandler                      http.Handler
	mux                             *http.ServeMux
	registeredRoutes                []RegisteredRoute
}

var _ http.Handler = (*Router)(nil)

func NewRouter(cfg RouterConfig) *Router {
	router := &Router{
		statusService:                   cfg.StatusService,
		slashService:                    cfg.SlashService,
		sessionService:                  cfg.SessionService,
		dialogOpener:                    cfg.DialogOpener,
		localizer:                       cfg.Localizer,
		slashToken:                      cfg.SlashToken,
		gitHubWebhookSecret:             cfg.GitHubWebhookSecret,
		maxSlashFormBytes:               cfg.MaxSlashFormBytes,
		maxGitHubWebhookBytes:           cfg.MaxGitHubWebhookBytes,
		interactionSecurity:             cfg.InteractionSecurity,
		mattermostResponses:             newMattermostResponseClient(cfg.MattermostSiteURL, cfg.MattermostInternalURL, cfg.MattermostResolver, cfg.MattermostDialer),
		threadPublisher:                 cfg.ThreadPublisher,
		logger:                          cfg.Logger,
		runtimeMCPBindingClientSPIFFEID: cfg.RuntimeMCPBindingClientSPIFFEID,
		mux:                             http.NewServeMux(),
	}
	if cfg.SessionService != nil {
		router.mcpHandler = newMCPHandler(cfg.SessionService, cfg.MaxMCPRequestBodyBytes)
	}
	registry := cfg.PrometheusRegistry
	if registry == nil {
		registry = prometheus.NewRegistry()
	}
	router.register(pathHealthz, RouteBoundaryCluster, http.HandlerFunc(router.handleHealth))
	router.register(pathHealthLivez, RouteBoundaryCluster, http.HandlerFunc(router.handleLivez))
	router.register(pathHealthReady, RouteBoundaryCluster, http.HandlerFunc(router.handleReady))
	router.register(pathReadyz, RouteBoundaryCluster, http.HandlerFunc(router.handleReady))
	router.register(pathMetrics, RouteBoundaryCluster, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry, EnableOpenMetrics: true}))
	router.register(pathAgentsSlash, RouteBoundaryPublic, http.HandlerFunc(router.handleAgentsSlash))
	router.register(pathAgentsAction, RouteBoundaryCluster, http.HandlerFunc(router.handleAgentsAction))
	router.register(pathAgentsDialog, RouteBoundaryCluster, http.HandlerFunc(router.handleAgentsDialog))
	router.register(pathGitHubWebhook, RouteBoundaryPublic, http.HandlerFunc(router.handleGitHubWebhook))
	router.register(pathRuntimeMCPBinding, RouteBoundaryCluster, http.HandlerFunc(router.handleRuntimeMCPBinding))
	if router.mcpHandler != nil {
		router.register(pathMCPSessions, RouteBoundaryCluster, router.mcpHandler)
	}
	assets := http.NotFoundHandler()
	if assetsDir := strings.TrimSpace(cfg.ControlCenterAssetsDir); assetsDir != "" {
		assets = http.StripPrefix(pathControlCenter, http.FileServer(http.Dir(assetsDir)))
	}
	router.register(strings.TrimSuffix(pathControlCenter, "/"), RouteBoundaryPublic, http.RedirectHandler(pathControlCenter, http.StatusTemporaryRedirect))
	router.register(pathControlCenter, RouteBoundaryPublic, assets)
	return router
}

func (router *Router) handleRuntimeMCPBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || router.sessionService == nil ||
		!requestHasSPIFFE(r, router.runtimeMCPBindingClientSPIFFEID) {
		writeJSON(w, http.StatusForbidden, transportmodels.ErrorResponse{Error: "runtime_mcp_binding_forbidden"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var request statusservice.RuntimeMCPBindingRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, (4<<10)+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		uuid.Validate(request.ControlSessionID) != nil {
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "runtime_mcp_binding_invalid"})
		return
	}
	binding, err := router.sessionService.EnsureRuntimeMCPBinding(r.Context(), request)
	if err != nil {
		router.logWarn("runtime MCP binding failed", "error", err)
		writeJSON(w, http.StatusConflict, transportmodels.ErrorResponse{Error: "runtime_mcp_binding_failed"})
		return
	}
	writeJSON(w, http.StatusOK, binding)
}

func requestHasSPIFFE(request *http.Request, expected string) bool {
	if expected == "" || request.TLS == nil || len(request.TLS.VerifiedChains) != 1 ||
		len(request.TLS.VerifiedChains[0]) == 0 {
		return false
	}
	certificate := request.TLS.VerifiedChains[0][0]
	return len(certificate.URIs) == 1 && certificate.URIs[0].String() == expected
}

func (router *Router) register(path string, boundary RouteBoundary, handler http.Handler) {
	router.mux.Handle(path, handler)
	router.registeredRoutes = append(router.registeredRoutes, RegisteredRoute{Path: path, Boundary: boundary})
}

func (router *Router) RegisteredRoutes() []RegisteredRoute {
	return append([]RegisteredRoute(nil), router.registeredRoutes...)
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
	if result.Card != nil {
		result.Card.ChannelID = strings.TrimSpace(r.PostForm.Get("channel_id"))
		result.Card.Interaction = statusservice.MattermostCardInteraction{Actor: statusservice.AuthenticatedActor{
			UserID:   strings.TrimSpace(r.PostForm.Get("user_id")),
			UserName: strings.TrimSpace(r.PostForm.Get("user_name")),
		}, Scope: statusservice.InteractionScope{}}
		if router.threadPublisher == nil {
			writeCommandResponse(w, http.StatusServiceUnavailable, ephemeral("interaction_capability_unavailable"))
			return
		}
		if _, err := router.threadPublisher.PostThreadCard(r.Context(), *result.Card); err != nil {
			router.logWarn("publish secured slash card failed", "error", err)
			writeCommandResponse(w, http.StatusServiceUnavailable, ephemeral("interaction_capability_unavailable"))
			return
		}
		result.Card = nil
		result.ChannelVisible = false
	}
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
		writeJSON(w, http.StatusGone, transportmodels.ErrorResponse{Error: "legacy_agent_turn_action_removed"})
		return
	}
	interaction, err := router.interactionSecurity.AuthenticateAction(r.Context(), statusservice.ActionCallback{
		Context:   request.Context,
		UserID:    strings.TrimSpace(request.UserId),
		ChannelID: strings.TrimSpace(request.ChannelId),
		PostID:    strings.TrimSpace(request.PostId),
	})
	if err != nil {
		router.writeInteractionDenied(w, err)
		return
	}
	if router.slashService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "slash_service_not_configured"})
		return
	}
	command := statusservice.MenuActionCommand{
		View:           contextString(request.Context, "view"),
		Command:        contextString(request.Context, "command"),
		Dialog:         contextString(request.Context, "dialog"),
		Action:         contextString(request.Context, "action"),
		Resource:       contextString(request.Context, "resource_type"),
		ID:             contextString(request.Context, "resource_id"),
		IdempotencyKey: contextString(request.Context, "idempotency_key"),
		Page:           contextInt(request.Context, "page"),
		UserID:         interaction.Actor.UserID,
		UserName:       interaction.Actor.UserName,
		ChannelID:      interaction.ChannelID,
		PostID:         interaction.CallbackPostID,
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
		if err := router.interactionSecurity.SealDialog(r.Context(), result.Dialog, interaction); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "interaction_capability_unavailable"})
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
		update, err := router.securedCardUpdate(r.Context(), result.Card, interaction)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "interaction_capability_unavailable"})
			return
		}
		response.Update = update
	}
	writeJSON(w, status, response)
}

func (router *Router) handleAgentTurnStopAction(w http.ResponseWriter, r *http.Request, request mattermostmodel.PostActionIntegrationRequest) {
	if router.sessionService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "agent_session_service_not_configured"})
		return
	}
	turnIDs := contextInt64List(request.Context, "turn_ids")
	resourceID, resourceErr := strconv.ParseInt(contextString(request.Context, "resource_id"), 10, 64)
	if len(turnIDs) != 1 || resourceErr != nil || resourceID <= 0 || turnIDs[0] != resourceID || contextString(request.Context, "resource_type") != "agent_session_turn" {
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_agent_turn_action"})
		return
	}
	callback := statusservice.ActionCallback{
		Context: request.Context, UserID: strings.TrimSpace(request.UserId),
		ChannelID: strings.TrimSpace(request.ChannelId), PostID: strings.TrimSpace(request.PostId),
	}
	var plan statusservice.StopAgentSessionTurnsPlan
	interaction, err := router.interactionSecurity.AuthenticateAgentTurnStopActionAtomic(r.Context(), callback, func(interaction statusservice.AuthenticatedInteraction) (bool, error) {
		return router.sessionService.ReconcilePendingStopAgentSessionTurnCard(r.Context(), interaction, callback)
	}, func(interaction statusservice.AuthenticatedInteraction, store adminrepo.Repository, consumedReplay bool) error {
		if interaction.ResourceType != "agent_session_turn" || interaction.ResourceID != strconv.FormatInt(resourceID, 10) || strings.TrimSpace(interaction.Scope.Session) == "" || strings.TrimSpace(interaction.Scope.Workspace) == "" {
			return statusservice.ErrInteractionAuthentication
		}
		var prepareErr error
		plan, prepareErr = router.sessionService.PrepareStopAgentSessionTurns(r.Context(), statusservice.StopAgentSessionTurnsCommand{
			TurnIDs: []int64{resourceID}, SessionKey: interaction.Scope.Session, WorkspaceScope: interaction.Scope.Workspace,
			UserID: interaction.Actor.UserID, UserName: interaction.Actor.UserName,
			ChannelID: interaction.ChannelID, PostID: interaction.CallbackPostID,
			CapabilityAction: interaction.Action,
		}, store)
		if prepareErr == nil && consumedReplay && !plan.ReconcileOnly() {
			return statusservice.ErrInteractionAuthentication
		}
		return prepareErr
	})
	if err != nil {
		if errors.Is(err, statusservice.ErrInteractionPreparation) {
			writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "interaction_capability_unavailable"})
			return
		}
		router.writeInteractionDenied(w, err)
		return
	}
	result, err := router.sessionService.FinalizeStopAgentSessionTurns(r.Context(), plan)
	if err != nil {
		router.logWarn("agent turn stop failed", "error", err)
		writeJSON(w, http.StatusBadGateway, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("chat.session.turn.stop.failed", nil)})
		return
	}
	response := &mattermostmodel.PostActionIntegrationResponse{EphemeralText: result.Message}
	if result.Card != nil {
		var update *mattermostmodel.Post
		updateErr := router.sessionService.GuardStopAgentSessionTurnsResponse(r.Context(), plan, func() error {
			var guardErr error
			update, guardErr = router.securedCardUpdate(r.Context(), result.Card, interaction)
			return guardErr
		})
		if updateErr != nil {
			if errors.Is(updateErr, adminrepo.ErrClusterAdminAdmissionDenied) {
				router.writeInteractionDenied(w, updateErr)
				return
			}
			writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "interaction_capability_unavailable"})
			return
		}
		response.Update = update
	}
	writeJSON(w, http.StatusOK, response)
}

func (router *Router) runAsyncMenuAction(parent context.Context, command statusservice.MenuActionCommand) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	result := router.slashService.HandleMenuAction(ctx, command)
	if result.StatusCode >= http.StatusBadRequest {
		router.logWarn("asynchronous Mattermost menu action failed", "action", command.Action, "status", result.StatusCode)
	}
}

func (router *Router) handleAgentTurnAction(w http.ResponseWriter, r *http.Request, request mattermostmodel.PostActionIntegrationRequest, interaction statusservice.AuthenticatedInteraction) {
	if router.sessionService == nil {
		writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "agent_session_service_not_configured"})
		return
	}
	switch contextString(request.Context, "action") {
	case "stop_turn", "recover_stop_turn":
		writeJSON(w, http.StatusBadRequest, transportmodels.ErrorResponse{Error: "invalid_agent_turn_action"})
	case "retry_turn":
		turnIDs := contextInt64List(request.Context, "turn_ids")
		if len(turnIDs) != 1 {
			writeJSON(w, http.StatusBadRequest, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("chat.session.turn.retry.failed", nil)})
			return
		}
		result, err := router.sessionService.RetryFailedTurn(r.Context(), statusservice.RetryAgentSessionTurnCommand{
			TurnID:    turnIDs[0],
			UserID:    interaction.Actor.UserID,
			UserName:  interaction.Actor.UserName,
			ChannelID: interaction.ChannelID,
			PostID:    interaction.CallbackPostID,
		})
		if err != nil {
			router.logWarn("agent turn retry failed", "error", err)
			writeJSON(w, http.StatusBadGateway, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("chat.session.turn.retry.failed", nil)})
			return
		}
		response := &mattermostmodel.PostActionIntegrationResponse{EphemeralText: result.Message}
		if result.Card != nil {
			update, err := router.securedCardUpdate(r.Context(), result.Card, interaction)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, transportmodels.ErrorResponse{Error: "interaction_capability_unavailable"})
				return
			}
			response.Update = update
		}
		writeJSON(w, http.StatusOK, response)
	default:
		writeJSON(w, http.StatusBadRequest, &mattermostmodel.PostActionIntegrationResponse{EphemeralText: router.t("menu.action.unknown", nil)})
	}
}

func (router *Router) securedCardUpdate(ctx context.Context, card *statusservice.MattermostCard, interaction statusservice.AuthenticatedInteraction) (*mattermostmodel.Post, error) {
	if card == nil || router.interactionSecurity == nil {
		return nil, fmt.Errorf("interaction security is not configured")
	}
	card.ChannelID = interaction.ChannelID
	card.PostID = interaction.CallbackPostID
	if err := router.interactionSecurity.SealCard(ctx, card, interaction.Actor, interaction.Scope); err != nil {
		return nil, err
	}
	return cardPost(*card), nil
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
	callbackID := strings.TrimSpace(request.CallbackId)
	if callbackID != dialogCallbackResult && router.slashService == nil {
		writeJSON(w, http.StatusServiceUnavailable, mattermostmodel.SubmitDialogResponse{Error: router.t("router.dialog.service_missing", nil)})
		return
	}
	if callbackID != dialogCallbackResult {
		validation := router.slashService.PrevalidateDialogSubmissionReadOnly(r.Context(), statusservice.DialogSubmissionCommand{
			CallbackID: callbackID,
			State:      strings.TrimSpace(request.State),
			UserID:     strings.TrimSpace(request.UserId),
			ChannelID:  strings.TrimSpace(request.ChannelId),
			TeamID:     strings.TrimSpace(request.TeamId),
			Submission: request.Submission,
			Cancelled:  request.Cancelled,
		})
		if validation.Error != "" || len(validation.Errors) > 0 {
			status := validation.StatusCode
			if status == 0 {
				status = http.StatusOK
			}
			writeJSON(w, status, mattermostmodel.SubmitDialogResponse{Error: validation.Error, Errors: validation.Errors})
			return
		}
	}
	callback := statusservice.DialogCallback{
		CallbackID: callbackID,
		State:      request.State,
		UserID:     strings.TrimSpace(request.UserId),
		ChannelID:  strings.TrimSpace(request.ChannelId),
	}
	prepare := func(statusservice.AuthenticatedInteraction) error {
		if strings.TrimSpace(request.URL) == "" {
			return nil
		}
		_, prepareErr := router.mattermostResponses.Prepare(r.Context(), request.URL)
		return prepareErr
	}
	var (
		interaction statusservice.AuthenticatedInteraction
		cleanState  string
		result      statusservice.DialogSubmissionResult
		err         error
	)
	if callbackID == dialogCallbackResult {
		interaction, cleanState, err = router.interactionSecurity.AuthenticateDialogPrepared(r.Context(), callback, prepare)
	} else {
		interaction, cleanState, err = router.interactionSecurity.AuthenticateDialogPreparedAtomic(
			r.Context(), callback, prepare,
			func(authenticated statusservice.AuthenticatedInteraction, transactionalState string, store adminrepo.Repository) error {
				result = router.slashService.HandleDialogSubmissionTransactional(r.Context(), statusservice.DialogSubmissionCommand{
					CallbackID: callbackID,
					State:      transactionalState,
					UserID:     authenticated.Actor.UserID,
					UserName:   authenticated.Actor.UserName,
					ChannelID:  authenticated.ChannelID,
					TeamID:     strings.TrimSpace(request.TeamId),
					Submission: request.Submission,
					Cancelled:  request.Cancelled,
				}, store)
				if result.Error != "" || len(result.Errors) > 0 {
					return statusservice.NewInteractionValidationError(result.StatusCode, result.Error, result.Errors)
				}
				return nil
			},
		)
	}
	if err != nil {
		var validationErr *statusservice.InteractionValidationError
		if errors.As(err, &validationErr) {
			status := validationErr.StatusCode
			if status == 0 {
				status = http.StatusOK
			}
			writeJSON(w, status, mattermostmodel.SubmitDialogResponse{Error: validationErr.ErrorText, Errors: validationErr.Fields})
			return
		}
		if errors.Is(err, statusservice.ErrInteractionPreparation) {
			writeJSON(w, http.StatusForbidden, mattermostmodel.SubmitDialogResponse{Error: "mattermost_response_url_denied"})
			return
		}
		router.writeDialogInteractionDenied(w, err)
		return
	}
	request.State = cleanState
	if callbackID == dialogCallbackResult {
		writeJSON(w, http.StatusOK, mattermostmodel.SubmitDialogResponse{Type: string(mattermostmodel.SubmitDialogResponseTypeOK)})
		return
	}
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
		if err := router.interactionSecurity.SealDialog(r.Context(), result.Dialog, interaction); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, mattermostmodel.SubmitDialogResponse{Error: "interaction_capability_unavailable"})
			return
		}
		writeJSON(w, status, mattermostmodel.SubmitDialogResponse{
			Type: string(mattermostmodel.SubmitDialogResponseTypeForm),
			Form: mattermostDialogForm(*result.Dialog),
		})
		return
	}
	if result.Card != nil {
		result.Card.ChannelID = interaction.ChannelID
		result.Card.Interaction = statusservice.MattermostCardInteraction{Actor: interaction.Actor, Scope: interaction.Scope}
		if router.threadPublisher == nil {
			writeJSON(w, http.StatusServiceUnavailable, mattermostmodel.SubmitDialogResponse{Error: "interaction_capability_unavailable"})
			return
		}
		if _, err := router.threadPublisher.PostThreadCard(r.Context(), *result.Card); err != nil {
			router.logWarn("publish secured dialog result failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, mattermostmodel.SubmitDialogResponse{Error: "interaction_capability_unavailable"})
			return
		}
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

func (router *Router) logWarn(message string, args ...any) {
	if router.logger != nil {
		router.logger.Warn(message, args...)
	}
}

func (router *Router) writeInteractionDenied(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	if errors.Is(err, statusservice.ErrInteractionAdmissionDenied) || errors.Is(err, statusservice.ErrInteractionAdmissionUnknown) {
		status = http.StatusForbidden
	}
	writeJSON(w, status, transportmodels.ErrorResponse{Error: "interaction_callback_denied"})
}

func (router *Router) writeDialogInteractionDenied(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	if errors.Is(err, statusservice.ErrInteractionAdmissionDenied) || errors.Is(err, statusservice.ErrInteractionAdmissionUnknown) {
		status = http.StatusForbidden
	}
	writeJSON(w, status, mattermostmodel.SubmitDialogResponse{Error: "interaction_callback_denied"})
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

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	prefix := "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
