package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	controlcenterapi "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/transport/http/generated"
)

func TestControlCenterHistoryUsesBearerAndRefreshesPersistedState(t *testing.T) {
	repository := &controlCenterHistoryRepository{items: []entity.AutomationHistoryItem{{
		ScheduledRunPublicID: "scheduled-run-11111111111111111111111111111111",
		Status:               "waiting_owner",
		Outcome:              "requires_human",
		OwnerAttentionID:     71,
		HumanDecisionStatus:  "open",
		DeliveryStatus:       "delivered",
		NextAction:           "wait_for_owner_response",
		UpdatedAt:            time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC),
	}}}
	service := statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
		Repository:              repository,
		Catalog:                 controlCenterHistoryCatalog{},
		StorageReady:            true,
		OwnerMattermostUsername: "owner",
	})
	router := NewRouter(RouterConfig{Automations: service, ControlCenterReadToken: "synthetic-read-token"})

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, pathAutomationHistory, nil))
	if unauthorized.Code != http.StatusUnauthorized || repository.calls != 0 {
		t.Fatalf("unauthorized status=%d repository_calls=%d", unauthorized.Code, repository.calls)
	}
	rawToken := httptest.NewRecorder()
	rawTokenRequest := httptest.NewRequest(http.MethodGet, pathAutomationHistory, nil)
	rawTokenRequest.Header.Set("Authorization", "synthetic-read-token")
	router.ServeHTTP(rawToken, rawTokenRequest)
	if rawToken.Code != http.StatusUnauthorized || repository.calls != 0 {
		t.Fatalf("raw token status=%d repository_calls=%d", rawToken.Code, repository.calls)
	}

	first := requestAutomationHistory(t, router)
	if len(first.Items) != 1 || first.Items[0].Status != controlcenterapi.AutomationHistoryItemStatusWaitingOwner || first.Items[0].HumanDecisionStatus == nil || *first.Items[0].HumanDecisionStatus != controlcenterapi.Open {
		t.Fatalf("pending response=%#v", first)
	}

	repository.setItems([]entity.AutomationHistoryItem{{
		ScheduledRunPublicID: "scheduled-run-11111111111111111111111111111111",
		Status:               "succeeded",
		Outcome:              "requires_human",
		OwnerAttentionID:     71,
		HumanDecisionStatus:  "resolved",
		DeliveryStatus:       "delivered",
		NextAction:           "none",
		UpdatedAt:            time.Date(2026, time.July, 22, 12, 1, 0, 0, time.UTC),
	}})
	second := requestAutomationHistory(t, router)
	if len(second.Items) != 1 || second.Items[0].Status != controlcenterapi.AutomationHistoryItemStatusSucceeded || second.Items[0].HumanDecisionStatus == nil || *second.Items[0].HumanDecisionStatus != controlcenterapi.Resolved || second.Items[0].NextAction != controlcenterapi.None {
		t.Fatalf("resolved response=%#v", second)
	}
}

func requestAutomationHistory(t *testing.T, router http.Handler) controlcenterapi.AutomationHistoryResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, pathAutomationHistory+"?limit=100", nil)
	request.Header.Set("Authorization", "Bearer synthetic-read-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("history cache policy=%q", response.Header().Get("Cache-Control"))
	}
	var body controlcenterapi.AutomationHistoryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	return body
}

type controlCenterHistoryRepository struct {
	automationsrepo.Repository
	mu    sync.Mutex
	items []entity.AutomationHistoryItem
	calls int
}

func (repository *controlCenterHistoryRepository) ListHistory(_ context.Context, ownerMattermostUsername string, limit int) ([]entity.AutomationHistoryItem, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.calls++
	if ownerMattermostUsername != "owner" || limit != 100 {
		return nil, automationsrepo.ErrForbidden
	}
	return append([]entity.AutomationHistoryItem(nil), repository.items...), nil
}

func (repository *controlCenterHistoryRepository) setItems(items []entity.AutomationHistoryItem) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.items = append([]entity.AutomationHistoryItem(nil), items...)
}

type controlCenterHistoryCatalog struct{}

func (controlCenterHistoryCatalog) GetProject(context.Context, int64) (entity.Project, error) {
	return entity.Project{}, nil
}

func (controlCenterHistoryCatalog) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return entity.AgentRole{}, nil
}

func (controlCenterHistoryCatalog) GetChat(context.Context, int64) (entity.Chat, error) {
	return entity.Chat{}, nil
}

func (controlCenterHistoryCatalog) ListChatParticipants(context.Context, int64) ([]entity.ChatParticipant, error) {
	return nil, nil
}

func (controlCenterHistoryCatalog) ListProjectRepositories(context.Context, int64) ([]entity.ProjectRepository, error) {
	return nil, nil
}
