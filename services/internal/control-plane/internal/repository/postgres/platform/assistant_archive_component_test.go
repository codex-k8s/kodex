package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testAssistantHistoryArchive(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	principal := func(actor string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
			ExternalActorID: actor, ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.conversations.archive"}, "control-api-gateway")
	}
	owner := principal("20000000-0000-4000-8000-000000000001")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "history-reader-project"}, Payload: command.ProjectInput{Name: "History reader", Purpose: "Actor isolation", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("history reader project: %v", err)
	}
	foreign := contextProjectReader(t, ctx, repository, service, owner, project.Project.Ref, "ASSISTANT")
	for _, key := range []string{"one", "two", "three"} {
		created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "history-create-" + key}, Payload: command.AssistantConversationInput{}})
		if err != nil || created.Conversation == nil {
			t.Fatalf("create history fixture: %v", err)
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.UpdateAssistantConversation, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "history-title-" + key, ExpectedVersion: &created.Conversation.Version}, Payload: command.AssistantConversationTitleInput{ConversationRef: created.Conversation.Ref, Title: "History archive " + key}})
		if err != nil {
			t.Fatal(err)
		}
	}
	filter := query.Filter{Query: "history archive", Page: query.Page{Size: 1}}
	var items []entity.AssistantConversation
	var firstToken string
	for {
		page, next, err := service.ListAssistantConversations(ctx, owner, filter)
		if err != nil || len(page) != 1 {
			t.Fatalf("history page: count=%d %v", len(page), err)
		}
		items = append(items, page...)
		if firstToken == "" {
			firstToken = next
		}
		if next == "" {
			break
		}
		filter.Page.Token = next
		if len(items) > 3 {
			t.Fatal("history cursor did not advance")
		}
	}
	if len(items) != 3 || items[0].Ref == items[1].Ref || items[1].Ref == items[2].Ref {
		t.Fatal("history pagination duplicated or omitted conversation")
	}
	changed := query.Filter{Query: "other", Page: query.Page{Size: 1, Token: firstToken}}
	if _, _, err := service.ListAssistantConversations(ctx, owner, changed); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("history cursor crossed search: %v", err)
	}
	changed.Query = "history archive"
	if _, _, err := service.ListAssistantConversations(ctx, foreign, changed); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("history cursor crossed actor: %v", err)
	}
	changed.Page.Token = ""
	if page, _, err := service.ListAssistantConversations(ctx, foreign, changed); err != nil || len(page) != 0 {
		t.Fatalf("another actor read conversations: count=%d %v", len(page), err)
	}
	archive := command.Command{Kind: command.ArchiveAssistantConversation, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "history-archive", ExpectedVersion: &items[0].Version}, Payload: command.AssistantConversationArchiveInput{ConversationRef: items[0].Ref}}
	wrong := archive
	wrong.Principal = foreign
	if _, err := service.Execute(ctx, wrong); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("another actor archived conversation: %v", err)
	}
	stale := int64(1)
	wrong = archive
	wrong.Mutation.ExpectedVersion = &stale
	if _, err := service.Execute(ctx, wrong); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale archive accepted: %v", err)
	}
	result, err := service.Execute(ctx, archive)
	if err != nil || result.Conversation == nil || result.Conversation.State != "ARCHIVED" {
		t.Fatalf("archive history: %v", err)
	}
	if replay, err := service.Execute(ctx, archive); err != nil || replay.Conversation == nil || replay.Conversation.Version != result.Conversation.Version {
		t.Fatalf("archive replay: %v", err)
	}
	active, _, err := service.ListAssistantConversations(ctx, owner, query.Filter{Query: "history archive", Page: query.Page{Size: 10}})
	if err != nil || len(active) != 2 {
		t.Fatalf("archived chat remains active: %d %v", len(active), err)
	}
	archived, _, err := service.ListAssistantConversations(ctx, owner, query.Filter{Query: "history archive", State: "ARCHIVED", Page: query.Page{Size: 10}})
	if err != nil || len(archived) != 1 || archived[0].Ref != items[0].Ref {
		t.Fatalf("archived history missing: %d %v", len(archived), err)
	}
}
