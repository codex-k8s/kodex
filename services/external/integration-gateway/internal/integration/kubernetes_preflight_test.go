package integration

import (
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPortForwardPreflightDecodesOnlyExactClosedBrokerDetail prevents generic or malformed diagnostics from becoming a no-effect failure claim.
// TestPortForwardPreflightDecodesOnlyExactClosedBrokerDetail запрещает превращать generic либо malformed diagnostics в no-effect failure claim.
func TestPortForwardPreflightDecodesOnlyExactClosedBrokerDetail(t *testing.T) {
	for _, mode := range []string{"valid", "reason", "domain", "metadata", "status", "duplicate", "plain"} {
		t.Run(mode, func(t *testing.T) {
			info := &errdetails.ErrorInfo{Domain: "kodex.secret-broker", Reason: managedkubernetes.ReasonPortForwardProfileUnsupported}
			code := codes.FailedPrecondition
			switch mode {
			case "reason":
				info.Reason = "UNKNOWN"
			case "domain":
				info.Domain = "other"
			case "metadata":
				info.Metadata = map[string]string{"target": "synthetic"}
			case "status":
				code = codes.Unavailable
			}
			result := status.New(code, "synthetic private diagnostic")
			result, err := result.WithDetails(info)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "duplicate" {
				result, err = result.WithDetails(info)
				if err != nil {
					t.Fatal(err)
				}
			}
			failure := result.Err()
			if mode == "plain" {
				failure = errors.New("synthetic private diagnostic")
			}
			if (kubernetesPreflightFromError(failure) != nil) != (mode == "valid") {
				t.Fatal("unregistered diagnostic became a typed preflight rejection")
			}
		})
	}
}

// TestPortForwardPreflightCompletionBindsOriginalPlanAndRequiresOwnerFailure proves the hint is explicit, single-attempt and cannot accept owner RUNNING/SUCCESS or mismatched pins.
// TestPortForwardPreflightCompletionBindsOriginalPlanAndRequiresOwnerFailure доказывает explicit single-attempt hint и отказ owner RUNNING/SUCCESS либо mismatched pins.
func TestPortForwardPreflightCompletionBindsOriginalPlanAndRequiresOwnerFailure(t *testing.T) {
	for _, mode := range []string{"valid", "success", "running", "pins", "lost"} {
		t.Run(mode, func(t *testing.T) {
			binding, running, completed := kubernetesOwnerReceipts(t)
			completed.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED
			fixture := &kubernetesOwnerFixture{complete: &cp.CompleteManagedKubernetesInvocationResponse{Receipt: completed}}
			switch mode {
			case "success":
				completed.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_SUCCEEDED
			case "running":
				fixture.complete.Receipt = running
			case "pins":
				completed.PlanDigest = running.InputDigest
			case "lost":
				fixture.completeErr = errors.New("synthetic lost ACK")
			}
			receipt, err := RejectKubernetesPortForwardPreflight(t.Context(), binding, running, fixture)
			if (err == nil) != (mode == "valid") || fixture.completeCalls != 1 || fixture.readCalls != 0 || fixture.completeRequest.PreflightRejection != cp.ManagedKubernetesPreflightRejection_MANAGED_KUBERNETES_PREFLIGHT_REJECTION_PORT_FORWARD_PROFILE_UNSUPPORTED || fixture.completeRequest.PlanRef != running.PlanRef {
				t.Fatal("preflight completion changed source or replayed mutation")
			}
			if mode != "valid" && receipt != nil {
				t.Fatal("invalid completion exposed metadata as accepted")
			}
		})
	}
}
