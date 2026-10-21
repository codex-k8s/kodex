package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	schedulerobservability "github.com/codex-k8s/kodex/services/jobs/automation-scheduler/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// schedulerStub records owner RPC requests and returns a controlled claim, renewal or failure
// so tests can inspect exact lease and idempotency behavior without a live control plane.
// RU: schedulerStub записывает RPC к владельцу и возвращает управляемые claim, продление
// или отказ для проверки точной аренды и идемпотентности без живого control plane.
type schedulerStub struct {
	claims           []*pb.ScheduleClaim
	materializeError error
	renewError       error
	materialized     []*pb.MaterializeScheduleOccurrenceRequest
	failed           []*pb.FailScheduleOccurrenceRequest
	claimLimit       int32
}

// TestTechnicalServerCancellationJoinsWorker verifies that cancellation shuts down the
// technical listener and joins its serving goroutine within a bounded time.
// RU: TestTechnicalServerCancellationJoinsWorker проверяет, что отмена останавливает
// технический listener и ограниченно дожидается его goroutine.
func TestTechnicalServerCancellationJoinsWorker(t *testing.T) {
	server, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0", ReadHeaderTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second,
		MaximumHeaderBytes: 16 << 10, MaximumConnections: 16}, serviceruntime.NewReadiness(), http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serveTechnical(server)(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("technical worker did not join")
	}
}

// ClaimDueSchedules consumes at most one queued synthetic claim and records the requested limit.
// RU: ClaimDueSchedules извлекает не более одного синтетического claim и сохраняет запрошенный лимит.
func (stub *schedulerStub) ClaimDueSchedules(_ context.Context, request *pb.ClaimDueSchedulesRequest, _ ...grpc.CallOption) (*pb.ClaimDueSchedulesResponse, error) {
	stub.claimLimit = request.GetLimit()
	if len(stub.claims) == 0 {
		return &pb.ClaimDueSchedulesResponse{}, nil
	}
	claim := stub.claims[0]
	stub.claims = stub.claims[1:]
	return &pb.ClaimDueSchedulesResponse{Claims: []*pb.ScheduleClaim{claim}}, nil
}

