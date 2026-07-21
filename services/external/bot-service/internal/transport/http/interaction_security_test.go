package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
)

type memoryInteractionRepository struct {
	mu            sync.Mutex
	capabilities  map[string]securityrepo.Capability
	inputs        map[string]securityrepo.IssueCapabilityInput
	admissions    map[string]bool
	consumes      int
	checks        int
	issues        int
	failIssueAt   int
	issueErr      error
	mutationStore adminrepo.Repository
}

func newMemoryInteractionSecurity() *statusservice.InteractionSecurityService {
	return statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
		Admission: fixedAdmission{status: statusservice.AdmissionAllowed},
		Repository: &memoryInteractionRepository{
			capabilities: map[string]securityrepo.Capability{},
			inputs:       map[string]securityrepo.IssueCapabilityInput{},
			admissions:   map[string]bool{},
		},
	})
}

func (repo *memoryInteractionRepository) IssueInteractionCapability(_ context.Context, input securityrepo.IssueCapabilityInput) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.issues++
	if repo.issueErr != nil && repo.issues >= repo.failIssueAt {
		return repo.issueErr
	}
	key := string(input.TokenHash)
	repo.inputs[key] = input
	repo.capabilities[key] = securityrepo.Capability{
		State:             input.State,
		Kind:              input.Kind,
		Operation:         input.Operation,
		ResourceType:      input.ResourceType,
		ResourceID:        input.ResourceID,
		ChannelID:         input.ChannelID,
		PostBinding:       input.PostBinding,
		ActorUserID:       input.ActorUserID,
		ActorUserName:     input.ActorUserName,
		InstallationScope: input.InstallationScope,
		WorkspaceScope:    input.WorkspaceScope,
		SessionScope:      input.SessionScope,
		IssuedAt:          input.IssuedAt,
		ExpiresAt:         input.ExpiresAt,
	}
	return nil
}

func (repo *memoryInteractionRepository) CheckInteractionCapability(_ context.Context, input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.checks++
	return repo.checkInteractionCapability(input)
}

func TestRouterReturnsRetryableFailureWhenActionCardCannotBeSealed(t *testing.T) {
	repository := &memoryInteractionRepository{
		capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{},
		failIssueAt: 2, issueErr: errors.New("synthetic capability repository failure"),
	}
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repository, Admission: fixedAdmission{status: statusservice.AdmissionAllowed}})
	router := testRouterWithDialogStore(&fakeDialogOpener{}, &fakeRouterAdminStore{}, security)
	body := testActionBody(t, router, map[string]any{"kind": "agents_menu", "view": "runtime"}, "")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/mattermost/actions/agents", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "interaction_capability_unavailable") {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}

	repository.issueErr = nil
	retryBody := testActionBody(t, router, map[string]any{"kind": "agents_menu", "view": "runtime"}, "")
	retryRecorder := httptest.NewRecorder()
	retryRequest := httptest.NewRequest("POST", "/mattermost/actions/agents", strings.NewReader(retryBody))
	retryRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(retryRecorder, retryRequest)
	if retryRecorder.Code != 200 {
		t.Fatalf("retry status = %d body = %s", retryRecorder.Code, retryRecorder.Body.String())
	}
}

func (repo *memoryInteractionRepository) ConsumeInteractionCapability(_ context.Context, input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.consumes++
	capability, err := repo.checkInteractionCapability(input)
	if err != nil {
		return securityrepo.Capability{}, err
	}
	capability.State = securityrepo.CapabilityStateConsumed
	capability.ConsumedAt = input.Now
	repo.capabilities[string(input.TokenHash)] = capability
	return capability, nil
}

func (repo *memoryInteractionRepository) ConsumeInteractionCapabilityWithMutation(
	ctx context.Context,
	input securityrepo.ConsumeCapabilityInput,
	mutation func(adminrepo.Repository) error,
) (securityrepo.Capability, error) {
	capability, err := repo.ConsumeInteractionCapability(ctx, input)
	if err != nil {
		return securityrepo.Capability{}, err
	}
	if err := mutation(repo.mutationStore); err != nil {
		repo.mu.Lock()
		capability.State = securityrepo.CapabilityStateUnused
		capability.ConsumedAt = time.Time{}
		repo.capabilities[string(input.TokenHash)] = capability
		repo.consumes--
		repo.mu.Unlock()
		return securityrepo.Capability{}, err
	}
	return capability, nil
}

