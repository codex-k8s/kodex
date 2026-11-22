package kubernetesoperation

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// portForwardOutputOwner retains the exact closed-candidate reply independently of a sibling SEND and native witness.
// portForwardOutputOwner сохраняет exact closed-candidate reply независимо от sibling SEND и native witness.
type portForwardOutputOwner struct {
	*frameOwnerFixture
	reply value.KubernetesFrameAdmission
}

// BeginKubernetesFrame captures actual metadata and returns only the explicitly selected synthetic owner disposition.
// BeginKubernetesFrame фиксирует actual metadata и возвращает лишь явно выбранный synthetic owner disposition.
func (owner *portForwardOutputOwner) BeginKubernetesFrame(_ context.Context, _ managedkubernetes.Binding, metadata value.KubernetesFrameMetadata) (value.KubernetesFrameAdmission, error) {
	owner.begins++
	owner.metadata = metadata
	return owner.reply, owner.beginErr
}

// portForwardOutputFrame builds one canonical native event, not an owner input or execution admission.
// portForwardOutputFrame создаёт canonical native event, не owner input либо execution admission.
func portForwardOutputFrame(t *testing.T, generation, ordinal uint64) []byte {
	t.Helper()
	raw, err := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{Action: managedkubernetes.PortForwardConnectionData, ConnectionRef: "kpf_" + base64.RawURLEncoding.EncodeToString(make([]byte, 18)), Generation: generation, NativeOrdinal: ordinal, Body: []byte("tcp")})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestPortForwardClosedOutputPreservesSiblingSequenceAndBudget proves a closed native candidate creates no gap, ACK or refunded byte budget.
// TestPortForwardClosedOutputPreservesSiblingSequenceAndBudget доказывает отсутствие gap, ACK либо возврата byte budget при закрытом native candidate.
func TestPortForwardClosedOutputPreservesSiblingSequenceAndBudget(t *testing.T) {
	window, base := outputWindowFixture(t)
	window.grant.OutputChannels = []string{"PORT_FORWARD"}
	owner := &portForwardOutputOwner{frameOwnerFixture: &base.frameOwnerFixture, reply: value.KubernetesFrameAdmission{Disposition: "CLOSED"}}
	window.owner = owner
	raw := portForwardOutputFrame(t, 1, 1)
	if window.Stage(t.Context(), "PORT_FORWARD", raw) != nil {
		t.Fatal("stage failed")
	}
	retained := window.frame.Payload
	frame, err := window.Deliver(outputDeliveryContext(t))
	if err != nil || frame.Sequence != 0 || frame.Payload != nil || window.frame.Sequence != 0 || window.nextSequence != 0 || window.acknowledged != 0 || window.deliveredBytes != 0 || window.stagedBytes != uint64(len(raw)) || owner.completes != 0 {
		t.Fatal("closed candidate acquired credit or altered accounting")
	}
	for _, b := range retained {
		if b != 0 {
			t.Fatal("closed candidate retained private bytes")
		}
	}
	if owner.metadata.PortForward == nil || owner.metadata.PortForward.Generation != 1 || owner.metadata.PortForward.NativeOrdinal != 1 || owner.metadata.ByteCount != uint32(len(raw)) {
		t.Fatal("owner missed exact native witness")
	}
	owner.reply = value.KubernetesFrameAdmission{Disposition: "SEND", DeliveryOrdinal: 1, ExpiresAt: time.Now().Add(time.Second)}
	if window.Stage(t.Context(), "PORT_FORWARD", portForwardOutputFrame(t, 2, 1)) != nil {
		t.Fatal("sibling blocked")
	}
	next, err := window.Deliver(outputDeliveryContext(t))
	if err != nil || next.Sequence != 1 || next.DeliveryOrdinal != 1 || owner.metadata.PortForward.Generation != 2 || window.stagedBytes != 2*uint64(len(raw)) {
		t.Fatal("sibling skipped sequence or refunded dropped bytes")
	}
	if window.Acknowledge(t.Context(), next.Sequence) != nil || owner.completes != 1 {
		t.Fatal("sibling ACK failed")
	}
}

// TestPortForwardClosedOutputRejectsAmbiguousReplies refuses malformed CLOSED, uncertain owner ACKs, ordinary channels and already admitted frames.
// TestPortForwardClosedOutputRejectsAmbiguousReplies отклоняет malformed CLOSED, uncertain owner ACK, ordinary channels и already admitted frames.
func TestPortForwardClosedOutputRejectsAmbiguousReplies(t *testing.T) {
	for _, scenario := range []string{"deadline", "ordinal", "uncertain", "ordinary", "admitted"} {
		t.Run(scenario, func(t *testing.T) {
			window, base := outputWindowFixture(t)
			window.grant.OutputChannels = []string{"PORT_FORWARD", "STDOUT"}
			owner := &portForwardOutputOwner{frameOwnerFixture: &base.frameOwnerFixture, reply: value.KubernetesFrameAdmission{Disposition: "CLOSED"}}
			window.owner = owner
			channel, payload := "PORT_FORWARD", portForwardOutputFrame(t, 1, 1)
			switch scenario {
			case "deadline":
				owner.reply.ExpiresAt = time.Now().Add(time.Second)
			case "ordinal":
				owner.reply.DeliveryOrdinal = 1
			case "uncertain":
				owner.beginErr = errors.New("synthetic lost reply")
			case "ordinary":
				channel, payload = "STDOUT", []byte("safe output")
			}
			if window.Stage(t.Context(), channel, payload) != nil {
				t.Fatal("stage failed")
			}
			if scenario == "admitted" {
				owner.reply = value.KubernetesFrameAdmission{Disposition: "SEND", DeliveryOrdinal: 1, ExpiresAt: time.Now().Add(time.Second)}
				if _, err := window.Deliver(outputDeliveryContext(t)); err != nil {
					t.Fatal(err)
				}
				owner.reply = value.KubernetesFrameAdmission{Disposition: "CLOSED"}
			}
			if _, err := window.Deliver(outputDeliveryContext(t)); err == nil || window.frame.Sequence != 1 || owner.completes != 0 {
				t.Fatal("ambiguous closure discarded an unproven frame")
			}
		})
	}
}
