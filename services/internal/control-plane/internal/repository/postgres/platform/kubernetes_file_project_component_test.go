package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryFileOwnerProjectGraph reads server-assigned IDs before testing the canonical project purge.
// RU: queryFileOwnerProjectGraph читает server-assigned IDs перед canonical project purge.
//
//go:embed testdata/sql/kubernetes_file_owner_project_graph.sql
var queryFileOwnerProjectGraph string

// queryFileOwnerProjectPurged checks exact removed graph rows and the detached metadata-only prepared receipt.
// RU: queryFileOwnerProjectPurged проверяет удалённые graph rows и detached metadata-only prepared receipt.
//
//go:embed testdata/sql/kubernetes_file_owner_project_purged.sql
var queryFileOwnerProjectPurged string

// queryFileOwnerUnallocatedPVC validates the exact session/name tuple without claiming Kubernetes absence.
// This disposable fixture creates no PVC or Kubernetes client; any active workspace custody rejects the test.
// RU: queryFileOwnerUnallocatedPVC проверяет exact session/name tuple, не утверждая Kubernetes absence.
// Disposable fixture не создаёт PVC либо Kubernetes client; active workspace custody отклоняет тест.
//
//go:embed testdata/sql/kubernetes_file_owner_unallocated_pvc.sql
var queryFileOwnerUnallocatedPVC string

// assertFileProjectTrashBlocked proves that unknown deletion cannot strand the original root's repair authority.
// It uses the real public project/recovery services and requires ACTIVE/version preservation before closure.
// RU: assertFileProjectTrashBlocked доказывает, что unknown deletion не блокирует root repair authority.
// Использует public project/recovery services и требует сохранения ACTIVE/version до closure.
func assertFileProjectTrashBlocked(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, actor value.Principal, projectRef string) {
	t.Helper()
	if err := repository.ConfigureProjectPurgeStorage("https://disposable.invalid", "fixture", "fixture", true); err != nil {
		t.Fatal("configure disposable FILE project purge")
	}
	actor.Permission = "platform.query.projects.get"
	before, err := service.GetProject(ctx, actor, projectRef)
	if err != nil || before.Lifecycle != "ACTIVE" {
		t.Fatal("read active FILE project before unknown cleanup", err)
	}
	actor.Permission = "platform.command.projects.trash"
	_, err = service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "file-project-blocked-trash", ExpectedVersion: &before.Version}, Payload: command.ProjectLifecycleInput{Ref: projectRef}})
	if !errors.Is(err, errs.ErrKubernetesStagingReconciliationRequired) {
		t.Fatal("project trash bypassed unknown FILE cleanup", err)
	}
	actor.Permission = "platform.query.projects.get"
	after, err := service.GetProject(ctx, actor, projectRef)
	if err != nil || after.Lifecycle != before.Lifecycle || after.Version != before.Version {
		t.Fatal("blocked FILE project trash changed repair eligibility", err)
	}
	actor.Permission = "platform.query.projects.purge-recovery.get"
	recovery, err := service.GetProjectPurgeRecovery(ctx, actor, projectRef)
	if err != nil || recovery.State != "BLOCKED" || recovery.ReasonCode != managedkubernetes.ProjectStagingReconciliationReason || recovery.UnknownContentCount < 1 || recovery.CanResolve || !recovery.ProjectPresent {
		t.Fatal("FILE project blocker lost authoritative recovery readback", err)
	}
}

