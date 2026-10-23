package websockettransport

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// runRefreshRecorder tracks started refs, active/maximum concurrency and child deadlines under a mutex, delegating each graph read to a configurable synthetic callback.
// runRefreshRecorder под mutex считает начатые ссылки, текущую и максимальную параллельность и сроки дочерних запросов, передавая чтение настраиваемому тестовому callback.
type runRefreshRecorder struct {
	grpc.ClientConnInterface
	mu                    sync.Mutex
	started               []string
	active, maximumActive int
	deadlines             []time.Time
	read                  func(context.Context, string) (*cp.GetRunGraphResponse, error)
}

// Invoke accepts only complete GetRunGraph requests/responses, records concurrency/deadline evidence and merges successful synthetic data; deferred accounting runs after the callback returns, not merely after context cancellation.
// Invoke принимает только полные GetRunGraph request/response, запоминает параллельность и срок и объединяет успешные тестовые данные; отложенный учёт выполняется после возврата callback, а не только отмены контекста.
func (c *runRefreshRecorder) Invoke(ctx context.Context, _ string, request, response any, _ ...grpc.CallOption) error {
	input, ok := request.(*cp.GetRunGraphRequest)
	out, responseOK := response.(*cp.GetRunGraphResponse)
	if !ok || !responseOK || !input.IncludeRunSnapshots {
		return fmt.Errorf("unexpected synthetic query")
	}
	c.mu.Lock()
	c.started = append(c.started, input.RunRef)
	c.active++
	c.maximumActive = max(c.maximumActive, c.active)
	deadline, _ := ctx.Deadline()
	c.deadlines = append(c.deadlines, deadline)
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.active--; c.mu.Unlock() }()
	value, err := c.read(ctx, input.RunRef)
	if err == nil {
		proto.Merge(out, value)
	}
	return err
}

// refreshSnapshot returns an independent complete fixture retargeted to the supplied root while preserving graph/run/session bindings for its child.
// refreshSnapshot возвращает независимый полный тестовый снимок с переданным корнем и сохранёнными привязками графа, запуска и сессии ребёнка.
func refreshSnapshot(ref string) *cp.GetRunGraphResponse {
	value := completeRunSnapshotFixture()
	value.Run.Ref, value.Run.RootRunRef, value.Graph.RunRef = ref, ref, ref
	value.Runs[0] = value.Run
	value.Runs[1].RootRunRef, value.Runs[1].ParentRunRef = ref, ref
	return value
}

// runRefreshFixture creates a fake query client and the requested number of registered root subscriptions with fixed cursors and buffered frames; it opens no real RPC connection.
// runRefreshFixture создаёт фиктивный query client и запрошенное число зарегистрированных корневых подписок с фиксированными курсорами и буфером кадров без настоящего RPC-соединения.
func runRefreshFixture(t *testing.T, count int) (*sessionMultiplexer, *runRefreshRecorder, *platformWakeRaceRecorder) {
	t.Helper()
	m, frames := platformWakeRaceFixture(t, 8)
	c := &runRefreshRecorder{read: func(_ context.Context, ref string) (*cp.GetRunGraphResponse, error) { return refreshSnapshot(ref), nil }}
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	for index := range count {
		ref := fmt.Sprintf("run_root%04d", index+1)
		m.runs[ref] = &runSubscription{rootRef: ref, requestRef: fmt.Sprintf("request_run%04d", index+1), cursor: 7, available: true}
	}
	return m, c, frames
}

// runRefreshFrameCounts counts complete run envelopes and only RUN_UNAVAILABLE stream problems, ignoring unrelated frame kinds for these assertions.
// runRefreshFrameCounts считает полные конверты запусков и только stream problems RUN_UNAVAILABLE, игнорируя посторонние виды кадров в этих проверках.
func runRefreshFrameCounts(frames []any) (snapshots, problems int) {
	for _, frame := range frames {
		switch value := frame.(type) {
		case generated.RunSnapshotEnvelope:
			snapshots++
		case generated.StreamProblemEnvelope:
			if value.Code == "RUN_UNAVAILABLE" {
				problems++
			}
		}
	}
	return
}

