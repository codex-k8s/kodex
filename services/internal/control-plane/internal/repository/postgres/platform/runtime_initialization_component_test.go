package platform

import (
	"context"
	_ "embed"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryInitializationCompletionProof reads existing owner storage and immutable cardinality.
// queryInitializationCompletionProof читает existing owner storage и immutable cardinality.
//
//go:embed testdata/sql/runtime_initialization_completion_proof.sql
var queryInitializationCompletionProof string

// TestRuntimeInitializationCompletionComponent proves durable owner summary after caller cleanup.
// All business state uses native commands; local termination-file deletion is explicitly not a
// live Pod cleanup proof. RC lifecycle tests separately verify ACK-before-DeleteTurn ordering.
// TestRuntimeInitializationCompletionComponent проверяет durable owner summary после caller cleanup.
// Business state создаётся native commands; удаление local termination file не доказывает live Pod
// cleanup. Отдельные RC lifecycle tests проверяют порядок ACK-before-DeleteTurn.
func TestRuntimeInitializationCompletionComponent(t *testing.T) {
	for _, pair := range [][2]string{{"ARTIFACT_TRANSFER", "AUTHORITY_REJECTED"}, {"UNKNOWN", "UNKNOWN"}} {
		t.Run(pair[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal("open isolated initialization owner fixture")
			}
			defer pool.Close()
			_, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
			execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
				t.Helper()
				result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload})
				if err != nil {
					t.Fatalf("native initialization fixture command failed: %s", kind)
				}
				return result
			}
			project := execute(command.CreateProject, owner, "init-project", command.ProjectInput{Name: "Initialization persistence", Language: "ru"}).Project
			if project == nil {
				t.Fatal("initialization project missing")
			}
			agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "init-worker", "Initialization worker")
			launched := execute(command.LaunchRun, owner, "init-launch", command.LaunchRunInput{ProjectRef: project.Ref, Task: "Read pinned files without provider work", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			if launched == nil {
				t.Fatal("initialization run missing")
			}
			claims := execute(command.ClaimExecution, worker, "init-claim", command.LeaseInput{WorkloadInstance: "initialization-fixture", Limit: 1}).RuntimeItems
			if len(claims) != 1 || stringMap(claims[0], "runRef") != launched.Ref {
				t.Fatal("exact initialization lease missing")
			}
			claim := claims[0]
			summary := runtimecontract.InitializationFailureSummary(pair[0], pair[1])
			recordFile := filepath.Join(t.TempDir(), "termination")
			if err := os.WriteFile(recordFile, []byte(summary), 0600); err != nil {
				t.Fatal("write synthetic caller record")
			}
			payload := command.CompleteExecutionInput{LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), ResultSummary: summary, SafeErrorCode: runtimecontract.InitializationFailureCode}
			completed := execute(command.CompleteExecution, worker, "init-complete", payload)
			if completed.Run == nil || completed.Run.State != "FAILED" {
				t.Fatal("owner initialization failure not committed")
			}
			if err := os.Remove(recordFile); err != nil {
				t.Fatal("caller cleanup failed")
			}
			assertStored := func() {
				t.Helper()
				var nodeSummary, runSummary, code string
				var events, receipts int64
				if err := pool.QueryRow(ctx, queryInitializationCompletionProof, stringMap(claim, "nodeRef"), "init-complete").Scan(&nodeSummary, &runSummary, &code, &events, &receipts); err != nil {
					t.Fatal("read durable initialization owner proof")
				}
				if nodeSummary != summary || runSummary != summary || code != runtimecontract.InitializationFailureCode || events != 1 || receipts != 1 {
					t.Fatal("summary or immutable event/receipt cardinality changed")
				}
			}
			assertStored()
			replay := execute(command.CompleteExecution, worker, "init-complete", payload)
			if !replay.Duplicate {
				t.Fatal("exact failure replay was not recognized")
			}
			assertStored()
			bad := payload
			bad.ResultSummary = "PRIVATE_SENTINEL"
			if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "init-invalid"}, Payload: bad}); err == nil {
				t.Fatal("malformed init summary accepted")
			}
			assertStored()
			_, graph, err := service.GetRunGraph(ctx, owner, launched.Ref)
			if err != nil {
				t.Fatal("protected initialization graph read failed")
			}
			found := false
			for _, node := range graph.Nodes {
				if node.Ref == stringMap(claim, "nodeRef") {
					found = node.ProgressSummary == summary && node.SafeErrorCode == runtimecontract.InitializationFailureCode
				}
			}
			if !found {
				t.Fatal("public graph lost safe initialization explanation")
			}
		})
	}
}
