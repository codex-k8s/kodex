package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRunCatalogTotals(t *testing.T, ctx context.Context, service *platformservice.Service, owner, reader value.Principal, projectRef, agentRef string) {
	t.Helper()
	created, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "catalog-total-second-run"},
		Payload:  command.LaunchRunInput{ProjectRef: projectRef, Title: "Readback run 100%_literal", Task: "Verify filtered totals", Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}}})
	if err != nil {
		t.Fatal(err)
	}
	filter := query.Filter{ProjectRef: projectRef, Query: "Readback run", States: []string{"RUNNING", "CANCELLED"}, Page: query.Page{Size: 1}}
	first, total, next, err := service.ListRuns(ctx, reader, filter)
	if err != nil || len(first) != 1 || total != 2 || next == "" {
		t.Fatalf("run first page count: len=%d total=%d cursor=%t err=%v", len(first), total, next != "", err)
	}
	filter.Page.Token = next
	filter.States = []string{"CANCELLED", "RUNNING"}
	second, total, last, err := service.ListRuns(ctx, reader, filter)
	if err != nil || len(second) != 1 || total != 2 || last != "" || first[0].Ref == second[0].Ref {
		t.Fatalf("run next page count: total=%d err=%v", total, err)
	}
	filter.Query = "changed"
	if _, _, _, err := service.ListRuns(ctx, reader, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("changed query cursor: %v", err)
	}
	filter.Query = "Readback run"
	if _, _, _, err := service.ListRuns(ctx, owner, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("changed actor cursor: %v", err)
	}
	foreign := reader
	foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000099"
	if _, _, _, err := service.ListRuns(ctx, foreign, query.Filter{}); err == nil {
		t.Fatal("foreign tenant received run count")
	}
	other, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "catalog-hidden-project"},
		Payload:  command.ProjectInput{Name: "Hidden catalog", Purpose: "Verify exact count access", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	hiddenAgent := createLifecycleAgent(t, ctx, service, owner, other.Project.Ref, "catalog-hidden-agent", "Hidden catalog agent")
	hidden, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "catalog-hidden-run"},
		Payload:  command.LaunchRunInput{ProjectRef: other.Project.Ref, Title: "Readback run hidden", Task: "Check count eligibility", Target: entity.RunTarget{Type: "AGENT", Ref: hiddenAgent.Ref}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		principal value.Principal
		total     int64
	}{{reader, 2}, {owner, 3}} {
		items, total, _, err := service.ListRuns(ctx, check.principal, query.Filter{Query: "Readback run"})
		if err != nil || total != check.total || int64(len(items)) != check.total {
			t.Fatalf("run owner eligibility count: %d err=%v", total, err)
		}
	}
	if items, total, _, err := service.ListRuns(ctx, reader, query.Filter{ProjectRef: other.Project.Ref}); err != nil || len(items) != 0 || total != 0 {
		t.Fatalf("hidden project count: %d err=%v", total, err)
	}
	hiddenVersion := hidden.Run.Version
	for i, target := range []string{projectRef, other.Project.Ref, ""} {
		name := []string{"catalog-access-visible.txt", "catalog-access-hidden.txt", "catalog-access-private.txt"}[i]
		if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: name}, platformrepo.ArtifactUpload{
			ProjectRef: target, FileName: name, MediaType: "text/plain", SizeBytes: 4, Reader: strings.NewReader("safe"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct {
		principal value.Principal
		total     int64
	}{{reader, 1}, {owner, 3}} {
		items, total, _, err := service.ListArtifacts(ctx, check.principal, query.Filter{Query: "catalog-access-"})
		if err != nil || total != check.total || int64(len(items)) != check.total {
			t.Fatalf("artifact owner eligibility count: %d err=%v", total, err)
		}
	}
	if _, _, _, err := service.ListArtifacts(ctx, foreign, query.Filter{}); err == nil {
		t.Fatal("foreign tenant received artifact count")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "catalog-hidden-cancel", ExpectedVersion: &hiddenVersion}, Payload: command.RunCommandInput{RunRef: hidden.Run.Ref, Reason: "Complete catalog fixture"}}); err != nil {
		t.Fatal(err)
	}
	for _, states := range [][]string{{"QUEUED", "QUEUED"}, {"UNKNOWN"}, {"UNSPECIFIED"}} {
		if _, _, _, err := service.ListRuns(ctx, reader, query.Filter{States: states}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("invalid states accepted: %v", err)
		}
	}
	for text, want := range map[string]int64{"%_": 1, "absent-catalog-query": 0} {
		items, total, _, err := service.ListRuns(ctx, reader, query.Filter{ProjectRef: projectRef, Query: text})
		if err != nil || total != want || int64(len(items)) != want {
			t.Fatalf("literal run query total: %d err=%v", total, err)
		}
	}
	version := created.Run.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "catalog-total-second-cancel", ExpectedVersion: &version}, Payload: command.RunCommandInput{RunRef: created.Run.Ref, Reason: "Complete catalog fixture"}}); err != nil {
		t.Fatal(err)
	}
}

func testArtifactCatalogTotals(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, firstProject string) {
	t.Helper()
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "catalog-total-other-project"}, Payload: command.ProjectInput{Name: "Catalog totals", Purpose: "Verify global eligibility", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	for i, projectRef := range []string{firstProject, project.Project.Ref, ""} {
		name := []string{"catalog-total-one.txt", "catalog-total-two.txt", "catalog-total-100%_literal.txt"}[i]
		if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: name}, platformrepo.ArtifactUpload{ProjectRef: projectRef, FileName: name, MediaType: "text/plain", SizeBytes: 4, Reader: strings.NewReader("safe")}); err != nil {
			t.Fatal(err)
		}
	}
	filter := query.Filter{Query: "catalog-total-", Page: query.Page{Size: 1}}
	seen := map[string]bool{}
	for {
		items, total, next, err := service.ListArtifacts(ctx, owner, filter)
		if err != nil || total != 3 || len(items) != 1 || seen[items[0].Ref] {
			t.Fatalf("global artifact page: len=%d total=%d err=%v", len(items), total, err)
		}
		seen[items[0].Ref] = true
		if next == "" {
			break
		}
		changed := filter
		changed.Page.Token = next
		changed.ProjectRef = firstProject
		if _, _, _, err := service.ListArtifacts(ctx, owner, changed); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("changed project cursor: %v", err)
		}
		changed = filter
		changed.Page.Token = next
		changed.Query = "other"
		if _, _, _, err := service.ListArtifacts(ctx, owner, changed); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("changed artifact query cursor: %v", err)
		}
		filter.Page.Token = next
	}
	if len(seen) != 3 {
		t.Fatalf("global artifact pagination: %d", len(seen))
	}
	for _, f := range []query.Filter{{Query: "catalog-total-", ProjectRef: firstProject}, {Query: "%_"}} {
		items, total, _, err := service.ListArtifacts(ctx, owner, f)
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("artifact exact filtered count: %d err=%v", total, err)
		}
	}
	items, total, next, err := service.ListArtifacts(ctx, owner, query.Filter{Query: "absent-catalog-query"})
	if err != nil || total != 0 || len(items) != 0 || next != "" {
		t.Fatalf("empty artifact count: %d err=%v", total, err)
	}
}