func (repo *memoryInteractionRepository) checkInteractionCapability(input securityrepo.ConsumeCapabilityInput) (securityrepo.Capability, error) {
	key := string(input.TokenHash)
	capability, ok := repo.capabilities[key]
	if !ok {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityNotFound
	}
	if capability.State == securityrepo.CapabilityStateConsumed || !capability.ConsumedAt.IsZero() {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityConsumed
	}
	if capability.State != securityrepo.CapabilityStateUnused {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityInactive
	}
	if !capability.ExpiresAt.After(input.Now) {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityExpired
	}
	issued := repo.inputs[key]
	if capability.Kind != input.Kind || capability.Operation != input.Operation ||
		capability.ResourceType != input.ResourceType || capability.ResourceID != input.ResourceID ||
		capability.ChannelID != input.ChannelID || capability.PostBinding != input.PostBinding ||
		capability.ActorUserID != input.ActorUserID || !bytes.Equal(issued.ContextHash, input.ContextHash) {
		return securityrepo.Capability{}, securityrepo.ErrCapabilityBinding
	}
	return capability, nil
}

func (repo *memoryInteractionRepository) TransitionInteractionCapabilities(_ context.Context, input securityrepo.TransitionCapabilitiesInput) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, tokenHash := range input.TokenHashes {
		capability, ok := repo.capabilities[string(tokenHash)]
		if !ok || capability.State != input.From {
			return securityrepo.ErrCapabilityInactive
		}
	}
	for _, tokenHash := range input.TokenHashes {
		key := string(tokenHash)
		capability := repo.capabilities[key]
		capability.State = input.To
		repo.capabilities[key] = capability
	}
	return nil
}

func (repo *memoryInteractionRepository) AdmitExistingClusterAdmin(_ context.Context, input securityrepo.ClusterAdminAdmissionInput) (bool, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return repo.admissions[fmt.Sprintf("%s:%s:%d:%s", input.SubjectType, input.SubjectKey, input.ProjectID, input.ProfileName)], nil
}

func testActionBody(t *testing.T, router *Router, actionContext map[string]any, triggerID string) string {
	t.Helper()
	card := statusservice.MattermostCard{
		ChannelID: "channel-1",
		PostID:    "post-1",
		Actions: []statusservice.MattermostCardAction{{
			ID:      "test-action",
			Context: actionContext,
		}},
	}
	if err := router.interactionSecurity.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{
		UserID:   "owner",
		UserName: "owner",
	}, statusservice.InteractionScope{Workspace: "workspace-1", Session: "session-1"}); err != nil {
		t.Fatalf("SealCard() error = %v", err)
	}
	payload := map[string]any{
		"user_id":    "owner",
		"user_name":  "spoofed-body-name",
		"channel_id": "channel-1",
		"post_id":    "post-1",
		"trigger_id": triggerID,
		"context":    card.Actions[0].Context,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}

func testDialogBody(t *testing.T, router *Router, callbackID string, state map[string]any, submission map[string]any) string {
	t.Helper()
	encodedState, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal(state) error = %v", err)
	}
	dialog := statusservice.MattermostDialog{CallbackID: callbackID, State: string(encodedState)}
	if err := router.interactionSecurity.SealDialog(context.Background(), &dialog, statusservice.AuthenticatedInteraction{
		Actor:       statusservice.AuthenticatedActor{UserID: "owner", UserName: "owner"},
		Scope:       statusservice.InteractionScope{Installation: "single-installation", Workspace: "workspace-1", Session: "session-1"},
		ChannelID:   "channel-1",
		PostBinding: "post-1",
	}); err != nil {
		t.Fatalf("SealDialog() error = %v", err)
	}
	payload := map[string]any{
		"callback_id": callbackID,
		"state":       dialog.State,
		"user_id":     "owner",
		"user_name":   "spoofed-body-name",
		"channel_id":  "channel-1",
		"team_id":     "team-1",
		"submission":  submission,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v", err)
	}
	return string(encoded)
}

func testDialogBodyWithURL(t *testing.T, router *Router, callbackID string, state map[string]any, submission map[string]any, responseURL string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(testDialogBody(t, router, callbackID, state, submission)), &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}
	payload["url"] = responseURL
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v", err)
	}
	return string(encoded)
}

type fixedAdmission struct {
	status statusservice.AdmissionStatus
}

func (admission fixedAdmission) Admit(context.Context, statusservice.InteractionAdmissionRequest) statusservice.InteractionAdmissionDecision {
	return statusservice.InteractionAdmissionDecision{Status: admission.status, Reason: "test_decision"}
}

func TestInteractionCapabilityNegativeMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any, *statusservice.ActionCallback)
	}{
		{name: "missing", mutate: func(ctx map[string]any, _ *statusservice.ActionCallback) { delete(ctx, "capability") }},
		{name: "forged", mutate: func(ctx map[string]any, _ *statusservice.ActionCallback) { ctx["capability"] = "forged" }},
		{name: "wrong operation", mutate: func(ctx map[string]any, _ *statusservice.ActionCallback) { ctx["view"] = "other" }},
		{name: "wrong resource", mutate: func(ctx map[string]any, _ *statusservice.ActionCallback) { ctx["resource_id"] = "other" }},
		{name: "wrong channel", mutate: func(_ map[string]any, callback *statusservice.ActionCallback) { callback.ChannelID = "other" }},
		{name: "wrong callback post", mutate: func(_ map[string]any, callback *statusservice.ActionCallback) { callback.PostID = "other" }},
		{name: "wrong post binding", mutate: func(ctx map[string]any, _ *statusservice.ActionCallback) { ctx["capability_post_binding"] = "other" }},
		{name: "spoofed actor", mutate: func(_ map[string]any, callback *statusservice.ActionCallback) { callback.UserID = "attacker" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &memoryInteractionRepository{capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}}
			security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repo})
			card := statusservice.MattermostCard{ChannelID: "channel-1", PostID: "post-1", Actions: []statusservice.MattermostCardAction{{Context: map[string]any{
				"kind": "agents_menu", "view": "projects", "resource_type": "project", "resource_id": "17",
			}}}}
			if err := security.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner", UserName: "trusted-owner"}, statusservice.InteractionScope{}); err != nil {
				t.Fatal(err)
			}
			callback := statusservice.ActionCallback{Context: card.Actions[0].Context, UserID: "owner", ChannelID: "channel-1", PostID: "post-1"}
			test.mutate(callback.Context, &callback)
			if _, err := security.AuthenticateAction(context.Background(), callback); err == nil {
				t.Fatal("AuthenticateAction() unexpectedly succeeded")
			}
		})
	}
}

func TestInteractionCapabilityReplayAndAdmissionDeny(t *testing.T) {
	for _, status := range []statusservice.AdmissionStatus{statusservice.AdmissionAllowed, statusservice.AdmissionDenied, statusservice.AdmissionIndeterminate} {
		t.Run(string(status), func(t *testing.T) {
			repo := &memoryInteractionRepository{capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}}
			security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repo, Admission: fixedAdmission{status: status}})
			card := statusservice.MattermostCard{ChannelID: "channel-1", PostID: "post-1", Actions: []statusservice.MattermostCardAction{{Context: map[string]any{"kind": "agents_menu", "view": "projects"}}}}
			if err := security.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner", UserName: "trusted-owner"}, statusservice.InteractionScope{}); err != nil {
				t.Fatal(err)
			}
			callback := statusservice.ActionCallback{Context: card.Actions[0].Context, UserID: "owner", ChannelID: "channel-1", PostID: "post-1"}
			interaction, err := security.AuthenticateAction(context.Background(), callback)
			switch status {
			case statusservice.AdmissionAllowed:
				if err != nil || interaction.Actor.UserName != "trusted-owner" {
					t.Fatalf("AuthenticateAction() = %#v, %v", interaction, err)
				}
				if _, replayErr := security.AuthenticateAction(context.Background(), callback); !errors.Is(replayErr, statusservice.ErrInteractionAuthentication) {
					t.Fatalf("replay error = %v", replayErr)
				}
			case statusservice.AdmissionDenied:
				if !errors.Is(err, statusservice.ErrInteractionAdmissionDenied) {
					t.Fatalf("denied error = %v", err)
				}
			default:
				if !errors.Is(err, statusservice.ErrInteractionAdmissionUnknown) {
					t.Fatalf("indeterminate error = %v", err)
				}
			}
			if status != statusservice.AdmissionAllowed {
				if repo.consumes != 0 {
					t.Fatalf("отклонённый допуск погасил capability: consumes=%d", repo.consumes)
				}
				recovered := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repo, Admission: fixedAdmission{status: statusservice.AdmissionAllowed}})
				if _, retryErr := recovered.AuthenticateAction(context.Background(), callback); retryErr != nil {
					t.Fatalf("retry after admission recovery = %v", retryErr)
				}
				if _, replayErr := recovered.AuthenticateAction(context.Background(), callback); !errors.Is(replayErr, statusservice.ErrInteractionAuthentication) {
					t.Fatalf("replay after recovered retry = %v", replayErr)
				}
			}
		})
	}
}

