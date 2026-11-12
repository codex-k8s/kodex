package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// nativeCompletionRevokeProof reads only terminal owner metadata and publication counts.
// nativeCompletionRevokeProof читает только terminal metadata владельца и число публикаций.
//
//go:embed testdata/sql/native_completion_revoke_proof.sql
var nativeCompletionRevokeProof string

// revokedCompletionStore triggers a real owner revoke after a successful synthetic Put.
// It counts exact cleanup calls without providing production durability evidence.
// revokedCompletionStore запускает настоящий owner revoke после synthetic Put.
// Считает точные cleanup calls, не доказывая production durability.
type revokedCompletionStore struct {
	*objectstoragetest.Store
	afterPut      func()
	puts, deletes int
}

// Put persists synthetic bytes before invoking the one-shot owner mutation outside any SQL transaction.
// Put сохраняет synthetic bytes до однократной owner mutation вне SQL transaction.
func (store *revokedCompletionStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	receipt, err := store.Store.Put(ctx, input)
	if err == nil {
		store.puts++
		if callback := store.afterPut; callback != nil {
			store.afterPut = nil
			callback()
		}
	}
	return receipt, err
}

// Delete counts successful version-specific removal without changing the store's selection rules.
// Delete считает успешные удаления точной версии без изменения правил выбора объекта.
func (store *revokedCompletionStore) Delete(ctx context.Context, key, version string) error {
	err := store.Store.Delete(ctx, key, version)
	if err == nil {
		store.deletes++
	}
	return err
}

