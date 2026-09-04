package grpc

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestEnvironmentDraftPolicyRoundTrip(t *testing.T) {
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	draft := castEnvironmentDraft(&entity.RuntimeEnvironmentDraft{
		Specification: entity.RuntimeEnvironmentDraftSpecification{Policy: policy},
	})
	spec, err := domainEnvironmentDraftSpecification(draft.Specification)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.Policy, policy) {
		t.Fatalf("policy changed through draft response: got %#v, want %#v", spec.Policy, policy)
	}
}
