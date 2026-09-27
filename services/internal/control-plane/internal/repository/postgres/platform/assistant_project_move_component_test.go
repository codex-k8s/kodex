package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testAssistantConversationProjectMove(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	prepareObservedWarmFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.assistant.conversations.project.move",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-move-project"},
		Payload:  command.ProjectInput{Name: "Assistant move destination", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create destination: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-move-conversation"}, Payload: command.AssistantConversationInput{}})
	if err != nil || created.Conversation == nil || created.Conversation.ProjectRef != "" {
		t.Fatalf("create global conversation: %v", err)
	}
	move := command.Command{Kind: command.MoveAssistantConversationToProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-move-to-existing-project", ExpectedVersion: &created.Conversation.Version},
		Payload:  command.AssistantConversationProjectInput{ConversationRef: created.Conversation.Ref, ProjectRef: project.Project.Ref}}
	foreign := contextProjectReader(t, ctx, repository, service, owner, project.Project.Ref, "ASSISTANT_MOVE")
	otherActor := move
	otherActor.Principal = foreign
	if _, err := service.Execute(ctx, otherActor); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("another actor moved private conversation: %v", err)
	}
	stale := move
	staleVersion := created.Conversation.Version + 1
	stale.Mutation.ExpectedVersion = &staleVersion
	stale.Mutation.IdempotencyKey = "assistant-move-stale-version"
	if _, err := service.Execute(ctx, stale); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale move accepted: %v", err)
	}
	moved, err := service.Execute(ctx, move)
	if err != nil || moved.Conversation == nil || moved.Conversation.ProjectRef != project.Project.Ref ||
		moved.Conversation.Version != created.Conversation.Version+1 || moved.Conversation.Context.EntityRef != project.Project.Ref {
		t.Fatalf("move conversation: conversation=%#v err=%v", moved.Conversation, err)
	}
	if replay, err := service.Execute(ctx, move); err != nil || replay.Conversation == nil || replay.Conversation.Version != moved.Conversation.Version {
		t.Fatalf("move replay: conversation=%#v err=%v", replay.Conversation, err)
	}
	global, _, err := service.ListAssistantConversations(ctx, owner, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatal(err)
	}
	inOrganizationHistory := false
	for _, conversation := range global {
		if conversation.Ref == moved.Conversation.Ref {
			inOrganizationHistory = conversation.ProjectRef == project.Project.Ref
		}
	}
	if !inOrganizationHistory {
		t.Fatal("moved conversation lost its project in organization history")
	}
	inProject, _, err := service.ListAssistantConversations(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 10}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, conversation := range inProject {
		found = found || conversation.Ref == moved.Conversation.Ref
	}
	if !found {
		t.Fatal("moved conversation is missing from project history")
	}
}
