package grpc

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestKubernetesFrameAdmissionSeparatesCreditAndReadback prevents ACK/UNKNOWN from requiring fake expiry.
// It also rejects missing/output-overflow ordinals and ordinals smuggled into INPUT credit.
// TestKubernetesFrameAdmissionSeparatesCreditAndReadback не требует fake expiry для ACK/UNKNOWN.
// Также отвергает missing/output-overflow ordinal и ordinal внутри INPUT credit.
func TestKubernetesFrameAdmissionSeparatesCreditAndReadback(t *testing.T) {
	for _, disposition := range []enum.ManagedKubernetesFrameDisposition{enum.KubernetesFrameAcknowledged, enum.KubernetesFrameUnknown} {
		if got := managedKubernetesFrameAdmissionToProto(entity.ManagedKubernetesFrameAdmission{Disposition: disposition}, enum.KubernetesFrameOutput); got == nil || got.ExpiresAt != nil || got.DeliveryOrdinal != 0 {
			t.Fatal("metadata-only frame reply required invented credit")
		}
	}
	for _, test := range []struct {
		direction enum.ManagedKubernetesFrameDirection
		ordinal   uint32
		valid     bool
	}{{enum.KubernetesFrameOutput, 1, true}, {enum.KubernetesFrameOutput, 4, true}, {enum.KubernetesFrameOutput, 0, false}, {enum.KubernetesFrameOutput, 5, false}, {enum.KubernetesFrameInput, 0, true}, {enum.KubernetesFrameInput, 1, false}} {
		got := managedKubernetesFrameAdmissionToProto(entity.ManagedKubernetesFrameAdmission{Disposition: enum.KubernetesFrameSend, ExpiresAt: time.Now().Add(time.Second), DeliveryOrdinal: test.ordinal}, test.direction)
		if (got != nil) != test.valid {
			t.Fatal("invalid delivery ordinal crossed frame mapper", test)
		}
	}
}

// TestKubernetesFrameRetentionIsOnlyInputSendMetadata prevents readback/output replies from carrying an owner-selected OPEN snapshot.
// TestKubernetesFrameRetentionIsOnlyInputSendMetadata запрещает owner-selected OPEN snapshot в readback/output replies.
func TestKubernetesFrameRetentionIsOnlyInputSendMetadata(t *testing.T) {
	for _, mode := range []string{"valid", "output", "unknown", "ack", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			set := &managedkubernetes.PortForwardRetentionSet{Connections: []managedkubernetes.PortForwardConnectionIdentity{{ConnectionRef: "kpf_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 18)), Generation: 3, Slot: 0}}}
			input := entity.ManagedKubernetesFrameAdmission{Disposition: enum.KubernetesFrameSend, ExpiresAt: time.Now().Add(time.Second), PortForwardRetention: set}
			direction := enum.KubernetesFrameInput
			switch mode {
			case "output":
				direction = enum.KubernetesFrameOutput
				input.DeliveryOrdinal = 1
			case "unknown":
				input.Disposition = enum.KubernetesFrameUnknown
			case "ack":
				input.Disposition = enum.KubernetesFrameAcknowledged
			case "invalid":
				set.Connections[0].Generation = 0
			}
			got := managedKubernetesFrameAdmissionToProto(input, direction)
			if (got != nil) != (mode == "valid") {
				t.Fatal("retention crossed invalid frame disposition", mode)
			}
			if got != nil && (len(got.PortForwardRetention) != 1 || got.PortForwardRetention[0].Generation != 3) {
				t.Fatal("retention tuple changed")
			}
		})
	}
}
