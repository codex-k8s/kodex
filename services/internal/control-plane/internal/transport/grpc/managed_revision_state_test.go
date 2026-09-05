package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestManagedRevisionProtoStateRoundTrip(t *testing.T) {
	for index, state := range []string{"DRAFT", "VALID", "INVALID", "PUBLISHED", "SUPERSEDED", "DISCARDED"} {
		t.Run(state, func(t *testing.T) {
			value := castManagedRevision(&entity.ManagedConfigurationRevision{Ref: "mrev_abcdefgh", Revision: 2, State: state})
			if int32(value.State) != int32(index+1) || value.State.String() != "MANAGED_CONFIGURATION_STATE_"+state {
				t.Fatalf("state mapping: got %s (%d), want %s (%d)", value.State, value.State, state, index+1)
			}
			raw, err := proto.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded controlplanev1.ManagedConfigurationRevision
			if err := proto.Unmarshal(raw, &decoded); err != nil || decoded.State != value.State {
				t.Fatalf("binary roundtrip: %v", err)
			}
			raw, err = protojson.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			decoded.Reset()
			if err := protojson.Unmarshal(raw, &decoded); err != nil || decoded.State != value.State {
				t.Fatalf("JSON roundtrip: %v", err)
			}
		})
	}
}
