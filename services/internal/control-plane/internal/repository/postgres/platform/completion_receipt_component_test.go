package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
)

// queryCompletionReceiptProof observes only synthetic cardinalities and immutable identity checks.
// queryCompletionReceiptProof читает только synthetic cardinalities и immutable identity checks.
//
//go:embed testdata/sql/completion_receipt_proof.sql
var queryCompletionReceiptProof string

// completionReceiptProof contains safe result counts and booleans without authority locators.
// completionReceiptProof содержит безопасные result counts и bool без authority locators.
type completionReceiptProof struct {
	RootState, LeaseState                                                                         string
	Artifacts, Revisions, Content, Receipts, Audit, Turns, ArtifactEvents, Callbacks, Invocations int64
	AgentCapability, RevisionCapability, ActorActive, FenceMatches, GenerationMatches             bool
}

// completionCallbackOutcome retains one owner result privately and exposes only a closed error class.
// completionCallbackOutcome хранит один owner result приватно и раскрывает только closed error class.
type completionCallbackOutcome struct {
	result command.Result
	err    error
}

// completionOutcomeClass never formats arbitrary database or application error text.
// completionOutcomeClass никогда не форматирует произвольный database/application error text.
func completionOutcomeClass(err error) string {
	switch {
	case err == nil:
		return "SUCCEEDED"
	case errors.Is(err, errs.ErrForbidden):
		return "FORBIDDEN"
	case errors.Is(err, errs.ErrNotFound):
		return "NOT_FOUND"
	case errors.Is(err, errs.ErrIdempotencyReuse):
		return "INTENT_REUSE"
	case errors.Is(err, errs.ErrConflict):
		return "CONFLICT"
	default:
		return "OTHER_ERROR"
	}
}

