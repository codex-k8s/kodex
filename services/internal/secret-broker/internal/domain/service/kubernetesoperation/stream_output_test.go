package kubernetesoperation

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// outputOwnerFixture models repeated OUTPUT credit separately from the non-replayable INPUT fixture.
// outputOwnerFixture моделирует повтор OUTPUT credit отдельно от INPUT fixture без replay.
type outputOwnerFixture struct {
	frameOwnerFixture
	previous    value.KubernetesFrameMetadata
	ordinal     uint32
	nativeUntil time.Time
}

// BeginKubernetesFrame admits only the same output tuple until completion; it never reads a target.
// BeginKubernetesFrame допускает только тот же output tuple до completion и не читает target.
func (owner *outputOwnerFixture) BeginKubernetesFrame(ctx context.Context, binding managedkubernetes.Binding, metadata value.KubernetesFrameMetadata) (value.KubernetesFrameAdmission, error) {
	owner.begins++
	nativeUntil := metadata.NativeAuthorizedUntil
	owner.nativeUntil = nativeUntil
	metadata.NativeAuthorizedUntil = time.Time{}
	if !nativeUntil.After(time.Now()) {
		return value.KubernetesFrameAdmission{}, errors.New("synthetic missing native witness")
	}
	if metadata.Direction != "OUTPUT" || owner.previous.Sequence == metadata.Sequence && owner.previous != metadata {
		return value.KubernetesFrameAdmission{}, errors.New("synthetic output metadata conflict")
	}
	if owner.previous.Sequence != metadata.Sequence {
		owner.ordinal = 0
	}
	owner.ordinal++
	owner.previous = metadata
	if owner.beginHook != nil {
		owner.beginHook()
	}
	admission := owner.admission
	admission.DeliveryOrdinal = owner.ordinal
	return admission, owner.beginErr
}

// outputWindowFixture creates only local bounded state with synthetic owner pins.
// outputWindowFixture создаёт только local bounded state с synthetic owner pins.
func outputWindowFixture(t *testing.T) (*OutputWindow, *outputOwnerFixture) {
	t.Helper()
	_, binding, grant, _, base, _ := inputFrameFixture(t)
	owner := &outputOwnerFixture{frameOwnerFixture: *base}
	owner.admission.ExpiresAt = time.Now().Add(2 * time.Second)
	window, err := NewOutputWindow(owner, binding, grant)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(window.Close)
	return window, owner
}

