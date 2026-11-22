package kubernetesoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// forwardOpenWriterFixture observes the separate protected admission port; generic writes cannot stand in for owner-bound OPEN.
// forwardOpenWriterFixture наблюдает отдельный protected admission port; generic writes не заменяют owner-bound OPEN.
type forwardOpenWriterFixture struct {
	frameWriterFixture
	opens    int
	retained managedkubernetes.PortForwardRetentionSet
}

// WritePortForwardOpen retains the owned snapshot synchronously without opening an actual socket.
// WritePortForwardOpen синхронно сохраняет owned snapshot без открытия настоящего socket.
func (w *forwardOpenWriterFixture) WritePortForwardOpen(_ context.Context, _ value.KubernetesStreamFrame, retained managedkubernetes.PortForwardRetentionSet) error {
	w.opens++
	w.retained = retained
	return nil
}

// TestInputRelayPortForwardOpenRequiresProtectedRetention rejects missing, foreign, stale and non-SEND snapshots before any native write or false ACK.
// TestInputRelayPortForwardOpenRequiresProtectedRetention отклоняет missing, foreign, stale и non-SEND snapshots до native write либо ложного ACK.
func TestInputRelayPortForwardOpenRequiresProtectedRetention(t *testing.T) {
	for _, mode := range []string{"valid", "writer_missing", "missing", "foreign", "future", "unknown", "ack", "non_open"} {
		t.Run(mode, func(t *testing.T) {
			relay, binding, grant, frame, owner, ordinary := inputFrameFixture(t)
			ref := "kpf_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{19}, 18))
			forward := managedkubernetes.PortForwardConnectionFrame{ConnectionRef: ref, Generation: 4, Slot: 0, Action: managedkubernetes.PortForwardConnectionOpen}
			if mode == "non_open" {
				forward.Action = managedkubernetes.PortForwardConnectionClose
			}
			raw, err := managedkubernetes.EncodePortForwardConnectionFrame(forward)
			if err != nil {
				t.Fatal(err)
			}
			frame.Channel = "PORT_FORWARD"
			frame.Payload = raw
			sum := sha256.Sum256(raw)
			frame.PayloadDigest = hex.EncodeToString(sum[:])
			grant.InputChannels = []string{"PORT_FORWARD"}
			owner.admission.PortForwardRetention = &managedkubernetes.PortForwardRetentionSet{Connections: []managedkubernetes.PortForwardConnectionIdentity{{ConnectionRef: ref, Generation: 4, Slot: 0}}}
			switch mode {
			case "missing":
				owner.admission.PortForwardRetention = nil
			case "foreign":
				owner.admission.PortForwardRetention.Connections[0].Slot = 1
			case "future":
				owner.admission.PortForwardRetention.Connections[0].Generation++
			case "unknown":
				owner.admission.Disposition = "UNKNOWN"
			case "ack":
				owner.admission.Disposition = "ACKNOWLEDGED"
			}
			writer := &forwardOpenWriterFixture{}
			if mode == "writer_missing" {
				_, attempted, err := relay.Send(t.Context(), binding, grant, frame, ordinary)
				if err == nil || attempted || owner.begins != 0 || ordinary.writes != 0 {
					t.Fatal("missing purpose port consumed authority")
				}
				return
			}
			ack, attempted, err := relay.Send(t.Context(), binding, grant, frame, writer)
			if mode != "valid" {
				if err == nil || attempted || ack != 0 || writer.opens != 0 || writer.writes != 0 || owner.completes != 0 {
					t.Fatal("malformed OPEN reached native writer", mode)
				}
				return
			}
			if err != nil || !attempted || ack != frame.Sequence || writer.opens != 1 || writer.writes != 0 || owner.completes != 1 {
				t.Fatal("exact protected OPEN was not joined")
			}
			writer.retained.Connections[0].Generation++
			if owner.admission.PortForwardRetention.Connections[0].Generation != 4 {
				t.Fatal("native writer borrowed owner metadata")
			}
		})
	}
}
