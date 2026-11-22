package kubernetesoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// frameOwnerFixture models durable sequence consumption independently from lost responses.
// frameOwnerFixture моделирует durable расход sequence независимо от потерянных ответов.
type frameOwnerFixture struct {
	admission             value.KubernetesFrameAdmission
	beginErr, completeErr error
	beginHook             func()
	completeHook          func(context.Context)
	begins, completes     int
	metadata              value.KubernetesFrameMetadata
}

// BeginKubernetesFrame retains only metadata and never automatically reissues send credit.
// BeginKubernetesFrame сохраняет только metadata и не перевыдаёт send credit автоматически.
func (owner *frameOwnerFixture) BeginKubernetesFrame(_ context.Context, _ managedkubernetes.Binding, metadata value.KubernetesFrameMetadata) (value.KubernetesFrameAdmission, error) {
	owner.begins++
	owner.metadata = metadata
	if owner.beginHook != nil {
		owner.beginHook()
	}
	admission := owner.admission
	if admission.Disposition == "SEND" {
		owner.admission.Disposition = "UNKNOWN"
	}
	return admission, owner.beginErr
}

// CompleteKubernetesFrame can lose its response after consuming credit, as a remote owner can.
// CompleteKubernetesFrame может потерять ответ после расхода credit, как удалённый owner.
func (owner *frameOwnerFixture) CompleteKubernetesFrame(ctx context.Context, _ managedkubernetes.Binding, metadata value.KubernetesFrameMetadata) (uint64, error) {
	owner.completes++
	if owner.completeHook != nil {
		owner.completeHook(ctx)
	}
	owner.admission.Disposition = "ACKNOWLEDGED"
	return metadata.Sequence, owner.completeErr
}

// frameWriterFixture observes owned bytes only during a synchronous joined write.
// frameWriterFixture наблюдает owned bytes только во время синхронного joined write.
type frameWriterFixture struct {
	writes int
	write  func(context.Context, value.KubernetesStreamFrame) error
}

// WriteFrame exposes one bounded transport call without implicit retries.
// WriteFrame предоставляет один bounded transport call без неявных retries.
func (writer *frameWriterFixture) WriteFrame(ctx context.Context, frame value.KubernetesStreamFrame) error {
	writer.writes++
	if writer.write != nil {
		return writer.write(ctx, frame)
	}
	return nil
}

// inputFrameFixture provides synthetic owner pins and a small binary input frame.
// inputFrameFixture задаёт synthetic owner pins и небольшой binary input frame.
func inputFrameFixture(t *testing.T) (*InputRelay, managedkubernetes.Binding, value.KubernetesStreamGrant, value.KubernetesStreamFrame, *frameOwnerFixture, *frameWriterFixture) {
	t.Helper()
	expires := time.Now().Add(time.Minute)
	binding := managedkubernetes.Binding{InvocationRef: "inv_synthetic", WorkloadInstance: "worker_synthetic", LeaseRef: "lease_synthetic", Fence: "fence_synthetic", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	grant := value.KubernetesStreamGrant{Ref: "stream_synthetic", Generation: 1, ExpiresAt: expires, MaximumFrameBytes: 1024, MaximumTotalBytes: 8192, InputChannels: []string{"STDIN"}, OutputChannels: []string{"STDOUT"}}
	payload := []byte("synthetic-input\x00")
	digest := sha256.Sum256(payload)
	frame := value.KubernetesStreamFrame{Sequence: 1, Channel: "STDIN", Payload: payload, PayloadDigest: hex.EncodeToString(digest[:])}
	owner := &frameOwnerFixture{admission: value.KubernetesFrameAdmission{Disposition: "SEND", ExpiresAt: expires}}
	relay, err := NewInputRelay(owner)
	if err != nil {
		t.Fatal(err)
	}
	return relay, binding, grant, frame, owner, &frameWriterFixture{}
}

// TestInputRelayLostCompletionDoesNotResend proves that a duplicate receipt never duplicates stdin.
// TestInputRelayLostCompletionDoesNotResend доказывает, что повтор receipt не повторяет stdin.
func TestInputRelayLostCompletionDoesNotResend(t *testing.T) {
	relay, binding, grant, frame, owner, writer := inputFrameFixture(t)
	owner.completeErr = errors.New("synthetic lost response")
	ack, attempted, err := relay.Send(t.Context(), binding, grant, frame, writer)
	if err == nil || ack != 0 || !attempted || writer.writes != 1 {
		t.Fatal("lost completion concealed uncertainty")
	}
	ack, attempted, err = relay.Send(t.Context(), binding, grant, frame, writer)
	if err != nil || ack != frame.Sequence || attempted || writer.writes != 1 || owner.completes != 1 {
		t.Fatal("duplicate completion resent input")
	}
}

// TestInputRelayPartialWriteCannotReceiveSecondCredit preserves an uncertain accepted input prefix.
// TestInputRelayPartialWriteCannotReceiveSecondCredit сохраняет uncertainty принятого input prefix.
func TestInputRelayPartialWriteCannotReceiveSecondCredit(t *testing.T) {
	relay, binding, grant, frame, owner, writer := inputFrameFixture(t)
	writer.write = func(context.Context, value.KubernetesStreamFrame) error {
		return errors.New("synthetic partial transport write")
	}
	ack, attempted, err := relay.Send(t.Context(), binding, grant, frame, writer)
	if err == nil || ack != 0 || !attempted || writer.writes != 1 || owner.completes != 0 {
		t.Fatal("partial write was acknowledged or lost uncertainty")
	}
	ack, attempted, err = relay.Send(t.Context(), binding, grant, frame, writer)
	if err == nil || ack != 0 || attempted || writer.writes != 1 || owner.completes != 0 {
		t.Fatal("uncertain input was replayed")
	}
}

// TestInputRelayUnknownAndRejectedCreditNeverWrites covers ambiguous admission and expired authority.
// TestInputRelayUnknownAndRejectedCreditNeverWrites проверяет ambiguous admission и истёкшие полномочия.
func TestInputRelayUnknownAndRejectedCreditNeverWrites(t *testing.T) {
	for _, scenario := range []string{"unknown", "malformed", "lost-begin", "expired", "extended", "cancelled", "output-ordinal"} {
		t.Run(scenario, func(t *testing.T) {
			relay, binding, grant, frame, owner, writer := inputFrameFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "output-ordinal":
				owner.admission.DeliveryOrdinal = 1
			case "unknown":
				owner.admission.Disposition = "UNKNOWN"
			case "malformed":
				owner.admission.Disposition = "UNRECOGNIZED"
			case "lost-begin":
				owner.beginErr = errors.New("synthetic lost response")
			case "expired":
				owner.admission.ExpiresAt = time.Now().Add(-time.Second)
			case "extended":
				owner.admission.ExpiresAt = grant.ExpiresAt.Add(time.Second)
			case "cancelled":
				owner.beginHook = cancel
			}
			ack, attempted, err := relay.Send(ctx, binding, grant, frame, writer)
			if err == nil || ack != 0 || attempted || writer.writes != 0 || owner.completes != 0 {
				t.Fatal("unproven send credit reached transport")
			}
		})
	}
}