// TestCompletionReceiptComponent reproduces the common preflight receipt/lease race with two
// independent native payloads and actual ordinary-agent authority. It never runs DAG/SSH/integration
// continuations, creates a fake lease, or changes any production guard to arrange its schedule.
// TestCompletionReceiptComponent воспроизводит общую preflight receipt/lease гонку двумя
// независимыми native payloads и настоящей authority обычного агента. Не запускает DAG/SSH/integration
// continuations, не создаёт fake lease и не меняет production guards ради расписания.
func TestCompletionReceiptComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated completion fixture")
	}
	trace := newCompletionReceiptTrace()
	defer trace.release()
	config.ConnConfig.Tracer = trace
	config.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open isolated completion fixture")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	store := &completionTrackedStore{Store: repository.objects, trace: trace}
	repository.objects = store
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		actor := owner
		if kind == command.ClaimExecution {
			actor = worker
		}
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "completion-fixture-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("completion fixture command failed: kind=%s outcome=%s", kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Completion receipt", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "completion-fixture-agent", "Исполнитель")
	agent = *execute(command.ChangeAgentCapability, "artifact-cap", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	run := execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Создать маленький проверенный материал.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	items := execute(command.ClaimExecution, "claim", nil, command.LeaseInput{WorkloadInstance: "completion-receipt-fixture", Limit: 1}).RuntimeItems
	if len(items) != 1 || stringMap(items[0], "runRef") != run.Ref {
		t.Fatal("actual ordinary-agent claim missing")
	}
	claimed := items[0]
	body := []byte("Проверенный материал completion receipt.\n")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	usage := turnUsageFixture()
	request := &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: "completion-receipt-exact"}, LeaseRef: stringMap(claimed, "leaseRef"), Fence: stringMap(claimed, "fence"), Generation: runtimeRevisionMapInt64(claimed, "generation"), Success: true, ResultSummary: "Verified completion fixture", Artifacts: []*controlplanev1.CompletedArtifactInput{{FileName: "report.md", MediaType: "text/markdown", SizeBytes: int64(len(body)), Content: body, Sha256: digest}}, Usage: &controlplanev1.TokenUsage{TotalTokens: usage.TotalTokens, InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens, OutputTokens: usage.OutputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens, ModelContextWindow: usage.ModelContextWindow}}
	wire, err := proto.Marshal(request)
	if err != nil {
		t.Fatal("encode native completion fixture")
	}
	a, keyA := completionPayloadFromWire(t, wire)
	b, keyB := completionPayloadFromWire(t, wire)
	if keyA != keyB || &a.Artifacts[0] == &b.Artifacts[0] || &a.Artifacts[0].Content[0] == &b.Artifacts[0].Content[0] {
		t.Fatal("native completion payloads are not independent")
	}
	proof := func() completionReceiptProof {
		t.Helper()
		var proof completionReceiptProof
		fence := sha256.Sum256([]byte(request.Fence))
		if err := pool.QueryRow(ctx, queryCompletionReceiptProof, pgx.StrictNamedArgs{"run_ref": run.Ref, "lease_ref": request.LeaseRef, "receipt_key": keyA, "artifact_digest": "sha256:" + digest, "fence_digest": hex.EncodeToString(fence[:]), "generation": request.Generation}).Scan(&proof.RootState, &proof.LeaseState, &proof.Artifacts, &proof.Revisions, &proof.Content, &proof.Receipts, &proof.Audit, &proof.Turns, &proof.ArtifactEvents, &proof.Callbacks, &proof.Invocations, &proof.AgentCapability, &proof.RevisionCapability, &proof.ActorActive, &proof.FenceMatches, &proof.GenerationMatches); err != nil {
			t.Fatal("read synthetic completion proof")
		}
		return proof
	}
	call := func(callback string, payload command.CompleteExecutionInput, key string, actor value.Principal) completionCallbackOutcome {
		callbackContext := context.WithValue(ctx, completionCallbackKey{}, callback)
		result, err := service.Execute(callbackContext, command.Command{Kind: command.CompleteExecution, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload})
		return completionCallbackOutcome{result: result, err: err}
	}
	aResult, bResult := make(chan completionCallbackOutcome, 1), make(chan completionCallbackOutcome, 1)
	go func() { aResult <- call("A", a, keyA, worker) }()
	select {
	case <-trace.aHeld:
	case <-ctx.Done():
		trace.release()
		t.Fatal("first completion did not reach main lease lock")
	}
	go func() { bResult <- call("B", b, keyB, worker) }()
	select {
	case <-trace.bWindow:
	case <-ctx.Done():
		trace.release()
		t.Fatal("second completion did not reach deterministic preflight window")
	}
	trace.release()
	var first, second completionCallbackOutcome
	select {
	case first = <-aResult:
	case <-ctx.Done():
		t.Fatal("first completion did not finish")
	}
	select {
	case second = <-bResult:
	case <-ctx.Done():
		t.Fatal("second completion did not finish")
	}
	stored := proof()
	trace.mu.Lock()
	events := append([]completionTraceEvent(nil), trace.events...)
	windowMode := trace.windowMode
	trace.mu.Unlock()
	report, _ := json.Marshal(map[string]any{"callbackA": completionOutcomeClass(first.err), "callbackB": completionOutcomeClass(second.err), "window": windowMode, "proof": stored, "puts": store.puts.Load(), "objectStoreInsideTransaction": store.insideTx.Load(), "stages": events})
	t.Log("COMPLETION_RECEIPT_PROOF " + string(report))
	if first.err != nil || second.err != nil {
		t.Fatalf("same completion must return one receipt: first=%s second=%s", completionOutcomeClass(first.err), completionOutcomeClass(second.err))
	}
	if first.result.Run == nil || second.result.Run == nil || first.result.Run.Ref != second.result.Run.Ref || len(first.result.CreatedRefs) != 1 || !reflect.DeepEqual(first.result.CreatedRefs, second.result.CreatedRefs) {
		t.Fatal("concurrent callbacks returned different owner outcomes")
	}
	if stored.RootState != "SUCCEEDED" || stored.LeaseState != "COMPLETED" || stored.Artifacts != 1 || stored.Revisions != 1 || stored.Content != 1 || stored.Receipts != 1 || stored.Audit != 1 || stored.Turns != 1 || stored.ArtifactEvents != 1 || stored.Callbacks != 0 || stored.Invocations != 0 || !stored.AgentCapability || !stored.RevisionCapability || !stored.ActorActive || !stored.FenceMatches || !stored.GenerationMatches || store.puts.Load() != 1 || store.insideTx.Load() {
		t.Fatal("completion receipt cardinality, pins or external-effect transaction boundary changed")
	}
	// Lost ACK: ещё один независимо decoded запрос того же intent не повторяет persistence/PUT.
	replayed, key := completionPayloadFromWire(t, wire)
	replay := call("REPLAY", replayed, key, worker)
	if replay.err != nil || replay.result.Run == nil || replay.result.Run.Ref != first.result.Run.Ref || proof() != stored || store.puts.Load() != 1 {
		t.Fatal("fresh sequential receipt replay duplicated or lost committed outcome")
	}
	changed, key := completionPayloadFromWire(t, wire)
	changed.ResultSummary = "Different semantic completion"
	if got := call("NEGATIVE", changed, key, worker); !errors.Is(got.err, errs.ErrIdempotencyReuse) {
		t.Fatal("same receipt key accepted another semantic intent")
	}
	wrongLease, key := completionPayloadFromWire(t, wire)
	wrongLease.Fence += "invalid"
	if got := call("NEGATIVE", wrongLease, key+"-fresh", worker); !errors.Is(got.err, errs.ErrForbidden) {
		t.Fatal("fresh invalid lease bypassed completion guards")
	}
	foreign, key := completionPayloadFromWire(t, wire)
	foreignWorker := worker
	foreignWorker.AuthorityTenant = "00000000-0000-4000-8000-000000000000"
	if got := call("NEGATIVE", foreign, key, foreignWorker); got.err == nil {
		t.Fatal("foreign tenant obtained another completion receipt")
	}
	if proof() != stored || store.puts.Load() != 1 {
		t.Fatal("closed completion negatives persisted state or repeated ObjectStore effect")
	}
	read, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil || read.Usage != usage {
		t.Fatal("receipt replay lost the original usage")
	}
}