func TestInteractionCapabilitySeparatesCommandStateFromAdmissionResource(t *testing.T) {
	newFixture := func(t *testing.T) (*statusservice.InteractionSecurityService, *memoryInteractionRepository, statusservice.ActionCallback) {
		t.Helper()
		repo := &memoryInteractionRepository{
			capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{},
		}
		security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
			Repository: repo, Admission: fixedAdmission{status: statusservice.AdmissionAllowed},
		})
		card := statusservice.MattermostCard{ChannelID: "channel-1", PostID: "post-1", Actions: []statusservice.MattermostCardAction{{Context: map[string]any{
			"view": "chats", "action": "thread_repository_select", "resource_type": "thread_context",
			"resource_id": "encoded-thread-and-repository", "capability_resource_id": "296",
		}}}}
		if err := security.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner", UserName: "trusted-owner"}, statusservice.InteractionScope{}); err != nil {
			t.Fatal(err)
		}
		for _, issued := range repo.inputs {
			if issued.ResourceType != "thread_context" || issued.ResourceID != "296" {
				t.Fatalf("issued capability resource = %q/%q", issued.ResourceType, issued.ResourceID)
			}
		}
		return security, repo, statusservice.ActionCallback{
			Context: card.Actions[0].Context, UserID: "owner", ChannelID: "channel-1", PostID: "post-1",
		}
	}

	security, repo, callback := newFixture(t)
	mutated := false
	interaction, err := security.AuthenticateActionAtomic(context.Background(), callback, func(interaction statusservice.AuthenticatedInteraction, _ adminrepo.Repository) error {
		mutated = true
		if interaction.ResourceType != "thread_context" || interaction.ResourceID != "296" {
			t.Fatalf("authenticated resource = %q/%q", interaction.ResourceType, interaction.ResourceID)
		}
		if got := callback.Context["resource_id"]; got != "encoded-thread-and-repository" {
			t.Fatalf("command state = %#v", got)
		}
		return nil
	})
	if err != nil || !mutated || interaction.Actor.UserName != "trusted-owner" || repo.consumes != 1 {
		t.Fatalf("AuthenticateActionAtomic() = %#v, %v, mutated=%t consumes=%d", interaction, err, mutated, repo.consumes)
	}
	if _, replayErr := security.AuthenticateAction(context.Background(), callback); !errors.Is(replayErr, statusservice.ErrInteractionAuthentication) {
		t.Fatalf("replay error = %v", replayErr)
	}

	for _, key := range []string{"resource_id", "capability_resource_id"} {
		t.Run("tampered "+key, func(t *testing.T) {
			security, _, callback := newFixture(t)
			callback.Context[key] = "tampered"
			if _, err := security.AuthenticateAction(context.Background(), callback); !errors.Is(err, statusservice.ErrInteractionAuthentication) {
				t.Fatalf("tampered callback error = %v", err)
			}
		})
	}
}

func TestInteractionCapabilityExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	repo := &memoryInteractionRepository{capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}}
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
		Repository: repo,
		Now:        func() time.Time { return now },
	})
	card := statusservice.MattermostCard{ChannelID: "channel-1", PostID: "post-1", Actions: []statusservice.MattermostCardAction{{Context: map[string]any{"kind": "agents_menu", "view": "projects"}}}}
	if err := security.SealCard(context.Background(), &card, statusservice.AuthenticatedActor{UserID: "owner"}, statusservice.InteractionScope{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Hour)
	_, err := security.AuthenticateAction(context.Background(), statusservice.ActionCallback{Context: card.Actions[0].Context, UserID: "owner", ChannelID: "channel-1", PostID: "post-1"})
	if !errors.Is(err, statusservice.ErrInteractionAuthentication) {
		t.Fatalf("expired error = %v", err)
	}
}

