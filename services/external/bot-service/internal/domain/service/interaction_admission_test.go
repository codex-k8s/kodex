package service

import (
	"context"
	"errors"
	"testing"

	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
)

type admissionActorVerifier struct {
	allowed bool
	err     error
	calls   int
	userID  string
	channel string
}

func (verifier *admissionActorVerifier) VerifyInteractionActor(_ context.Context, userID string, channelID string) (bool, error) {
	verifier.calls++
	verifier.userID = userID
	verifier.channel = channelID
	return verifier.allowed, verifier.err
}

type admissionResourceRepository struct {
	allowed bool
	err     error
	calls   int
	input   securityrepo.InteractionResourceAdmissionInput
}

func (repository *admissionResourceRepository) AdmitInteractionResource(_ context.Context, input securityrepo.InteractionResourceAdmissionInput) (bool, error) {
	repository.calls++
	repository.input = input
	return repository.allowed, repository.err
}

func TestServerSideInteractionAdmissionAllowsOnlyTypedOperationsAndResources(t *testing.T) {
	tests := []struct {
		name         string
		actionKey    string
		operation    string
		resourceType string
		resourceID   string
		session      string
	}{
		{name: "menu view", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;view=main"},
		{name: "list project", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=list;view=projects", resourceType: menuResourceProject, resourceID: "17"},
		{name: "show repository", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=show;view=repositories", resourceType: menuResourceRepository, resourceID: "github:codex-k8s/matter-codex"},
		{name: "show role", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=show;view=roles", resourceType: menuResourceAgentRole, resourceID: "5"},
		{name: "show chat", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=show;view=chats", resourceType: menuResourceChat, resourceID: "6"},
		{name: "runtime variable", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=show;view=runtime", resourceType: menuResourceRuntimeVar, resourceID: "7"},
		{name: "openai status", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=openai_status;view=openai", resourceType: menuResourceOpenAIAccount, resourceID: "primary"},
		{name: "github repositories", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=repository_repos;view=github", resourceType: menuResourceGitHubAccount, resourceID: "agent"},
		{name: "profile enable", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=profile_enable;view=profiles", resourceType: menuResourceProfile, resourceID: "developer"},
		{name: "prompt render", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=prompt_render;view=prompts", resourceType: menuResourcePromptTemplate, resourceID: "developer/default"},
		{name: "runtime cleanup", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=runtime_cleanup;view=runtime", resourceType: menuResourceRun, resourceID: "run-1"},
		{name: "system status", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=system_status;view=system", resourceType: menuResourceSystem},
		{name: "runtime smoke", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=runtime_smoke;view=runtime", resourceType: menuResourceRuntime},
		{name: "thread repository", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;action=thread_repository_select;view=repositories", resourceType: menuResourceThreadContext, resourceID: "10"},
		{name: "stop turn", actionKey: "mattermost.callback.action", operation: "action;kind=agent_turn;action=stop_turn", resourceType: "agent_session_turn", resourceID: "11", session: "session-1"},
		{name: "retry turn", actionKey: "mattermost.callback.action", operation: "action;kind=agent_turn;action=retry_turn", resourceType: "agent_session_turn", resourceID: "11", session: "session-1"},
		{name: "repository add dialog", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;dialog=repo_add;view=repositories"},
		{name: "project chat dialog", actionKey: "mattermost.callback.action", operation: "action;kind=agents_menu;dialog=chat_create;view=chats", resourceType: menuResourceProject, resourceID: "17"},
		{name: "repository submit", actionKey: "mattermost.callback.dialog", operation: "dialog;callback_id=agents_repo_add"},
		{name: "role submit", actionKey: "mattermost.callback.dialog", operation: "dialog;callback_id=agents_agent_role_upsert", resourceType: menuResourceProject, resourceID: "17"},
		{name: "dialog result", actionKey: "mattermost.callback.dialog", operation: "dialog;callback_id=agents_dialog_result"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &admissionActorVerifier{allowed: true}
			resources := &admissionResourceRepository{allowed: true}
			admission := NewServerSideInteractionAdmission("single-installation", verifier, resources)
			decision := admission.Admit(context.Background(), InteractionAdmissionRequest{
				ActionKey: test.actionKey, Operation: test.operation, ResourceType: test.resourceType, ResourceID: test.resourceID,
				Actor:     AuthenticatedActor{UserID: "actor-id", UserName: "untrusted-body-name"},
				Scope:     InteractionScope{Installation: "single-installation", Workspace: "workspace-1", Session: test.session},
				ChannelID: "channel-1", PostID: "post-1",
			})
			if decision.Status != AdmissionAllowed {
				t.Fatalf("decision = %#v", decision)
			}
			if verifier.calls != 1 || verifier.userID != "actor-id" || verifier.channel != "channel-1" {
				t.Fatalf("verified subject = %#v", verifier)
			}
			if resources.calls != 1 || resources.input.Operation != test.operation || resources.input.ResourceType != test.resourceType || resources.input.ResourceID != test.resourceID || resources.input.PostID != "post-1" {
				t.Fatalf("resource admission = %#v", resources)
			}
		})
	}
}

func TestServerSideInteractionAdmissionFailsClosedBeforeResourceMutation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*InteractionAdmissionRequest, *admissionActorVerifier, *admissionResourceRepository)
		want      AdmissionStatus
		actorCall int
		resCall   int
	}{
		{name: "unknown action", want: AdmissionIndeterminate, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.ActionKey = "unknown"
		}},
		{name: "missing subject", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.Actor.UserID = ""
		}},
		{name: "missing channel", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.ChannelID = ""
		}},
		{name: "missing post", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.PostID = ""
		}},
		{name: "installation mismatch", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.Scope.Installation = "other"
		}},
		{name: "workspace mismatch", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.Scope.Workspace = "bad scope"
		}},
		{name: "session missing", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.Scope.Session = ""
		}},
		{name: "unknown operation", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.Operation = "action;kind=agent_turn;action=delete_turn"
		}},
		{name: "resource mismatch", want: AdmissionDenied, mutate: func(request *InteractionAdmissionRequest, _ *admissionActorVerifier, _ *admissionResourceRepository) {
			request.ResourceType = "project"
		}},
		{name: "subject denied", want: AdmissionDenied, actorCall: 1, mutate: func(_ *InteractionAdmissionRequest, verifier *admissionActorVerifier, _ *admissionResourceRepository) {
			verifier.allowed = false
		}},
		{name: "subject indeterminate", want: AdmissionIndeterminate, actorCall: 1, mutate: func(_ *InteractionAdmissionRequest, verifier *admissionActorVerifier, _ *admissionResourceRepository) {
			verifier.err = errors.New("synthetic verifier failure")
		}},
		{name: "resource denied", want: AdmissionDenied, actorCall: 1, resCall: 1, mutate: func(_ *InteractionAdmissionRequest, _ *admissionActorVerifier, resources *admissionResourceRepository) {
			resources.allowed = false
		}},
		{name: "resource indeterminate", want: AdmissionIndeterminate, actorCall: 1, resCall: 1, mutate: func(_ *InteractionAdmissionRequest, _ *admissionActorVerifier, resources *admissionResourceRepository) {
			resources.err = errors.New("synthetic repository failure")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := InteractionAdmissionRequest{
				ActionKey: "mattermost.callback.action", Operation: "action;kind=agent_turn;action=stop_turn",
				ResourceType: "agent_session_turn", ResourceID: "11", Actor: AuthenticatedActor{UserID: "actor-id"},
				Scope:     InteractionScope{Installation: "single-installation", Workspace: "workspace-1", Session: "session-1"},
				ChannelID: "channel-1", PostID: "post-1",
			}
			verifier := &admissionActorVerifier{allowed: true}
			resources := &admissionResourceRepository{allowed: true}
			test.mutate(&request, verifier, resources)
			decision := NewServerSideInteractionAdmission("single-installation", verifier, resources).Admit(context.Background(), request)
			if decision.Status != test.want {
				t.Fatalf("decision = %#v, want %s", decision, test.want)
			}
			if verifier.calls != test.actorCall || resources.calls != test.resCall {
				t.Fatalf("side effects: actor=%d resource=%d", verifier.calls, resources.calls)
			}
		})
	}
}
