package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
)

// TestKubernetesCaptureSealOwnerComponent seals an owner-ACK prefix on cancellation without
// inventing native join, then retains an exact late ACK as a new immutable capture generation.
// The actual artifact-retention identity publishes the first prefix, cleans its private stage,
// then reconstructs the late-ACK successor from the exact canonical prior revision plus new tail.
// Live native transport, deployed S3 and signed RPC authority remain separate checks.
// TestKubernetesCaptureSealOwnerComponent фиксирует owner-ACK prefix при cancel без выдуманного
// native join, затем сохраняет exact late ACK новой immutable capture generation. Actual database
// identity artifact-retention публикует first prefix, очищает private stage, затем собирает late-ACK
// successor из exact canonical prior revision и нового tail. Live native transport, deployed S3
// и signed RPC authority проверяются отдельно.
func TestKubernetesCaptureSealOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Second)
	defer cancel()
	repository, service, broker, gateway, binding, _, admission, _, actor := newKubernetesInvocationStreamResultFixtureWithActor(t, ctx)
	// Only this fixture opts into a complete immutable version map for qualified lost-ACK recovery.
	// Только этот fixture использует полную immutable version map для qualified lost-ACK recovery.
	repository.objects = objectstoragetest.NewVersioned()
	spoolDirectory, err := repository.ConfigureKubernetesCaptureSpool(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer spoolDirectory.Close()
	// assertSpool checks useful bytes through the bounded reader rather than a transcript slice.
	// assertSpool проверяет useful bytes через bounded reader, не transcript slice.
	assertSpool := func(spool *kubernetesCaptureSpool, expected string) bool {
		t.Helper()
		if spool == nil {
			return false
		}
		reader := &kubernetesCaptureSpoolReader{spool: spool}
		if _, err := reader.Seek(0, io.SeekStart); err != nil {
			return false
		}
		body, err := io.ReadAll(io.LimitReader(reader, int64(len(expected)+1)))
		defer clear(body)
		_, rewindErr := reader.Seek(0, io.SeekStart)
		return err == nil && rewindErr == nil && string(body) == expected
	}
	stream := admission.Stream.Ref
	stageKubernetesCleanupInput(t, ctx, repository, service, actor, broker, gateway, binding, stream)
	// stageOutput crosses actual frame credit and private versioned object acceptance once.
	// stageOutput проходит actual frame credit и private versioned object acceptance один раз.
	stageOutput := func(sequence uint64, body string, ack bool) command.ManagedKubernetesBeginFrame {
		payload := []byte(body)
		sum := sha256.Sum256(payload)
		frame := command.ManagedKubernetesBeginFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: sequence, Channel: "STDOUT", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: uint32(len(payload)), NativeAuthorizedUntil: time.Now().Add(4 * time.Second)}
		broker.Permission = "platform.runtime.kubernetes.frame.begin"
		credit, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, frame)
		if err != nil {
			t.Fatal("admit capture source frame", err)
		}
		gateway.Permission = "platform.runtime.kubernetes.output.accept"
		receipt, err := service.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: stream, Sequence: sequence, DeliveryOrdinal: credit.DeliveryOrdinal}, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, Payload: payload})
		if err != nil || receipt.State != enum.KubernetesOutputAccepted {
			t.Fatal("stage capture source frame", err)
		}
		if ack {
			broker.Permission = "platform.runtime.kubernetes.frame.complete"
			if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: sequence, PayloadDigest: frame.PayloadDigest}); err != nil {
				t.Fatal("ack capture source frame", err)
			}
		}
		return frame
	}
	stageOutput(1, "confirmed prefix", true)
	late := stageOutput(2, " confirmed late tail", false)
	gateway.Permission = "platform.runtime.kubernetes.cancel"
	if _, err := service.CancelManagedKubernetesInvocation(ctx, gateway, binding); err != nil {
		t.Fatal("close capture owner graph", err)
	}
	// readSeal observes only owner-assigned immutable metadata, never private staged bytes.
	// readSeal читает только owner-assigned immutable metadata, не private staged bytes.
	readSeal := func(generation int64) (string, string, kubernetesCaptureSource) {
		var ref, raw, digest string
		err := repository.pool.QueryRow(ctx, `SELECT capture.ref,capture.source_snapshot,capture.snapshot_digest FROM control_plane.kubernetes_output_captures capture JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id WHERE stream.ref=$1 AND capture.capture_generation=$2`, stream, generation).Scan(&ref, &raw, &digest)
		if err != nil {
			t.Fatal("read sealed capture source", err)
		}
		sum := sha256.Sum256([]byte(raw))
		var source kubernetesCaptureSource
		if digest != hex.EncodeToString(sum[:]) || internalrpcauth.DecodeStrictJSON([]byte(raw), &source) != nil || strings.Contains(raw, binding.Fence) {
			t.Fatal("capture source commitment is invalid")
		}
		return ref, digest, source
	}
	firstRef, firstDigest, first := readSeal(1)
	if first.AcknowledgedWatermark != 1 || first.OutputWatermark != 2 || first.TransportJoined || first.NativeTerminalKnown || first.OwnerState != "LOST" {
		t.Fatal("capture invented native completion")
	}
	// The collector reads only the frozen ACK prefix; staged-but-unacknowledged bytes stay private.
	// Collector читает только frozen ACK prefix; staged-but-unacknowledged bytes остаются private.
	var firstID, organizationID, streamID string
	if err := repository.pool.QueryRow(ctx, `SELECT id::text,organization_id::text,stream_id::text FROM control_plane.kubernetes_output_captures WHERE ref=$1`, firstRef).Scan(&firstID, &organizationID, &streamID); err != nil {
		t.Fatal("read capture test origin", err)
	}
	firstAssembly, err := repository.assembleKubernetesCapture(ctx, kubernetesCaptureTaskRecord{id: firstID, organizationID: organizationID, streamID: streamID, source: first, receipt: entity.ManagedKubernetesCaptureReceipt{SnapshotDigest: firstDigest}})
	if err != nil || firstAssembly.Prefix != 1 || firstAssembly.Kind != enum.KubernetesCapturePartial || !assertSpool(firstAssembly.Channels["STDOUT"], "confirmed prefix") {
		t.Fatal("sealed capture disclosed an unacknowledged tail", err)
	}
	firstAssembly.clear()
	config := repository.pool.Config().Copy()
	config.ConnConfig.User = "artifact_retention_runtime_g1"
	config.ConnConfig.Password = ""
	consumer, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open disposable artifact consumer", err)
	}
	defer consumer.Close()
	if err := repository.ConfigureArtifactAntivirusMode("disabled"); err != nil {
		t.Fatal(err)
	}
	publisher := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "artifact-retention", Operation: "platform.artifacts.kubernetes-capture.finalize"}, "artifact-retention")
	cleanupKubernetesInputBeforeCaptureRepair(t, ctx, repository, service, consumer, actor, firstRef)
	originalStore := repository.objects
	lostPut := &kubernetesCaptureLostPutStore{Store: originalStore}
	repository.objects = lostPut
	defer func() { repository.objects = originalStore }()
	repairKey := exerciseKubernetesCaptureRepair(t, ctx, repository, service, consumer, actor, publisher, firstRef, firstDigest, first)
	if lostPut.puts.Load() != 1 {
		t.Fatal("unknown publication wrote more than once before repair")
	}

	// claimCapture uses only the independently authenticated retention database identity.
	// claimCapture использует только independently authenticated retention database identity.
	claimCapture := func(expectedRef, expectedSnapshot string, expectedGeneration int64) value.ManagedKubernetesCaptureTaskBinding {
		t.Helper()
		var captureRef, leaseRef, fence, snapshot string
		var generation int64
		var expires time.Time
		err := consumer.QueryRow(ctx, `SELECT capture_ref,lease_ref,fence,generation,snapshot_digest,expires_at FROM control_plane.kubernetes_capture_claim($1,1,60)`, "capture-component-worker").Scan(&captureRef, &leaseRef, &fence, &generation, &snapshot, &expires)
		if err != nil || captureRef != expectedRef || snapshot != expectedSnapshot || generation != expectedGeneration || leaseRef == "" || len(fence) < 32 || !expires.After(time.Now()) {
			t.Fatal("claim exact capture source", err)
		}
		return value.ManagedKubernetesCaptureTaskBinding{CaptureRef: captureRef, LeaseRef: leaseRef, Fence: fence, Generation: generation, WorkloadInstance: "capture-component-worker", SnapshotDigest: snapshot}
	}
	firstTask := claimCapture(firstRef, firstDigest, 4)
	firstPublished, err := service.FinalizeManagedKubernetesCapture(ctx, publisher, firstTask)
	if err != nil || firstPublished.Kind != enum.KubernetesCapturePartial || firstPublished.State != enum.KubernetesCaptureComplete || len(firstPublished.Artifacts) != 1 {
		t.Fatal("publish first confirmed prefix", err)
	}
	if lostPut.puts.Load() != 1 {
		t.Fatal("repair repeated an unknown prepared-content PUT")
	}
	firstArtifact := firstPublished.Artifacts[0]
	actor.Permission = "platform.integrations.kubernetes.capture.get"
	repairedStatus, err := service.GetKubernetesCaptureStatus(ctx, actor, query.KubernetesCaptureStatusRead{CaptureRef: firstRef, RepairIdempotencyKey: repairKey})
	if err != nil || repairedStatus.View.State != enum.KubernetesCaptureComplete || repairedStatus.View.AttemptCount != 4 || repairedStatus.RepairReceipt == nil || repairedStatus.RepairReceipt.AttemptCount != 3 {
		t.Fatal("publication lost immutable repair history", err)
	}
	// Cleanup removes only the exact confirmed stage version after canonical adoption. Metadata
	// and the unacknowledged tail stay retained, so quota is physical rather than a lifetime count.
	// Cleanup удаляет exact confirmed stage version после canonical adoption. Metadata и
	// unacknowledged tail сохраняются: quota считает physical storage, не lifetime count.
	var kind, stageID, key, version, etag, digest string
	var size, cleanupGeneration int64
	var uncertain bool
	err = consumer.QueryRow(ctx, `SELECT source_kind,ledger_id::text,object_key,object_version,object_etag,digest,size_bytes,generation,uncertain FROM control_plane.kubernetes_stage_cleanup_claim($1,1,60)`, "capture-component-cleanup").Scan(&kind, &stageID, &key, &version, &etag, &digest, &size, &cleanupGeneration, &uncertain)
	if err != nil || kind != "KUBERNETES_OUTPUT" || uncertain || size != int64(len("confirmed prefix")) {
		t.Fatal("claim only published confirmed output stage", err)
	}
	cleanupGeneration = exerciseKubernetesStagingCleanup(t, ctx, repository, service, consumer, actor, firstRef, stageID, kind, version, etag, cleanupGeneration, 2)
	stored, err := repository.objects.Head(ctx, key, version)
	if err != nil || stored.Key != key || stored.VersionID != version || stored.ETag != etag || stored.Digest != digest || stored.SizeBytes != size {
		t.Fatal("cleanup changed exact versioned receipt", err)
	}
	if err := repository.objects.Delete(ctx, key, version); err != nil {
		t.Fatal("delete confirmed physical stage", err)
	}
	if _, err := repository.objects.Head(ctx, key, version); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("exact stage deletion has no authoritative absence", err)
	}
	var cleaned bool
	if err := consumer.QueryRow(ctx, `SELECT control_plane.kubernetes_stage_cleanup_complete($1,$2,$3,$4,$5,$6,true)`, stageID, kind, "capture-component-cleanup", cleanupGeneration, version, etag).Scan(&cleaned); err != nil || !cleaned {
		t.Fatal("record exact stage cleanup", err)
	}
	var retainedStages, physicalStages int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE payload_purged_at IS NULL) FROM control_plane.kubernetes_output_stages WHERE stream_id=$1`, streamID).Scan(&retainedStages, &physicalStages); err != nil || retainedStages != 2 || physicalStages != 1 {
		t.Fatal("physical quota did not release bytes while retaining receipts", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	for replay := 0; replay < 2; replay++ {
		if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, command.ManagedKubernetesCompleteFrame{StreamRef: stream, Direction: enum.KubernetesFrameOutput, Sequence: late.Sequence, PayloadDigest: late.PayloadDigest}); err != nil {
			t.Fatal("retain late accepted output ACK", err)
		}
	}
	secondRef, secondDigest, second := readSeal(2)
	if second.AcknowledgedWatermark != 2 || second.OutputWatermark != 2 || second.PreviousCaptureRef != firstRef || second.TransportJoined || second.OwnerState != "LOST" || secondDigest == firstDigest {
		t.Fatal("late ACK did not create an exact successor capture")
	}
	_, retainedDigest, retained := readSeal(1)
	if retainedDigest != firstDigest || retained.AcknowledgedWatermark != 1 {
		t.Fatal("late ACK expanded a sealed prefix")
	}
	var count int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.kubernetes_output_captures capture JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id WHERE stream.ref=$1`, stream).Scan(&count); err != nil || count != 2 {
		t.Fatal("late ACK replay created duplicate capture", err)
	}
	task := claimCapture(secondRef, secondDigest, 1)
	record, err := readKubernetesCaptureTask(ctx, repository.pool, task)
	if err != nil {
		t.Fatal("read claimed capture commitment", err)
	}
	assembly, err := repository.assembleKubernetesCapture(ctx, record)
	if err != nil || assembly.Prefix != 2 || assembly.Kind != enum.KubernetesCapturePartial || !assertSpool(assembly.Channels["STDOUT"], "confirmed prefix confirmed late tail") {
		t.Fatal("late ACK has no exact reachable capture bytes", err)
	}
	assembly.clear()
	if err := consumer.QueryRow(ctx, `SELECT capture_ref FROM control_plane.kubernetes_capture_claim($1,1,60)`, "capture-component-other").Scan(&key); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("active capture task was claimed twice", err)
	}
	if _, err := consumer.Exec(ctx, `SELECT source_snapshot FROM control_plane.kubernetes_output_captures`); err == nil {
		t.Fatal("artifact consumer obtained direct source read authority")
	}

	published, err := service.FinalizeManagedKubernetesCapture(ctx, publisher, task)
	if err != nil || published.Kind != enum.KubernetesCapturePartial || published.State != enum.KubernetesCaptureComplete || len(published.Artifacts) != 1 {
		t.Fatal("publish retained partial capture", err)
	}
	artifact := published.Artifacts[0]
	if artifact.ArtifactRef != firstArtifact.ArtifactRef || artifact.RevisionRef == firstArtifact.RevisionRef {
		t.Fatal("late ACK replaced immutable history instead of appending a stable artifact revision")
	}
	actor.Permission = "platform.command.artifacts.download"
	oldDownload, err := service.DownloadArtifactRevision(ctx, actor, firstArtifact.ArtifactRef, firstArtifact.RevisionRef, "DOWNLOAD")
	if err != nil {
		t.Fatal("prior immutable capture revision was lost", err)
	}
	oldBody, err := io.ReadAll(oldDownload.Reader)
	_ = oldDownload.Reader.Close()
	if err != nil || string(oldBody) != "confirmed prefix" {
		t.Fatal("late ACK changed the prior artifact revision", err)
	}
	clear(oldBody)
	actor.Permission = "platform.query.artifacts.get"
	visible, err := service.GetArtifact(ctx, actor, artifact.ArtifactRef)
	if err != nil || visible.CaptureOrigin == nil || visible.CaptureOrigin.Kind != "PARTIAL" || visible.CaptureOrigin.NativeTerminalKnown || visible.CaptureOrigin.TransportJoined || visible.CaptureOrigin.AcknowledgedPrefix != 2 || visible.CaptureOrigin.CaptureGeneration != 2 || visible.ScanState != "NOT_SCANNED" {
		t.Fatal("captured artifact erased source/scan truth", err)
	}
	actor.Permission = "platform.command.artifacts.download"
	download, err := service.DownloadArtifactRevision(ctx, actor, artifact.ArtifactRef, artifact.RevisionRef, "DOWNLOAD")
	if err != nil {
		t.Fatal("download retained captured revision", err)
	}
	body, err := io.ReadAll(download.Reader)
	_ = download.Reader.Close()
	if err != nil || string(body) != "confirmed prefix confirmed late tail" {
		t.Fatal("captured revision bytes are not useful", err)
	}
	clear(body)
	// History pagination keeps both immutable capture generations without integration replay.
	// History pagination сохраняет обе immutable capture generations без integration replay.
	historyReader := actor
	historyReader.Permission = "platform.integrations.kubernetes.capture.list"
	firstPage, err := service.ListKubernetesCaptures(ctx, historyReader, query.KubernetesCaptureList{RunRef: second.RunRef, NodeRef: second.NodeRef, Page: query.Page{Size: 1}})
	if err != nil || len(firstPage.Items) != 1 || firstPage.Next != firstPage.Items[0].CaptureRef {
		t.Fatal("capture history first page lost its cursor", err)
	}
	lastPage, err := service.ListKubernetesCaptures(ctx, historyReader, query.KubernetesCaptureList{RunRef: second.RunRef, NodeRef: second.NodeRef, Page: query.Page{Size: 1, Token: firstPage.Next}})
	if err != nil || len(lastPage.Items) != 1 || lastPage.Next != "" || lastPage.Items[0].CaptureRef == firstPage.Items[0].CaptureRef {
		t.Fatal("capture history pagination replayed or lost a generation", err)
	}
	seenHistory := map[string]bool{firstPage.Items[0].CaptureRef: true, lastPage.Items[0].CaptureRef: true}
	if !seenHistory[firstRef] || !seenHistory[secondRef] {
		t.Fatal("capture history changed immutable source refs")
	}
	// Public project artifact permissions cannot transfer personal captured output to another user.
	// Public project artifact permissions не передают personal captured output другому user.
	identity := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000008741", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Capture project reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.artifacts.get"}
	if _, err := repository.ResolveProofAuthority(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbound capture reader was admitted", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, actor, query.Filter{Query: identity.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("register capture project reader", err)
	}
	receiptAccessBinding(t, ctx, service, actor, subjects[0].Ref, "capture-project-reader", []string{"project.view", "run.view", "artifact.view", "artifact.download"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: visible.ProjectRef})
	reader := resolvedTestPrincipal(t, ctx, repository, identity, "control-api-gateway")
	if _, err := service.GetArtifact(ctx, reader, artifact.ArtifactRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("project artifact.view exposed personal capture", err)
	}
	reader.Permission = "platform.integrations.kubernetes.capture.get"
	if _, err := service.GetKubernetesCaptureStatus(ctx, reader, query.KubernetesCaptureStatusRead{CaptureRef: secondRef}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("project reader obtained foreign capture repair metadata", err)
	}
	reader.Permission = "platform.integrations.kubernetes.capture.list"
	if page, err := service.ListKubernetesCaptures(ctx, reader, query.KubernetesCaptureList{RunRef: second.RunRef, NodeRef: second.NodeRef}); !errors.Is(err, errs.ErrNotFound) || len(page.Items) != 0 || page.Next != "" {
		t.Fatal("project reader discovered foreign capture metadata", err)
	}
	reader.Permission = "platform.query.artifacts.revisions.get"
	if _, err := service.GetArtifactRevision(ctx, reader, artifact.ArtifactRef, artifact.RevisionRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("project reader obtained personal capture revision", err)
	}
	reader.Permission = "platform.command.artifacts.download"
	if download, err := service.DownloadArtifactRevision(ctx, reader, artifact.ArtifactRef, artifact.RevisionRef, "DOWNLOAD"); !errors.Is(err, errs.ErrNotFound) {
		if download.Reader != nil {
			_ = download.Reader.Close()
		}
		t.Fatal("project reader obtained personal capture bytes", err)
	}
	publisher.Permission = "platform.artifacts.kubernetes-capture.get"
	readback, err := service.GetManagedKubernetesCapture(ctx, publisher, task)
	if err != nil || len(readback.Artifacts) != 1 || readback.Artifacts[0] != artifact {
		t.Fatal("lost publication ACK has no exact receipt", err)
	}
	exerciseKubernetesArtifactRestoreAndPurge(t, ctx, repository, service, consumer, actor, reader, artifact.ArtifactRef, artifact.RevisionRef)
}
