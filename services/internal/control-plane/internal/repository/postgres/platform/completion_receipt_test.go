package platform

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// completionCallbackKey marks only the two independent synthetic callbacks, never authority.
// completionCallbackKey отмечает только два независимых synthetic callback, не authority.
type completionCallbackKey struct{}

// completionStageKey carries a closed query label without SQL text or arguments.
// completionStageKey передаёт закрытый query label без SQL либо arguments.
type completionStageKey struct{}

// completionTraceEvent stores bounded ordering evidence without identifiers, content or errors.
// completionTraceEvent хранит bounded ordering evidence без identifiers, content либо errors.
type completionTraceEvent struct {
	Callback, Stage, Phase string
	Sequence               int
	Rows                   int64
	Failed                 bool
}

// completionReceiptTrace holds A after its main transaction locks the lease. B releases the
// test window either after the old missing-receipt read or before the fixed advisory-lock wait.
// No scheduling branch changes production state or lets an invalid execution bypass a guard.
// completionReceiptTrace удерживает A после lease lock основной транзакции. B открывает тестовое
// окно после старого missing-receipt read либо перед ожиданием исправленной advisory lock.
// Выбор расписания не меняет production state и не разрешает обход execution guard.
type completionReceiptTrace struct {
	mu                        sync.Mutex
	sequence, aLeaseReads     int
	depth                     map[string]int
	events                    []completionTraceEvent
	aHeld, bWindow, releaseA  chan struct{}
	aOnce, bOnce, releaseOnce sync.Once
	windowMode                string
}

// newCompletionReceiptTrace constructs bounded synchronization channels for one isolated race.
// newCompletionReceiptTrace создаёт bounded synchronization channels одной изолированной гонки.
func newCompletionReceiptTrace() *completionReceiptTrace {
	return &completionReceiptTrace{depth: map[string]int{}, aHeld: make(chan struct{}), bWindow: make(chan struct{}), releaseA: make(chan struct{})}
}

// completionQueryStage recognizes only the repository queries needed to prove receipt ordering.
// completionQueryStage распознаёт только repository queries для доказательства порядка receipt.
func completionQueryStage(sql string) string {
	fields := strings.Fields(strings.ToLower(sql))
	if len(fields) > 0 {
		switch fields[0] {
		case "begin":
			return "BEGIN"
		case "commit":
			return "COMMIT"
		case "rollback":
			return "ROLLBACK"
		}
	}
	header := strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0]
	for _, known := range []struct{ sql, stage string }{{queryCommandsExecuteLockIdempotencyScope, "IDEMPOTENCY_LOCK"}, {queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, "RECEIPT_READ"}, {queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef, "LEASE_READ"}, {queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, "RECEIPT_INSERT"}} {
		if header == strings.SplitN(strings.TrimSpace(known.sql), "\n", 2)[0] {
			return known.stage
		}
	}
	return ""
}

// record appends only closed stage metadata while bounding the diagnostic trace to 128 entries.
// record добавляет только закрытые stage metadata, ограничивая diagnostic trace 128 записями.
func (trace *completionReceiptTrace) record(callback, stage, phase string, rows int64, failed bool) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.sequence++
	if len(trace.events) < 128 {
		trace.events = append(trace.events, completionTraceEvent{Callback: callback, Stage: stage, Phase: phase, Sequence: trace.sequence, Rows: rows, Failed: failed})
	}
}

// openBWindow signals a verified stage reached by B without waiting for its blocked lock to finish.
// openBWindow отмечает проверенный stage B, не ожидая завершения его заблокированной lock.
func (trace *completionReceiptTrace) openBWindow(mode string) {
	trace.bOnce.Do(func() { trace.mu.Lock(); trace.windowMode = mode; trace.mu.Unlock(); close(trace.bWindow) })
}

// TraceQueryStart recognizes B's preflight advisory attempt; A already owns the same lock.
// TraceQueryStart распознаёт preflight advisory attempt B; A уже владеет той же lock.
func (trace *completionReceiptTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	callback, _ := ctx.Value(completionCallbackKey{}).(string)
	stage := completionQueryStage(data.SQL)
	if callback != "A" && callback != "B" {
		return ctx
	}
	if stage != "" {
		trace.record(callback, stage, "START", 0, false)
	}
	if callback == "B" && stage == "IDEMPOTENCY_LOCK" {
		trace.openBWindow("LOCK_BEFORE_RECEIPT")
	}
	return context.WithValue(ctx, completionStageKey{}, stage)
}

