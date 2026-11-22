package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertKubernetesFrameOwnerAccounting uses one actual current outer claim and metadata-only
// stream fixture to prove durable input/output credit accounting. It does not prove handshake,
// owner child preparation, runtime payload delivery or a selected Kubernetes resource permission.
// assertKubernetesFrameOwnerAccounting использует actual current outer claim и metadata-only
// stream fixture для durable input/output credit accounting. Не доказывает handshake, owner child
// preparation, runtime payload delivery либо selected Kubernetes resource permission.
func assertKubernetesFrameOwnerAccounting(t *testing.T, ctx context.Context, repository *Repository, broker value.Principal, binding value.ManagedKubernetesBinding, streamRef string) {
	t.Helper()
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	originalStore := repository.objects
	storage := &kubernetesOutputStorageFixture{Store: originalStore}
	repository.objects = storage
	defer func() { repository.objects = originalStore }()
	sum := sha256.Sum256([]byte("body"))
	output := command.ManagedKubernetesBeginFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameOutput, NativeAuthorizedUntil: time.Now().Add(4 * time.Second), Sequence: 1, Channel: "STDOUT", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: 4}
	missing := output
	missing.NativeAuthorizedUntil = time.Time{}
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, missing); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("OUTPUT without native witness was admitted", err)
	}
	stale := output
	stale.NativeAuthorizedUntil = time.Now().Add(-time.Second)
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, stale); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("expired native OUTPUT witness was admitted", err)
	}
	for delivery := 0; delivery < 4; delivery++ {
		output.NativeAuthorizedUntil = time.Now().Add(4 * time.Second)
		admission, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, output)
		if err != nil || admission.Disposition != enum.KubernetesFrameSend || admission.ExpiresAt.IsZero() || admission.DeliveryOrdinal != uint32(delivery+1) || admission.ExpiresAt.After(output.NativeAuthorizedUntil) || admission.ExpiresAt.Nanosecond()%1000 != 0 {
			t.Fatal("fresh retained OUTPUT delivery was not credited", delivery, err)
		}
		var persistedDeadline time.Time
		var persistedOrdinal uint32
		if err := repository.pool.QueryRow(ctx, `SELECT grant_expires_at,delivery_credits FROM control_plane.kubernetes_execution_frames frame JOIN control_plane.kubernetes_execution_streams stream ON stream.id=frame.stream_id WHERE stream.ref=$1 AND frame.direction='OUTPUT' AND frame.sequence=1`, streamRef).Scan(&persistedDeadline, &persistedOrdinal); err != nil || !persistedDeadline.Equal(admission.ExpiresAt) || persistedOrdinal != admission.DeliveryOrdinal {
			t.Fatal("OUTPUT receipt differs from persisted ordinal credit", err)
		}
		assertKubernetesOutputStageDelivery(t, ctx, repository, storage, binding, output, admission)
		if delivery == 0 {
			concurrent := assertKubernetesOutputOrdinalOverlap(t, ctx, repository, broker, binding, output)
			assertKubernetesOutputStageDelivery(t, ctx, repository, storage, binding, output, concurrent)
			delivery++
		}
	}
	capped, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, output)
	if err != nil || capped.Disposition != enum.KubernetesFrameUnknown || capped.DeliveryOrdinal != 0 || !capped.ExpiresAt.IsZero() {
		t.Fatal("OUTPUT exceeded four delivery credits", err)
	}
	changed := output
	changed.PayloadDigest = strings.Repeat("d", 64)
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, changed); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("OUTPUT replay changed immutable payload digest", err)
	}
	next := output
	next.Sequence = 2
	next.ByteCount = 32768
	nextSum := sha256.Sum256([]byte(strings.Repeat("x", 32768)))
	next.PayloadDigest = hex.EncodeToString(nextSum[:])
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, next); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("second OUTPUT bypassed pending single buffer", err)
	}
	ack := command.ManagedKubernetesCompleteFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameOutput, Sequence: 1, PayloadDigest: output.PayloadDigest}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	for repeat := 0; repeat < 2; repeat++ {
		if sequence, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, ack); err != nil || sequence != 1 {
			t.Fatal("exact output ACK receipt was not idempotent", err)
		}
	}
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	acknowledged, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, output)
	if err != nil || acknowledged.Disposition != enum.KubernetesFrameAcknowledged {
		t.Fatal("ACK output was redelivered", err)
	}
	if admission, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, next); err != nil || admission.Disposition != enum.KubernetesFrameSend {
		t.Fatal("next OUTPUT lost fresh credit", err)
	}
	assertKubernetesOutputUnknownWrite(t, ctx, repository, storage, binding, next)
	input := command.ManagedKubernetesBeginFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameInput, Sequence: 1, Channel: "STDIN", PayloadDigest: strings.Repeat("a", 64), ByteCount: 4}
	installKubernetesMetadataInputStage(t, ctx, repository, binding, input)
	if admission, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, input); err != nil || admission.Disposition != enum.KubernetesFrameSend || admission.DeliveryOrdinal != 0 {
		t.Fatal("first INPUT was not admitted", err)
	}
	uncertain, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, input)
	if err != nil || uncertain.Disposition != enum.KubernetesFrameUnknown {
		t.Fatal("uncertain INPUT was authorized for resend", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	inputACK := command.ManagedKubernetesCompleteFrame{StreamRef: streamRef, Direction: enum.KubernetesFrameInput, Sequence: 1, PayloadDigest: input.PayloadDigest}
	if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, inputACK); err != nil {
		t.Fatal("record exact INPUT ACK", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	eof := input
	eof.Sequence = 2
	eof.ByteCount = 0
	eof.EndOfInput = true
	eof.PayloadDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	installKubernetesMetadataInputStage(t, ctx, repository, binding, eof)
	if admission, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, eof); err != nil || admission.Disposition != enum.KubernetesFrameSend {
		t.Fatal("STDIN half-close was not admitted", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.complete"
	inputACK.Sequence = 2
	inputACK.PayloadDigest = eof.PayloadDigest
	if _, err := repository.CompleteManagedKubernetesFrame(ctx, broker, binding, inputACK); err != nil {
		t.Fatal("record STDIN half-close ACK", err)
	}
	broker.Permission = "platform.runtime.kubernetes.frame.begin"
	input.Sequence = 3
	if _, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, input); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("closed STDIN gained fresh input credit", err)
	}
	input.Channel = "TERMINAL_RESIZE"
	input.ByteCount = 8
	installKubernetesMetadataInputStage(t, ctx, repository, binding, input)
	if admission, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, input); err != nil || admission.Disposition != enum.KubernetesFrameSend {
		t.Fatal("STDIN half-close incorrectly closed terminal resize", err)
	}
	if admission, err := repository.BeginManagedKubernetesFrame(ctx, broker, binding, next); err != nil || admission.Disposition != enum.KubernetesFrameUnknown {
		t.Fatal("OUTPUT replay exceeded actual byte budget", err)
	}
	var logical, actual uint64
	var deliveries int
	if err := repository.pool.QueryRow(ctx, `SELECT stream.logical_bytes,stream.delivered_bytes,frame.delivery_credits FROM control_plane.kubernetes_execution_streams stream JOIN control_plane.kubernetes_execution_frames frame ON frame.stream_id=stream.id WHERE stream.ref=$1 AND frame.direction='OUTPUT' AND frame.sequence=1`, streamRef).Scan(&logical, &actual, &deliveries); err != nil || logical != 32784 || actual != 32796 || deliveries != 4 {
		t.Fatal("frame accounting lost logical/actual byte distinction", err)
	}
}

