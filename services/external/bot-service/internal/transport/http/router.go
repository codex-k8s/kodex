package http

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	transportmodels "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http/models"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	pathHealthz     = "/healthz"
	pathHealthLivez = "/health/livez"
	pathHealthReady = "/health/readyz"
	pathReadyz      = "/readyz"
	pathMetrics     = "/metrics"
	pathAgentsSlash = "/mattermost/slash/agents"
)

type RouterConfig struct {
	StatusService      *statusservice.StatusService
	SlashToken         string
	MaxSlashFormBytes  int64
	PrometheusRegistry *prometheus.Registry
	Logger             *slog.Logger
}

type Router struct {
	statusService     *statusservice.StatusService
	slashToken        string
	maxSlashFormBytes int64
	logger            *slog.Logger
	mux               *http.ServeMux
}

var _ http.Handler = (*Router)(nil)

func NewRouter(cfg RouterConfig) *Router {
	router := &Router{
		statusService:     cfg.StatusService,
		slashToken:        cfg.SlashToken,
		maxSlashFormBytes: cfg.MaxSlashFormBytes,
		logger:            cfg.Logger,
		mux:               http.NewServeMux(),
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
		writeCommandResponse(w, http.StatusBadRequest, ephemeral("matter-codex: invalid slash request."))
		return
	}
	if strings.TrimSpace(router.slashToken) == "" {
		writeCommandResponse(w, http.StatusServiceUnavailable, ephemeral("matter-codex bot-service запущен, но slash token еще не настроен."))
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(router.slashToken)) != 1 {
		writeCommandResponse(w, http.StatusUnauthorized, ephemeral("matter-codex: slash token не прошел проверку."))
		return
	}

	text := strings.TrimSpace(r.PostForm.Get("text"))
	if text == "" || text == "status" {
		writeCommandResponse(w, http.StatusOK, ephemeral(router.statusService.SlashStatusText()))
		return
	}
	writeCommandResponse(w, http.StatusOK, ephemeral("matter-codex: доступна команда `/agents status`."))
}

func (router *Router) logWarn(message string, args ...any) {
	if router.logger != nil {
		router.logger.Warn(message, args...)
	}
}

func healthResponse(snapshot value.StatusSnapshot) transportmodels.HealthResponse {
	return transportmodels.HealthResponse{
		Status:               snapshot.Status,
		Service:              snapshot.ServiceName,
		Version:              string(snapshot.ServiceVersion),
		MattermostConfigured: snapshot.MattermostConfigured,
		BotTokenConfigured:   snapshot.BotTokenConfigured,
		SlashTokenConfigured: snapshot.SlashTokenConfigured,
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
