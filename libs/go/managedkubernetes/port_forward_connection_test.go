package managedkubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// portForwardConnectionFixture binds synthetic owner bytes to one canonical connection without granting a native action.
// portForwardConnectionFixture связывает synthetic owner bytes с canonical connection без выдачи native action.
func portForwardConnectionFixture(action PortForwardConnectionAction) PortForwardConnectionFrame {
	frame := PortForwardConnectionFrame{Action: action, Slot: 1, Generation: 9, ConnectionRef: "kpf_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 18))}
	if action == PortForwardConnectionData {
		frame.Body = []byte{0, 0xff, 7, 1}
	}
	return frame
}

// TestPortForwardConnectionFramePreservesCanonicalOwnedEnvelope proves exact control/data bytes, slot narrowing, independent encoding ownership and diagnostic redaction.
// TestPortForwardConnectionFramePreservesCanonicalOwnedEnvelope доказывает exact control/data bytes, slot narrowing, independent encoding ownership и diagnostic redaction.
func TestPortForwardConnectionFramePreservesCanonicalOwnedEnvelope(t *testing.T) {
	for _, action := range []PortForwardConnectionAction{PortForwardConnectionOpen, PortForwardConnectionData, PortForwardConnectionHalfClose, PortForwardConnectionClose} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			frame := portForwardConnectionFixture(action)
			raw, err := EncodePortForwardConnectionFrame(frame)
			if err != nil || len(raw) != PortForwardConnectionFrameHeaderBytes+len(frame.Body) {
				t.Fatal("encode canonical connection envelope")
			}
			defer clear(raw)
			parsed, err := DecodePortForwardConnectionFrame(raw)
			if err != nil || parsed.Action != frame.Action || parsed.Slot != frame.Slot || parsed.Generation != frame.Generation || parsed.ConnectionRef != frame.ConnectionRef || !bytes.Equal(parsed.Body, frame.Body) || parsed.Validate(2) != nil || parsed.Validate(1) == nil {
				t.Fatal("connection tuple or approved slots changed")
			}
			copy, err := EncodePortForwardConnectionFrame(parsed)
			if err != nil || !bytes.Equal(copy, raw) {
				t.Fatal("connection envelope was not canonical")
			}
			defer clear(copy)
			if len(frame.Body) != 0 {
				frame.Body[0] = 11
				if raw[PortForwardConnectionFrameHeaderBytes] != 0 {
					t.Fatal("encoder borrowed caller TCP bytes")
				}
				copy[PortForwardConnectionFrameHeaderBytes] = 12
				if parsed.Body[0] != 0 {
					t.Fatal("encoder shared decoded TCP bytes")
				}
			}
			parsed.Body = []byte("private-tcp-sentinel")
			encoded, err := json.Marshal(parsed)
			if err != nil || bytes.Contains(encoded, parsed.Body) || bytes.Contains([]byte(fmt.Sprintf("%#v", parsed)), parsed.Body) {
				t.Fatal("private TCP body entered JSON or diagnostics")
			}
		})
	}
}

// TestPortForwardConnectionFrameRejectsMalformedAndLegacyWire keeps unknown versions, forged locators, controls, generations and oversized bodies outside native effects.
// TestPortForwardConnectionFrameRejectsMalformedAndLegacyWire оставляет unknown versions, forged locators, controls, generations и oversized bodies вне native effects.
func TestPortForwardConnectionFrameRejectsMalformedAndLegacyWire(t *testing.T) {
	good, err := EncodePortForwardConnectionFrame(portForwardConnectionFixture(PortForwardConnectionData))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"empty", "legacy", "truncated", "magic", "version", "action", "slot", "zero-generation", "overflow-generation", "length", "prefix", "reference-alphabet", "control-data", "empty-data", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			raw := bytes.Clone(good)
			switch mode {
			case "empty":
				raw = nil
			case "legacy":
				raw = []byte{1, 'a'}
			case "truncated":
				raw = raw[:PortForwardConnectionFrameHeaderBytes-1]
			case "magic":
				raw[0] = 'x'
			case "version":
				raw[3]++
			case "action":
				raw[4] = 9
			case "slot":
				raw[5] = MaximumForwardPorts
			case "zero-generation":
				binary.BigEndian.PutUint64(raw[6:14], 0)
			case "overflow-generation":
				binary.BigEndian.PutUint64(raw[6:14], math.MaxInt64+1)
			case "length":
				raw[14]--
			case "prefix":
				raw[15] = 'x'
			case "reference-alphabet":
				raw[19] = '/'
			case "control-data":
				raw[4] = byte(PortForwardConnectionHalfClose)
			case "empty-data":
				raw = raw[:PortForwardConnectionFrameHeaderBytes]
			case "oversize":
				raw = append(raw, make([]byte, MaximumFrameBytes)...)
			}
			if _, err := DecodePortForwardConnectionFrame(raw); err == nil {
				t.Fatal("malformed connection envelope was admitted")
			}
		})
	}
	frame := portForwardConnectionFixture(PortForwardConnectionData)
	frame.Generation = math.MaxInt64
	frame.Body = make([]byte, MaximumFrameBytes-PortForwardConnectionFrameHeaderBytes)
	if raw, err := EncodePortForwardConnectionFrame(frame); err != nil || len(raw) != MaximumFrameBytes {
		t.Fatal("exact byte/generation boundary refused")
	}
	frame.Body = append(frame.Body, 1)
	if _, err := EncodePortForwardConnectionFrame(frame); err == nil {
		t.Fatal("data escaped frame-minus-header byte budget")
	}
}

