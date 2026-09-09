package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	fixture "github.com/codex-k8s/kodex/services/internal/control-plane/testdata/runtime-snapshot"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestRuntimeGrantSnapshotCasterMatchesCompleteWireFixture(t *testing.T) {
	claim := castClaim(fixture.Snapshot(t))
	raw, err := proto.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	received := &cp.ClaimedExecution{}
	if err := proto.Unmarshal(raw, received); err != nil {
		t.Fatal(err)
	}
	want := &cp.ClaimedExecution{}
	if err := protojson.Unmarshal(fixture.Claim, want); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(want, received) {
		t.Fatalf("synthetic owner snapshot lost fields on wire: %s", protojson.Format(received))
	}
}
