package grpc

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestPublishedEnvironmentDraftRejectsIncompleteOwnerResult(t *testing.T) {
	for _, result := range []command.Result{
		{},
		{RuntimeEnvironmentDraft: &entity.RuntimeEnvironmentDraft{State: "PUBLISHED"}},
		{RuntimeEnvironment: &entity.RuntimeEnvironmentSet{Ref: "environment-test"}},
		{RuntimeEnvironment: &entity.RuntimeEnvironmentSet{Ref: "environment-test"}, RuntimeEnvironmentDraft: &entity.RuntimeEnvironmentDraft{State: "PUBLISHED", PublishedEnvironmentRef: "other"}},
	} {
		response, err := castPublishedEnvironmentDraft(result)
		if response != nil || status.Code(err) != codes.Internal {
			t.Fatalf("incomplete owner result was accepted: %v", err)
		}
	}
	response, err := castPublishedEnvironmentDraft(command.Result{RuntimeEnvironment: &entity.RuntimeEnvironmentSet{Ref: "environment-test"},
		RuntimeEnvironmentDraft: &entity.RuntimeEnvironmentDraft{State: "PUBLISHED", PublishedEnvironmentRef: "environment-test"}})
	if err != nil || response.Environment.Ref != "environment-test" {
		t.Fatalf("complete owner result: %v", err)
	}
}