func TestRouterRejectsUnauthenticatedDialogWithZeroSideEffects(t *testing.T) {
	tests := []struct {
		name       string
		admission  statusservice.AdmissionStatus
		mutate     func(map[string]any, map[string]any)
		wantStatus int
	}{
		{
			name: "missing", wantStatus: 401,
			mutate: func(_ map[string]any, state map[string]any) { delete(state, "capability") },
		},
		{
			name: "forged", wantStatus: 401,
			mutate: func(_ map[string]any, state map[string]any) { state["capability"] = "forged" },
		},
		{
			name: "wrong binding", wantStatus: 401,
			mutate: func(_ map[string]any, state map[string]any) { state["capability_post_binding"] = "other-post" },
		},
		{
			name: "spoofed actor", wantStatus: 401,
			mutate: func(payload map[string]any, _ map[string]any) { payload["user_id"] = "attacker" },
		},
		{
			name: "denied admission", admission: statusservice.AdmissionDenied, wantStatus: 403,
		},
		{
			name: "indeterminate admission", admission: statusservice.AdmissionIndeterminate, wantStatus: 403,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &memoryInteractionRepository{capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}}
			cfg := statusservice.InteractionSecurityConfig{Repository: repository}
			if test.admission != "" {
				cfg.Admission = fixedAdmission{status: test.admission}
			}
			security := statusservice.NewInteractionSecurityService(cfg)
			store := &fakeRouterAdminStore{}
			router := testRouterWithDialogStore(&fakeDialogOpener{}, store, security)
			body := testDialogBody(t, router, "agents_repo_add", map[string]any{"view": "repositories"}, map[string]any{
				"provider": "github", "repository": "codex-k8s/matter-codex", "default_branch": "main",
			})
			var payload map[string]any
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal([]byte(payload["state"].(string)), &state); err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(payload, state)
			}
			encodedState, _ := json.Marshal(state)
			payload["state"] = string(encodedState)
			encoded, _ := json.Marshal(payload)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/mattermost/dialogs/agents", bytes.NewReader(encoded))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
			if store.upsertCount != 0 || store.auditRecorded {
				t.Fatalf("отклонённый callback создал побочный эффект: upserts=%d audit=%t", store.upsertCount, store.auditRecorded)
			}
		})
	}
}

func TestRouterRejectsReplayedDialogWithoutSecondSideEffect(t *testing.T) {
	store := &fakeRouterAdminStore{}
	router := testRouterWithDialogStore(&fakeDialogOpener{}, store, newMemoryInteractionSecurity())
	body := testDialogBody(t, router, "agents_repo_add", map[string]any{"view": "repositories"}, map[string]any{
		"provider": "github", "repository": "codex-k8s/matter-codex", "default_branch": "main",
	})
	for attempt, wantStatus := range []int{200, 401} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/mattermost/dialogs/agents", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != wantStatus {
			t.Fatalf("attempt %d status = %d body = %s", attempt+1, recorder.Code, recorder.Body.String())
		}
	}
	if store.upsertCount != 1 {
		t.Fatalf("repository side effects = %d, want 1", store.upsertCount)
	}
	if store.lastAudit.ActorUser != "owner" {
		t.Fatalf("audit actor = %q, body user_name must not override authenticated actor", store.lastAudit.ActorUser)
	}
}

func TestRouterKeepsDialogCapabilityUnusedForCorrectableFieldError(t *testing.T) {
	repository := &memoryInteractionRepository{
		capabilities: map[string]securityrepo.Capability{},
		inputs:       map[string]securityrepo.IssueCapabilityInput{},
		admissions:   map[string]bool{},
	}
	security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{
		Repository: repository,
		Admission:  fixedAdmission{status: statusservice.AdmissionAllowed},
	})
	store := &fakeRouterAdminStore{}
	router := testRouterWithDialogStore(&fakeDialogOpener{}, store, security)
	resolver := &fakeMattermostResolver{addresses: [][]net.IPAddr{{{IP: net.ParseIP("10.20.30.40")}}}}
	dialer := &pipeMattermostDialer{}
	router.mattermostResponses = newMattermostResponseClient("", "http://mattermost.test:8065", resolver, dialer)
	body := testDialogBodyWithURL(t, router, "agents_repo_add", map[string]any{"view": "repositories"}, map[string]any{
		"provider": "github", "repository": "bad value", "default_branch": "main",
	}, "http://mattermost.test:8065/hooks/response-id")

	invalidRecorder := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest("POST", "/mattermost/dialogs/agents", strings.NewReader(body))
	invalidRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusOK || !strings.Contains(invalidRecorder.Body.String(), "repository") {
		t.Fatalf("field validation status=%d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
	if repository.checks != 0 || repository.consumes != 0 || store.upsertCount != 0 || store.auditRecorded || resolver.calls != 0 || len(dialer.addresses) != 0 {
		t.Fatalf("field error вызвала побочные эффекты: checks=%d consumes=%d upserts=%d audit=%t resolves=%d dials=%d",
			repository.checks, repository.consumes, store.upsertCount, store.auditRecorded, resolver.calls, len(dialer.addresses))
	}

	var correctedPayload map[string]any
	if err := json.Unmarshal([]byte(body), &correctedPayload); err != nil {
		t.Fatal(err)
	}
	correctedPayload["submission"] = map[string]any{
		"provider": "github", "repository": "codex-k8s/matter-codex", "default_branch": "main",
	}
	correctedBody, err := json.Marshal(correctedPayload)
	if err != nil {
		t.Fatal(err)
	}
	for attempt, wantStatus := range []int{http.StatusOK, http.StatusUnauthorized} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "/mattermost/dialogs/agents", bytes.NewReader(correctedBody))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != wantStatus {
			t.Fatalf("corrected attempt %d status=%d body=%s", attempt+1, recorder.Code, recorder.Body.String())
		}
	}
	if repository.consumes != 1 || store.upsertCount != 1 {
		t.Fatalf("corrected retry/replay: consumes=%d upserts=%d", repository.consumes, store.upsertCount)
	}
}

