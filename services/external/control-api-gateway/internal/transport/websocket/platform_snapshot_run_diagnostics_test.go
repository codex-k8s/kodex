package websockettransport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// runSnapshotStageRecorder simulates run/gate/overview owner reads with one deadline, recording stage/page order and optionally injecting transport or typed/projection failures.
// runSnapshotStageRecorder имитирует чтения запусков, подтверждений и обзора с единым сроком, запоминает этапы и страницы и при необходимости подставляет транспортный либо типизированный отказ проекции.
type runSnapshotStageRecorder struct {
	grpc.ClientConnInterface
	deadline          time.Time
	stages            []platformSnapshotReadStage
	failStage         platformSnapshotReadStage
	invalidProjection platformSnapshotReadStage
	invalidTyped      bool
	failure           error
	pages             []int32
}

// Invoke rejects a changed deadline, records the response-selected stage and requested page size, injects configured malformed values or errors and denies unrelated reads without network access.
// Invoke отклоняет изменённый срок, записывает этап по типу ответа и размер страницы, подставляет настроенные неверные значения или ошибки и запрещает посторонние чтения без сети.
func (c *runSnapshotStageRecorder) Invoke(ctx context.Context, _ string, request, response any, _ ...grpc.CallOption) error {
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.Equal(c.deadline) {
		return errors.New("run stage changed its aggregate parent deadline")
	}
	var stage platformSnapshotReadStage
	switch out := response.(type) {
	case *cp.ListRunsResponse:
		stage = platformSnapshotRunsListStage
		c.pages = append(c.pages, request.(*cp.ListRunsRequest).Page.PageSize)
		out.Page = &cp.PageInfo{}
		if c.invalidProjection == stage {
			run := completeRunSnapshotFixture().Run
			run.Title = string([]byte{0xff})
			out.Runs, out.Total = []*cp.Run{run}, 1
		}
		if c.invalidTyped {
			run := completeRunSnapshotFixture().Run
			run.State = cp.RunState(999)
			out.Runs, out.Total = []*cp.Run{run}, 1
		}
	case *cp.ListOwnerGatesResponse:
		stage = platformSnapshotOwnerGatesListStage
		c.pages = append(c.pages, request.(*cp.ListOwnerGatesRequest).Page.PageSize)
		out.Page = &cp.PageInfo{}
		if c.invalidProjection == stage {
			out.Gates = []*cp.OwnerGate{{Ref: string([]byte{0xff})}}
		}
	case *cp.GetOverviewResponse:
		stage = platformSnapshotOverviewGetStage
		out.Overview = &cp.Overview{}
		if c.invalidProjection == stage {
			run := completeRunSnapshotFixture().Run
			run.Title = string([]byte{0xff})
			out.Overview.ActiveRuns = []*cp.Run{run}
		}
	default:
		return status.Error(codes.PermissionDenied, "synthetic unrelated read denied")
	}
	c.stages = append(c.stages, stage)
	if stage == c.failStage {
		return c.failure
	}
	return nil
}

// runSnapshotStageFixture creates a bounded parent context with test cleanup, a fake query client and buffered outbound queue at a fixed synthetic project/cursor.
// runSnapshotStageFixture создаёт ограниченный родительский контекст с тестовой очисткой, фиктивный query client и буфер исходящих кадров для фиксированных тестовых проекта и курсора.
func runSnapshotStageFixture(t *testing.T, c *runSnapshotStageRecorder) (*sessionMultiplexer, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), heartbeatWakeTimeout)
	t.Cleanup(cancel)
	c.deadline, _ = ctx.Deadline()
	m := &sessionMultiplexer{ctx: t.Context(), server: &Server{query: cp.NewPlatformQueryServiceClient(c)},
		projectRef: "prj_fixture01", platformCursor: 123, localize: func(code string) string { return code },
		outbound: make(chan outboundFrame, 8)}
	return m, ctx
}

// TestPlatformSnapshotRunRPCStagesPreserveMappingAndPrivacy tests each owner RPC against four status failures, preserving stage/order/status and page size without partial frames or cursor changes; permission denial stays silent.
// TestPlatformSnapshotRunRPCStagesPreserveMappingAndPrivacy проверяет каждый owner RPC с четырьмя отказами статуса, сохраняя этап, порядок, статус и страницу без частичных кадров и изменения курсора; отказ в разрешении не логируется.
func TestPlatformSnapshotRunRPCStagesPreserveMappingAndPrivacy(t *testing.T) {
	stages := []platformSnapshotReadStage{platformSnapshotRunsListStage, platformSnapshotOwnerGatesListStage, platformSnapshotOverviewGetStage}
	for index, stage := range stages {
		for _, code := range []codes.Code{codes.DeadlineExceeded, codes.Unavailable, codes.PermissionDenied, codes.Canceled} {
			t.Run(string(stage)+"/"+code.String(), func(t *testing.T) {
				cause := status.Error(code, "PRIVATE_SENTINEL")
				c := &runSnapshotStageRecorder{failStage: stage, failure: cause}
				m, ctx := runSnapshotStageFixture(t, c)
				rows := snapshotDiagnosticLogs(t, func() {
					_, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindRun})
					if !errors.Is(err, cause) || status.Code(err) != code || platformSnapshotErrorStage("RUN", err) != string(stage) {
						t.Fatal("run stage changed the owner error or failed to identify its RPC")
					}
				})
				wantRows := 1
				if code == codes.PermissionDenied {
					wantRows = 0
				}
				if !reflect.DeepEqual(c.stages, stages[:index+1]) || len(rows) != wantRows || len(m.outbound) != 0 || m.platformCursor != 123 || ctx.Err() != nil {
					t.Fatal("run attribution retried, published partial data or changed cancellation/cursor")
				}
				if wantRows == 1 && (len(rows[0]) != 14 || rows[0][platformSnapshotReadStageKey] != string(stage) ||
					rows[0][runSnapshotDiagnosticGRPCCodeKey] != code.String() || rows[0][platformSnapshotAttemptKey] != float64(0) || rows[0][platformSnapshotAssistantPageKey] != float64(0)) {
					t.Fatal("run failure changed the exact single closed diagnostic shape")
				}
				for _, size := range c.pages {
					if size != 50 {
						t.Fatal("assistant cold page changed the unrelated run page")
					}
				}
			})
		}
	}
}