// TestRunSnapshotRefreshParallelSharedBudget requires two synthetic slow reads to overlap under the exact earlier parent deadline, publish two snapshots and leave no active worker after refresh returns.
// TestRunSnapshotRefreshParallelSharedBudget требует перекрытия двух медленных тестовых чтений с точным ранним сроком родителя, выдачи двух снимков и отсутствия активных работников после возврата.
func TestRunSnapshotRefreshParallelSharedBudget(t *testing.T) {
	m, c, frames := runRefreshFixture(t, 2)
	parent, cancel := context.WithTimeout(m.ctx, 250*time.Millisecond)
	defer cancel()
	expected, _ := parent.Deadline()
	c.read = func(ctx context.Context, ref string) (*cp.GetRunGraphResponse, error) {
		timer := time.NewTimer(150 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-timer.C:
			return refreshSnapshot(ref), nil
		}
	}
	if !m.refreshSubscribedRunsWithin(parent) {
		t.Fatal("bounded owner refresh failed")
	}
	snapshots, problems := runRefreshFrameCounts(platformWakeRaceFrames(frames))
	if snapshots != 2 || problems != 0 || c.maximumActive != 2 || c.active != 0 {
		t.Fatal("serial reads exhausted the sibling budget or workers were not joined")
	}
	for _, deadline := range c.deadlines {
		if !deadline.Equal(expected) {
			t.Fatal("read restarted the shared owner deadline")
		}
	}
}

// TestRunSnapshotRefreshExpiredParentDoesNotStartRPC starts with an expired parent and requires zero owner RPCs plus one closed problem per registered root, preserving transport success semantics.
// TestRunSnapshotRefreshExpiredParentDoesNotStartRPC начинает с истёкшего родителя и требует ноль owner RPC и одну закрытую ошибку каждому корню, сохраняя семантику успешного транспорта.
func TestRunSnapshotRefreshExpiredParentDoesNotStartRPC(t *testing.T) {
	m, c, frames := runRefreshFixture(t, 3)
	parent, cancel := context.WithDeadline(m.ctx, time.Now().Add(-time.Second))
	defer cancel()
	if !m.refreshSubscribedRunsWithin(parent) {
		t.Fatal("deadline did not retain closed stream availability semantics")
	}
	snapshots, problems := runRefreshFrameCounts(platformWakeRaceFrames(frames))
	if len(c.started) != 0 || snapshots != 0 || problems != 3 {
		t.Fatal("expired parent started owner RPC or published an unverified graph")
	}
}

