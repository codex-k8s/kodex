package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesCleanupClaimSQL calls the real retention runtime claim, without fabricating counters.
// kubernetesCleanupClaimSQL вызывает real retention runtime claim без подстановки counters.
//
//go:embed testdata/sql/kubernetes_cleanup_claim.sql
var kubernetesCleanupClaimSQL string

// kubernetesCleanupFinishSQL records a bounded exact cleanup outcome under its original fence.
// kubernetesCleanupFinishSQL сохраняет bounded exact cleanup outcome под original fence.
//
//go:embed testdata/sql/kubernetes_cleanup_finish.sql
var kubernetesCleanupFinishSQL string

// kubernetesCleanupCountersSQL observes retained immutable object pins and lifetime attempts.
// kubernetesCleanupCountersSQL читает retained immutable object pins и lifetime attempts.
//
//go:embed testdata/sql/kubernetes_cleanup_counters.sql
var kubernetesCleanupCountersSQL string

// exerciseKubernetesStagingCleanup exhausts three real claims, repairs through the public service,
// recovers the same immutable acknowledgement and rejects an old completion fence. The caller then
// deletes the repaired exact version and proves physical quota recovery in the existing fixture.
// exerciseKubernetesStagingCleanup исчерпывает три real claims, восстанавливает цикл public service,
// сверяет immutable acknowledgement и отклоняет старый completion fence. Затем existing fixture
// удаляет repaired exact version и доказывает physical quota recovery.
func exerciseKubernetesStagingCleanup(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, consumer *pgxpool.Pool, actor value.Principal, captureRef, stageID, kind, version, etag string, firstGeneration, expectedOutstanding int64) int64 {
	t.Helper()
	const worker = "capture-component-cleanup"
	generation := firstGeneration
	for attempt := int64(1); attempt <= 3; attempt++ {
		var done bool
		if err := consumer.QueryRow(ctx, kubernetesCleanupFinishSQL, stageID, kind, worker, generation, version, etag, false).Scan(&done); err != nil || !done {
			t.Fatal("record failed cleanup attempt", err)
		}
		var cycle, total, repair int64
		var storedVersion, storedETag string
		var purged bool
		if err := repository.pool.QueryRow(ctx, kubernetesCleanupCountersSQL, stageID, kind).Scan(&cycle, &total, &repair, &storedVersion, &storedETag, &purged); err != nil || cycle != attempt || total != attempt || repair != 0 || purged || storedVersion != version || storedETag != etag {
			t.Fatal("cleanup failure changed immutable history", err)
		}
		if attempt < 3 {
			var source, id, key, v, e, digest string
			var size int64
			var uncertain bool
			if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, worker).Scan(&source, &id, &key, &v, &e, &digest, &size, &generation, &uncertain); err != nil || id != stageID || source != kind || v != version || e != etag || uncertain != (version == "") {
				t.Fatal("claim next cleanup cycle attempt", err)
			}
		}
	}
	var ignored string
	if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, worker).Scan(&ignored); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("exhausted cleanup remained claimable", err)
	}
	actor.Permission = "platform.integrations.kubernetes.capture.get"
	before, err := service.GetKubernetesCaptureStatus(ctx, actor, query.KubernetesCaptureStatusRead{CaptureRef: captureRef})
	if err != nil || before.View.Cleanup == nil || before.View.Cleanup.EligibleExhaustedCount != 1 || before.View.Cleanup.ExhaustedCount != 1 || before.View.Cleanup.OutstandingCount != expectedOutstanding {
		t.Fatal("exhausted cleanup is undiscoverable", err)
	}
	original := before.View.Cleanup
	key, err := newRef("cleanup-repair")
	if err != nil {
		t.Fatal(err)
	}
	actor.Permission = ""
	for operation, method := range controlplaneclient.ControlAPIGatewayOperations() {
		if method == cp.ManagedKubernetesCaptureService_RepairManagedKubernetesStagingCleanup_FullMethodName {
			actor.Permission = operation
		}
	}
	if actor.Permission == "" {
		t.Fatal("cleanup RPC is missing from the public producer registry")
	}
	mutation := value.Mutation{IdempotencyKey: key, ExpectedVersion: &original.Version}
	result, err := service.RepairKubernetesStagingCleanup(ctx, actor, mutation, command.KubernetesCaptureRepair{CaptureRef: captureRef})
	if err != nil || result.CleanupReceipt == nil || result.CleanupReceipt.RepairedCount != 1 || result.CleanupReceipt.RemainingExhaustedCount != 0 || result.View.Cleanup == nil || result.View.Cleanup.Version != original.Version+1 || result.View.Cleanup.OutstandingCount != original.OutstandingCount || result.View.Cleanup.OutstandingBytes != original.OutstandingBytes {
		t.Fatal("repair invented deletion or failed exact exhausted cycle", err)
	}
	replay, err := service.RepairKubernetesStagingCleanup(ctx, actor, mutation, command.KubernetesCaptureRepair{CaptureRef: captureRef})
	if err != nil || replay.CleanupReceipt == nil || *replay.CleanupReceipt != *result.CleanupReceipt {
		t.Fatal("cleanup repair replay changed immutable acknowledgement", err)
	}
	mutation.ExpectedVersion = &result.View.Cleanup.Version
	if _, err := service.RepairKubernetesStagingCleanup(ctx, actor, mutation, command.KubernetesCaptureRepair{CaptureRef: captureRef}); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("cleanup repair key accepted a changed intent", err)
	}
	actor.Permission = "platform.integrations.kubernetes.capture.get"
	readback, err := service.GetKubernetesCaptureStatus(ctx, actor, query.KubernetesCaptureStatusRead{CaptureRef: captureRef, CleanupIdempotencyKey: key})
	if err != nil || readback.CleanupReceipt == nil || *readback.CleanupReceipt != *result.CleanupReceipt {
		t.Fatal("lost cleanup ACK has no original-key readback", err)
	}
	var stale bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupFinishSQL, stageID, kind, worker, generation, version, etag, true).Scan(&stale); err != nil || stale {
		t.Fatal("repair admitted an old deletion fence", err)
	}
	var source, id, objectKey, v, e, digest string
	var size int64
	var uncertain bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, worker).Scan(&source, &id, &objectKey, &v, &e, &digest, &size, &generation, &uncertain); err != nil || id != stageID || source != kind || v != version || e != etag || uncertain != (version == "") {
		t.Fatal("claim repaired exact immutable stage", err)
	}
	var cycle, total, repair int64
	var storedVersion, storedETag string
	var purged bool
	if err := repository.pool.QueryRow(ctx, kubernetesCleanupCountersSQL, stageID, kind).Scan(&cycle, &total, &repair, &storedVersion, &storedETag, &purged); err != nil || cycle != 1 || total != 4 || repair != 1 || purged || storedVersion != version || storedETag != etag {
		t.Fatal("repair reset total history or released quota early", err)
	}
	return generation
}