// TestPlatformSnapshotRunProjectionStageIsClosedAndSingle injects malformed run, gate or overview fields and requires one closed run-projection diagnostic without later RPCs or partial publication.
// TestPlatformSnapshotRunProjectionStageIsClosedAndSingle подставляет неверные поля запуска, подтверждения или обзора и требует одну закрытую диагностику проекции без последующих RPC и частичной публикации.
func TestPlatformSnapshotRunProjectionStageIsClosedAndSingle(t *testing.T) {
	for index, stage := range []platformSnapshotReadStage{platformSnapshotRunsListStage, platformSnapshotOwnerGatesListStage, platformSnapshotOverviewGetStage} {
		t.Run(string(stage), func(t *testing.T) {
			c := &runSnapshotStageRecorder{invalidProjection: stage}
			m, ctx := runSnapshotStageFixture(t, c)
			rows := snapshotDiagnosticLogs(t, func() {
				_, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindRun})
				if err == nil || platformSnapshotErrorStage("RUN", err) != string(platformSnapshotRunProjectStage) {
					t.Fatal("malformed run projection was accepted or lost its closed stage")
				}
			})
			if len(c.stages) != index+1 || len(rows) != 1 || len(rows[0]) != 14 ||
				rows[0][platformSnapshotReadStageKey] != string(platformSnapshotRunProjectStage) || len(m.outbound) != 0 || m.platformCursor != 123 {
				t.Fatal("projection failure leaked an error, duplicated diagnostics or emitted partial data")
			}
		})
	}
}

// TestPlatformSnapshotRunSuccessStillReadsEveryOwnerRPC repeats successful snapshot reads and independently checks all three owner RPCs run again in order with no failure logs or parent cancellation.
// TestPlatformSnapshotRunSuccessStillReadsEveryOwnerRPC повторяет успешные чтения и независимо проверяет повтор всех трёх owner RPC в прежнем порядке без логов отказа и отмены родителя.
func TestPlatformSnapshotRunSuccessStillReadsEveryOwnerRPC(t *testing.T) {
	c := &runSnapshotStageRecorder{}
	m, ctx := runSnapshotStageFixture(t, c)
	rows := snapshotDiagnosticLogs(t, func() {
		for range 2 {
			value, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindRun})
			if err != nil || value.Snapshot.Catalog == nil || value.Snapshot.Overview == nil {
				t.Fatal("successful run bundle lost a complete owner projection")
			}
		}
	})
	want := []platformSnapshotReadStage{platformSnapshotRunsListStage, platformSnapshotOwnerGatesListStage, platformSnapshotOverviewGetStage,
		platformSnapshotRunsListStage, platformSnapshotOwnerGatesListStage, platformSnapshotOverviewGetStage}
	if !reflect.DeepEqual(c.stages, want) || len(rows) != 0 || ctx.Err() != nil {
		t.Fatal("run stage attribution reused data, altered RPC ordering or logged success as failure")
	}
}

// TestPlatformSnapshotRunTypedProjectionFailureHasOneClosedStage injects an unknown typed run state and requires the existing invalid-snapshot error and one closed diagnostic after all owner reads, without frames or cursor advancement.
// TestPlatformSnapshotRunTypedProjectionFailureHasOneClosedStage подставляет неизвестное типизированное состояние запуска и требует прежнюю ошибку неверного снимка и одну закрытую запись после чтений без кадров и продвижения курсора.
func TestPlatformSnapshotRunTypedProjectionFailureHasOneClosedStage(t *testing.T) {
	c := &runSnapshotStageRecorder{invalidTyped: true}
	m, ctx := runSnapshotStageFixture(t, c)
	rows := snapshotDiagnosticLogs(t, func() {
		_, err := m.boundedPlatformSnapshotWithin(ctx, generated.PlatformSnapshotEnvelope{Kind: generated.PlatformResourceKindRun})
		if !errors.Is(err, errPlatformSnapshotInvalid) {
			t.Fatal("typed run projection changed its existing closed error")
		}
	})
	if len(c.stages) != 3 || len(rows) != 1 || len(rows[0]) != 14 || rows[0]["msg"] != platformSnapshotReadFailure ||
		rows[0][platformSnapshotReadStageKey] != string(platformSnapshotRunProjectStage) || len(m.outbound) != 0 || m.platformCursor != 123 {
		t.Fatal("typed projection emitted partial data or lost the single closed failure stage")
	}
}
