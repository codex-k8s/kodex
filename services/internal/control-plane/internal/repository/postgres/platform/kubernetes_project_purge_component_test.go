package platform

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesProjectBoundarySnapshotSQL commits only to the disposable project's lifecycle and
// invocation/root fences, without returning private credentials or logging their contents.
// kubernetesProjectBoundarySnapshotSQL связывает только lifecycle disposable проекта и fences
// invocation/root, не возвращая private credentials и не записывая их содержимое в logs.
//
//go:embed testdata/sql/kubernetes_project_boundary_snapshot.sql
var kubernetesProjectBoundarySnapshotSQL string

// kubernetesArtifactPurgeStageSQL observes retained provenance and remaining stage eligibility
// after canonical artifact purge removes physical revision pointers.
// kubernetesArtifactPurgeStageSQL читает retained provenance и eligibility оставшихся stages
// после удаления физических revision pointers каноническим artifact purge.
//
//go:embed testdata/sql/kubernetes_artifact_purge_stage.sql
var kubernetesArtifactPurgeStageSQL string

// exerciseKubernetesProjectUnknownBoundary proves that a real uncertain prepared PUT blocks
// public project deletion before cancellation changes the owner graph. The same original actor
// can still read capture metadata and the existing safe recovery count; no abandon or PUT occurs.
// exerciseKubernetesProjectUnknownBoundary доказывает, что actual uncertain prepared PUT запрещает
// public удаление проекта до изменения owner graph отменой. Тот же original actor сохраняет чтение
// capture metadata и existing safe recovery count; abandon либо PUT здесь не выполняются.
func exerciseKubernetesProjectUnknownBoundary(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, actor value.Principal, captureRef string) {
	t.Helper()
	var ref, before string
	var version int64
	if err := r.pool.QueryRow(ctx, kubernetesProjectBoundarySnapshotSQL, captureRef).Scan(&ref, &version, &before); err != nil {
		t.Fatal("read original project graph", err)
	}
	if err := r.ConfigureProjectPurgeStorage("https://disposable.invalid", "fixture", "fixture", true); err != nil {
		t.Fatal(err)
	}
	_, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "capture-unknown-project-trash", ExpectedVersion: &version}, Payload: command.ProjectLifecycleInput{Ref: ref}})
	if !errors.Is(err, errs.ErrKubernetesStagingReconciliationRequired) {
		t.Fatal("unknown capture did not preserve project eligibility", err)
	}
	var afterRef, after string
	var afterVersion int64
	if err := r.pool.QueryRow(ctx, kubernetesProjectBoundarySnapshotSQL, captureRef).Scan(&afterRef, &afterVersion, &after); err != nil || afterRef != ref || afterVersion != version || after != before {
		t.Fatal("rejected trash changed original execution graph", err)
	}
	actor.Permission = "platform.query.projects.purge-recovery.get"
	view, err := service.GetProjectPurgeRecovery(ctx, actor, ref)
	if err != nil || view.State != "BLOCKED" || view.ReasonCode != managedkubernetes.ProjectStagingReconciliationReason || view.UnknownContentCount < 1 || view.CanResolve || !view.ProjectPresent {
		t.Fatal("blocked staging has no safe reachable readback", err)
	}
	actor.Permission = "platform.integrations.kubernetes.capture.get"
	if _, err := service.GetKubernetesCaptureStatus(ctx, actor, query.KubernetesCaptureStatusRead{CaptureRef: captureRef}); err != nil {
		t.Fatal("rejected trash lost original-root capture read", err)
	}
}

