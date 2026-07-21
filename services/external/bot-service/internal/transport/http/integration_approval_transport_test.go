package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

type integrationApprovalMutationStore struct {
	adminrepo.Repository
	service *integrationMCPStub
}

func (store *integrationApprovalMutationStore) DecideIntegrationApproval(ctx context.Context, input integrations.ApprovalDecisionInput) (integrations.Invocation, error) {
	result, err := store.service.DecideApproval(ctx, input)
	return integrations.Invocation{Status: result.Status, ApprovalPublicID: result.ApprovalID}, err
}

func newIntegrationApprovalTestSecurity(service *integrationMCPStub) *statusservice.InteractionSecurityService {
	security, repository := newMemoryInteractionSecurityWithRepository()
	repository.mutationStore = &integrationApprovalMutationStore{service: service}
	return security
}

func TestIntegrationApprovalUsesSealedExactHumanCallback(t *testing.T) {
	service := &integrationMCPStub{}
	security := newIntegrationApprovalTestSecurity(service)
	localizer, err := texti18n.New(texti18n.RussianLocale)
	if err != nil {
		t.Fatalf("localizer: %v", err)
	}
	router := NewRouter(RouterConfig{
		SessionService: newSessionBarrierServiceOnly(), IntegrationService: service,
		InteractionSecurity: security, Localizer: localizer, MaxSlashFormBytes: 64 * 1024,
	})
	contextValue := map[string]any{
		"kind": "integration_approval", "action": "approve",
		"resource_type": "integration_approval", "resource_id": "apr_0123456789abcdef0123456789abcdef",
		"approval_binding_sha256": strings.Repeat("a", 64),
	}
	body := testActionBody(t, router, contextValue, "")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body))
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	service.mu.Lock()
	if service.decisionCalls != 1 || service.lastDecision.ActorUserID != "owner" ||
		service.lastDecision.ChannelID != "channel-1" || service.lastDecision.PostID != "post-1" ||
		service.lastDecision.Decision != integrations.ApprovalDecisionApprove {
		t.Fatalf("exact decision = %+v calls=%d", service.lastDecision, service.decisionCalls)
	}
	service.mu.Unlock()
	if strings.Contains(recorder.Body.String(), "capability") || strings.Contains(recorder.Body.String(), "credential") {
		t.Fatalf("response раскрыл внутренний capability/credential: %s", recorder.Body.String())
	}

	replay := httptest.NewRecorder()
	router.ServeHTTP(replay, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	if replay.Code == http.StatusOK {
		t.Fatal("one-use Mattermost action была принята повторно")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.decisionCalls != 1 {
		t.Fatalf("replayed callback reached decision service: calls=%d", service.decisionCalls)
	}
}

func TestIntegrationApprovalRejectsSelfApprovalAndTamper(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"agent self approval", func(payload map[string]any) { payload["user_id"] = "agent-session-bot" }},
		{"approval hash tamper", func(payload map[string]any) {
			payload["context"].(map[string]any)["approval_binding_sha256"] = strings.Repeat("b", 64)
		}},
		{"foreign post", func(payload map[string]any) { payload["post_id"] = "foreign-post" }},
		{"foreign channel", func(payload map[string]any) { payload["channel_id"] = "foreign-channel" }},
		{"missing callback token", func(payload map[string]any) {
			delete(payload["context"].(map[string]any), "capability")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &integrationMCPStub{}
			security := newIntegrationApprovalTestSecurity(service)
			localizer, err := texti18n.New(texti18n.RussianLocale)
			if err != nil {
				t.Fatalf("localizer: %v", err)
			}
			router := NewRouter(RouterConfig{
				SessionService: newSessionBarrierServiceOnly(), IntegrationService: service,
				InteractionSecurity: security, Localizer: localizer, MaxSlashFormBytes: 64 * 1024,
			})
			body := testActionBody(t, router, map[string]any{
				"kind": "integration_approval", "action": "approve",
				"resource_type": "integration_approval", "resource_id": "apr_0123456789abcdef0123456789abcdef",
				"approval_binding_sha256": strings.Repeat("a", 64),
			}, "")
			payload := map[string]any{}
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("decode callback: %v", err)
			}
			test.mutate(payload)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("encode callback: %v", err)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(string(encoded))))
			if recorder.Code == http.StatusOK {
				t.Fatalf("tampered callback accepted: %s", recorder.Body.String())
			}
			service.mu.Lock()
			defer service.mu.Unlock()
			if service.decisionCalls != 0 {
				t.Fatalf("tampered callback reached decision service: calls=%d", service.decisionCalls)
			}
		})
	}
}

func TestIntegrationApprovalRejectsExpiredCallback(t *testing.T) {
	service := &integrationMCPStub{}
	security, repository := newMemoryInteractionSecurityWithRepository()
	repository.mutationStore = &integrationApprovalMutationStore{service: service}
	router := NewRouter(RouterConfig{
		SessionService: newSessionBarrierServiceOnly(), IntegrationService: service,
		InteractionSecurity: security, MaxSlashFormBytes: 64 * 1024,
	})
	body := testActionBody(t, router, map[string]any{
		"kind": "integration_approval", "action": "approve",
		"resource_type": "integration_approval", "resource_id": "apr_0123456789abcdef0123456789abcdef",
		"approval_binding_sha256": strings.Repeat("a", 64),
	}, "")
	repository.mu.Lock()
	for key, capability := range repository.capabilities {
		capability.ExpiresAt = time.Now().UTC().Add(-time.Minute)
		repository.capabilities[key] = capability
	}
	repository.mu.Unlock()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	if recorder.Code == http.StatusOK {
		t.Fatalf("expired callback accepted: %s", recorder.Body.String())
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.decisionCalls != 0 {
		t.Fatalf("expired callback reached decision service: calls=%d", service.decisionCalls)
	}
}

func TestIntegrationApprovalDecisionAndCallbackConsumeAreAtomic(t *testing.T) {
	service := &integrationMCPStub{decisionErr: errors.New("synthetic decision failure")}
	security := newIntegrationApprovalTestSecurity(service)
	router := NewRouter(RouterConfig{
		SessionService: newSessionBarrierServiceOnly(), IntegrationService: service,
		InteractionSecurity: security, MaxSlashFormBytes: 64 * 1024,
	})
	body := testActionBody(t, router, map[string]any{
		"kind": "integration_approval", "action": "approve",
		"resource_type": "integration_approval", "resource_id": "apr_0123456789abcdef0123456789abcdef",
		"approval_binding_sha256": strings.Repeat("a", 64),
	}, "")
	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	if failed.Code == http.StatusOK {
		t.Fatalf("failed decision returned success: %s", failed.Body.String())
	}
	service.mu.Lock()
	service.decisionErr = nil
	service.mu.Unlock()
	retry := httptest.NewRecorder()
	router.ServeHTTP(retry, httptest.NewRequest(http.MethodPost, pathAgentsAction, strings.NewReader(body)))
	if retry.Code != http.StatusOK {
		t.Fatalf("atomic retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.decisionCalls != 2 {
		t.Fatalf("atomic decision attempts=%d", service.decisionCalls)
	}
}
