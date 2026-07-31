package grpcserver

import (
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestStrictProtoCodecAcceptsCanonicalSingularField(t *testing.T) {
	message := &wrapperspb.StringValue{}
	if err := StrictProtoCodec().Unmarshal([]byte{0x0a, 0x01, 'a'}, message); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if HasMalformedProto(message) || message.Value != "a" {
		t.Fatalf("decoded message = %#v", message)
	}
}

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