// RenewScheduleOccurrence echoes the requested lease pins with a future expiry unless the
// fixture explicitly injects a renewal failure.
// RU: RenewScheduleOccurrence возвращает те же привязки аренды с будущим сроком, если
// фикстура явно не задаёт отказ продления.
func (stub *schedulerStub) RenewScheduleOccurrence(_ context.Context, request *pb.RenewScheduleOccurrenceRequest, _ ...grpc.CallOption) (*pb.RenewScheduleOccurrenceResponse, error) {
	if stub.renewError != nil {
		return nil, stub.renewError
	}
	return &pb.RenewScheduleOccurrenceResponse{Lease: &pb.WorkLease{Ref: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}, nil
}

// MaterializeScheduleOccurrence records the exact mutation request before returning the
// configured synthetic owner result.
// RU: MaterializeScheduleOccurrence сохраняет точный запрос изменения до возврата
// заданного синтетического результата владельца.
func (stub *schedulerStub) MaterializeScheduleOccurrence(_ context.Context, request *pb.MaterializeScheduleOccurrenceRequest, _ ...grpc.CallOption) (*pb.MaterializeScheduleOccurrenceResponse, error) {
	stub.materialized = append(stub.materialized, request)
	return &pb.MaterializeScheduleOccurrenceResponse{}, stub.materializeError
}

// FailScheduleOccurrence records the safe terminal report without performing an owner mutation.
// RU: FailScheduleOccurrence записывает безопасное сообщение об отказе без изменения состояния владельца.
func (stub *schedulerStub) FailScheduleOccurrence(_ context.Context, request *pb.FailScheduleOccurrenceRequest, _ ...grpc.CallOption) (*pb.FailScheduleOccurrenceResponse, error) {
	stub.failed = append(stub.failed, request)
	return &pb.FailScheduleOccurrenceResponse{}, nil
}

// scheduleClaimFixture builds a synthetic, fully pinned occurrence with the chosen lease
// generation and a short future expiry.
// RU: scheduleClaimFixture создаёт синтетический occurrence со всеми привязками,
// выбранным поколением аренды и небольшим будущим сроком.
func scheduleClaimFixture(generation int64) *pb.ScheduleClaim {
	digest := strings.Repeat("a", 64)
	return &pb.ScheduleClaim{Schedule: &pb.Schedule{Ref: "sch_fixture", Version: 1}, OccurrenceRef: "occ_fixture",
		ScheduledFor: timestamppb.Now(), InputDigest: digest, ScheduleRevisionRef: "srev_fixture", ScheduleRevision: 1,
		ScheduleRevisionDigest: digest, Attempt: int32(generation), TargetRef: "agt_fixture", TargetVersion: 1,
		TargetDigest: digest, AutomationTextDigest: digest, PromptInputsDigest: digest,
		Lease: &pb.WorkLease{Ref: "lea_fixture", Fence: "synthetic-fence", Generation: generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}
}

// TestSchedulerProcessesOneLeaseAtATimeAndPinsAttemptKey checks one-claim requests, distinct
// keys for different generations and a stable key when the same attempt is replayed.
// RU: TestSchedulerProcessesOneLeaseAtATimeAndPinsAttemptKey проверяет запросы по одному claim,
// разные ключи поколений и неизменный ключ при повторе той же попытки.
func TestSchedulerProcessesOneLeaseAtATimeAndPinsAttemptKey(t *testing.T) {
	config := Config{RPCDeadline: time.Second, DueLimit: 2, InstanceID: "replica"}
	stub := &schedulerStub{claims: []*pb.ScheduleClaim{scheduleClaimFixture(1), scheduleClaimFixture(2)}}
	count, err := materializeDue(t.Context(), stub, schedulerobservability.NewMetrics(), config)
	if err != nil || count != 2 || stub.claimLimit != 1 || len(stub.materialized) != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if stub.materialized[0].Mutation.IdempotencyKey == stub.materialized[1].Mutation.IdempotencyKey {
		t.Fatal("attempts shared an idempotency key")
	}
	replay := &schedulerStub{claims: []*pb.ScheduleClaim{scheduleClaimFixture(1)}}
	if _, err := materializeDue(t.Context(), replay, schedulerobservability.NewMetrics(), config); err != nil {
		t.Fatal(err)
	}
	if replay.materialized[0].Mutation.IdempotencyKey != stub.materialized[0].Mutation.IdempotencyKey {
		t.Fatal("same attempt changed idempotency key")
	}
}

// TestSchedulerFailureAndInvalidSnapshot verifies retry classification, rejection of missing
// snapshot provenance and refusal to materialize after a lost renewal.
// RU: TestSchedulerFailureAndInvalidSnapshot проверяет класс повтора, отказ при неполном
// происхождении снимка и запрет материализации после несостоявшегося продления.
func TestSchedulerFailureAndInvalidSnapshot(t *testing.T) {
	for _, test := range []struct {
		name      string
		code      codes.Code
		retryable bool
	}{
		{"unavailable", codes.Unavailable, true}, {"conflict", codes.Aborted, true},
		{"capacity", codes.ResourceExhausted, true}, {"denied", codes.PermissionDenied, false},
		{"invalid", codes.InvalidArgument, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &schedulerStub{claims: []*pb.ScheduleClaim{scheduleClaimFixture(1)}, materializeError: status.Error(test.code, "synthetic error")}
			count, err := materializeDue(t.Context(), stub, schedulerobservability.NewMetrics(), Config{RPCDeadline: time.Second, DueLimit: 1})
			if err == nil || count != 0 || len(stub.failed) != 1 || stub.failed[0].Retryable != test.retryable {
				t.Fatalf("failure not recorded: count=%d err=%v", count, err)
			}
		})
	}
	invalid := scheduleClaimFixture(1)
	invalid.PromptInputsDigest = ""
	stub := &schedulerStub{claims: []*pb.ScheduleClaim{invalid}}
	if _, err := materializeDue(t.Context(), stub, schedulerobservability.NewMetrics(), Config{RPCDeadline: time.Second, DueLimit: 1}); err == nil || len(stub.materialized) != 0 {
		t.Fatal("invalid snapshot was materialized")
	}
	stub = &schedulerStub{claims: []*pb.ScheduleClaim{scheduleClaimFixture(1)}, renewError: status.Error(codes.PermissionDenied, "synthetic error")}
	if _, err := materializeDue(t.Context(), stub, schedulerobservability.NewMetrics(), Config{RPCDeadline: time.Second, DueLimit: 1}); err == nil || len(stub.materialized) != 0 {
		t.Fatal("lost lease was materialized")
	}
}
