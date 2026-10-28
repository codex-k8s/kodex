package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
)

var (
	// queryCommitAckAbort aborts only a disposable synthetic transaction before its real COMMIT.
	// queryCommitAckAbort прерывает только disposable synthetic transaction до её настоящего COMMIT.
	//go:embed testdata/sql/commit_ack_abort.sql
	queryCommitAckAbort string
	// queryCommitAckProof reads committed ownership/cardinality independently of the damaged connection.
	// queryCommitAckProof читает committed ownership/cardinality независимо от повреждённого соединения.
	//go:embed testdata/sql/commit_ack_proof.sql
	queryCommitAckProof string
)

// TestCommitAckArtifactComponent proves both object-writing owner paths under real commit ACK loss.
// Every fault follows a genuine receipt INSERT and server COMMIT; no ready state is inserted by hand.
// TestCommitAckArtifactComponent проверяет оба object-writing owner path при настоящей потере COMMIT ACK.
// Каждый fault следует за настоящими receipt INSERT и server COMMIT; готовые state не вставляются вручную.
func TestCommitAckArtifactComponent(t *testing.T) {
	if os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN") == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	for _, path := range []string{"COMPLETE", "UPLOAD"} {
		for _, fault := range []string{"ACK_EOF", "ACK_CANCEL", "KNOWN_ROLLBACK"} {
			t.Run(path+"/"+fault, func(t *testing.T) { testCommitAckArtifact(t, ctx, path, fault) })
		}
	}
}

