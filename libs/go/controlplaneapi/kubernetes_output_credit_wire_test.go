package controlplaneapi

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

// TestKubernetesNativeOutputCreditWire retains the broker witness and owner ordinal through real Proto.
// These fields are metadata only; this test does not mint native RBAC authority or owner frame credit.
// TestKubernetesNativeOutputCreditWire сохраняет broker witness и owner ordinal через real Proto.
// Поля лишь metadata; тест не выдаёт native RBAC authority либо owner frame credit.
func TestKubernetesNativeOutputCreditWire(t *testing.T) {
	request := &cp.BeginManagedKubernetesFrameRequest{NativeAuthorizedUntil: timestamppb.New(time.Unix(1800000000, 0))}
	raw, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded cp.BeginManagedKubernetesFrameRequest
	if proto.Unmarshal(raw, &decoded) != nil || !proto.Equal(request, &decoded) || decoded.NativeAuthorizedUntil == nil {
		t.Fatal("native witness lost on wire")
	}
	response := &cp.BeginManagedKubernetesFrameResponse{DeliveryOrdinal: 4}
	raw, err = proto.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var reply cp.BeginManagedKubernetesFrameResponse
	if proto.Unmarshal(raw, &reply) != nil || reply.DeliveryOrdinal != 4 {
		t.Fatal("delivery ordinal lost on wire")
	}
}