// TestRunSnapshotRefreshRoundRobinDeadlineFairness uses four short-budget rounds to ensure the two-worker starting pair rotates through sorted roots, all unfinished roots fail closed and no worker remains active.
// TestRunSnapshotRefreshRoundRobinDeadlineFairness четырьмя раундами короткого бюджета проверяет смену стартовой пары двух работников по отсортированным корням, закрытый отказ непрочитанным корням и отсутствие активных работников.
func TestRunSnapshotRefreshRoundRobinDeadlineFairness(t *testing.T) {
	m, c, frames := runRefreshFixture(t, 4)
	c.read = func(ctx context.Context, _ string) (*cp.GetRunGraphResponse, error) {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	refs := m.sortedRunRefs()
	for round := range 4 {
		parent, cancel := context.WithTimeout(m.ctx, 40*time.Millisecond)
		if !m.refreshSubscribedRunsWithin(parent) {
			t.Fatal("deadline terminated healthy socket")
		}
		cancel()
		c.mu.Lock()
		started := slices.Clone(c.started)
		c.started = nil
		active, maximumActive := c.active, c.maximumActive
		c.mu.Unlock()
		want := []string{refs[round], refs[(round+1)%len(refs)]}
		slices.Sort(started)
		slices.Sort(want)
		if !slices.Equal(started, want) || active != 0 || maximumActive > 2 {
			t.Fatal("shared deadline starved lexical tail or allowed an unjoined third read")
		}
		snapshots, problems := runRefreshFrameCounts(platformWakeRaceFrames(frames))
		if snapshots != 0 || problems != 4 {
			t.Fatal("unfinished owner reads fabricated availability")
		}
	}
}

// TestRunSnapshotRefreshCancellationJoinsWorkers exercises parent and socket cancellation through the shared join assertion rather than merely observing context.Done.
// TestRunSnapshotRefreshCancellationJoinsWorkers проверяет отмену родителя и сокета через общую проверку ожидания работников, а не только наблюдение context.Done.
func TestRunSnapshotRefreshCancellationJoinsWorkers(t *testing.T) {
	t.Run("parent", func(t *testing.T) { assertRunRefreshCancellationJoined(t, false) })
	t.Run("socket", func(t *testing.T) { assertRunRefreshCancellationJoined(t, true) })
}

// assertRunRefreshCancellationJoined holds cooperative callbacks after they observe cancellation, proves refresh cannot return before their explicit release, then requires joined workers, false transport outcome and no frames.
// assertRunRefreshCancellationJoined удерживает кооперативные callbacks после наблюдения отмены, доказывает невозможность возврата до явного освобождения, затем требует завершённых работников, false транспорта и отсутствие кадров.
func assertRunRefreshCancellationJoined(t *testing.T, cancelSocket bool) {
	t.Helper()
	m, c, frames := runRefreshFixture(t, 4)
	parent, cancel := context.WithCancel(m.ctx)
	defer cancel()
	if cancelSocket {
		m.ctx, parent = parent, t.Context()
	}
	started, stopped := make(chan struct{}, 2), make(chan struct{}, 2)
	finish := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(finish) })
	c.read = func(ctx context.Context, _ string) (*cp.GetRunGraphResponse, error) {
		started <- struct{}{}
		<-ctx.Done()
		stopped <- struct{}{}
		<-finish
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	result := make(chan bool, 1)
	go func() { result <- m.refreshSubscribedRunsWithin(parent) }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("bounded workers did not start")
		}
	}
	cancel()
	for range 2 {
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("cancellation did not reach every owner read")
		}
	}
	select {
	case <-result:
		t.Fatal("refresh returned before child cleanup joined")
	default:
	}
	release.Do(func() { close(finish) })
	select {
	case ok := <-result:
		if ok || c.active != 0 || len(c.started) != 2 || len(platformWakeRaceFrames(frames)) != 0 {
			t.Fatal("cancelled refresh published or restarted owner work")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled refresh did not join")
	}
}

// TestRunSnapshotRefreshClosedOwnerResults injects denial or malformed root/completeness/cursor into one of two reads, requiring that root to fail closed while its independent sibling still publishes successfully.
// TestRunSnapshotRefreshClosedOwnerResults подставляет запрет либо неверные корень, полноту или курсор одному из двух чтений, требуя закрытого отказа этому корню при успешной публикации независимого соседа.
func TestRunSnapshotRefreshClosedOwnerResults(t *testing.T) {
	for _, condition := range []string{"denied", "wrong-root", "incomplete", "wrong-cursor"} {
		t.Run(condition, func(t *testing.T) {
			m, c, frames := runRefreshFixture(t, 2)
			c.read = func(_ context.Context, ref string) (*cp.GetRunGraphResponse, error) {
				value := refreshSnapshot(ref)
				if ref == "run_root0001" {
					switch condition {
					case "denied":
						return nil, status.Error(codes.PermissionDenied, "synthetic owner rejected")
					case "wrong-root":
						value.Run.RootRunRef = "run_foreign01"
					case "incomplete":
						value.Runs = value.Runs[:1]
					case "wrong-cursor":
						value.Graph.Sequence++
					}
				}
				return value, nil
			}
			if !m.refreshSubscribedRuns() {
				t.Fatal("closed result terminated independent subscription")
			}
			snapshots, problems := runRefreshFrameCounts(platformWakeRaceFrames(frames))
			if snapshots != 1 || problems != 1 || m.runs["run_root0001"].available || !m.runs["run_root0002"].available || c.active != 0 {
				t.Fatal("owner rejection or partial graph was published as complete")
			}
		})
	}
}