// testCommitAckArtifact uses actual native completion claims or the public upload service with
// a separate non-injected pool for authoritative readback and fresh owner downloads.
// testCommitAckArtifact использует настоящие native completion claims либо публичный upload service
// и отдельный non-injected pool для authoritative readback и свежего owner download.
func testCommitAckArtifact(t *testing.T, ctx context.Context, path, fault string) {
	t.Helper()
	dsn := isolatedAssistantComponentDSN(t)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse isolated commit ACK fixture")
	}
	trace := newCommitAckTrace(io.ErrUnexpectedEOF)
	trace.rollback = fault == "KNOWN_ROLLBACK"
	config.ConnConfig.Tracer = trace
	config.ConnConfig.BuildFrontend = trace.build
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open injected commit ACK fixture")
	}
	defer pool.Close()
	readPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open independent commit ACK reader")
	}
	defer readPool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	store := &commitAckStore{Store: repository.objects}
	repository.objects = store
	readerRepository := *repository
	readerRepository.pool = readPool
	readerService, err := platformservice.New(&readerRepository)
	if err != nil {
		t.Fatal("configure independent owner read service")
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		actor := owner
		if kind == command.ClaimExecution {
			actor = worker
		}
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "commit-ack-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("commit ACK setup failed: outcome=%s", completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Commit ACK fixture", Language: "ru"}).Project
	body := []byte("Неизменяемый проверенный материал COMMIT ACK.\n")
	digest := sha256.Sum256(body)
	digestHex := hex.EncodeToString(digest[:])
	key, operation := "commit-ack-upload", "artifact.upload"
	var wire []byte
	var runRef string
	if path == "COMPLETE" {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "commit-ack-agent", "Исполнитель")
		agent = *execute(command.ChangeAgentCapability, "capability", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		run := execute(command.LaunchRun, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Создать проверенный материал.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
		runRef = run.Ref
		items := execute(command.ClaimExecution, "claim", nil, command.LeaseInput{WorkloadInstance: "commit-ack-fixture", Limit: 1}).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runRef") != run.Ref {
			t.Fatal("real ordinary-agent claim missing")
		}
		claimed := items[0]
		usage := turnUsageFixture()
		request := &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: "commit-ack-complete"}, LeaseRef: stringMap(claimed, "leaseRef"), Fence: stringMap(claimed, "fence"), Generation: runtimeRevisionMapInt64(claimed, "generation"), Success: true, ResultSummary: "Verified commit ACK fixture", Artifacts: []*controlplanev1.CompletedArtifactInput{{FileName: "receipt.md", MediaType: "text/markdown", SizeBytes: int64(len(body)), Content: body, Sha256: digestHex}}, Usage: &controlplanev1.TokenUsage{TotalTokens: usage.TotalTokens, InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens, OutputTokens: usage.OutputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens, ModelContextWindow: usage.ModelContextWindow}}
		wire, err = proto.Marshal(request)
		if err != nil {
			t.Fatal("encode native commit ACK fixture")
		}
		_, key = completionPayloadFromWire(t, wire)
		operation = "controlplane.complete_execution"
	}
	call := func(callCtx context.Context, actor value.Principal, changed bool) (string, error) {
		t.Helper()
		if path == "UPLOAD" {
			content := body
			if changed {
				content = []byte("Different upload content")
			}
			item, err := service.UploadArtifact(callCtx, actor, value.Mutation{IdempotencyKey: key}, platformrepo.ArtifactUpload{ProjectRef: project.Ref, FileName: "receipt.md", MediaType: "text/markdown", SizeBytes: int64(len(content)), Reader: bytes.NewReader(content)})
			return item.Ref, err
		}
		payload, payloadKey := completionPayloadFromWire(t, wire)
		if changed {
			payload.ResultSummary = "Different completion intent"
		}
		result, err := service.Execute(callCtx, command.Command{Kind: command.CompleteExecution, Principal: actor, Mutation: value.Mutation{IdempotencyKey: payloadKey}, Payload: payload})
		if len(result.CreatedRefs) == 1 {
			return result.CreatedRefs[0], err
		}
		return "", err
	}
	actor := owner
	if path == "COMPLETE" {
		actor = worker
	}
	faultContext, faultCancel := context.WithCancel(ctx)
	defer faultCancel()
	if fault == "ACK_CANCEL" {
		trace.failure = context.Canceled
		trace.cancel = faultCancel
	}
	_, firstErr := call(context.WithValue(faultContext, commitAckFaultKey{}, true), actor, false)
	if !errors.Is(firstErr, errs.ErrConflict) {
		t.Fatalf("injected commit must stay unsuccessful: outcome=%s", completionOutcomeClass(firstErr))
	}
	trace.mu.Lock()
	selected := trace.selected
	trace.mu.Unlock()
	if selected == nil {
		t.Fatal("exact receipt COMMIT was not selected")
	}
	store.mu.Lock()
	if len(store.receipts) != 1 {
		store.mu.Unlock()
		t.Fatal("first owner call did not perform exactly one PUT")
	}
	receipt := store.receipts[0]
	store.mu.Unlock()
	var ref string
	var artifacts, content, receipts, audits, events int64
	readProof := func() {
		t.Helper()
		if err := readPool.QueryRow(ctx, queryCommitAckProof, pgx.StrictNamedArgs{"project_ref": project.Ref, "receipt_key": key, "operation": operation, "object_key": receipt.Key, "object_version": receipt.VersionID, "digest": receipt.Digest}).Scan(&ref, &artifacts, &content, &receipts, &audits, &events); err != nil {
			t.Fatal("read independent commit ACK proof")
		}
	}
	readProof()
	object, objectErr := store.Get(ctx, receipt.Key, receipt.VersionID)
	var observed []byte
	if objectErr == nil {
		observed, err = io.ReadAll(object.Body)
		closeErr := object.Body.Close()
		if err != nil || closeErr != nil {
			t.Fatal("read exact committed object body")
		}
	}
	preserved := objectErr == nil && bytes.Equal(observed, body)
	t.Logf("COMMIT_ACK_PROOF path=%s fault=%s commit_idle=%t ack_withheld=%t known_abort=%t artifacts=%d content=%d receipts=%d audits=%d events=%d deletes=%d exact_bytes=%t", path, fault, selected.idle.Load(), selected.fired.Load(), trace.rollbacks.Load() == 1 && trace.rollbackTag.Load(), artifacts, content, receipts, audits, events, store.deletes.Load(), preserved)
	if fault == "KNOWN_ROLLBACK" {
		if trace.rollbacks.Load() != 1 || !trace.rollbackTag.Load() || selected.fired.Load() || artifacts != 0 || content != 0 || receipts != 0 || audits != 0 || events != 0 || store.deletes.Load() != 1 || !errors.Is(objectErr, objectstorage.ErrNotFound) {
			t.Fatal("known rollback did not clean only uncommitted content")
		}
		return
	}
	if !selected.idle.Load() || !selected.fired.Load() || artifacts != 1 || content != 1 || receipts != 1 || audits != 1 || events != 1 || store.deletes.Load() != 0 || !preserved {
		t.Fatal("committed artifact was lost after unknown COMMIT ACK")
	}
	download, err := readerService.DownloadArtifact(ctx, owner, ref, "DOWNLOAD")
	if err != nil {
		t.Fatalf("independent owner download failed: outcome=%s", completionOutcomeClass(err))
	}
	actual, readErr := io.ReadAll(download.Reader)
	closeErr := download.Reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, body) || download.Artifact.Digest != receipt.Digest {
		t.Fatal("owner download changed exact committed bytes")
	}
	replayedRef, replayErr := call(ctx, actor, false)
	if replayErr != nil || replayedRef != ref {
		t.Fatalf("unknown COMMIT replay failed: outcome=%s", completionOutcomeClass(replayErr))
	}
	if _, err := call(ctx, actor, true); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("changed intent accepted after unknown COMMIT")
	}
	foreign := actor
	foreign.AuthorityTenant = "00000000-0000-4000-8000-000000000000"
	if _, err := call(ctx, foreign, false); err == nil {
		t.Fatal("foreign actor received committed receipt")
	}
	readProof()
	store.mu.Lock()
	puts := len(store.receipts)
	store.mu.Unlock()
	if puts != 1 || store.deletes.Load() != 0 || artifacts != 1 || content != 1 || receipts != 1 || audits != 1 || events != 1 {
		t.Fatal("replay or negatives duplicated owner effects")
	}
	if runRef != "" {
		read, err := readerService.GetRun(ctx, owner, runRef)
		if err != nil || read.State != "SUCCEEDED" || read.Usage != turnUsageFixture() {
			t.Fatal("unknown COMMIT lost original completion or usage")
		}
	}
}
