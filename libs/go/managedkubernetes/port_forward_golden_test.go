package managedkubernetes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// portForwardGoldenFrame shares canonical direction-specific vectors with the Node owner-local consumer; all counters remain decimal strings in the fixture.
// portForwardGoldenFrame разделяет canonical direction-specific vectors с Node owner-local consumer; counters в fixture остаются decimal strings.
type portForwardGoldenFrame struct {
	Name, Direction, Generation, NativeOrdinal, ConnectionRef, BodyBase64, RawBase64 string
	Action, Slot                                                                     byte
}

// TestPortForwardGoldenWireMatchesOwnerLocalCodec anchors cross-language vectors to the actual Go encoder and parser, including integers above JavaScript's exact range.
// TestPortForwardGoldenWireMatchesOwnerLocalCodec закрепляет cross-language vectors за actual Go encoder/parser, включая integers выше точного JavaScript range.
func TestPortForwardGoldenWireMatchesOwnerLocalCodec(t *testing.T) {
	raw, err := os.ReadFile("testdata/port_forward_frames.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []portForwardGoldenFrame
	if json.Unmarshal(raw, &cases) != nil || len(cases) == 0 {
		t.Fatal("canonical wire fixture is invalid")
	}
	for _, item := range cases {
		t.Run(item.Name, func(t *testing.T) {
			generation, e1 := strconv.ParseUint(item.Generation, 10, 64)
			ordinal, e2 := strconv.ParseUint(item.NativeOrdinal, 10, 64)
			body, e3 := base64.StdEncoding.DecodeString(item.BodyBase64)
			wire, e4 := base64.StdEncoding.DecodeString(item.RawBase64)
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
				t.Fatal("canonical fixture coordinates are invalid")
			}
			defer clear(body)
			defer clear(wire)
			frame := PortForwardConnectionFrame{ConnectionRef: item.ConnectionRef, Generation: generation, NativeOrdinal: ordinal, Slot: item.Slot, Action: PortForwardConnectionAction(item.Action), Body: body}
			encoded, err := EncodePortForwardConnectionFrame(frame)
			defer clear(encoded)
			if err != nil || !bytes.Equal(encoded, wire) {
				t.Fatal("cross-language canonical encoding changed")
			}
			parsed, err := DecodePortForwardConnectionFrame(wire)
			if err != nil || parsed.ConnectionRef != frame.ConnectionRef || parsed.Generation != generation || parsed.NativeOrdinal != ordinal || !bytes.Equal(parsed.Body, body) {
				t.Fatal("cross-language wire coordinates changed")
			}
			if item.Direction == "OUTPUT" {
				if parsed.ValidateOutput(16) != nil {
					t.Fatal("valid output vector rejected")
				}
			} else if item.Direction != "INPUT" || parsed.ValidateInput(16) != nil {
				t.Fatal("valid input vector rejected")
			}
		})
	}
}