// TraceQueryEnd freezes A's second lease read, which belongs to its main owner transaction;
// old B must finish its absent-receipt read before A may commit. Context cancellation unblocks A.
// TraceQueryEnd удерживает второй lease read A в основной owner transaction;
// старый B должен закончить absent-receipt read до commit A. Отмена context освобождает A.
func (trace *completionReceiptTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	callback, _ := ctx.Value(completionCallbackKey{}).(string)
	stage, _ := ctx.Value(completionStageKey{}).(string)
	if (callback != "A" && callback != "B") || stage == "" {
		return
	}
	trace.record(callback, stage, "END", data.CommandTag.RowsAffected(), data.Err != nil)
	trace.mu.Lock()
	if data.Err == nil {
		switch stage {
		case "BEGIN":
			trace.depth[callback]++
		case "COMMIT", "ROLLBACK":
			trace.depth[callback]--
		}
	}
	hold := false
	if callback == "A" && stage == "LEASE_READ" && data.Err == nil {
		trace.aLeaseReads++
		hold = trace.aLeaseReads == 2
	}
	trace.mu.Unlock()
	if callback == "B" && stage == "RECEIPT_READ" && data.CommandTag.RowsAffected() == 0 && data.Err == nil {
		trace.openBWindow("MISSING_RECEIPT_BEFORE_LEASE")
	}
	if hold {
		trace.aOnce.Do(func() { close(trace.aHeld) })
		select {
		case <-trace.releaseA:
		case <-ctx.Done():
		}
	}
}

// release safely unblocks the held transaction even when a fixture assertion fails early.
// release безопасно освобождает транзакцию даже при раннем отказе fixture assertion.
func (trace *completionReceiptTrace) release() {
	trace.releaseOnce.Do(func() { close(trace.releaseA) })
}

// completionTrackedStore counts immutable writes and detects an ObjectStore effect inside a DB tx.
// completionTrackedStore считает immutable writes и обнаруживает ObjectStore effect внутри DB tx.
type completionTrackedStore struct {
	objectstorage.Store
	trace    *completionReceiptTrace
	puts     atomic.Int64
	insideTx atomic.Bool
}

// Put delegates the synthetic store operation after recording only count and transaction presence.
// Put передаёт synthetic store operation, записав только count и наличие транзакции.
func (store *completionTrackedStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	callback, _ := ctx.Value(completionCallbackKey{}).(string)
	store.trace.mu.Lock()
	depth := store.trace.depth[callback]
	store.trace.mu.Unlock()
	if depth != 0 {
		store.insideTx.Store(true)
	}
	store.puts.Add(1)
	return store.Store.Put(ctx, input)
}

// completionPayloadFromWire independently decodes native protobuf bytes and mirrors the existing
// CompleteExecution transport mapping; trusted principal/lease authority is supplied separately.
// completionPayloadFromWire независимо декодирует native protobuf bytes и повторяет существующий
// transport mapping CompleteExecution; trusted principal/lease authority задаётся отдельно.
func completionPayloadFromWire(t *testing.T, wire []byte) (value command.CompleteExecutionInput, key string) {
	t.Helper()
	var request controlplanev1.CompleteExecutionRequest
	if proto.Unmarshal(wire, &request) != nil {
		t.Fatal("decode native completion fixture")
	}
	value = command.CompleteExecutionInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Success: request.GetSuccess(), ResultSummary: request.GetResultSummary(), SafeErrorCode: request.GetSafeErrorCode(), CodexSessionID: request.GetCodexSessionId(), ArchiveRelativePath: request.GetCodexArchiveRelativePath(), ArchiveSHA256: request.GetCodexArchiveSha256(), ArchiveSizeBytes: request.GetCodexArchiveSizeBytes()}
	if usage := request.GetUsage(); usage != nil {
		value.Usage = entity.TokenUsage{TotalTokens: usage.GetTotalTokens(), InputTokens: usage.GetInputTokens(), CachedInputTokens: usage.GetCachedInputTokens(), CacheWriteInputTokens: usage.GetCacheWriteInputTokens(), OutputTokens: usage.GetOutputTokens(), ReasoningOutputTokens: usage.GetReasoningOutputTokens(), ModelContextWindow: usage.GetModelContextWindow()}
	}
	for _, artifact := range request.GetArtifacts() {
		value.Artifacts = append(value.Artifacts, command.CompletedArtifact{FileName: artifact.GetFileName(), MediaType: artifact.GetMediaType(), SizeBytes: artifact.GetSizeBytes(), SHA256: artifact.GetSha256(), Content: artifact.GetContent()})
	}
	return value, request.GetMutation().GetIdempotencyKey()
}

