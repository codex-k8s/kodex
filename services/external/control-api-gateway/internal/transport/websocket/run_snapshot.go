package websockettransport

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	httpgenerated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/protobuf/proto"
)

const (
	// maximumRunSnapshotItems limits the complete root/child/predecessor run snapshot set to 128 records before browser projection; it is not a separate graph-node count limit.
	// maximumRunSnapshotItems ограничивает полный набор снимков корня, детей и предшественников 128 записями до браузерной проекции; это не отдельный предел числа узлов графа.
	maximumRunSnapshotItems = 128
	// runSnapshotReadTimeout caps each owner graph read at five seconds without extending an earlier parent deadline.
	// runSnapshotReadTimeout ограничивает каждое чтение графа у владельца пятью секундами, не продлевая более ранний срок родителя.
	runSnapshotReadTimeout = 5 * time.Second
	// runSnapshotRefreshTimeout bounds the shared refresh context at ten seconds; cooperative RPC cancellation and worker joining still determine return time.
	// runSnapshotRefreshTimeout ограничивает общий контекст обновления десятью секундами; время возврата по-прежнему зависит от кооперативной отмены RPC и ожидания работников.
	runSnapshotRefreshTimeout = 10 * time.Second
	// runSnapshotReadWorkers permits at most two concurrent owner reads per refresh, not concurrent mutation of the socket or subscription map.
	// runSnapshotReadWorkers разрешает не более двух одновременных чтений у владельца за обновление, но не параллельное изменение сокета или таблицы подписок.
	runSnapshotReadWorkers = 2
)

// errRunSnapshotInvalid is the closed failure for incomplete, inconsistently bound or oversized run projections, without embedding owner payloads or references in its text.
// errRunSnapshotInvalid — закрытая ошибка неполной, несогласованной или чрезмерной проекции запуска без данных владельца и ссылок в тексте.
var errRunSnapshotInvalid = errors.New("run snapshot projection is invalid")

// readRunSnapshot delegates an exact root read with the socket lifecycle context, requesting the full owner run-snapshot set.
// readRunSnapshot передаёт чтение точного корня с жизненным контекстом сокета, запрашивая полный набор снимков запусков у владельца.
func (multiplexer *sessionMultiplexer) readRunSnapshot(ref string) (*controlplanev1.GetRunGraphResponse, error) {
	return multiplexer.readRunSnapshotWithin(multiplexer.ctx, ref)
}

// readRunSnapshotWithin derives a bounded child context and requests GetRunGraph with IncludeRunSnapshots=true, observing any failure without changing the returned response/error or authenticating the root locally.
// readRunSnapshotWithin создаёт ограниченный дочерний контекст и запрашивает GetRunGraph с IncludeRunSnapshots=true, наблюдая отказ без изменения возвращаемых ответа и ошибки и без локальной авторизации корня.
func (multiplexer *sessionMultiplexer) readRunSnapshotWithin(parent context.Context, ref string) (*controlplanev1.GetRunGraphResponse, error) {
	started := time.Now()
	parentBudget := runSnapshotParentBudget(parent, started)
	ctx, cancel := context.WithTimeout(parent, runSnapshotReadTimeout)
	defer cancel()
	snapshot, err := multiplexer.server.query.GetRunGraph(ctx, &controlplanev1.GetRunGraphRequest{RunRef: ref, IncludeRunSnapshots: true})
	observeRunSnapshotReadFailure(parent, ctx, started, parentBudget, err)
	return snapshot, err
}

// refreshSubscribedRuns refreshes only registered socket subscriptions under the existing lifecycle context; it does not enumerate all accessible runs.
// refreshSubscribedRuns обновляет только зарегистрированные подписки сокета в существующем жизненном контексте, не перечисляя все доступные запуски.
func (multiplexer *sessionMultiplexer) refreshSubscribedRuns() bool {
	return multiplexer.refreshSubscribedRunsWithin(multiplexer.ctx)
}

