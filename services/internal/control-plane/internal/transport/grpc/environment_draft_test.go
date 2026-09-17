package grpc

import (
	"context"
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
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
	result := command.Result{RuntimeEnvironment: &entity.RuntimeEnvironmentSet{Ref: "environment-test", Version: 1, CurrentVersion: entity.RuntimeEnvironmentVersion{Ref: "revision-test", Digest: "validated"}},
		RuntimeEnvironmentDraft: &entity.RuntimeEnvironmentDraft{Ref: "draft-test", State: "PUBLISHED", Version: 2, ValidationDigest: "validated", PublishedEnvironmentRef: "environment-test"},
		RevisionImpactPlan:      &entity.RevisionImpactPlan{Ref: "plan-test", Digest: "plan-digest", Kind: "RUNTIME_ENVIRONMENT", State: "APPLIED", Version: 2, DraftRef: "draft-test", DraftVersion: 1, TargetDigest: "validated", PublishedRevisionRef: "revision-test"}}
	response, err := castPublishedEnvironmentDraft(result)
	if err != nil || response.Environment.Ref != "environment-test" {
		t.Fatalf("complete owner result: %v", err)
	}
	result.RevisionImpactPlan.PublishedRevisionRef = "other"
	if response, err := castPublishedEnvironmentDraft(result); response != nil || status.Code(err) != codes.Internal {
		t.Fatal("mismatched plan effect accepted")
	}
}

func TestEnvironmentDraftPreservesAbsentPolicy(t *testing.T) {
	draft := castEnvironmentDraft(&entity.RuntimeEnvironmentDraft{})
	if draft.Specification.Policy != nil {
		t.Fatal("absent policy was materialized")
	}
	spec, err := domainEnvironmentDraftSpecification(draft.Specification)
	if err != nil || !reflect.DeepEqual(spec.Policy, runtimecontract.RuntimeEnvironmentPolicy{}) {
		t.Fatalf("absent policy round trip: %v", err)
	}
}

func TestEnvironmentDraftRejectsInvalidPolicyAsInvalidArgument(t *testing.T) {
	server := &Server{}
	request := &controlplanev1.CreateRuntimeEnvironmentDraftRequest{Specification: &controlplanev1.RuntimeEnvironmentDraftSpecification{
		Policy: &controlplanev1.RuntimeEnvironmentPolicyInput{
			Resources:        &controlplanev1.RuntimeResourcePolicy{},
			KubernetesAccess: controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_UNSPECIFIED,
		},
	}}
	if _, err := server.CreateRuntimeEnvironmentDraft(context.Background(), request); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid policy returned %v", status.Code(err))
	}
	if _, err := server.SaveRuntimeEnvironmentDraft(context.Background(), &controlplanev1.SaveRuntimeEnvironmentDraftRequest{Specification: request.Specification}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid saved policy returned %v", status.Code(err))
	}
}