func TestRouterRejectsUnsafeResponseURLBeforeBusinessSideEffects(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		addresses []net.IPAddr
	}{
		{name: "arbitrary origin", response: "https://attacker.example/hooks/value", addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}},
		{name: "ip literal", response: "https://127.0.0.1/hooks/value", addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}},
		{name: "private external resolution", response: "https://mattermost.example.com/hooks/value", addresses: []net.IPAddr{{IP: net.ParseIP("10.20.30.40")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeRouterAdminStore{}
			repository := &memoryInteractionRepository{capabilities: map[string]securityrepo.Capability{}, inputs: map[string]securityrepo.IssueCapabilityInput{}, admissions: map[string]bool{}}
			security := statusservice.NewInteractionSecurityService(statusservice.InteractionSecurityConfig{Repository: repository, Admission: fixedAdmission{status: statusservice.AdmissionAllowed}})
			router := testRouterWithDialogStore(&fakeDialogOpener{}, store, security)
			resolver := &fakeMattermostResolver{addresses: [][]net.IPAddr{test.addresses}}
			dialer := &pipeMattermostDialer{}
			router.mattermostResponses = newMattermostResponseClient("https://mattermost.example.com", "", resolver, dialer)
			body := testDialogBodyWithURL(t, router, "agents_repo_add", map[string]any{"view": "repositories"}, map[string]any{
				"provider": "github", "repository": "codex-k8s/matter-codex", "default_branch": "main",
			}, test.response)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("POST", "/mattermost/dialogs/agents", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			if recorder.Code != 403 {
				t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
			}
			if store.upsertCount != 0 || store.auditRecorded || len(dialer.addresses) != 0 {
				t.Fatalf("unsafe response_url caused side effects: upserts=%d audit=%t dial=%#v", store.upsertCount, store.auditRecorded, dialer.addresses)
			}
			if repository.consumes != 0 {
				t.Fatalf("unsafe response_url погасил capability: consumes=%d", repository.consumes)
			}

			var retryPayload map[string]any
			if err := json.Unmarshal([]byte(body), &retryPayload); err != nil {
				t.Fatal(err)
			}
			retryPayload["url"] = "http://mattermost.test:8065/hooks/value"
			retryBody, _ := json.Marshal(retryPayload)
			router.mattermostResponses = newMattermostResponseClient("", "http://mattermost.test:8065", &fakeMattermostResolver{addresses: [][]net.IPAddr{{{IP: net.ParseIP("10.20.30.40")}}}}, &pipeMattermostDialer{})
			for attempt, wantStatus := range []int{200, 401} {
				retryRecorder := httptest.NewRecorder()
				retryRequest := httptest.NewRequest("POST", "/mattermost/dialogs/agents", bytes.NewReader(retryBody))
				retryRequest.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(retryRecorder, retryRequest)
				if retryRecorder.Code != wantStatus {
					t.Fatalf("retry attempt %d status = %d body = %s", attempt+1, retryRecorder.Code, retryRecorder.Body.String())
				}
			}
			if repository.consumes != 1 || store.upsertCount != 1 {
				t.Fatalf("corrected retry/replay: consumes=%d upserts=%d", repository.consumes, store.upsertCount)
			}
		})
	}
}