// refreshSubscribedRunsWithin bounds registered roots, rotates their read order and uses at most two workers under one shared deadline. Workers return only RPC results; the caller alone publishes complete projections or marks a subscription unavailable. Deadline exhaustion produces closed failures for unread roots, while parent/socket cancellation or backpressure stops transport. Deferred cancellation and Wait join cooperative workers before return; no cached owner payload or authority is reused, and a noncooperative client is not forcibly terminated.
// refreshSubscribedRunsWithin ограничивает зарегистрированные корни, меняет начальный порядок чтения и использует не более двух работников с единым сроком. Работники возвращают только RPC-результаты; лишь вызывающий публикует полные проекции либо отмечает подписку недоступной. Исчерпание срока выдаёт закрытые отказы непрочитанным корням, а отмена родителя, сокета или перегрузка останавливает транспорт. Отложенные отмена и Wait ожидают кооперативных работников до возврата; старые данные и полномочия не переиспользуются, некооперативный клиент принудительно не завершается.
func (multiplexer *sessionMultiplexer) refreshSubscribedRunsWithin(parent context.Context) bool {
	if multiplexer.ctx.Err() != nil || errors.Is(parent.Err(), context.Canceled) {
		return false
	}
	refs := multiplexer.sortedRunRefs()
	if len(refs) == 0 {
		multiplexer.runRefreshOffset = 0
		return true
	}
	if len(refs) > maximumRunSubscriptions {
		return false
	}
	// The offset stores only this socket's read order, never payload or authority.
	// Сдвиг хранит только порядок чтений текущего socket, не payload или authority.
	start := multiplexer.runRefreshOffset % len(refs)
	refs = append(refs[start:], refs[:start]...)
	multiplexer.runRefreshOffset = (start + 1) % len(refs)
	ctx, cancel := context.WithTimeout(parent, runSnapshotRefreshTimeout)
	stop := context.AfterFunc(multiplexer.ctx, cancel)
	jobs := make(chan int, len(refs))
	results := make(chan runSnapshotReadResult, runSnapshotReadWorkers)
	for index := range refs {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	defer func() {
		stop()
		cancel()
		workers.Wait()
	}()
	for range min(runSnapshotReadWorkers, len(refs)) {
		workers.Go(func() {
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				snapshot, err := multiplexer.readRunSnapshotWithin(ctx, refs[index])
				select {
				case results <- runSnapshotReadResult{index: index, snapshot: snapshot, err: err}:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	completed := make([]bool, len(refs))
	for range refs {
		if multiplexer.ctx.Err() != nil || errors.Is(ctx.Err(), context.Canceled) {
			return false
		}
		var result runSnapshotReadResult
		select {
		case <-ctx.Done():
			cancel()
			workers.Wait()
			if multiplexer.ctx.Err() != nil || errors.Is(parent.Err(), context.Canceled) {
				return false
			}
			// The shared budget expired: unread subscriptions must not become available.
			// Истёк общий бюджет: непрочитанные подписки не получают готовность.
			for index, ref := range refs {
				if !completed[index] && !multiplexer.failRunSnapshot(multiplexer.runs[ref]) {
					return false
				}
			}
			return true
		case result = <-results:
		}
		if multiplexer.ctx.Err() != nil || errors.Is(ctx.Err(), context.Canceled) {
			return false
		}
		completed[result.index] = true
		subscription := multiplexer.runs[refs[result.index]]
		if result.err != nil || ctx.Err() != nil {
			if !multiplexer.failRunSnapshot(subscription) {
				return false
			}
			continue
		}
		if !multiplexer.sendRunSnapshot(subscription, result.snapshot) {
			return false
		}
	}
	return true
}

// runSnapshotReadResult carries a worker's input index, owner response and error back to the single subscription owner; workers do not mutate socket state themselves.
// runSnapshotReadResult передаёт индекс входа, ответ владельца и ошибку от работника единственному владельцу подписок; сами работники состояние сокета не меняют.
type runSnapshotReadResult struct {
	index    int
	snapshot *controlplanev1.GetRunGraphResponse
	err      error
}

// failRunSnapshot marks the nonnil subscription unavailable and enqueues its exact RUN_UNAVAILABLE problem at the existing cursor, returning queue-admission success without advancing that cursor.
// failRunSnapshot отмечает ненулевую подписку недоступной и ставит её точный RUN_UNAVAILABLE на прежнем курсоре, возвращая успех допуска очереди без продвижения курсора.
func (multiplexer *sessionMultiplexer) failRunSnapshot(subscription *runSubscription) bool {
	subscription.available = false
	return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "RUN_UNAVAILABLE")
}

// projectCompleteRunSnapshot checks the established subscription's root/graph revision/sequence, exact expected run set, node/edge references, project bindings and retry/continuation predecessor evidence. It projects each run with matching session-readiness and bounded archive-task metadata, then measures the entire generated envelope. Failure returns a closed error and emits only the projection diagnostic; success does not enqueue or advance state. This validates an owner read, not authorization to create or execute graph nodes.
// projectCompleteRunSnapshot проверяет корень, ревизию и sequence графа установленной подписки, точный ожидаемый набор запусков, ссылки узлов и рёбер, проектные привязки и свидетельства предшественников retry/continuation. Каждый запуск проецируется с согласованной готовностью сессии и ограниченной metadata архивной задачи, затем измеряется полный generated-конверт. Отказ возвращает закрытую ошибку и только диагностику проекции; успех не ставит кадр в очередь и не продвигает состояние. Проверяется чтение у владельца, а не право создавать или выполнять узлы.
func projectCompleteRunSnapshot(value *controlplanev1.GetRunGraphResponse, subscription *runSubscription, localize func(string) string) (result generated.RunSnapshotEnvelope, projectionErr error) {
	started := time.Now()
	defer func() { observeRunSnapshotProjectionFailure(started, projectionErr) }()
	ref := subscription.rootRef
	graph := value.GetGraph()
	root := value.GetRun()
	if root.GetRef() != ref || root.GetRootRunRef() != ref || graph.GetRunRef() != ref ||
		root.GetGraphRevision() != graph.GetRevision() || root.GetLastEventSequence() != graph.GetSequence() ||
		graph.GetSequence() < subscription.cursor || len(value.GetRuns()) < 1 || len(value.GetRuns()) > maximumRunSnapshotItems {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	expected := map[string]bool{ref: true}
	nodeRefs, edgeRefs := map[string]bool{}, map[string]bool{}
	for _, node := range graph.GetNodes() {
		if !safeRef.MatchString(node.GetRunRef()) || !safeRef.MatchString(node.GetRef()) || nodeRefs[node.GetRef()] {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		nodeRefs[node.GetRef()] = true
		expected[node.GetRunRef()] = true
		for _, ref := range node.GetChildRunRefs() {
			if !safeRef.MatchString(ref) {
				return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
			}
			expected[ref] = true
		}
	}
	for _, edge := range graph.GetEdges() {
		if !safeRef.MatchString(edge.GetRef()) || edgeRefs[edge.GetRef()] || !nodeRefs[edge.GetSourceNodeRef()] || !nodeRefs[edge.GetTargetNodeRef()] {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		edgeRefs[edge.GetRef()] = true
	}
	if len(expected) != len(value.GetRuns()) {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	runs := make([]httpgenerated.Run, 0, len(value.GetRuns()))
	refs := make(map[string]bool, len(expected))
	for ref := range expected {
		refs[ref] = true
	}
	predecessors, valid := snapshotRetryPredecessors(graph, value.GetRuns())
	if !valid {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	for _, run := range value.GetRuns() {
		if run.GetRef() == ref && !proto.Equal(root, run) {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		crossRoot := run.GetRef() == run.GetRootRunRef() && run.GetParentRunRef() != run.GetRef() && refs[run.GetParentRunRef()]
		if !expected[run.GetRef()] || (run.GetRootRunRef() != ref && !crossRoot && !predecessors[run.GetRef()]) || run.GetProjectRef() != root.GetProjectRef() {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		delete(expected, run.GetRef())
		projected, err := projectProto[httpgenerated.Run](run, localize)
		readiness := projected.SessionReadiness
		if err != nil || (run.GetSessionRef() != "" && readiness == nil) ||
			(readiness != nil && (string(readiness.SessionRef) != string(projected.SessionRef) || !readiness.Reason.Valid() || !readiness.StorageState.Valid())) {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		if readiness != nil && readiness.LatestArchiveTask != nil {
			task := readiness.LatestArchiveTask
			if !task.Kind.Valid() || !task.State.Valid() || !task.SafeErrorCode.Valid() || !safeRef.MatchString(string(task.Ref)) || task.Attempt < 0 || task.MaximumAttempts < 1 || task.MaximumAttempts > 5 || task.Attempt > task.MaximumAttempts {
				return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
			}
		}
		runs = append(runs, projected)
	}
	if len(expected) != 0 {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	projected, err := projectRunGraph(graph, localize)
	if err != nil {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	envelope := generated.RunSnapshotEnvelope{
		Type: "RUN_GRAPH_SNAPSHOT", RequestRef: subscription.requestRef, StreamKind: "RUN", StreamRef: ref,
		Cursor: graph.GetSequence(), Snapshot: projected, Runs: runs,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) > maximumFrameBytes {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	return envelope, nil
}

// snapshotRetryPredecessors validates cross-run RETRY_OF/CONTINUES root-process links, matching retry pins or a succeeded predecessor sharing the nonempty session, unique incoming lineage and absence of cycles. Same-run continuation edges are left to other graph checks. It returns admitted predecessor references for read projection, not grants or new run state; retry predecessor terminality is not checked here.
// snapshotRetryPredecessors проверяет межзапусковые корневые RETRY_OF/CONTINUES, точные retry-привязки либо успешного предшественника с общей непустой сессией, единственную входящую связь и отсутствие циклов. CONTINUES внутри одного запуска оставляются другим проверкам графа. Возвращаются допустимые ссылки предшественников для проекции чтения, а не grants или новое состояние; терминальность retry-предшественника здесь не проверяется.
func snapshotRetryPredecessors(graph *controlplanev1.RunGraph, values []*controlplanev1.Run) (map[string]bool, bool) {
	nodes := map[string]*controlplanev1.RunNode{}
	for _, node := range graph.GetNodes() {
		nodes[node.GetRef()] = node
	}
	runs := map[string]*controlplanev1.Run{}
	for _, run := range values {
		runs[run.GetRef()] = run
	}
	links := map[string]string{}
	previous := map[string]bool{}
	for _, edge := range graph.GetEdges() {
		if edge.GetType() != controlplanev1.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF && edge.GetType() != controlplanev1.RunEdgeType_RUN_EDGE_TYPE_CONTINUES {
			continue
		}
		oldNode, newNode := nodes[edge.GetSourceNodeRef()], nodes[edge.GetTargetNodeRef()]
		if edge.GetType() == controlplanev1.RunEdgeType_RUN_EDGE_TYPE_CONTINUES && oldNode != nil && newNode != nil && oldNode.GetRunRef() == newNode.GetRunRef() {
			continue
		}
		old, fresh := runs[oldNode.GetRunRef()], runs[newNode.GetRunRef()]
		if old == nil || fresh == nil || !validPredecessorAnchors(edge.GetType(), oldNode, newNode, old, fresh) || old.GetRef() != old.GetRootRunRef() || fresh.GetRef() != fresh.GetRootRunRef() || edge.GetRunRef() != fresh.GetRef() || old.GetRef() == fresh.GetRef() || links[fresh.GetRef()] != "" {
			return nil, false
		}
		if (edge.GetType() == controlplanev1.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF && fresh.GetRetryOfRunRef() != old.GetRef()) ||
			(edge.GetType() == controlplanev1.RunEdgeType_RUN_EDGE_TYPE_CONTINUES && (old.GetSessionRef() == "" || fresh.GetSessionRef() != old.GetSessionRef() || old.GetState() != controlplanev1.RunState_RUN_STATE_SUCCEEDED || fresh.GetRetryOfRunRef() != "")) {
			return nil, false
		}
		links[fresh.GetRef()] = old.GetRef()
		previous[old.GetRef()] = true
	}
	for _, run := range values {
		if run.GetRetryOfRunRef() != "" && runs[run.GetRetryOfRunRef()] != nil && links[run.GetRef()] != run.GetRetryOfRunRef() {
			return nil, false
		}
	}
	for ref := range links {
		seen := map[string]bool{}
		for current := ref; current != ""; current = links[current] {
			if seen[current] {
				return nil, false
			}
			seen[current] = true
		}
	}
	return previous, true
}

// validPredecessorAnchors projects only ordinary process roots or an exact assistant retry. It checks immutable owner pins, root-node agent bindings and session/attempt continuity without accepting arbitrary execution nodes.
// validPredecessorAnchors проецирует только корни обычного процесса либо точный повтор помощника. Проверяет неизменяемые привязки владельца, сотрудника корневого узла и последовательность сессии/попытки, не принимая произвольные узлы исполнения.
func validPredecessorAnchors(edgeType controlplanev1.RunEdgeType, oldNode, newNode *controlplanev1.RunNode, old, fresh *controlplanev1.Run) bool {
	if oldNode.GetType() == controlplanev1.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS && newNode.GetType() == controlplanev1.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS {
		return true
	}
	if edgeType != controlplanev1.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF || oldNode.GetType() != controlplanev1.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION || newNode.GetType() != controlplanev1.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION || oldNode.GetParentNodeRef() != "" || newNode.GetParentNodeRef() != "" {
		return false
	}
	pin, next := old.GetAssistantPin(), fresh.GetAssistantPin()
	return old.GetSource() == controlplanev1.RunSource_RUN_SOURCE_SYSTEM_ASSISTANT && fresh.GetSource() == controlplanev1.RunSource_RUN_SOURCE_SYSTEM_ASSISTANT &&
		pin != nil && next != nil && proto.Equal(pin, next) && pin.GetAssistantRef() != "" &&
		old.GetTarget().GetSystemAssistantRef() == pin.GetAssistantRef() && fresh.GetTarget().GetSystemAssistantRef() == pin.GetAssistantRef() &&
		oldNode.GetAgentRef() == pin.GetAssistantRef() && newNode.GetAgentRef() == pin.GetAssistantRef() &&
		old.GetSessionRef() != "" && old.GetSessionRef() == fresh.GetSessionRef() && old.GetAttempt() > 0 && fresh.GetAttempt() > old.GetAttempt() && fresh.GetAttempt()-old.GetAttempt() == 1
}