// TestOutputWindowRetainsExactBytesUntilAcknowledged proves replay reads only the retained private copy.
// TestOutputWindowRetainsExactBytesUntilAcknowledged доказывает replay только сохранённой private copy.
func TestOutputWindowRetainsExactBytesUntilAcknowledged(t *testing.T) {
	window, owner := outputWindowFixture(t)
	input := []byte("synthetic-output")
	if window.Stage(t.Context(), "STDOUT", input) != nil {
		t.Fatal("stage failed")
	}
	clear(input)
	first, err := window.Deliver(outputDeliveryContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Payload) != "synthetic-output" {
		t.Fatal("caller mutated retained output")
	}
	first.Payload[0] = 'X'
	second, err := window.Deliver(outputDeliveryContext(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Payload) != "synthetic-output" || first.PayloadDigest != second.PayloadDigest || first.Sequence != second.Sequence || owner.begins != 2 || window.stagedBytes != uint64(len(input)) || window.deliveredBytes != 2*uint64(len(input)) {
		t.Fatal("output replay changed bytes or bypassed fresh admission")
	}
	retained := window.frame.Payload
	if window.Acknowledge(t.Context(), second.Sequence+1) == nil || owner.completes != 0 {
		t.Fatal("foreign acknowledgement reached owner")
	}
	if window.Acknowledge(t.Context(), second.Sequence) != nil || !bytes.Equal(retained, make([]byte, len(retained))) {
		t.Fatal("ack did not destroy retained bytes")
	}
	if window.Acknowledge(t.Context(), second.Sequence) != nil || owner.completes != 1 {
		t.Fatal("duplicate ack spent another receipt")
	}
	if _, err := window.Deliver(outputDeliveryContext(t)); err == nil {
		t.Fatal("acknowledged output replayed")
	}
	owner.admission.Disposition = "SEND"
	if window.Stage(t.Context(), "STDOUT", []byte("next")) != nil {
		t.Fatal("next output blocked after acknowledgement")
	}
	next, err := window.Deliver(outputDeliveryContext(t))
	if err != nil || next.Sequence != second.Sequence+1 {
		t.Fatal("next output reused sequence")
	}
}

// TestOutputWindowLostReceiptRetriesOnlyMetadata retains data until readback without redelivering it.
// TestOutputWindowLostReceiptRetriesOnlyMetadata хранит data до readback без повторной доставки.
func TestOutputWindowLostReceiptRetriesOnlyMetadata(t *testing.T) {
	window, owner := outputWindowFixture(t)
	if window.Stage(t.Context(), "STDOUT", []byte("synthetic")) != nil {
		t.Fatal("stage failed")
	}
	frame, err := window.Deliver(outputDeliveryContext(t))
	if err != nil {
		t.Fatal(err)
	}
	owner.completeErr = errors.New("synthetic lost receipt")
	if window.Acknowledge(t.Context(), frame.Sequence) == nil {
		t.Fatal("lost receipt claimed success")
	}
	if _, err := window.Deliver(outputDeliveryContext(t)); err == nil || owner.begins != 1 {
		t.Fatal("ack uncertainty redelivered output")
	}
	if window.Stage(t.Context(), "STDOUT", []byte("replacement")) == nil {
		t.Fatal("unacknowledged output overwritten")
	}
	owner.completeErr = nil
	if window.Acknowledge(t.Context(), frame.Sequence) != nil || owner.completes != 2 || len(window.frame.Payload) != 0 {
		t.Fatal("metadata-only receipt recovery failed")
	}
}

// TestOutputWindowCreditRevocationAndBounds never exposes bytes on invalid or exhausted owner credit.
// TestOutputWindowCreditRevocationAndBounds не выдаёт bytes при invalid либо exhausted owner credit.
func TestOutputWindowCreditRevocationAndBounds(t *testing.T) {
	for _, scenario := range []string{"revoked", "unknown", "acked", "expired", "extended", "cancelled", "corrupt", "delivery-limit", "byte-limit"} {
		t.Run(scenario, func(t *testing.T) {
			window, owner := outputWindowFixture(t)
			if window.Stage(t.Context(), "STDOUT", []byte("synthetic")) != nil {
				t.Fatal("stage failed")
			}
			ctx, cancel := context.WithCancel(outputDeliveryContext(t))
			defer cancel()
			switch scenario {
			case "revoked":
				owner.beginErr = errors.New("synthetic revoked authority")
			case "unknown":
				owner.admission.Disposition = "UNKNOWN"
			case "acked":
				owner.admission.Disposition = "ACKNOWLEDGED"
			case "expired":
				owner.admission.ExpiresAt = time.Now().Add(-time.Second)
			case "extended":
				owner.admission.ExpiresAt = window.grant.ExpiresAt.Add(time.Second)
			case "cancelled":
				owner.beginHook = cancel
			case "corrupt":
				window.frame.Payload[0] ^= 1
			case "delivery-limit":
				for range maximumOutputDeliveries {
					if _, err := window.Deliver(ctx); err != nil {
						t.Fatal("bounded replay prematurely failed")
					}
				}
			case "byte-limit":
				window.deliveredBytes = window.grant.MaximumTotalBytes
			}
			frame, err := window.Deliver(ctx)
			if err == nil || len(frame.Payload) != 0 || owner.completes != 0 {
				t.Fatal("unadmitted output escaped")
			}
			if scenario == "delivery-limit" && owner.begins != maximumOutputDeliveries {
				t.Fatal("delivery cap made excess owner request")
			}
		})
	}
}

// TestOutputWindowCloseNeverInventsReceipt erases only local bytes and cannot claim delivery or remote exit.
// TestOutputWindowCloseNeverInventsReceipt стирает только local bytes, не утверждая delivery либо remote exit.
func TestOutputWindowCloseNeverInventsReceipt(t *testing.T) {
	window, owner := outputWindowFixture(t)
	if window.Stage(t.Context(), "STDOUT", []byte("synthetic")) != nil {
		t.Fatal("stage failed")
	}
	retained := window.frame.Payload
	window.Close()
	window.Close()
	if !bytes.Equal(retained, make([]byte, len(retained))) || owner.completes != 0 {
		t.Fatal("close retained bytes or forged receipt")
	}
	if window.Stage(t.Context(), "STDOUT", []byte("new")) == nil || window.Acknowledge(t.Context(), 0) == nil {
		t.Fatal("closed buffer resurrected")
	}
	if _, err := window.Deliver(outputDeliveryContext(t)); err == nil {
		t.Fatal("closed buffer delivered bytes")
	}
}

// TestOutputWindowSerializesConcurrentStage avoids overwrite while bounded exchanges contend.
// TestOutputWindowSerializesConcurrentStage исключает overwrite при одновременных bounded exchanges.
func TestOutputWindowSerializesConcurrentStage(t *testing.T) {
	window, _ := outputWindowFixture(t)
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if window.Stage(t.Context(), "STDOUT", []byte("synthetic")) == nil {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	if accepted.Load() != 1 {
		t.Fatal("multiple outputs overwrote one pending buffer")
	}
	<-window.access
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if window.Stage(ctx, "STDOUT", []byte("cancelled")) == nil {
		t.Fatal("cancelled exchange entered pending queue")
	}
	window.release()
}

// outputDeliveryContext models the finite credit supplied by actual native stream authorization.
// outputDeliveryContext моделирует конечный допуск от actual native stream authorization.
func outputDeliveryContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestOutputWindowSequenceUsesStorageRepresentation preserves one-buffer delivery beyond the former
// lifetime count and rejects bigint overflow without staging bytes or requesting authority.
// TestOutputWindowSequenceUsesStorageRepresentation сохраняет one-buffer delivery после прежнего
// lifetime count и отвергает bigint overflow без staging bytes либо запроса authority.
func TestOutputWindowSequenceUsesStorageRepresentation(t *testing.T) {
	for _, previous := range []uint64{4096, 1 << 20, managedkubernetes.MaximumFrameSequence - 1} {
		window, owner := outputWindowFixture(t)
		window.nextSequence = previous
		if window.Stage(t.Context(), "STDOUT", []byte("x")) != nil {
			t.Fatal("small useful frame rejected after prior cursor", previous)
		}
		frame, err := window.Deliver(outputDeliveryContext(t))
		if err != nil || frame.Sequence != previous+1 || len(frame.Payload) != 1 || owner.begins != 1 {
			t.Fatal("representable output sequence denied", previous, err)
		}
		clear(frame.Payload)
		if window.Acknowledge(t.Context(), frame.Sequence) != nil {
			t.Fatal("large sequence acknowledgement failed")
		}
		if previous == managedkubernetes.MaximumFrameSequence-1 && window.Stage(t.Context(), "STDOUT", []byte("y")) == nil {
			t.Fatal("sequence overflow admitted")
		}
	}
}