// TestCompletionReceiptPayloadsIndependent prevents the old shared backing-array fixture defect.
// TestCompletionReceiptPayloadsIndependent предотвращает старый fixture defect общего backing array.
func TestCompletionReceiptPayloadsIndependent(t *testing.T) {
	wire, err := proto.Marshal(&controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: "same-key"}, Success: true, Artifacts: []*controlplanev1.CompletedArtifactInput{{FileName: "report.md", Content: []byte("immutable bytes")}}})
	if err != nil {
		t.Fatal("encode native fixture")
	}
	a, keyA := completionPayloadFromWire(t, wire)
	b, keyB := completionPayloadFromWire(t, wire)
	if keyA != keyB || &a.Artifacts[0] == &b.Artifacts[0] || &a.Artifacts[0].Content[0] == &b.Artifacts[0].Content[0] {
		t.Fatal("independent native callbacks share artifact storage")
	}
	a.Artifacts[0].Prepared = &command.PreparedArtifact{Ref: "synthetic"}
	a.Artifacts[0].Content = nil
	if b.Artifacts[0].Prepared != nil || string(b.Artifacts[0].Content) != "immutable bytes" {
		t.Fatal("one callback preparation changed another decoded request")
	}
}

// TestCompletionReceiptWindowTracer validates both old and fixed synchronization branches without DB.
// TestCompletionReceiptWindowTracer проверяет обе ветки синхронизации, старую и исправленную, без БД.
func TestCompletionReceiptWindowTracer(t *testing.T) {
	for _, mode := range []string{"LOCK_BEFORE_RECEIPT", "MISSING_RECEIPT_BEFORE_LEASE"} {
		t.Run(mode, func(t *testing.T) {
			trace := newCompletionReceiptTrace()
			defer trace.release()
			ctx := context.WithValue(t.Context(), completionCallbackKey{}, "B")
			if mode == "LOCK_BEFORE_RECEIPT" {
				trace.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: queryCommandsExecuteLockIdempotencyScope})
			} else {
				ctx = trace.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation})
				trace.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
			}
			select {
			case <-trace.bWindow:
			default:
				t.Fatal("deterministic callback window was not signaled")
			}
			if trace.windowMode != mode {
				t.Fatal("callback trace reported a different closed ordering stage")
			}
		})
	}
}

// TestCompletionReceiptTracerCancellation proves the held owner transaction can always leave its
// test-only barrier on cancellation, so a failed fixture cannot indefinitely retain DB locks.
// TestCompletionReceiptTracerCancellation доказывает выход удерживаемой owner transaction из
// test-only barrier при отмене, чтобы failed fixture не удерживала DB locks бесконечно.
func TestCompletionReceiptTracerCancellation(t *testing.T) {
	trace := newCompletionReceiptTrace()
	defer trace.release()
	bounded, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(context.WithValue(bounded, completionCallbackKey{}, "A"))
	defer cancel()
	ctx = trace.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef})
	trace.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	finished := make(chan struct{})
	go func() { trace.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{}); close(finished) }()
	select {
	case <-trace.aHeld:
	case <-bounded.Done():
		t.Fatal("held callback barrier was not entered")
	}
	cancel()
	select {
	case <-finished:
	case <-bounded.Done():
		t.Fatal("held callback did not honor cancellation")
	}
}
