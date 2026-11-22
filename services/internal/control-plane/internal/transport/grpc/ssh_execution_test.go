package grpc

import (
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestSSHExecutionRPCRequiresExactOwner denies every new RPC without verified method context before touching absent persistence; unknown nested wire fields cannot acquire a lease through decoding.
// TestSSHExecutionRPCRequiresExactOwner отклоняет каждый новый RPC без verified method context до обращения к absent persistence; unknown nested wire fields не приобретают lease через decode.
func TestSSHExecutionRPCRequiresExactOwner(t *testing.T) {
	server := &Server{}
	if _, err := server.BeginSSHExecution(t.Context(), &cp.BeginSSHExecutionRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("begin admitted unsigned principal", err)
	}
	if _, err := server.CompleteSSHExecution(t.Context(), &cp.CompleteSSHExecutionRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("complete admitted unsigned principal", err)
	}
	if _, err := server.GetSSHExecution(t.Context(), &cp.GetSSHExecutionRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("get admitted unsigned principal", err)
	}
	binding := &cp.ManagedSSHExecutionBinding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	if _, valid := sshExecutionBinding(binding); !valid {
		t.Fatal("valid original binding rejected")
	}
	wire, err := proto.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	wire = append(wire, 0x98, 0x06, 0x01)
	var decoded cp.ManagedSSHExecutionBinding
	if err := proto.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, valid := sshExecutionBinding(&decoded); valid {
		t.Fatal("unknown nested field accepted")
	}
	if _, ok := sshExecutionOutcome(cp.ManagedSSHExecutionOutcome(77)); ok {
		t.Fatal("future outcome defaulted")
	}
}

// TestSSHExecutionReceiptCannotPromoteUnreportedOrDiscardedOutput checks the actual transport projection against mixed metadata, future state and output without fresh publication.
// TestSSHExecutionReceiptCannotPromoteUnreportedOrDiscardedOutput проверяет actual transport projection для mixed metadata, future state и output без fresh publication.
func TestSSHExecutionReceiptCannotPromoteUnreportedOrDiscardedOutput(t *testing.T) {
	original := entity.SSHExecutionReceipt{AttemptRef: "sxe_fixture", SnapshotDigest: strings.Repeat("b", 64), State: "UNREPORTED", OwnerState: "RUNNING", AuthorizedUntil: time.Now().Add(time.Second)}
	if _, err := castSSHExecutionReceipt(original); err != nil {
		t.Fatal("valid original active metadata rejected", err)
	}
	for _, mutate := range []func(*entity.SSHExecutionReceipt){
		func(r *entity.SSHExecutionReceipt) { r.Applied = true },
		func(r *entity.SSHExecutionReceipt) { r.Recorded = true },
		func(r *entity.SSHExecutionReceipt) { r.State = "SUCCEEDED" },
		func(r *entity.SSHExecutionReceipt) { r.AuthorizedUntil = time.Now().Add(time.Minute) },
		func(r *entity.SSHExecutionReceipt) { r.Observation.Outcome = managedssh.ExecutionUnknown },
	} {
		changed := original
		mutate(&changed)
		if _, err := castSSHExecutionReceipt(changed); err == nil {
			t.Fatal("mixed original receipt became valid")
		}
	}
	discarded := entity.SSHExecutionReceipt{AttemptRef: "sxe_fixture", SnapshotDigest: strings.Repeat("b", 64), State: "DISCARDED", OwnerState: "CANCELLED", Recorded: true, Observation: managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}}
	if _, err := castSSHExecutionReceipt(discarded); err != nil {
		t.Fatal("discarded metadata rejected", err)
	}
	discarded.Observation.Result = []byte(`{}`)
	if _, err := castSSHExecutionReceipt(discarded); err == nil {
		t.Fatal("discarded result became output")
	}
}
