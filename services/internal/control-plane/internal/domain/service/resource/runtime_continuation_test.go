package resource

import (
	"errors"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestRuntimeResourcePolicyClosedCapabilities(t *testing.T) {
	tests := []struct {
		name           string
		capabilities   []string
		resourceClass  string
		clusterProfile string
	}{
		{name: "default", resourceClass: "STANDARD", clusterProfile: "NONE"},
		{
			name:          "high memory read only",
			capabilities:  []string{"runtime.resource.high-memory", "runtime.cluster.read"},
			resourceClass: "HIGH_MEMORY", clusterProfile: "PROJECT_READ_ONLY",
		},
		{
			name: "accelerated operator",
			capabilities: []string{
				"runtime.resource.high-memory", "runtime.resource.accelerated",
				"runtime.cluster.read", "runtime.cluster.workload-operator",
			},
			resourceClass: "ACCELERATED", clusterProfile: "PROJECT_WORKLOAD_OPERATOR",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceClass, clusterProfile := runtimeResourcePolicy(entity.RoleSpec{
				Capabilities: test.capabilities,
			})
			if resourceClass != test.resourceClass || clusterProfile != test.clusterProfile {
				t.Fatalf("unexpected runtime policy: %s/%s", resourceClass, clusterProfile)
			}
		})
	}
}

func TestRuntimeMutationRejectsStaleFenceAndAuthority(t *testing.T) {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	execution := RuntimeExecution{
		TurnID: "3ed0d109-5eba-4e4e-8b98-f755f6e6fc6b", Attempt: 2,
		ImmutableInputSHA256: digest, WorkloadID: "runtime-controller",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		GrantGeneration:  7, Version: 4, Fence: 9, State: "RUNNING",
	}
	principal := value.Principal{
		CallerWorkload: "runtime-controller", CallerSPIFFEID: execution.WorkloadSPIFFEID,
		AuthorityReference: execution.TurnID, AuthorityRevision: 2,
		AuthorityDigest: digest, AuthorityGrantGeneration: 7,
	}
	input := RuntimeExecutionInput{
		Principal: principal, ExpectedVersion: 4, ExpectedFence: 9,
		ExpectedGrantGeneration: 7,
	}
	if err := matchRuntimeMutation(execution, input, "RUNNING"); err != nil {
		t.Fatalf("exact mutation rejected: %v", err)
	}

	staleFence := input
	staleFence.ExpectedFence = 8
	if err := matchRuntimeMutation(execution, staleFence); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale fence returned %v", err)
	}

	staleGrant := input
	staleGrant.Principal.AuthorityGrantGeneration = 6
	if err := matchRuntimeMutation(execution, staleGrant); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("stale authority returned %v", err)
	}

	foreignSPIFFE := input
	foreignSPIFFE.Principal.CallerSPIFFEID = "spiffe://mattercodex.local/ns/foreign/sa/runtime-controller"
	if err := matchRuntimeMutation(execution, foreignSPIFFE); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign SPIFFE returned %v", err)
	}
}

func TestIntegrationGatewayBindingIsExact(t *testing.T) {
	digest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	continuation := IntegrationContinuation{
		TurnID: "a189a33f-fea7-4d20-96f0-b5a05c6a5c5c", Attempt: 3,
		ImmutableInputSHA256: digest, GrantGeneration: 11,
	}
	principal := value.Principal{
		AuthorityReference: continuation.TurnID, AuthorityRevision: 3,
		AuthorityDigest: digest, AuthorityGrantGeneration: 11,
	}
	if err := matchIntegrationGateway(continuation, principal); err != nil {
		t.Fatalf("exact integration binding rejected: %v", err)
	}
	principal.AuthorityDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := matchIntegrationGateway(continuation, principal); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("changed request tuple returned %v", err)
	}
}

func TestRuntimeExpiryIsClosedTurnTransition(t *testing.T) {
	for _, state := range []enum.State{enum.StateClaimed, enum.StateRunning} {
		if !enum.TransitionAllowed(enum.KindTurn, state, enum.StateExpired) {
			t.Fatalf("runtime expiry transition from %s is unavailable", state)
		}
	}
	if enum.TransitionAllowed(enum.KindTurn, enum.StateWaitingExternal, enum.StateExpired) {
		t.Fatal("suspended integration turn must not expire through runtime path")
	}
}
