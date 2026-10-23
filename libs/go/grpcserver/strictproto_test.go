package grpcserver

import (
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestStrictProtoCodecAcceptsCanonicalSingularField preserves acceptance of
// one valid known protobuf field without a malformed marker.
// RU: TestStrictProtoCodecAcceptsCanonicalSingularField проверяет принятие
// одного корректного известного поля без метки ошибки.
func TestStrictProtoCodecAcceptsCanonicalSingularField(t *testing.T) {
	message := &wrapperspb.StringValue{}
	if err := StrictProtoCodec().Unmarshal([]byte{0x0a, 0x01, 'a'}, message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if HasMalformedProto(message) || message.Value != "a" {
		t.Fatalf("decoded message = %#v", message)
	}
}

// TestStrictProtoCodecMarksDuplicateUnknownAndBrokenWire requires a marker
// for duplicate singular fields, unknown fields and truncated wire values.
// RU: TestStrictProtoCodecMarksDuplicateUnknownAndBrokenWire требует метку
// для повтора одиночного поля, чужого поля и повреждённого wire-значения.
func TestStrictProtoCodecMarksDuplicateUnknownAndBrokenWire(t *testing.T) {
	for name, raw := range map[string][]byte{
		"duplicate": {0x0a, 0x01, 'a', 0x0a, 0x01, 'b'},
		"unknown":   {0x10, 0x01},
		"broken":    {0x0a, 0xff},
	} {
		t.Run(name, func(t *testing.T) {
			message := &wrapperspb.StringValue{}
			if err := StrictProtoCodec().Unmarshal(raw, message); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if !HasMalformedProto(message) {
				t.Fatal("malformed protobuf payload was not marked")
			}
		})
	}
}