// TestInputRelayCopiesBeforeAdmissionAndClearsAfterJoin prevents mutation during owner round trips.
// TestInputRelayCopiesBeforeAdmissionAndClearsAfterJoin исключает подмену bytes во время owner round trip.
func TestInputRelayCopiesBeforeAdmissionAndClearsAfterJoin(t *testing.T) {
	relay, binding, grant, frame, owner, writer := inputFrameFixture(t)
	original := bytes.Clone(frame.Payload)
	owner.beginHook = func() { clear(frame.Payload) }
	var borrowed []byte
	writer.write = func(ctx context.Context, observed value.KubernetesStreamFrame) error {
		if !bytes.Equal(observed.Payload, original) {
			t.Fatal("caller mutation changed admitted bytes")
		}
		if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(owner.admission.ExpiresAt) {
			t.Fatal("transport lacks owner deadline")
		}
		borrowed = observed.Payload
		return nil
	}
	if _, _, err := relay.Send(t.Context(), binding, grant, frame, writer); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(borrowed, make([]byte, len(original))) || owner.metadata.PayloadDigest != frame.PayloadDigest {
		t.Fatal("private bytes retained or digest changed")
	}
}

// TestInputRelayCancellationAfterWriteStillReportsEvidence separates writer lifetime from cleanup.
// TestInputRelayCancellationAfterWriteStillReportsEvidence отделяет writer lifetime от cleanup.
func TestInputRelayCancellationAfterWriteStillReportsEvidence(t *testing.T) {
	relay, binding, grant, frame, owner, writer := inputFrameFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer.write = func(context.Context, value.KubernetesStreamFrame) error { cancel(); return nil }
	owner.completeHook = func(ctx context.Context) {
		if ctx.Err() != nil {
			t.Fatal("evidence inherited caller cancellation")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("evidence has no bounded cleanup budget")
		}
	}
	ack, attempted, err := relay.Send(ctx, binding, grant, frame, writer)
	if err != nil || !attempted || ack != 1 || owner.completes != 1 {
		t.Fatal("joined write lost its evidence")
	}
}

// TestInputRelayInvalidInputNeverContactsOwner rejects tampering before sequence admission.
// TestInputRelayInvalidInputNeverContactsOwner отклоняет подмену до sequence admission.
func TestInputRelayInvalidInputNeverContactsOwner(t *testing.T) {
	for _, scenario := range []string{"digest", "channel", "size", "sequence", "generation", "expired-grant", "health-binding", "output-ordinal", "output-credit"} {
		t.Run(scenario, func(t *testing.T) {
			relay, binding, grant, frame, owner, writer := inputFrameFixture(t)
			switch scenario {
			case "output-ordinal":
				frame.DeliveryOrdinal = 1
			case "output-credit":
				frame.AuthorizedUntil = time.Now().Add(time.Second)
			case "digest":
				frame.Payload[0] ^= 1
			case "channel":
				frame.Channel = "STDOUT"
			case "size":
				frame.Payload = make([]byte, managedkubernetes.MaximumFrameBytes+1)
			case "sequence":
				frame.Sequence = 0
			case "generation":
				grant.Generation++
			case "expired-grant":
				grant.ExpiresAt = time.Now().Add(-time.Second)
			case "health-binding":
				binding.ConnectionTestRef, binding.InvocationRef = "health_synthetic", ""
			}
			_, _, err := relay.Send(t.Context(), binding, grant, frame, writer)
			if !errors.Is(err, targetrepo.ErrFrameUnavailable) || owner.begins != 0 || writer.writes != 0 {
				t.Fatal("invalid input reached owner or transport")
			}
		})
	}
}
