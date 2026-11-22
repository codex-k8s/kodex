package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesCleanupPreparedRecordSQL binds the independently recovered exact provider version before deletion.
// kubernetesCleanupPreparedRecordSQL связывает independently recovered exact provider version до удаления.
//
//go:embed testdata/sql/kubernetes_cleanup_prepared_record.sql
var kubernetesCleanupPreparedRecordSQL string

// TestKubernetesCapturePreparedCleanupOwnerComponent deletes a superseded unadopted prepared intent
// after three failed cleanup attempts and explicit original-owner repair. A later capture is still
// assembled from retained stages and published under its own immutable intent, never by replaying
// the old uncertain PUT. Disabled AV and local immutable object storage are explicit fixture bounds.
// TestKubernetesCapturePreparedCleanupOwnerComponent удаляет superseded unadopted prepared intent
// после трёх failed cleanup attempts и explicit original-owner repair. Следующий capture собирается
// из retained stages и публикуется под своим immutable intent, без повтора старого uncertain PUT.
// Disabled AV и local immutable object storage — явные ограничения fixture.
func TestKubernetesCapturePreparedCleanupOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
	defer cancel()
	repository, service, broker, gateway, binding, _, admission, _, actor := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
	spool, err := repository.ConfigureKubernetesCaptureSpool(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	if err := repository.ConfigureArtifactAntivirusMode("disabled"); err != nil {
		t.Fatal(err)
	}
	stream := admission.Stream.Ref
	// stageOutput owns test bytes only until the actual owner stage acceptance returns.
	// stageOutput владеет test bytes только до ответа actual owner stage acceptance.
	stageOutput := func(sequence uint64, body string, ack bool) command.ManagedKubernetesBeginFrame {
		payload := []byte(body)
		defer clear(payload)
		sum := sha256.Sum256(payload)
		frame := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: sequence, Channel: "STDOUT", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: uint32(len(payload)), NativeAuthorizedUntil: time.Now().Add(4 * time.Second)}
		broker.Permission = "platform.runtime.kubernetes.frame.begin"
		credit, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, frame)
		if err != nil {
			t.Fatal("admit prepared cleanup source", err)
		}
		gateway.Permission = "platform.runtime.kubernetes.output.accept"
		receipt, err := service.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: stream, Sequence: sequence, DeliveryOrdinal: credit.DeliveryOrdinal}, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, Payload: payload})
		if err != nil || receipt.State != enum.KubernetesOutputAccepted {
			t.Fatal("stage prepared cleanup source", err)
		}
		if ack {
			broker.Permission = "platform.runtime.kubernetes.frame.complete"
			if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: sequence, PayloadDigest: frame.PayloadDigest}); err != nil {
				t.Fatal("ack prepared cleanup source", err)
			}
		}
		return frame
	}
	stageOutput(1, "prepared prefix", true)
	late := stageOutput(2, " retained tail", false)
	gateway.Permission = "platform.runtime.kubernetes.cancel"
	if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
		t.Fatal(err)
	}
	config := repository.pool.Config().Copy()
	config.ConnConfig.User = "artifact_retention_runtime_g1"
	config.ConnConfig.Password = ""
	consumer, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	publisher := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "artifact-retention", Operation: "platform.artifacts.kubernetes-capture.finalize"}, "artifact-retention")
	// claimCapture reads the real task producer with an independent retention database identity.
	// claimCapture читает real task producer с independent retention database identity.
	claimCapture := func() value.ManagedKubernetesCaptureTaskBinding {
		var ref, lease, fence, digest string
		var generation int64
		var expires time.Time
		if err := consumer.QueryRow(ctx, kubernetesCaptureRepairClaimSQL, "prepared-cleanup-publisher").Scan(&ref, &lease, &fence, &generation, &digest, &expires); err != nil || !expires.After(time.Now()) {
			t.Fatal("claim prepared cleanup publication", err)
		}
		return value.ManagedKubernetesCaptureTaskBinding{CaptureRef: ref, LeaseRef: lease, Fence: fence, Generation: generation, SnapshotDigest: digest, WorkloadInstance: "prepared-cleanup-publisher"}
	}
	first := claimCapture()
	original := repository.objects
	lost := &kubernetesCaptureLostPutStore{Store: original}
	repository.objects = lost
	defer func() { repository.objects = original }()
	if err := exerciseKubernetesPreparedWriterAgainstTrash(t,ctx,repository,actor,first.CaptureRef,lost,func(publishContext context.Context)error{
		_,err:=service.FinalizeManagedKubernetesCapture(publishContext,publisher,first);return err
	}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("retain uncertain prepared intent", err)
	}
	exerciseKubernetesProjectUnknownBoundary(t, ctx, repository, service, actor, first.CaptureRef)
	if tag, err := repository.pool.Exec(ctx, kubernetesCaptureRepairAbortSQL, first.CaptureRef, first.LeaseRef, first.Generation); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("close failed prepared publication attempt", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: late.Sequence, PayloadDigest: late.PayloadDigest}); err != nil {
		t.Fatal("retain successor source after prepared uncertainty", err)
	}
	var kind, id, key, version, etag, digest string
	var size, generation int64
	var uncertain bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, "capture-component-cleanup").Scan(&kind, &id, &key, &version, &etag, &digest, &size, &generation, &uncertain); err != nil || kind != "KUBERNETES_PREPARED" || !uncertain || version != "" || etag != "" {
		t.Fatal("claim only superseded uncertain prepared content", err)
	}
	generation = exerciseKubernetesStagingCleanup(t, ctx, repository, service, consumer, actor, first.CaptureRef, id, kind, version, etag, generation, 3)
	head, err := repository.objects.Head(ctx, key, "")
	if err != nil || head.Key != key || head.Digest != digest || head.SizeBytes != size || head.VersionID == "" || head.ETag == "" {
		t.Fatal("reconcile exact unknown prepared provider version", err)
	}
	var recorded bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupPreparedRecordSQL, id, "capture-component-cleanup", generation, key, head.VersionID, head.ETag, digest, size).Scan(&recorded); err != nil || !recorded {
		t.Fatal("record exact prepared cleanup version", err)
	}
	if err := repository.objects.Delete(ctx, key, head.VersionID); err != nil {
		t.Fatal("delete exact superseded prepared version", err)
	}
	if _, err := repository.objects.Head(ctx, key, head.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("prepared deletion lacks exact absence", err)
	}
	if err := consumer.QueryRow(ctx, kubernetesCleanupFinishSQL, id, kind, "capture-component-cleanup", generation, head.VersionID, head.ETag, true).Scan(&recorded); err != nil || !recorded {
		t.Fatal("finish prepared cleanup after repair", err)
	}
	var cycle, total, repair int64
	var storedVersion, storedETag string
	var purged bool
	if err := repository.pool.QueryRow(ctx, kubernetesCleanupCountersSQL, id, kind).Scan(&cycle, &total, &repair, &storedVersion, &storedETag, &purged); err != nil || cycle != 1 || total != 4 || repair != 1 || !purged {
		t.Fatal("prepared cleanup lost total attempts or receipt", err)
	}
	actor.Permission = "platform.integrations.kubernetes.capture.get"
	status, err := service.GetKubernetesCaptureStatus(ctx, actor, query.KubernetesCaptureStatusRead{CaptureRef: first.CaptureRef})
	if err != nil || status.View.State != enum.KubernetesCaptureSuperseded || status.View.Cleanup == nil || status.View.Cleanup.OutstandingCount != 2 || status.View.Cleanup.ExhaustedCount != 0 {
		t.Fatal("prepared quota did not recover without reopening old capture", err)
	}
	next := claimCapture()
	if next.CaptureRef == first.CaptureRef {
		t.Fatal("superseded prepared intent reopened")
	}
	published, err := service.FinalizeManagedKubernetesCapture(ctx, publisher, next)
	if err != nil || published.State != enum.KubernetesCaptureComplete || len(published.Artifacts) != 1 || lost.puts.Load() != 2 {
		t.Fatal("cleanup poisoned successor or replayed old PUT", err)
	}
	actor.Permission = "platform.command.artifacts.download"
	a := published.Artifacts[0]
	download, err := service.DownloadArtifactRevision(ctx, actor, a.ArtifactRef, a.RevisionRef, "DOWNLOAD")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.LimitReader(download.Reader, 64))
	closeErr := download.Reader.Close()
	defer clear(body)
	if err != nil || closeErr != nil || string(body) != "prepared prefix retained tail" {
		t.Fatal("prepared cleanup lost retained useful history", err)
	}
	// Leave only durable receipts, so the next component's platform-wide cleanup claim is isolated.
	// Оставляются только durable receipts, чтобы platform-wide cleanup claim следующего case был изолирован.
	for item := 0; item < 2; item++ {
		if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, "capture-component-cleanup").Scan(&kind, &id, &key, &version, &etag, &digest, &size, &generation, &uncertain); err != nil || kind != "KUBERNETES_OUTPUT" || uncertain {
			t.Fatal("claim retained published prepared-test stage", err)
		}
		head, err := repository.objects.Head(ctx, key, version)
		if err != nil || head.ETag != etag || head.Digest != digest || head.SizeBytes != size {
			t.Fatal("verify prepared-test output cleanup receipt", err)
		}
		if err := repository.objects.Delete(ctx, key, version); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.objects.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
			t.Fatal("prepared-test output version survived deletion", err)
		}
		if err := consumer.QueryRow(ctx, kubernetesCleanupFinishSQL, id, kind, "capture-component-cleanup", generation, version, etag, true).Scan(&recorded); err != nil || !recorded {
			t.Fatal("finish prepared-test stage cleanup", err)
		}
	}

}