// TestPortForwardNativeWitnessSeparatesInputAndOutput binds a native candidate identity to exact canonical bytes without letting public INPUT assign that witness.
// TestPortForwardNativeWitnessSeparatesInputAndOutput связывает native candidate identity с exact canonical bytes без назначения witness публичным INPUT.
func TestPortForwardNativeWitnessSeparatesInputAndOutput(t *testing.T) {
	frame := portForwardConnectionFixture(PortForwardConnectionData)
	if frame.ValidateInput(2) != nil || frame.ValidateOutput(2) == nil {
		t.Fatal("input became native output")
	}
	frame.NativeOrdinal = 7
	if frame.ValidateInput(2) == nil || frame.ValidateOutput(2) != nil {
		t.Fatal("native witness entered public input")
	}
	raw, err := EncodePortForwardConnectionFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	witness, err := PortForwardOutputWitness(raw)
	if err != nil || witness.ConnectionRef != frame.ConnectionRef || witness.Generation != frame.Generation || witness.NativeOrdinal != 7 || witness.Slot != frame.Slot || witness.Action != PortForwardConnectionData || witness.ValidateOutput(uint32(len(raw)), 2) != nil {
		t.Fatal("native event witness changed canonical tuple")
	}
	if witness.ValidateOutput(PortForwardConnectionFrameHeaderBytes, 2) == nil || witness.ValidateOutput(uint32(len(raw)), 1) == nil {
		t.Fatal("witness lost DATA byte or exact slot bounds")
	}
	frame.Action = PortForwardConnectionOpen
	frame.Body = nil
	if frame.ValidateOutput(2) == nil {
		t.Fatal("OUTPUT fabricated connection OPEN")
	}
	frame.Action = PortForwardConnectionHalfClose
	if frame.ValidateOutput(2) != nil {
		t.Fatal("native read-end witness rejected")
	}
	frame.NativeOrdinal = math.MaxInt64 + 1
	if _, err := EncodePortForwardConnectionFrame(frame); err == nil {
		t.Fatal("native event escaped persisted integer bound")
	}
}

// TestPortForwardPublicInputRequiresCanonicalInputDirection accepts protected connection envelopes and rejects old EOF/slot-only or native-output witnesses.
// TestPortForwardPublicInputRequiresCanonicalInputDirection принимает protected connection envelopes и отклоняет old EOF/slot-only либо native-output witnesses.
func TestPortForwardPublicInputRequiresCanonicalInputDirection(t *testing.T) {
	for _, action := range []PortForwardConnectionAction{PortForwardConnectionOpen, PortForwardConnectionData, PortForwardConnectionHalfClose, PortForwardConnectionClose} {
		frame := portForwardConnectionFixture(action)
		raw, err := EncodePortForwardConnectionFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if ValidatePublicStreamInput("PORT_FORWARD", raw, hex.EncodeToString(digest[:]), false) != nil {
			t.Fatal("canonical input syntax rejected")
		}
		if ValidatePublicStreamInput("PORT_FORWARD", raw, hex.EncodeToString(digest[:]), true) == nil {
			t.Fatal("legacy whole tunnel EOF admitted")
		}
		if action != PortForwardConnectionOpen {
			frame.NativeOrdinal = 1
			raw, err = EncodePortForwardConnectionFrame(frame)
			if err != nil {
				t.Fatal(err)
			}
			digest = sha256.Sum256(raw)
			if ValidatePublicStreamInput("PORT_FORWARD", raw, hex.EncodeToString(digest[:]), false) == nil {
				t.Fatal("native output event accepted as input")
			}
		}
	}
}