// exerciseFileProjectLifecycle reuses public Trash/Restore/Purge plus the existing owner inventory/finalizer.
// Exact local object deletions are fixture effects; it proves database graph closure, not the deployed purge worker.
// RU: exerciseFileProjectLifecycle использует public Trash/Restore/Purge и existing owner inventory/finalizer.
// Exact local object deletions являются fixture effects; проверяется DB graph, не deployed purge worker.
func exerciseFileProjectLifecycle(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, current scope, actor value.Principal, projectRef, sourceRef string, store *fileOwnerCountStore) {
	t.Helper()
	var projectID, sourceID, invocationID, preparedID, streamID string
	if err := repository.pool.QueryRow(ctx, queryFileOwnerProjectGraph, current.organizationID, sourceRef).Scan(&projectID, &sourceID, &invocationID, &preparedID, &streamID); err != nil {
		t.Fatal("read exact FILE project graph", err)
	}
	putsBefore := store.puts.Load()
	execute := newComponentCommandExecutor(t, ctx, service, "file-project-")
	foreign := execute(command.CreateProject, actor, "foreign", nil, command.ProjectInput{Name: "Retained independent project", Language: "ru"}).Project
	foreignBody := "retained independent artifact"
	foreignArtifact, err := service.UploadArtifact(ctx, actor, value.Mutation{IdempotencyKey: "file-project-foreign-artifact"}, port.ArtifactUpload{ProjectRef: foreign.Ref, FileName: "retained.txt", MediaType: "text/plain", SizeBytes: int64(len(foreignBody)), Reader: strings.NewReader(foreignBody)})
	if err != nil {
		t.Fatal("upload independent project artifact", err)
	}
	if store.puts.Load() != putsBefore+1 {
		t.Fatal("foreign fixture artifact did not perform exactly one PUT")
	}
	actor.Permission = "platform.query.projects.get"
	before, err := service.GetProject(ctx, actor, projectRef)
	if err != nil {
		t.Fatal("read cleaned FILE project", err)
	}
	trashed := execute(command.TrashProject, actor, "trash", &before.Version, command.ProjectLifecycleInput{Ref: projectRef}).Project
	restored := execute(command.RestoreProject, actor, "restore", &trashed.Version, command.ProjectLifecycleInput{Ref: projectRef}).Project
	if restored.Lifecycle != "ACTIVE" {
		t.Fatal("FILE project restore did not return ACTIVE")
	}
	trashed = execute(command.TrashProject, actor, "trash-again", &restored.Version, command.ProjectLifecycleInput{Ref: projectRef}).Project
	pending := execute(command.PurgeProject, actor, "purge", &trashed.Version, command.ProjectLifecycleInput{Ref: projectRef}).Project
	if pending.Lifecycle != "PURGE_PENDING" {
		t.Fatal("FILE project purge did not enter PURGE_PENDING")
	}
	candidate := ProjectPurgeCandidate{OrganizationID: current.organizationID, OrganizationRef: current.organizationRef, ProjectID: projectID, ProjectRef: projectRef}
	items, err := repository.ProjectPurgeInventory(ctx, candidate)
	if err != nil {
		t.Fatal("read FILE project inventory", err)
	}
	objects, pvcs := 0, 0
	for _, item := range items {
		switch item.Kind {
		case "OBJECT":
			objects++
		case "PVC":
			var owned bool
			if err := repository.pool.QueryRow(ctx, queryFileOwnerUnallocatedPVC, current.organizationID, projectID, item.Version, item.Target).Scan(&owned); err != nil || !owned {
				t.Fatal("fixture PVC tuple or workspace custody is not closed", err)
			}
			pvcs++
		default:
			t.Fatal("FILE project inventory contains an unsupported kind")
		}
	}
	if objects != 2 {
		t.Fatalf("FILE project inventory lost original versions: objects=%d pvcs=%d total=%d", objects, pvcs, len(items))
	}
	// Computed PVC tuples are validated above; this fixture never allocated their external resources.
	// Проверены computed PVC tuples; fixture никогда не создавал соответствующие external resources.
	for _, item := range items {
		if item.Kind == "PVC" {
			continue
		}
		if item.Version == "" || strings.Contains(item.Target, "/kubernetes-file-inputs/") {
			t.Fatal("project purge could delete an admitted FILE source again")
		}
		shared, err := repository.ProjectPurgeObjectIsShared(ctx, candidate, item.Target)
		if err != nil || shared {
			t.Fatal("project inventory object is not exclusive", err)
		}
		if err := store.DeleteOnce(ctx, item.Target, item.Version); err != nil {
			t.Fatal("delete exact disposable project artifact version", err)
		}
	}
	if err := repository.FinalizeProjectPurge(ctx, candidate, items); err != nil {
		t.Fatal("finalize FILE project graph", err)
	}
	var sources, frames, admissionFK, origins, invocations, streams, inputs, prepared int64
	if err := repository.pool.QueryRow(ctx, queryFileOwnerProjectPurged, current.organizationID, sourceID, invocationID, preparedID, streamID, sourceRef).Scan(&sources, &frames, &admissionFK, &origins, &invocations, &streams, &inputs, &prepared); err != nil || sources+frames+origins+invocations+streams+inputs != 0 || admissionFK != 1 || prepared != 1 {
		t.Fatal("FILE project graph or detached provenance survived incorrectly", err)
	}
	if err := repository.FinalizeProjectPurge(ctx, candidate, items); err != nil {
		t.Fatal("replay FILE project purge", err)
	}
	actor.Permission = "platform.query.artifacts.get"
	retained, err := service.GetArtifact(ctx, actor, foreignArtifact.Ref)
	if err != nil || retained.CurrentRevisionRef != foreignArtifact.CurrentRevisionRef || retained.ProjectRef != foreign.Ref {
		t.Fatal("FILE project purge changed another project's artifact", err)
	}
	if store.puts.Load() != putsBefore+1 {
		t.Fatal("FILE project lifecycle rematerialized source bytes")
	}
}