// exerciseKubernetesArtifactRestoreAndPurge uses the canonical artifact commands to restore the
// exact personal capture, retains foreign-actor denial, and purges immutable revisions before
// deleting the remaining protected stage with the existing retention worker.
// exerciseKubernetesArtifactRestoreAndPurge использует канонические artifact commands для restore
// точного personal capture, сохраняет foreign-actor denial и purges immutable revisions до удаления
// оставшегося protected stage существующим retention worker.
func exerciseKubernetesArtifactRestoreAndPurge(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, consumer *pgxpool.Pool, actor, foreign value.Principal, artifactRef, revisionRef string) {
	t.Helper()
	actor.Permission = "platform.query.artifacts.get"
	before, err := service.GetArtifact(ctx, actor, artifactRef)
	if err != nil || before.CaptureOrigin == nil {
		t.Fatal("read captured artifact before trash", err)
	}
	// deleteArtifact invokes the current canonical impact/version path for each separate deletion.
	// deleteArtifact вызывает текущий canonical impact/version path для каждого отдельного удаления.
	deleteArtifact := func(key string) {
		t.Helper()
		current, err := service.GetArtifact(ctx, actor, artifactRef)
		if err != nil {
			t.Fatal(err)
		}
		impact, err := service.GetArtifactImpact(ctx, actor, artifactRef, "DELETE")
		if err != nil || !impact.Permitted {
			t.Fatal("capture delete impact", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &current.Version}, Payload: command.ArtifactLifecycleInput{ArtifactRef: artifactRef, ImpactDigest: impact.Digest}}); err != nil {
			t.Fatal("delete exact captured artifact", err)
		}
	}
	deleteArtifact("capture-artifact-trash-one")
	deleted, err := service.GetArtifact(ctx, actor, artifactRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RestoreArtifact, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "capture-artifact-restore", ExpectedVersion: &deleted.Version}, Payload: command.ArtifactLifecycleInput{ArtifactRef: artifactRef}}); err != nil {
		t.Fatal("restore captured artifact", err)
	}
	restored, err := service.GetArtifact(ctx, actor, artifactRef)
	if err != nil || !reflect.DeepEqual(restored.CaptureOrigin, before.CaptureOrigin) {
		t.Fatal("restore changed captured provenance", err)
	}
	actor.Permission = "platform.command.artifacts.download"
	download, err := service.DownloadArtifactRevision(ctx, actor, artifactRef, revisionRef, "DOWNLOAD")
	if err != nil {
		t.Fatal("restore lost exact captured bytes", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(download.Reader, 1024))
	closeErr := download.Reader.Close()
	defer clear(body)
	if readErr != nil || closeErr != nil || string(body) != "confirmed prefix confirmed late tail" {
		t.Fatal("restore changed captured bytes", readErr, closeErr)
	}
	foreign.Permission = "platform.query.artifacts.get"
	if _, err := service.GetArtifact(ctx, foreign, artifactRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("restore expanded captured visibility", err)
	}
	deleteArtifact("capture-artifact-trash-two")
	deleted, err = service.GetArtifact(ctx, actor, artifactRef)
	if err != nil {
		t.Fatal(err)
	}
	impact, err := service.GetArtifactImpact(ctx, actor, artifactRef, "PURGE")
	if err != nil || impact.Permitted || !slices.Contains(impact.Blockers, "ARTIFACT_HAS_BINDINGS") {
		t.Fatal("active RUN_RESULT binding did not protect captured artifact", impact.Blockers, err)
	}
	actor.Permission = "platform.query.runs.get"
	parent, err := service.GetRun(ctx, actor, before.RunRef)
	if err != nil {
		t.Fatal("read original capture run for explicit fixture cancellation", err)
	}
	if parent.RootRunRef != "" && parent.RootRunRef != parent.Ref {
		parent, err = service.GetRun(ctx, actor, parent.RootRunRef)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "capture-purge-close-run", ExpectedVersion: &parent.Version}, Payload: command.RunCommandInput{RunRef: parent.Ref}}); err != nil {
		t.Fatal("close active RUN_RESULT owner before purge", err)
	}
	actor.Permission = "platform.query.artifacts.impact.get"
	impact, err = service.GetArtifactImpact(ctx, actor, artifactRef, "PURGE")
	if err != nil || !impact.Permitted {
		t.Fatal("capture purge impact", impact.Blockers, err)
	}
	exerciseManualCapturePurge(t, ctx, r, service, consumer, actor, foreign, artifactRef, impact.Digest, deleted.Version)
	var retained bool
	if err := r.pool.QueryRow(ctx, kubernetesArtifactPurgeStageSQL, artifactRef).Scan(&retained); err != nil || !retained {
		t.Fatal("purged capture stranded private staged bytes", err)
	}
	// The protected consumer claim below proves eligibility without granting its private helper to public runtime.
	// Protected consumer claim ниже доказывает eligibility без выдачи private helper публичному runtime.
	var kind, id, key, version, etag, digest string
	var size, generation int64
	var uncertain, done bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, "capture-component-cleanup").Scan(&kind, &id, &key, &version, &etag, &digest, &size, &generation, &uncertain); err != nil || kind != "KUBERNETES_OUTPUT" || uncertain {
		t.Fatal("claim published stage after artifact purge", err)
	}
	if err := r.objects.Delete(ctx, key, version); err != nil {
		t.Fatal(err)
	}
	if _, err := r.objects.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("purged artifact stage still exists", err)
	}
	if err := consumer.QueryRow(ctx, kubernetesCleanupFinishSQL, id, kind, "capture-component-cleanup", generation, version, etag, true).Scan(&done); err != nil || !done {
		t.Fatal("record purged artifact stage cleanup", err)
	}
}

