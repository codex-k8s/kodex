package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type claimNextReplayRPC struct {
	controlplanev1.ControlPlaneServiceClient
	claimError         error
	claimCalls         int
	materializeCalls   int
	processRunEffects  int
	materializationKey string
	materialized       bool
	claimDisposition   controlplanev1.ScheduleOccurrenceClaimDisposition
}

func (rpc *claimNextReplayRPC) ClaimScheduleOccurrence(
	_ context.Context,
	_ *controlplanev1.ClaimScheduleOccurrenceRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.ClaimScheduleOccurrenceResponse, error) {
	rpc.claimCalls++
	if rpc.claimError != nil {
		return nil, rpc.claimError
	}
	disposition := controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_RESERVED
	if rpc.claimDisposition != controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_UNSPECIFIED {
		disposition = rpc.claimDisposition
	} else if rpc.materialized {
		disposition = controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_MATERIALIZED
	}
	return &controlplanev1.ClaimScheduleOccurrenceResponse{
		Occurrence: &controlplanev1.ScheduleOccurrence{
			OccurrenceId: "occurrence-rejoin", Attempt: 1,
		},
		MaterializationCapability: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		MaterializationIdempotencyKey: "owner-materialization-key",
		ProjectId:                     "project-rejoin",
		CapabilityExpiresAt:           timestamppb.New(time.Now().UTC().Add(time.Minute)),
		Disposition:                   disposition,
	}, nil
}

func TestClaimNextMapsOnlyTypedRetiredDisposition(t *testing.T) {
	rpc := &claimNextReplayRPC{
		claimDisposition: controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_RETIRED,
	}
	client := &Client{
		shared: &sharedclient.Client{ControlPlane: rpc}, rpcDeadline: time.Second,
	}
	if _, err := client.ClaimNext(context.Background(), "retired-key"); !errors.Is(err, ErrClaimRetired) {
		t.Fatalf("typed retirement was not propagated: %v", err)
	}
	if rpc.materializeCalls != 0 {
		t.Fatal("retired claim attempted to use a capability")
	}
}

func (rpc *claimNextReplayRPC) MaterializeScheduleOccurrence(
	_ context.Context,
	request *controlplanev1.MaterializeScheduleOccurrenceRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.MaterializeScheduleOccurrenceResponse, error) {
	rpc.materializeCalls++
	if rpc.materializationKey == "" {
		rpc.materializationKey = request.GetIdempotencyKey()
	} else if rpc.materializationKey != request.GetIdempotencyKey() {
		return nil, status.Error(codes.AlreadyExists, "materialization semantic key changed")
	}
	if !rpc.materialized {
		rpc.materialized = true
		rpc.processRunEffects++
	}
	if rpc.materializeCalls <= 2 {
		return nil, status.Error(codes.DeadlineExceeded, "committed response is unknown")
	}
	return &controlplanev1.MaterializeScheduleOccurrenceResponse{
		Occurrence: &controlplanev1.ScheduleOccurrence{
			OccurrenceId: "occurrence-rejoin", Attempt: 1,
			LeaseExpiresAt: timestamppb.New(time.Now().UTC().Add(time.Minute)),
		},
		CompletionCapability: "completion-capability",
	}, nil
}

func TestClaimNextRejoinsCommittedMaterializationAcrossCycles(t *testing.T) {
	rpc := &claimNextReplayRPC{}
	client := &Client{
		shared: &sharedclient.Client{ControlPlane: rpc}, rpcDeadline: time.Second,
	}
	key := "claim-semantic-key"
	if _, err := client.ClaimNext(context.Background(), key); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("first cycle did not retain unknown outcome: %v", err)
	}
	claim, err := client.ClaimNext(context.Background(), key)
	if err != nil {
		t.Fatalf("second cycle did not rejoin committed materialization: %v", err)
	}
	if claim.ProjectID != "project-rejoin" || claim.OccurrenceID != "occurrence-rejoin" ||
		claim.Attempt != 1 || claim.LeaseToken != "completion-capability" {
		t.Fatalf("rejoin returned a different exact result: %+v", claim)
	}
	if rpc.claimCalls != 2 || rpc.materializeCalls != 3 || rpc.processRunEffects != 1 {
		t.Fatalf("composite replay repeated graph effect: claims=%d materializations=%d process_runs=%d",
			rpc.claimCalls, rpc.materializeCalls, rpc.processRunEffects)
	}
	wantKey := "owner-materialization-key"
	if rpc.materializationKey != wantKey {
		t.Fatalf("materialization semantic key changed: got %q want %q", rpc.materializationKey, wantKey)
	}
}

func TestClaimNextDoesNotRetirePermissionOrIdempotencyMismatch(t *testing.T) {
	for _, test := range []struct {
		name string
		code codes.Code
	}{
		{name: "permission", code: codes.PermissionDenied},
		{name: "idempotency", code: codes.AlreadyExists},
	} {
		t.Run(test.name, func(t *testing.T) {
			rpc := &claimNextReplayRPC{claimError: status.Error(test.code, "closed mismatch")}
			client := &Client{
				shared: &sharedclient.Client{ControlPlane: rpc}, rpcDeadline: time.Second,
			}
			_, err := client.ClaimNext(context.Background(), "stable-key")
			if status.Code(err) != test.code || errors.Is(err, ErrClaimRetired) || errors.Is(err, ErrNoWork) {
				t.Fatalf("mismatch became retired/empty success: %v", err)
			}
		})
	}
}
