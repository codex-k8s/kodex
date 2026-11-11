package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestNativeSessionOwnerComponent drives real owner commands and PostgreSQL CAS with synthetic worker metadata.
// It proves no engine/file/S3 custody; those boundaries have independent native and worker qualification.
// TestNativeSessionOwnerComponent выполняет настоящие owner commands и PostgreSQL CAS с synthetic worker metadata.
// Не доказывает engine/file/S3 custody: эти границы квалифицируются отдельно.
func TestNativeSessionOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	var constraintDigests []byte
	if err := pool.QueryRow(ctx, `SELECT jsonb_object_agg(conname,md5(pg_get_constraintdef(oid))) FROM pg_constraint WHERE conrelid IN ('control_plane.session_storage'::regclass,'control_plane.session_archives'::regclass) AND conname IN ('session_storage_native_shape','session_storage_native_writer','session_storage_native_writer_state','session_archives_format_shape')`).Scan(&constraintDigests); err != nil {
		t.Fatal("inspect native constraint metadata", err)
	}
	t.Logf("canonical native constraint digests: %s", constraintDigests)
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, r)
	configureComponentProviderAndRoleImages(t, r)
	profile := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "synthetic", BaseURL: "https://model.example.invalid/v1", AddressCIDRs: []string{"192.0.2.1/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "gpt-5", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	if r.ConfigureModelProvider(profile, "gpt-5") != nil || r.Bootstrap(ctx) != nil {
		t.Fatal("bootstrap native owner fixture")
	}
	seedObservedCatalogFixture(t, ctx, r, func(observation *port.ProviderModelCatalogObservation) {
		observation.Models = []port.ProviderModelCatalogRecord{{ID: "gpt-5"}}
	})
	owner, worker := resolveRunLaunchComponentPrincipals(t, ctx, r)
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "native-project"}, Payload: command.ProjectInput{Name: "Native owner fixture", Purpose: "Synthetic checkpoint lifecycle", Language: "ru"}})
	if err != nil || project.Project == nil {
		t.Fatal("create native project", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "native-agent", "Native specialist")
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "native-launch"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Native checkpoint", Task: "Return the synthetic marker.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}})
	if err != nil || launched.Run == nil {
		t.Fatal("launch native run", err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-claim-1"}, Payload: command.LeaseInput{WorkloadInstance: "native-owner-fixture", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim native writer: items=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	var organizationID string
	if pool.QueryRow(ctx, `SELECT organization_id::text FROM control_plane.projects WHERE ref=$1`, project.Project.Ref).Scan(&organizationID) != nil {
		t.Fatal("read synthetic owner identity")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, sourceErr := nativeSessionSource(ctx, tx, scope{organizationID: organizationID}, lease)
	_ = tx.Rollback(ctx)
	if !errors.Is(sourceErr, errs.ErrConflict) {
		t.Fatal("unconfirmed initial writer allowed an empty recovery", sourceErr)
	}
	payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Success: true, ResultSummary: "Synthetic native result", Usage: turnUsageFixture()}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-missing"}, Payload: payload}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("native success accepted without state", err)
	}
	path, _ := runtimecontract.NativeSessionExportPath("ses_NativeOwner", 1)
	checkpoint := runtimecontract.NativeSessionCheckpoint{SessionID: "ses_NativeOwner", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: path, SHA256: strings.Repeat("d", 64), SizeBytes: 100}
	payload.NativeSession = &checkpoint
	for i := 0; i < 2; i++ {
		completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-complete"}, Payload: payload})
		if err != nil || completed.Run == nil || completed.Run.State != "SUCCEEDED" {
			t.Fatal("native complete/replay failed", err)
		}
	}
	continued, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "native-next"}, Payload: command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: launched.Run.Ref, Task: "Continue with fresh instructions."}})
	if err != nil || continued.Run == nil {
		t.Fatal("authorize next native turn", err)
	}
	claimed, err = service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-claim-2"}, Payload: command.LeaseInput{WorkloadInstance: "native-owner-fixture", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim native continuation: items=%d err=%v", len(claimed.RuntimeItems), err)
	}
	var stored map[string]any
	raw, err := json.Marshal(claimed.RuntimeItems[0])
	if err != nil || json.Unmarshal(raw, &stored) != nil {
		t.Fatal("decode native claim")
	}
	// The owner supplies an exact prior checkpoint rather than inventing a new session from text.
	// Owner передаёт exact prior checkpoint, а не создаёт новую session из текста.
	raw, err = json.Marshal(stored["nativeSession"])
	got, decodeErr := runtimecontract.DecodeNativeSessionCheckpoint(raw)
	if err != nil || decodeErr != nil || got != checkpoint {
		t.Fatal("owner lost exact native checkpoint", decodeErr)
	}
	lease = claimed.RuntimeItems[0]
	var version int64
	if pool.QueryRow(ctx, `SELECT version FROM control_plane.runs WHERE ref=$1`, continued.Run.Ref).Scan(&version) != nil {
		t.Fatal("read current synthetic run version")
	}
	cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "native-stop", ExpectedVersion: &version}, Payload: command.RunCommandInput{RunRef: continued.Run.Ref, Reason: "Synthetic stop"}})
	if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatal("stop native execution", err)
	}
	checkpoint.Generation = 2
	checkpoint.RelativePath, _ = runtimecontract.NativeSessionExportPath(checkpoint.SessionID, 2)
	checkpoint.SHA256 = strings.Repeat("e", 64)
	payload = command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Success: true, ResultSummary: "Observed result after owner stop", Usage: turnUsageFixture(), NativeSession: &checkpoint}
	wrong := payload
	wrong.Fence = "fnc_foreign"
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-late-foreign"}, Payload: wrong}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign stopped-writer fence accepted", err)
	}
	for i := 0; i < 2; i++ {
		fact, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-late-fact"}, Payload: payload})
		if err != nil || fact.Run == nil || fact.Run.State != "CANCELLED" || fact.Graph == nil || fact.Runtime["nativeCheckpointRecorded"] != true {
			t.Fatal("stopped checkpoint revived the run or was lost", err)
		}
	}
	continued, err = service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "native-explicit-resume"}, Payload: command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: continued.Run.Ref, Task: "Explicitly resume the stopped native session."}})
	if err != nil || continued.Run == nil {
		t.Fatal("explicitly resume native session", err)
	}
	claimed, err = service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-claim-3"}, Payload: command.LeaseInput{WorkloadInstance: "native-owner-fixture", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim after stop: items=%d err=%v", len(claimed.RuntimeItems), err)
	}
	raw, _ = json.Marshal(claimed.RuntimeItems[0]["nativeSession"])
	got, decodeErr = runtimecontract.DecodeNativeSessionCheckpoint(raw)
	if decodeErr != nil || got != checkpoint {
		t.Fatal("explicit resume lost stopped writer history", decodeErr)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "native-stale-after-new-writer"}, Payload: payload}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("old checkpoint overwrote a newer native writer", err)
	}
}