// stageKubernetesCleanupInput crosses actual Submit/Claim/Begin/Complete before cancellation.
// The returned metadata is not a new input credit; payload ownership is cleared on every path.
// stageKubernetesCleanupInput проходит actual Submit/Claim/Begin/Complete до cancellation.
// Возвращённые metadata не дают input credit; owned payload очищается на каждом пути.
func stageKubernetesCleanupInput(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, actor, broker, gateway value.Principal, binding value.ManagedKubernetesBinding, stream string) {
	t.Helper()
	actor.Permission = "platform.integrations.kubernetes.input.get"
	view, err := service.GetKubernetesStreamInput(ctx, actor, query.KubernetesStreamInputRead{InvocationRef: binding.InvocationRef, StreamRef: stream})
	if err != nil {
		t.Fatal("read cleanup input owner", err)
	}
	body := []byte("cleanup-owned-input")
	defer clear(body)
	digest := sha256.Sum256(body)
	actor.Permission = "platform.integrations.kubernetes.input.submit"
	queued, err := service.SubmitKubernetesStreamInput(ctx, actor, value.Mutation{IdempotencyKey: "cleanup-input-once", ExpectedVersion: &view.View.Version}, command.KubernetesStreamInputSubmit{InvocationRef: binding.InvocationRef, StreamRef: stream, Channel: "STDIN", Payload: body, PayloadDigest: hex.EncodeToString(digest[:])})
	if err != nil || queued.Submission == nil {
		t.Fatal("reserve cleanup input source", err)
	}
	gateway.Permission = "platform.runtime.kubernetes.input.claim"
	delivery, err := service.ClaimManagedKubernetesInputFrame(ctx, gateway, binding, stream)
	if err != nil || delivery.Frame == nil || delivery.Disposition != enum.KubernetesInputFrameDeliver {
		t.Fatal("claim cleanup input source", err)
	}
	defer clear(delivery.Frame.Payload)
	frame := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: delivery.Frame.Sequence, Channel: delivery.Frame.Channel, PayloadDigest: delivery.Frame.PayloadDigest, ByteCount: delivery.Frame.ByteCount}
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, frame); err != nil {
		t.Fatal("begin cleanup input source", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameInput, Sequence: frame.Sequence, PayloadDigest: frame.PayloadDigest}); err != nil {
		t.Fatal("ack cleanup input source", err)
	}
}

// cleanupKubernetesInputBeforeCaptureRepair removes only the exhausted acknowledged INPUT object.
// Public repair preserves the original invocation/capture states and quota until exact deletion.
// cleanupKubernetesInputBeforeCaptureRepair удаляет только exhausted acknowledged INPUT object.
// Public repair сохраняет invocation/capture states и quota до exact deletion.
func cleanupKubernetesInputBeforeCaptureRepair(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, consumer *pgxpool.Pool, actor value.Principal, captureRef string) {
	t.Helper()
	var kind, id, key, version, etag, digest string
	var size, generation int64
	var uncertain bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, "capture-component-cleanup").Scan(&kind, &id, &key, &version, &etag, &digest, &size, &generation, &uncertain); err != nil || kind != "KUBERNETES_INPUT" || uncertain {
		t.Fatal("claim acknowledged cleanup input", err)
	}
	generation = exerciseKubernetesStagingCleanup(t, ctx, repository, service, consumer, actor, captureRef, id, kind, version, etag, generation, 3)
	head, err := repository.objects.Head(ctx, key, version)
	if err != nil || head.ETag != etag || head.Digest != digest || head.SizeBytes != size {
		t.Fatal("cleanup input changed exact receipt", err)
	}
	if err := repository.objects.Delete(ctx, key, version); err != nil {
		t.Fatal("delete exact input version", err)
	}
	if _, err := repository.objects.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("input deletion lacks exact absence", err)
	}
	var done bool
	if err := consumer.QueryRow(ctx, kubernetesCleanupFinishSQL, id, kind, "capture-component-cleanup", generation, version, etag, true).Scan(&done); err != nil || !done {
		t.Fatal("record exact input deletion", err)
	}
	var cycle, total, repair int64
	var storedVersion, storedETag string
	var purged bool
	if err := repository.pool.QueryRow(ctx, kubernetesCleanupCountersSQL, id, kind).Scan(&cycle, &total, &repair, &storedVersion, &storedETag, &purged); err != nil || !purged || total != 4 || cycle != 1 || repair != 1 || storedVersion != version || storedETag != etag {
		t.Fatal("input quota or immutable cleanup receipt was lost", err)
	}
}
