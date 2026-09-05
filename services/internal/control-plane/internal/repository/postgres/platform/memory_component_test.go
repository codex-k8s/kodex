package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testMemoryRecords(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.memory-records.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "memory-project"}, Payload: command.ProjectInput{Name: "Memory lifecycle", Purpose: "Memory retention test", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(kind command.Kind, key string, version *int64, payload command.MemoryRecordInput) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: payload})
	}
	spec := entity.MemoryRecordSpecification{Title: "Decisions", Summary: "Initial summary", RetentionUntil: time.Now().UTC().Add(24 * time.Hour)}
	createPayload := command.MemoryRecordInput{ProjectRef: project.Project.Ref, Specification: spec}
	created, err := invoke(command.CreateMemoryRecord, "memory-create", nil, createPayload)
	if err != nil || created.MemoryRecord == nil {
		t.Fatalf("create memory: %v", err)
	}
	record := created.MemoryRecord
	first := record.CurrentRevision.Ref
	spec.Summary = "Revised summary"
	updated, err := invoke(command.ReviseMemoryRecord, "memory-revise", &record.Version, command.MemoryRecordInput{RecordRef: record.Ref, Specification: spec})
	if err != nil || updated.MemoryRecord == nil {
		t.Fatalf("revise memory: %v", err)
	}
	if updated.MemoryRecord.CurrentRevision.ParentRevisionRef != first {
		t.Fatal("revision lineage lost")
	}
	if _, err := invoke(command.ReviseMemoryRecord, "memory-stale", &record.Version, command.MemoryRecordInput{RecordRef: record.Ref, Specification: spec}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale revision: %v", err)
	}
	record = updated.MemoryRecord
	testContextVFS(t, ctx, service, owner, project.Project.Ref, record.Ref, "MEMORY", record.CurrentRevision.Digest, true)
	testContextBinding(t, ctx, repository, service, owner, project.Project.Ref, record.Ref, record.CurrentRevision.Ref, "memory-context", true)
	history, total, next, err := service.ListMemoryRecordRevisions(ctx, owner, record.Ref, query.Page{Size: 1})
	if err != nil || total != 2 || len(history) != 1 || next == "" {
		t.Fatalf("history page: total=%d count=%d next=%t err=%v", total, len(history), next != "", err)
	}
	history, _, _, err = service.ListMemoryRecordRevisions(ctx, owner, record.Ref, query.Page{Size: 1, Token: next})
	if err != nil || len(history) != 1 || history[0].Ref != first || history[0].Summary != "Initial summary" {
		t.Fatalf("immutable history: %v", err)
	}
	items, total, _, err := service.ListMemoryRecords(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 1}})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("memory catalog: total=%d count=%d err=%v", total, len(items), err)
	}
	for _, step := range []struct {
		kind command.Kind
		key  string
	}{{command.ArchiveMemoryRecord, "memory-archive"}, {command.RestoreMemoryRecord, "memory-restore"}, {command.ArchiveMemoryRecord, "memory-rearchive"}, {command.PurgeMemoryRecord, "memory-purge"}} {
		result, err := invoke(step.kind, step.key, &record.Version, command.MemoryRecordInput{RecordRef: record.Ref})
		if err != nil || result.MemoryRecord == nil {
			t.Fatalf("%s: %v", step.key, err)
		}
		record = result.MemoryRecord
		testContextVFS(t, ctx, service, owner, project.Project.Ref, record.Ref, "MEMORY", record.CurrentRevision.Digest, record.State == "ACTIVE")
	}
	if record.State != "PURGED" || !record.CurrentRevision.Redacted || record.CurrentRevision.Summary != "" {
		t.Fatal("purged summary remains visible")
	}
	replayed, err := invoke(command.CreateMemoryRecord, "memory-create", nil, createPayload)
	if err != nil || replayed.MemoryRecord == nil || replayed.MemoryRecord.CurrentRevision.Summary != "" || !replayed.MemoryRecord.CurrentRevision.Redacted {
		t.Fatalf("purged summary leaked through receipt: %v", err)
	}
	history, total, _, err = service.ListMemoryRecordRevisions(ctx, owner, record.Ref, query.Page{Size: 10})
	if err != nil || total != 2 {
		t.Fatalf("purged history: %v", err)
	}
	for _, revision := range history {
		if !revision.Redacted || revision.Summary != "" {
			t.Fatal("old purged summary remains visible")
		}
	}
	if _, err := invoke(command.RestoreMemoryRecord, "memory-restore-purged", &record.Version, command.MemoryRecordInput{RecordRef: record.Ref}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("restore purged: %v", err)
	}
}
