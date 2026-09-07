package snapshot

import (
	"os"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

func TestSTTIssuerLoadsExactParentFromCanonicalPolicy(t *testing.T) {
	raw, err := os.ReadFile("../../../../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var document publisherPolicyDocument
	if err := internalrpcauth.DecodeStrictJSON(raw, &document); err != nil {
		t.Fatal(err)
	}
	selected, err := selectRoleBindings(RoleIssuer, "stt-tts-service", document.Policy.OperationBindings)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, binding := range selected {
		seen[binding.OperationID] = true
		if binding.CallerWorkloadID != "stt-tts-service" && binding.OperationID != "platform.stt.transcribe" {
			t.Fatal("unrelated foreign operation was loaded")
		}
	}
	for _, operation := range []string{"platform.stt.transcribe", "platform.stt.policy.resolve", "platform.stt.credential.project"} {
		if !seen[operation] {
			t.Fatalf("required continuation binding is missing: %s", operation)
		}
	}
	verified, err := selectRoleBindings(RoleVerifier, "control-plane", document.Policy.OperationBindings)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range verified {
		if binding.TargetWorkloadID != "control-plane" {
			t.Fatal("verifier scope was widened")
		}
	}
}

func TestContinuationSelectorRejectsBrokenParentEdges(t *testing.T) {
	parent := operationBinding{OperationID: "platform.parent", CallerWorkloadID: "gateway", TargetWorkloadID: "worker", TargetSPIFFEID: "spiffe://example/worker", FullMethod: "/example.Parent/Run"}
	child := operationBinding{OperationID: "platform.child", CallerWorkloadID: "worker", CallerSPIFFEID: "spiffe://example/worker", TargetWorkloadID: "owner", FullMethod: "/example.Child/Run", Continuation: &continuationProfile{ParentOperationID: parent.OperationID, ParentFullMethod: parent.FullMethod}}
	for _, mutate := range []func([]operationBinding) []operationBinding{
		func(values []operationBinding) []operationBinding { return values[1:] },
		func(values []operationBinding) []operationBinding {
			values[0].FullMethod = "/example.Other/Run"
			return values
		},
		func(values []operationBinding) []operationBinding {
			values[0].TargetWorkloadID = "foreign"
			return values
		},
		func(values []operationBinding) []operationBinding {
			values[0].TargetSPIFFEID = "spiffe://example/foreign"
			return values
		},
		func(values []operationBinding) []operationBinding { return append(values, values[0]) },
	} {
		if _, err := selectRoleBindings(RoleIssuer, "worker", mutate([]operationBinding{parent, child})); err == nil {
			t.Fatal("invalid continuation graph was accepted")
		}
	}
	if selected, err := selectRoleBindings(RoleIssuer, "worker", []operationBinding{parent, child}); err != nil || len(selected) != 2 {
		t.Fatal("valid continuation edge was rejected")
	}
}