// exerciseKubernetesPreparedWriterAgainstTrash blocks the actual one-PUT body while its writer
// transaction owns the project SHARE lock. An independent public Trash reaches a proven database
// blocking edge, then sees the committed UNKNOWN intent and refuses closure. Both goroutines join
// before test dependencies close, including early failure and cancellation paths.
// exerciseKubernetesPreparedWriterAgainstTrash удерживает actual one-PUT body, пока writer Tx
// владеет project SHARE lock. Независимый public Trash достигает доказанного DB blocking edge,
// затем видит committed UNKNOWN intent и отклоняет closure. Обе goroutines завершаются до закрытия
// test dependencies, включая ранний отказ и отмену.
func exerciseKubernetesPreparedWriterAgainstTrash(t *testing.T, ctx context.Context, r *Repository, actor value.Principal, captureRef string, store *kubernetesCaptureLostPutStore, publish func(context.Context) error) error {
	t.Helper()
	var ref, snapshot string
	var version int64
	if err := r.pool.QueryRow(ctx, kubernetesProjectBoundarySnapshotSQL, captureRef).Scan(&ref, &version, &snapshot); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	store.entered, store.release = entered, release
	running, cancel := context.WithCancel(ctx)
	pubDone, trashDone := make(chan error, 1), make(chan error, 1)
	pubJoined, trashJoined, trashStarted, released := false, false, false, false
	defer func() {
		if !released {
			close(release)
		}
		cancel()
		join, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if !pubJoined {
			select {
			case <-pubDone:
			case <-join.Done():
				t.Error("prepared writer did not positively join")
			}
		}
		if trashStarted && !trashJoined {
			select {
			case <-trashDone:
			case <-join.Done():
				t.Error("project trash did not positively join")
			}
		}
	}()
	go func() { pubDone <- publish(running) }()
	wait, stop := context.WithTimeout(ctx, 5*time.Second)
	select {
	case <-entered:
	case <-wait.Done():
		stop()
		t.Fatal("prepared PUT boundary was not reached")
	}
	stop()
	tr := &kubernetesCaptureACLTrace{marker: queryProjectTrashLock, reached: make(chan struct{})}
	observed := newKubernetesCaptureACLService(t, ctx, r, tr)
	trashStarted = true
	go func() {
		_, err := observed.Execute(running, command.Command{Kind: command.TrashProject, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "capture-writer-project-trash", ExpectedVersion: &version}, Payload: command.ProjectLifecycleInput{Ref: ref}})
		trashDone <- err
	}()
	awaitKubernetesCaptureACLBlock(t, ctx, r, tr)
	close(release)
	released = true
	var pubErr, errorTrash error
	select {
	case pubErr = <-pubDone:
		pubJoined = true
	case <-ctx.Done():
		t.Fatal("prepared writer did not return")
	}
	select {
	case errorTrash = <-trashDone:
		trashJoined = true
	case <-ctx.Done():
		t.Fatal("project trash did not return")
	}
	if !errors.Is(errorTrash, errs.ErrKubernetesStagingReconciliationRequired) {
		t.Fatal("trash ignored uncertain writer after project lock wait", errorTrash)
	}
	if store.puts.Load() != 1 {
		t.Fatal("project closure retried prepared PUT")
	}
	return pubErr
}
