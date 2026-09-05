package app

import (
	"context"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sourceOwnerFixture struct {
	cp.ManagedConfigurationSourceWorkServiceClient
	work                        *cp.ManagedConfigurationSourceWork
	claims, completes, failures int
	complete                    *cp.CompleteManagedConfigurationSourceWorkRequest
	claimErr, completeErr       error
	foreign                     bool
	state                       cp.ManagedConfigurationSourceState
}

func (owner *sourceOwnerFixture) ClaimManagedConfigurationSourceWork(_ context.Context, request *cp.ClaimManagedConfigurationSourceWorkRequest, _ ...grpc.CallOption) (*cp.ClaimManagedConfigurationSourceWorkResponse, error) {
	owner.claims++
	if request.GetLimit() != 1 || request.GetClaimant() != "fixture" {
		return nil, errors.New("fixture claim changed")
	}
	return &cp.ClaimManagedConfigurationSourceWorkResponse{Work: []*cp.ManagedConfigurationSourceWork{owner.work}}, owner.claimErr
}

func (owner *sourceOwnerFixture) receipt() *cp.ManagedConfigurationGitSource {
	work := owner.work
	ref := work.GetSourceRef()
	if owner.foreign {
		ref = "csource_foreign"
	}
	return &cp.ManagedConfigurationGitSource{Ref: ref, Version: 1, Generation: work.GetLease().GetSourceGeneration(), ConnectionRef: work.GetConnectionRef(), ProviderKey: work.GetDefinitionKey(), RepositoryRef: work.GetRepositoryRef(), RefName: work.GetRefName(), Path: work.GetPath(), State: owner.state, AcceptedCommitSha: "fixture-commit", AcceptedContentSha256: "fixture-digest"}
}

func (owner *sourceOwnerFixture) CompleteManagedConfigurationSourceWork(_ context.Context, request *cp.CompleteManagedConfigurationSourceWorkRequest, _ ...grpc.CallOption) (*cp.CompleteManagedConfigurationSourceWorkResponse, error) {
	owner.completes++
	owner.complete = proto.Clone(request).(*cp.CompleteManagedConfigurationSourceWorkRequest)
	return &cp.CompleteManagedConfigurationSourceWorkResponse{Source: owner.receipt()}, owner.completeErr
}

func (owner *sourceOwnerFixture) FailManagedConfigurationSourceWork(_ context.Context, request *cp.FailManagedConfigurationSourceWorkRequest, _ ...grpc.CallOption) (*cp.FailManagedConfigurationSourceWorkResponse, error) {
	owner.failures++
	if !proto.Equal(request.GetLease(), owner.work.GetLease()) || request.GetFailureCode() == cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNSPECIFIED {
		return nil, errors.New("fixture failure binding changed")
	}
	return &cp.FailManagedConfigurationSourceWorkResponse{Source: owner.receipt()}, nil
}

type sourceAdapterFixture struct {
	calls   int
	raw     []byte
	failure bool
	cancel  context.CancelFunc
}

func (adapter *sourceAdapterFixture) ReadConfigurationSource(context.Context, *cp.ManagedConfigurationSourceWork) (integration.ConfigurationSourceResult, error) {
	adapter.calls++
	if adapter.cancel != nil {
		adapter.cancel()
	}
	if adapter.failure {
		return integration.ConfigurationSourceResult{}, errors.New("fixture source read unavailable")
	}
	return integration.ConfigurationSourceResult{Content: adapter.raw, CommitSHA: "fixture-commit", ContentSHA256: "fixture-digest", Ancestry: cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_INITIAL}, nil
}

func TestSourceWorkPreservesClaimAndDoesNotRetryLostACK(t *testing.T) {
	for _, mode := range []string{"complete", "invalid_candidate", "lost_ack", "failure", "retry_queued", "foreign_receipt", "nonterminal_receipt", "false_ready", "expired", "claim_denied", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			lease := &cp.ManagedConfigurationSourceLease{WorkRef: "cwork_fixture", SourceGeneration: 2, Attempt: 3, ClaimGeneration: 4, Claimant: "fixture", Fence: "fixture-fence", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
			owner := &sourceOwnerFixture{work: &cp.ManagedConfigurationSourceWork{Lease: lease, Deadline: timestamppb.New(time.Now().Add(time.Minute)), SourceRef: "csource_fixture", ConnectionRef: "int_fixture", DefinitionKey: "github", RepositoryRef: "fixture/repo", RefName: "main", Path: "recipe.json"}}
			adapter := &sourceAdapterFixture{raw: []byte("synthetic-configuration")}
			owner.state = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY
			switch mode {
			case "invalid_candidate":
				owner.state = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED
			case "lost_ack":
				owner.completeErr = errors.New("fixture acknowledgement lost")
			case "failure":
				adapter.failure = true
				owner.state = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED
			case "retry_queued":
				adapter.failure = true
				owner.state = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED
			case "nonterminal_receipt":
				owner.state = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_CLAIMED
			case "false_ready":
				adapter.failure = true
			case "foreign_receipt":
				owner.foreign = true
			case "expired":
				lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "claim_denied":
				owner.claimErr = errors.New("fixture authority rejected")
			case "cancel":
				adapter.cancel = cancel
			}
			metrics, err := businessmetrics.New(observability.NewMetrics("integration_source_fixture", "test", map[string]string{}))
			if err != nil {
				t.Fatal(err)
			}
			processed, err := processConfigurationSourceWork(ctx, owner, adapter, metrics, Config{InstanceID: "fixture", RequestTimeout: time.Second, OperationTimeout: 10 * time.Second})
			wantSuccess := mode == "complete" || mode == "invalid_candidate" || mode == "failure" || mode == "retry_queued"
			if (err == nil) != wantSuccess || wantSuccess && processed != 1 || owner.claims != 1 {
				t.Fatal("source lifecycle outcome was hidden")
			}
			if mode == "expired" || mode == "claim_denied" {
				if adapter.calls != 0 || owner.completes != 0 || owner.failures != 0 {
					t.Fatal("rejected source claim reached provider")
				}
			} else if adapter.failure {
				if owner.failures != 1 || owner.completes != 0 {
					t.Fatal("source failure was not reported exactly once")
				}
			} else if mode == "cancel" {
				if owner.completes != 0 || owner.failures != 0 {
					t.Fatal("cancelled source was finalized")
				}
			} else if owner.completes != 1 || owner.failures != 0 || !proto.Equal(owner.complete.GetLease(), lease) || string(owner.complete.GetContent()) != "synthetic-configuration" {
				t.Fatal("source completion changed or retried claim")
			}
			if adapter.calls > 0 && !adapter.failure {
				for _, value := range adapter.raw {
					if value != 0 {
						t.Fatal("source bytes survived completion")
					}
				}
			}
		})
	}
}

func TestWorkHealthRequiresFreshSuccessfulOwnerCycle(t *testing.T) {
	now := time.Now()
	health := &workCycleHealth{}
	if health.ready(now, time.Minute) {
		t.Fatal("unobserved owner work is ready")
	}
	health.record(now, nil)
	if !health.ready(now.Add(time.Second), time.Minute) || health.ready(now.Add(2*time.Minute), time.Minute) || health.ready(now.Add(-time.Second), time.Minute) {
		t.Fatal("owner observation freshness is invalid")
	}
	health.record(now, errors.New("fixture owner unavailable"))
	if health.ready(now, time.Minute) {
		t.Fatal("failed owner work remained ready")
	}
}
