package grpc

import (
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

func TestEmailAuthorizationBindingAndOperationContract(t *testing.T) {
	for value := range cp.EmailOperation_name {
		if value == 0 {
			continue
		}
		operation := cp.EmailOperation(value)
		if emailOperationProto(emailOperationFromProto(operation)) != operation {
			t.Fatalf("operation roundtrip: %s", operation)
		}
	}
	if emailOperationFromProto(cp.EmailOperation(99)) != "" || emailOperationProto("unknown") != 0 {
		t.Fatal("unknown operation accepted")
	}
	for _, test := range []bool{false, true} {
		source := entity.EmailExecutionBinding{LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 4,
			ExpiresAt: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)}
		if test {
			source.ConnectionTestRef = "test_fixture"
		} else {
			source.InvocationRef = "inv_fixture"
		}
		wire := castEmailBinding(source)
		raw, err := proto.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		var restored cp.EmailExecutionBinding
		if err := proto.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		decoded, err := emailBindingFromProto(&restored)
		if err != nil || decoded != source {
			t.Fatalf("binding roundtrip: %v", err)
		}
		wire.Source = nil
		if _, err := emailBindingFromProto(wire); err == nil {
			t.Fatal("source-free lease accepted")
		}
	}
}
