package kubernetesoperation

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestOperationsRoutesOnlyFreshOwnerFamilies exercises both real consumers without a caller-supplied selector.
// TestOperationsRoutesOnlyFreshOwnerFamilies проверяет оба real consumers без caller-supplied selector.
func TestOperationsRoutesOnlyFreshOwnerFamilies(t *testing.T) {
	for _, variant := range []string{"resource", "stream", "expired", "missing-binding"} {
		t.Run(variant, func(t *testing.T) {
			_, resourceOwner, _, plan, prepared := resourceExecutorFixture(t)
			plan.Children[0].State = "READY"
			planned := &resourcePlanFixture{resourceOwnerFixture: resourceOwner, plan: plan}
			resourceFactory := &resourceFactoryFixture{prepared: prepared}
			resources, err := NewResourceOperations(planned, resourceFactory)
			if err != nil {
				t.Fatal(err)
			}
			streams, streamOwner, native, streamBinding := streamOperationsFixture(t)
			operations, err := NewOperations(planned, resources, streams)
			if err != nil {
				t.Fatal(err)
			}
			binding := resourceOwner.binding
			if variant == "stream" {
				operations.owner, binding = streamOwner, streamBinding
			}
			if variant == "expired" {
				resourceOwner.snapshot.ExpiresAt = time.Now().Add(-time.Second)
			}
			if variant == "missing-binding" {
				binding = managedkubernetes.Binding{}
			}
			result, err := operations.Execute(t.Context(), binding)
			switch variant {
			case "resource":
				if err != nil || result.Effect.Result == nil || result.Stream != nil || prepared.executes != 1 || native.opens != 0 {
					t.Fatal("resource dispatched to wrong consumer", err)
				}
			case "stream":
				if err != nil || result.Stream == nil || result.Effect.Result != nil || native.opens != 1 || prepared.executes != 0 {
					t.Fatal("stream dispatched to wrong consumer", err)
				}
			default:
				if err == nil || native.opens != 0 || resourceFactory.calls != 0 {
					t.Fatal("invalid owner routing reached private execution")
				}
			}
		})
	}
}

// TestOperationsCancellationNeverFallsBack rejects an already canceled routing request before target effects.
// TestOperationsCancellationNeverFallsBack отклоняет canceled routing request до target effects без fallback.
func TestOperationsCancellationNeverFallsBack(t *testing.T) {
	streams, owner, native, binding := streamOperationsFixture(t)
	_, resourceOwner, _, plan, prepared := resourceExecutorFixture(t)
	resources, err := NewResourceOperations(&resourcePlanFixture{resourceOwnerFixture: resourceOwner, plan: plan}, &resourceFactoryFixture{prepared: prepared})
	if err != nil {
		t.Fatal(err)
	}
	operations, err := NewOperations(owner, resources, streams)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := operations.Execute(ctx, binding)
	if err == nil || result.Stream != nil || native.opens != 0 || prepared.executes != 0 {
		t.Fatal("canceled routing performed an effect")
	}
}
