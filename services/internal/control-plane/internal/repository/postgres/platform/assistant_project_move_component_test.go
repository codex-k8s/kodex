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
	turn, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-move-global-turn"}, Payload: command.AssistantTurnInput{
			ConversationRef: created.Conversation.Ref, Content: "Prepare the conversation for a project move", DeliveryMode: "QUEUE",
		}})
	if err != nil || turn.Conversation == nil {
		t.Fatalf("queue global assistant turn: conversation=%#v err=%v", turn.Conversation, err)
	}
	var runRef string
	if err := repository.pool.QueryRow(ctx, `SELECT run.ref
		FROM control_plane.runs run
		JOIN control_plane.assistant_conversations conversation ON conversation.session_id=run.session_id
		WHERE conversation.ref=$1 ORDER BY run.created_at DESC,run.ref DESC LIMIT 1`, created.Conversation.Ref).Scan(&runRef); err != nil {
		t.Fatalf("read global assistant run: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.complete",
	}, "runtime-controller")
	claimAndCompleteRun(t, ctx, service, worker, runRef, "assistant-move-global-run", false)
	conversations, _, err := service.ListAssistantConversations(ctx, owner, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatalf("refresh terminal assistant conversation: %v", err)
	}
	for index := range conversations {
		if conversations[index].Ref == created.Conversation.Ref {
			created.Conversation = &conversations[index]
			break
		}
	}
	if created.Conversation.Version <= turn.Conversation.Version {
		t.Fatalf("assistant completion did not advance conversation: %#v", created.Conversation)
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
	var mismatchedRuns, mismatchedEvents, mismatchedRevisions int
	if err := repository.pool.QueryRow(ctx, `WITH selected_session AS (
		SELECT session.id,session.project_id FROM control_plane.sessions session
		JOIN control_plane.assistant_conversations conversation ON conversation.session_id=session.id
		WHERE conversation.ref=$1
	), selected_runs AS (
		SELECT run.id FROM control_plane.runs run JOIN selected_session session ON session.id=run.session_id
	)
	SELECT
		(SELECT count(*) FROM control_plane.runs run CROSS JOIN selected_session session
		 WHERE run.id IN (SELECT id FROM selected_runs) AND run.project_id IS DISTINCT FROM session.project_id),
		(SELECT count(*) FROM control_plane.run_events event CROSS JOIN selected_session session
		 WHERE event.root_run_id IN (SELECT id FROM selected_runs) AND event.project_id IS DISTINCT FROM session.project_id),
		(SELECT count(*) FROM control_plane.runtime_revisions revision CROSS JOIN selected_session session
		 WHERE revision.session_id=session.id AND revision.project_id IS DISTINCT FROM session.project_id)`, moved.Conversation.Ref).
		Scan(&mismatchedRuns, &mismatchedEvents, &mismatchedRevisions); err != nil ||
		mismatchedRuns != 0 || mismatchedEvents != 0 || mismatchedRevisions != 0 {
		t.Fatalf("moved conversation retained foreign lineage: runs=%d events=%d revisions=%d err=%v",
			mismatchedRuns, mismatchedEvents, mismatchedRevisions, err)
	}
}