// TestRunSnapshotRefreshFreshReadsAndBounds refreshes the maximum subscription set twice, checking per-RPC deadlines, two-worker concurrency and fresh reads, then rejects an excess subscription before any further RPC.
// TestRunSnapshotRefreshFreshReadsAndBounds дважды обновляет максимальный набор подписок, проверяя сроки RPC, параллельность двух работников и свежие чтения, затем отклоняет лишнюю подписку до следующего RPC.
func TestRunSnapshotRefreshFreshReadsAndBounds(t *testing.T) {
	m, c, frames := runRefreshFixture(t, maximumRunSubscriptions)
	c.read = func(ctx context.Context, ref string) (*cp.GetRunGraphResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > runSnapshotReadTimeout || ctx.Err() != nil {
			return nil, fmt.Errorf("synthetic read escaped the per-RPC deadline")
		}
		return refreshSnapshot(ref), nil
	}
	for range 2 {
		if !m.refreshSubscribedRuns() {
			t.Fatal("bounded complete refresh failed")
		}
		snapshots, problems := runRefreshFrameCounts(platformWakeRaceFrames(frames))
		if snapshots != maximumRunSubscriptions || problems != 0 || c.maximumActive > 2 || c.active != 0 {
			t.Fatal("worker bound or complete fresh publication changed")
		}
	}
	if len(c.started) != 2*maximumRunSubscriptions {
		t.Fatal("second refresh reused old payload")
	}
	m.runs["run_excess01"] = &runSubscription{rootRef: "run_excess01"}
	if m.refreshSubscribedRuns() || len(c.started) != 2*maximumRunSubscriptions {
		t.Fatal("oversized jobs escaped the closed subscription bound")
	}
}

// TestRunSnapshotRefreshAlreadyCancelledNoRPC starts with canceled parent or socket context and requires immediate false without owner reads or published frames.
// TestRunSnapshotRefreshAlreadyCancelledNoRPC начинает с отменённым родителем либо контекстом сокета и требует немедленный false без чтений владельца и кадров.
func TestRunSnapshotRefreshAlreadyCancelledNoRPC(t *testing.T) {
	for _, cancelSocket := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelSocket), func(t *testing.T) {
			m, c, frames := runRefreshFixture(t, 2)
			parent, cancel := context.WithCancel(m.ctx)
			cancel()
			if cancelSocket {
				m.ctx, parent = parent, t.Context()
			}
			if m.refreshSubscribedRunsWithin(parent) || len(c.started) != 0 || len(platformWakeRaceFrames(frames)) != 0 {
				t.Fatal("cancelled parent or socket started owner work or publication")
			}
		})
	}
}

// TestRunSnapshotRefreshBackpressureCancelsAndJoins forces outbound admission failure while another owner read is active, requiring cancellation and worker joining before false is returned.
// TestRunSnapshotRefreshBackpressureCancelsAndJoins создаёт отказ исходящей очереди при другом активном чтении владельца и требует отмену и ожидание работников до возврата false.
func TestRunSnapshotRefreshBackpressureCancelsAndJoins(t *testing.T) {
	m, c, _ := runRefreshFixture(t, 2)
	m.outbound = make(chan outboundFrame)
	started := make(chan struct{}, 2)
	c.read = func(ctx context.Context, ref string) (*cp.GetRunGraphResponse, error) {
		started <- struct{}{}
		if ref == "run_root0001" {
			for len(started) < 2 {
				select {
				case <-ctx.Done():
					return nil, status.FromContextError(ctx.Err()).Err()
				case <-time.After(time.Millisecond):
				}
			}
			return refreshSnapshot(ref), nil
		}
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if m.refreshSubscribedRuns() || c.active != 0 || len(c.started) != 2 {
		t.Fatal("outbound failure returned without cancelling and joining owner reads")
	}
}