// installKubernetesMetadataInputStage supplies explicit synthetic source facts for the older metadata-only
// accounting fixture. It never loads private bytes and is not a public Submit/Claim or storage proof.
// installKubernetesMetadataInputStage создаёт explicit synthetic source facts старой metadata-only
// accounting fixture. Не читает private bytes и не доказывает public Submit/Claim либо storage.
func installKubernetesMetadataInputStage(t *testing.T, ctx context.Context, repository *Repository, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginFrame) {
	t.Helper()
	ref, err := newRef("kif")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := repository.pool.QueryRow(ctx, `INSERT INTO control_plane.kubernetes_input_stages(ref,organization_id,stream_id,actor_id,submission_key,intent_digest,sequence,channel,payload_digest,byte_count,end_of_input,state,object_key)
 SELECT $2,stream.organization_id,stream.id,pin.root_actor_id,$2,$6,$3,$4,$6,$5,$7,'ATTEMPTING','organizations/'||stream.organization_id::text||'/kubernetes-input-stages/'||$2
 FROM control_plane.kubernetes_execution_streams stream JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id
 JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id
 JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id
 WHERE stream.ref=$1 RETURNING id::text`, input.StreamRef, ref, input.Sequence, input.Channel, input.ByteCount, input.PayloadDigest, input.EndOfInput).Scan(&id); err != nil {
		t.Fatal("install metadata-only input source", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.kubernetes_input_stages SET state='QUEUED',object_version='synthetic-metadata-version',object_etag='synthetic-metadata-etag',recorded_at=clock_timestamp() WHERE id=$1::uuid`, id); err != nil {
		t.Fatal("record synthetic source metadata", err)
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.kubernetes_input_stages SET state='CLAIMED',delivery_expires_at=clock_timestamp()+interval '5 seconds',claimed_at=clock_timestamp() WHERE id=$1::uuid`, id); err != nil {
		t.Fatal("claim synthetic source metadata", err)
	}
}