// TestNativeCompletionCapabilityRevokeComponent proves denied artifact publication cannot discard
// an exact stopped native checkpoint, revive success, publish objects or duplicate terminal events.
// TestNativeCompletionCapabilityRevokeComponent доказывает, что отказ публикации artifacts не теряет
// exact stopped checkpoint, не возвращает SUCCESS, не публикует объекты и не дублирует terminal events.
func TestNativeCompletionCapabilityRevokeComponent(t *testing.T) {
	for _, mode := range []string{"before-staging", "after-staging", "after-cancel", "workflow-before-staging"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			pool := newIsolatedAssistantComponentPool(t, ctx)
			defer pool.Close()
			repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
			store := &revokedCompletionStore{Store: objectstoragetest.New()}
			repository.objects = store
			configureComponentArtifactScanner(t, repository)
			if err := repository.Bootstrap(ctx); err != nil {
				t.Fatal(err)
			}
			seedObservedCatalogFixture(t, ctx, repository)
			owner, worker := resolveRunLaunchComponentPrincipals(t, ctx, repository)
			service, err := platformservice.New(repository)
			if err != nil {
				t.Fatal(err)
			}
			project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "revoke-project"}, Payload: command.ProjectInput{Name: "Native revoke fixture", Purpose: "Synthetic completion", Language: "ru"}})
			if err != nil || project.Project == nil {
				t.Fatal("create project", err)
			}
			agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "revoke-agent", "Native revoke specialist")
			changeGrant := func(enabled bool, key string) {
				t.Helper()
				current, err := service.GetAgent(ctx, owner, agent.Ref)
				if err != nil {
					t.Fatal(err)
				}
				_, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &current.Version}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: enabled}})
				if err != nil {
					t.Fatal("change grant", err)
				}
			}
			changeGrant(true, "grant-before-claim")
			target := entity.RunTarget{Type: "AGENT", Ref: agent.Ref}
			if mode == "workflow-before-staging" {
				current, err := service.GetAgent(ctx, owner, agent.Ref)
				if err != nil {
					t.Fatal(err)
				}
				_, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "coordinator-delegate", ExpectedVersion: &current.Version}, Payload: command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.run.delegate", Enabled: true}})
				if err != nil {
					t.Fatal(err)
				}
				specialist := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "revoke-specialist", "Synthetic specialist")
				draft := entity.WorkflowVersion{Name: "Native revoke workflow", Purpose: "Synthetic coordinator completion", CoordinatorAgentRef: agent.Ref, Concurrency: 1, TimeoutSeconds: 180, CompletionCriteria: "Return verified result", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "first", Position: 1, Name: "First", AgentRef: specialist.Ref, Instructions: "Return the marker", ExpectedResult: "Synthetic result", TimeoutSeconds: 90}}}
				result, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "revoke-workflow-create"}, Payload: command.WorkflowInput{ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
				if err != nil || result.Workflow == nil {
					t.Fatal("create workflow", err)
				}
				for _, kind := range []command.Kind{command.ValidateWorkflow, command.PublishWorkflow} {
					result, err = service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "revoke-workflow-" + string(kind), ExpectedVersion: &result.Workflow.Version}, Payload: command.WorkflowInput{Ref: result.Workflow.Ref}})
					if err != nil || result.Workflow == nil {
						t.Fatal("publish workflow", err)
					}
				}
				target = entity.RunTarget{Type: "WORKFLOW", Ref: result.Workflow.Ref}
			}
			launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "revoke-launch"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Native revoke", Task: "Return synthetic result", Target: target}})
			if err != nil || launched.Run == nil {
				t.Fatal("launch", err)
			}
			claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "revoke-claim"}, Payload: command.LeaseInput{WorkloadInstance: "native-revoke-fixture", Limit: 1}})
			if err != nil || len(claimed.RuntimeItems) != 1 {
				t.Fatal("claim", err)
			}
			lease := claimed.RuntimeItems[0]
			payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Must not become published success", Usage: turnUsageFixture(), NativeSession: nativeCheckpointForClaimFixture(lease), Artifacts: []command.CompletedArtifact{workflowCaptureProgressArtifact("result.md", []byte("Synthetic denied result\n"))}}
			expectedState, expectedCode := "FAILED", "RUNTIME_INPUT_INVALID"
			if mode != "after-staging" {
				changeGrant(false, "revoke-before-staging")
				wrong := payload
				wrong.Fence = "fnc_foreign"
				if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "revoke-foreign"}, Payload: wrong}); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("foreign fence accepted", err)
				}

				missing := payload
				missing.NativeSession = nil
				if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "revoke-missing-checkpoint"}, Payload: missing}); !errors.Is(err, errs.ErrForbidden) {
					t.Fatal("missing checkpoint bypassed denial", err)
				}
				malformed := payload
				malformed.Artifacts = append([]command.CompletedArtifact(nil), payload.Artifacts...)
				malformed.Artifacts[0].SHA256 = "invalid"
				if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "revoke-malformed-artifact"}, Payload: malformed}); !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("malformed artifact bypassed denial", err)
				}
				if mode == "after-cancel" {
					run, err := service.GetRun(ctx, owner, launched.Run.Ref)
					if err != nil {
						t.Fatal(err)
					}
					result, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "revoke-stop", ExpectedVersion: &run.Version}, Payload: command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic owner stop"}})
					if err != nil || result.Run == nil || result.Run.State != "CANCELLED" {
						t.Fatal("stop", err)
					}
					expectedState, expectedCode = "CANCELLED", result.Run.SafeErrorCode
				}
			} else {
				store.afterPut = func() { changeGrant(false, "revoke-after-staging") }
			}
			var proof []byte
			frozenPayload, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				// Each RPC decodes fresh bytes; staging must not become part of the replayed client intent.
				// Каждый RPC разбирает свежие bytes; staging не входит в повторяемый клиентский intent.
				var request command.CompleteExecutionInput
				if json.Unmarshal(frozenPayload, &request) != nil {
					t.Fatal("decode completion replay")
				}
				completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "revoke-complete"}, Payload: request})
				if err != nil || completed.Run == nil || completed.Run.State != expectedState || completed.Run.SafeErrorCode != expectedCode || len(completed.CreatedRefs) != 0 {
					t.Fatalf("denied completion lost checkpoint or reported success: err=%v run=%v", err, completed.Run)
				}
				var current []byte
				if err := pool.QueryRow(ctx, nativeCompletionRevokeProof, launched.Run.Ref).Scan(&current); err != nil {
					t.Fatal(err)
				}
				var state struct {
					WriterOpen     bool
					Checkpoint     bool
					Leases         int
					Artifacts      int
					ArtifactEvents int
				}
				if json.Unmarshal(current, &state) != nil || state.WriterOpen || !state.Checkpoint || state.Leases != 0 || state.Artifacts != 0 || state.ArtifactEvents != 0 {
					t.Fatalf("terminal proof invalid: %s", current)
				}
				if attempt == 1 && string(current) != string(proof) {
					t.Fatal("replay changed terminal proof")
				}
				proof = current
			}
			expectedPuts := 0
			if mode == "after-staging" {
				expectedPuts = 1
			}
			if store.puts != expectedPuts || store.deletes != expectedPuts {
				t.Fatalf("staged object cleanup mismatch: puts=%d deletes=%d", store.puts, store.deletes)
			}
			continuation := command.Command{Kind: command.AddSessionTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "revoke-continue"}, Payload: command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: launched.Run.Ref, Task: "Continue without file privileges"}}
			if mode == "workflow-before-staging" {
				run, err := service.GetRun(ctx, owner, launched.Run.Ref)
				if err != nil {
					t.Fatal(err)
				}
				continuation.Kind, continuation.Payload, continuation.Mutation.ExpectedVersion = command.RetryRun, command.RunCommandInput{RunRef: run.Ref}, &run.Version
			}
			continued, err := service.Execute(ctx, continuation)
			if err != nil || continued.Run == nil {
				t.Fatal("continue", err)
			}
			next, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "revoke-next-claim"}, Payload: command.LeaseInput{WorkloadInstance: "native-revoke-fixture", Limit: 1}})
			if err != nil || len(next.RuntimeItems) != 1 {
				t.Fatal("resume denied checkpoint", err)
			}
			raw, _ := json.Marshal(next.RuntimeItems[0]["nativeSession"])
			restored, err := runtimecontract.DecodeNativeSessionCheckpoint(raw)
			if err != nil || restored != *payload.NativeSession {
				t.Fatal("exact checkpoint lost", err)
			}
			raw, _ = json.Marshal(next.RuntimeItems[0]["capabilities"])
			var capabilities []string
			if json.Unmarshal(raw, &capabilities) != nil {
				t.Fatal("read resumed capabilities")
			}
			for _, capability := range capabilities {
				if capability == runtimecontract.ArtifactCapability {
					t.Fatal("resume restored revoked capability")
				}
			}
		})
	}
}
