package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testConfigOverlayHistory(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "overlay-history-project"}, Payload: command.ProjectInput{Name: "Overlay history", Purpose: "Verify published revision selection", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "overlay-history-agent", "Overlay history agent")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	initial := view.PublishedOverlay
	invoke := func(kind command.Kind, key, content, ref string) entity.AgentRuntimeConfigurationView {
		t.Helper()
		version := view.AgentVersion
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, Payload: command.ConfigOverlayInput{AgentRef: agent.Ref, Content: content, PublishedOverlayRef: ref}})
		if err != nil || result.RuntimeConfiguration == nil {
			t.Fatalf("overlay mutation %s: %v", key, err)
		}
		view = *result.RuntimeConfiguration
		return view
	}
	unpublishedRef := ""
	for i, content := range []string{"personality = \"friendly\"\n", "personality = \"pragmatic\"\n"} {
		invoke(command.CreateConfigOverlayDraft, fmt.Sprintf("overlay-history-create-%d", i), content, "")
		unpublishedRef = view.DraftOverlay.Ref
		invoke(command.ValidateConfigOverlayDraft, fmt.Sprintf("overlay-history-validate-%d", i), "", "")
		invoke(command.PublishConfigOverlayDraft, fmt.Sprintf("overlay-history-publish-%d", i), "", "")
	}
	invoke(command.CreateConfigOverlayDraft, "overlay-history-unpublished", "personality = \"friendly\"\n", "")
	version := view.AgentVersion
	if _, err := service.Execute(ctx, command.Command{Kind: command.RollbackConfigOverlay, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "overlay-history-reject-unpublished", ExpectedVersion: &version},
		Payload:  command.ConfigOverlayInput{AgentRef: agent.Ref, PublishedOverlayRef: unpublishedRef},
	}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("rollback accepted an unpublished superseded draft: %v", err)
	}
	draftRef := view.DraftOverlay.Ref
	filter := query.Filter{ResourceRef: agent.Ref, Page: query.Page{Size: 1}}
	var refs []string
	var previous int64 = 1<<53 - 1
	firstCursor := ""
	for {
		items, total, next, err := service.ListConfigOverlayRevisions(ctx, owner, filter)
		if err != nil || total != 3 || len(items) != 1 || items[0].Revision >= previous {
			t.Fatalf("history page: len=%d total=%d err=%v", len(items), total, err)
		}
		item := items[0]
		previous = item.Revision
		refs = append(refs, item.Ref)
		exact, err := service.GetConfigOverlayRevision(ctx, owner, agent.Ref, item.Ref)
		if err != nil || exact.Content != item.Content || exact.Digest != item.Digest || exact.Revision != item.Revision {
			t.Fatalf("history exact read: %v", err)
		}
		if next == "" {
			break
		}
		if firstCursor == "" {
			firstCursor = next
		}
		filter.Page.Token = next
	}
	if len(refs) != 3 || refs[2] != initial.Ref {
		t.Fatalf("history missing initial revision: %v", refs)
	}
	if _, err := service.GetConfigOverlayRevision(ctx, owner, agent.Ref, draftRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("unpublished draft exposed: %v", err)
	}
	other := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "overlay-history-other-agent", "Other overlay agent")
	if _, err := service.GetConfigOverlayRevision(ctx, owner, other.Ref, initial.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign agent revision exposed: %v", err)
	}
	for _, changed := range []query.Filter{{ResourceRef: agent.Ref, Query: "changed", Page: query.Page{Token: firstCursor}}, {ResourceRef: other.Ref, Page: query.Page{Token: firstCursor}}} {
		if _, _, _, err := service.ListConfigOverlayRevisions(ctx, owner, changed); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("changed scope cursor accepted: %v", err)
		}
	}
	items, total, _, err := service.ListConfigOverlayRevisions(ctx, owner, query.Filter{ResourceRef: agent.Ref, Query: initial.Ref})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("filtered history count: %d err=%v", total, err)
	}
	items, total, _, err = service.ListConfigOverlayRevisions(ctx, owner, query.Filter{ResourceRef: agent.Ref, Query: "%_"})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("literal history query: %d err=%v", total, err)
	}
	foreign := owner
	foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000099"
	if _, _, _, err := service.ListConfigOverlayRevisions(ctx, foreign, query.Filter{ResourceRef: agent.Ref}); err == nil {
		t.Fatal("foreign tenant read history")
	}
	if _, err := service.GetConfigOverlayRevision(ctx, foreign, agent.Ref, initial.Ref); err == nil {
		t.Fatal("foreign tenant read revision")
	}
	if _, err := service.GetConfigOverlayRevision(ctx, owner, agent.Ref, "bad/ref"); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("malformed revision: %v", err)
	}
	invoke(command.RollbackConfigOverlay, "overlay-history-rollback", "", initial.Ref)
	if view.PublishedOverlay.Ref == initial.Ref || view.PublishedOverlay.Content != initial.Content || view.PublishedOverlay.Revision <= previous {
		t.Fatal("rollback did not create fresh publication")
	}
	items, total, _, err = service.ListConfigOverlayRevisions(ctx, owner, query.Filter{ResourceRef: agent.Ref})
	if err != nil || total != 4 || len(items) != 4 || items[0].Ref != view.PublishedOverlay.Ref {
		t.Fatalf("rollback history readback: %d err=%v", total, err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.ListConfigOverlayRevisions(ctx, owner, query.Filter{ResourceRef: assistant.Ref}); err != nil {
		t.Fatalf("system assistant history: %v", err)
	}
}
