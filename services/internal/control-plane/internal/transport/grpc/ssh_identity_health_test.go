package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

// TestSSHHealthRPCRequiresMethodAuthority rejects both private routes before absent owner I/O and keeps unknown nested binding fields closed over actual protobuf wire bytes.
// TestSSHHealthRPCRequiresMethodAuthority отклоняет private routes до absent owner I/O и закрывает unknown nested binding fields через actual protobuf wire bytes.
func TestSSHHealthRPCRequiresMethodAuthority(t *testing.T) {
	server := &Server{}
	if _, err := server.BeginSSHIdentityHealthProbe(t.Context(), &cp.BeginSSHIdentityHealthProbeRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("unverified health Begin admitted")
	}
	if _, err := server.CompleteSSHIdentityHealthProbe(t.Context(), &cp.CompleteSSHIdentityHealthProbeRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("unverified health Complete admitted")
	}
	binding := &cp.ManagedSSHHealthBinding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: "test_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	if _, ok := sshHealthBinding(binding); !ok {
		t.Fatal("original claim rejected")
	}
	binding.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	raw, err := proto.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var decoded cp.ManagedSSHHealthBinding
	if proto.Unmarshal(raw, &decoded) != nil {
		t.Fatal("decode test binding")
	}
	if _, ok := sshHealthBinding(&decoded); ok {
		t.Fatal("unknown private binding fields admitted")
	}
}
