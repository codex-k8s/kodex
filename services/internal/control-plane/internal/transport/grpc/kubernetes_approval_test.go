package grpc

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestKubernetesChildApprovalErrorBindsOnlyOwnerRefs proves the exact bounded waiting contract and
// keeps infrastructure failures or malformed metadata from appearing as an approvable HumanGate.
// TestKubernetesChildApprovalErrorBindsOnlyOwnerRefs проверяет exact bounded waiting contract и
// не выдаёт infrastructure failure либо malformed metadata за approvable HumanGate.
func TestKubernetesChildApprovalErrorBindsOnlyOwnerRefs(t *testing.T) {
	waiting := &platformservice.KubernetesApprovalRequiredError{GateRef: "gat_exact_child", ChildRef: "kch_exact_child", PlanRef: "kpl_exact_plan"}
	result := status.Convert(transportError(waiting))
	if result.Code() != codes.FailedPrecondition || len(result.Details()) != 1 {
		t.Fatal("missing exact waiting precondition")
	}
	info, ok := result.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.Domain != controlPlaneErrorDomain || info.Reason != platformservice.KubernetesApprovalRequiredReason || len(info.Metadata) != 3 || info.Metadata["gateRef"] != waiting.GateRef || info.Metadata["childRef"] != waiting.ChildRef || info.Metadata["planRef"] != waiting.PlanRef {
		t.Fatal("waiting metadata changed owner pins")
	}
	for _, err := range []error{errors.Join(waiting, errs.ErrUnavailable), &platformservice.KubernetesApprovalRequiredError{GateRef: "bad ref", ChildRef: waiting.ChildRef, PlanRef: waiting.PlanRef}} {
		result := status.Convert(transportError(err))
		if result.Code() != codes.Unavailable || len(result.Details()) != 0 {
			t.Fatal("unavailable or malformed Gate acquired waiting authority")
		}
	}
}
