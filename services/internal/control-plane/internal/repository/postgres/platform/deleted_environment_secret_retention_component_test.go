package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// deletedEnvironmentSecretHistory reads terminal metadata and immutable version history without changing business state or accessing secret values.
// deletedEnvironmentSecretHistory читает terminal metadata и историю immutable versions без изменения business state и доступа к значениям секретов.
//
//go:embed testdata/sql/deleted_environment_secret_history.sql
var deletedEnvironmentSecretHistory string

// secretRetentionClaimReadback checks queued work, immutable revisions, leases and event cardinality without altering the owner graph.
// secretRetentionClaimReadback проверяет queued work, immutable revisions, leases и event cardinality без изменения owner graph.
//
//go:embed testdata/sql/secret_retention_claim_readback.sql
var secretRetentionClaimReadback string

// TestDeletedEnvironmentSecretRetentionComponent drives native owner commands and synthetic broker/runtime callbacks on disposable PostgreSQL to distinguish history from current ENV, binding and active execution consumers.
// TestDeletedEnvironmentSecretRetentionComponent выполняет штатные owner commands и синтетические broker/runtime callbacks на disposable PostgreSQL, различая историю и действующих ENV, binding, execution consumers.
func TestDeletedEnvironmentSecretRetentionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("construct disposable secret retention pool")
	}
	defer pool.Close()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap secret retention repository")
	}
	seedObservedCatalogFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	consume := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.consume")
	complete := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.complete")
	recoverer := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.recover")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	t.Run("effective-version-fallback", func(t *testing.T) { testRuntimeEnvironmentCandidateVersionFallback(t, ctx, pool) })
	t.Run("system-project-context", func(t *testing.T) { testSystemProjectEnvironmentCandidate(t, ctx, repository, service, owner, worker) })
	for _, mode := range []string{"deleted-only", "binding-and-active-revision", "stale-candidate"} {
		t.Run(mode, func(t *testing.T) {
			invoke := func(kind command.Kind, key string, version *int64, payload any) (command.Result, error) {
				return service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "secret-retention-" + mode + "-" + key, ExpectedVersion: version}, Payload: payload})
			}
			execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
				t.Helper()
				result, err := invoke(kind, key, version, payload)
				if err != nil {
					t.Fatalf("%s %s: %v", kind, key, err)
				}
				return result
			}
			project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Secret retention " + mode, Language: "en"}).Project
			helper := execute(command.CreateProjectAssistant, "helper", nil, command.ProjectAssistantInput{
				ProjectRef: project.Ref, Name: "Retention helper", Purpose: "Bounded secret retention fixture", Instructions: "Use only explicitly assigned resources.",
			}).ProjectAssistant
			base, err := service.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
			if err != nil || base.Environment.CurrentVersion.Image.ArtifactRef == "" {
				t.Fatal("project helper has no exact native image")
			}
			createInput := platformrepo.RuntimeSecretPrepareInput{Kind: "CREATE", ProjectRef: project.Ref, Name: "Retention marker", ValueType: "STRING",
				ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "secret-retention-" + mode + "-create"}}
			prepared, err := service.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.create"), createInput)
			if err != nil {
				t.Fatalf("prepare marker: %v", err)
			}
			claim, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{OperationGrant: prepared.OperationGrant, ClaimantID: "retention-broker"})
			if err != nil {
				t.Fatalf("claim marker: %v", err)
			}
			name, err := runtimesecret.VersionedKubernetesName(claim.SecretRef, claim.TargetRevision)
			if err != nil {
				t.Fatal(err)
			}
			material := entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: name, SecretKey: "value",
				SecretUID: "10000000-0000-4000-8000-000000001875", SecretResourceVersion: "1875", ContentSHA256: runtimeSecretHashA}
			secret, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{
				OperationRef: prepared.OperationRef, ClaimantID: "retention-broker", ClaimGeneration: claim.ClaimGeneration, Materialization: &material})
			if err != nil {
				t.Fatalf("complete marker: %v", err)
			}
			environmentInput := command.RuntimeEnvironmentInput{ProjectRef: project.Ref, Name: "Retention environment", ImageArtifactRef: base.Environment.CurrentVersion.Image.ArtifactRef,
				Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(), SecretBindings: []entity.RuntimeSecretBinding{{Name: "RETENTION_MARKER", SecretRef: secret.Ref, Revision: secret.CurrentRevision}}}
			environment := execute(command.CreateRuntimeEnvironment, "environment", nil, environmentInput).RuntimeEnvironment
			retainedVersion := environment.CurrentVersion
			rotationExpectedVersion := secret.Version
			secret = completeRuntimeSecretRotate(t, ctx, service, runtimeSecretOwnerPrincipal(owner, "secret.rotate"), consume, complete, secret, runtimeSecretHashB, "secret-retention-"+mode+"-rotate")
			revokeInput := func(key string) platformrepo.RuntimeSecretPrepareInput {
				return platformrepo.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: secret.Ref,
					Mutation: value.Mutation{IdempotencyKey: "secret-retention-" + mode + "-revoke-" + key, ExpectedVersion: &secret.Version}}
			}
			assertBlocked := func(key string) {
				t.Helper()
				if _, err := service.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.revoke"), revokeInput(key)); !errors.Is(err, errs.ErrResourceInUse) {
					t.Fatalf("%s did not retain secret: %v", key, err)
				}
			}
			recoverOld := func(want string) {
				t.Helper()
				result, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverer, platformrepo.RuntimeSecretRecoveryInput{OperationRef: prepared.OperationRef, Materialization: material})
				if err != nil || result.Action != want {
					t.Fatalf("old revision recovery=%s want=%s: %v", result.Action, want, err)
				}
			}
			assertBlocked("active-environment")
			recoverOld("KEEP")
			var lease map[string]any
			var releaseCandidate func()
			var pendingCandidate <-chan secretRetentionClaimResult
			var pendingRunRef string
			var pendingVersion, pendingEvents int64
			if mode != "deleted-only" {
				bound := execute(command.BindAgentRuntimeEnvironment, "bind", &base.AgentVersion, command.RuntimeEnvironmentBindingInput{AgentRef: helper.AgentRef, EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref}).RuntimeConfiguration
				// Публикация пустой v2 сохраняет binding на v1: текущий head больше не удерживает R1.
				environmentInput.Ref, environmentInput.SecretBindings = environment.Ref, nil
				environment = execute(command.PublishRuntimeEnvironment, "empty-head", &environment.Version, environmentInput).RuntimeEnvironment
				assertBlocked("historical-binding")
				recoverOld("KEEP")
				conversation := execute(command.CreateAssistantConversation, "conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
				turn := execute(command.AddAssistantTurn, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Return a synthetic bounded result.", DeliveryMode: "QUEUE"}).Conversation
				if mode == "stale-candidate" {
					if len(turn.Turns) != 1 {
						t.Fatal("synthetic queued turn identity is ambiguous")
					}
					pendingRunRef = turn.Turns[0].RunRef
					var state string
					var states []string
					var revisions, leases int64
					if err := pool.QueryRow(ctx, secretRetentionClaimReadback, pendingRunRef).Scan(&state, &pendingVersion, &states, &revisions, &leases, &pendingEvents); err != nil ||
						state != "RUNNING" || len(states) != 1 || states[0] != "QUEUED" || revisions != 0 || leases != 0 {
						t.Fatal("stale-candidate fixture has no exact unclaimed native root")
					}
					releaseCandidate, pendingCandidate = pauseSecretRetentionClaim(t, ctx, repository, worker)
				} else {
					claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
						Mutation: value.Mutation{IdempotencyKey: "secret-retention-" + mode + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "secret-retention-worker", Limit: 10}})
					if err != nil {
						t.Fatalf("claim active revision: %v", err)
					}
					for _, item := range claimed.RuntimeItems {
						if stringMap(item, "sessionRef") == turn.SessionRef {
							lease = item
						}
					}
					if lease == nil || stringMap(lease, "runtimeRevisionRef") == "" {
						t.Fatal("native active runtime revision was not claimed")
					}
					projections, ok := lease["secretProjections"].([]runtimecontract.RuntimeSecretProjection)
					if !ok || len(projections) != 1 || projections[0].Name != "RETENTION_MARKER" || projections[0].SecretUID != material.SecretUID || projections[0].SecretName != material.SecretName ||
						projections[0].SecretKey != material.SecretKey || projections[0].SecretResourceVersion != material.SecretResourceVersion || projections[0].ContentSHA256 != material.ContentSHA256 ||
						stringMap(lease, "runtimeEnvironmentRef") != environment.Ref || stringMap(lease, "runtimeEnvironmentDigest") != retainedVersion.Digest {
						t.Fatal("active runtime did not pin the historical environment and exact R1 material")
					}
				}
				execute(command.BindAgentRuntimeEnvironment, "restore-binding", &bound.AgentVersion, command.RuntimeEnvironmentBindingInput{AgentRef: helper.AgentRef, EnvironmentRef: base.Environment.Ref, VersionRef: base.Environment.CurrentVersion.Ref})
				fresh, err := service.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
				if err != nil || fresh.Environment.Ref != base.Environment.Ref || len(fresh.Environment.CurrentVersion.SecretDescriptors) != 0 {
					t.Fatal("historical secret binding was not removed by native restore")
				}
				if mode == "binding-and-active-revision" {
					run, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
					if err != nil || run.State != "RUNNING" {
						t.Fatal("active revision protection has no running owner")
					}
					assertBlocked("active-revision-after-unbind")
					recoverOld("KEEP")
				}
			}
			environment = execute(command.SetRuntimeEnvironmentEnabled, "disable", &environment.Version, command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: environment.Ref, Enabled: false}).RuntimeEnvironment
			if mode == "deleted-only" {
				assertBlocked("disabled-environment")
				recoverOld("KEEP")
			}
			var beforeState, beforeCurrent, beforeHistory string
			var beforeVersion int64
			if err := pool.QueryRow(ctx, deletedEnvironmentSecretHistory, environment.Ref).Scan(&beforeState, &beforeVersion, &beforeCurrent, &beforeHistory); err != nil {
				t.Fatal("read history before delete")
			}
			environment = execute(command.DeleteRuntimeEnvironment, "delete", &environment.Version, command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: environment.Ref}).RuntimeEnvironment
			var afterState, afterCurrent, afterHistory string
			var afterVersion int64
			if err := pool.QueryRow(ctx, deletedEnvironmentSecretHistory, environment.Ref).Scan(&afterState, &afterVersion, &afterCurrent, &afterHistory); err != nil ||
				beforeState != "DISABLED" || afterState != "DELETED" || afterVersion != beforeVersion+1 || afterCurrent != beforeCurrent || afterHistory != beforeHistory {
				t.Fatal("deletion changed immutable history or lost terminal metadata")
			}
			if _, err := service.GetRuntimeEnvironment(ctx, owner, environment.Ref); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("deleted environment remained available for ordinary read: %v", err)
			}
			if mode == "binding-and-active-revision" {
				assertBlocked("active-revision-after-delete")
				recoverOld("KEEP")
				completeClaimedExecutionWithSummary(t, ctx, service, worker, lease, "secret-retention-"+mode, false, "Synthetic retention result")
				run, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
				if err != nil || run.State != "SUCCEEDED" {
					t.Fatalf("actual execution did not complete: %v", err)
				}
			}
			recoverOld("DELETE")
			currentName, err := runtimesecret.VersionedKubernetesName(secret.Ref, secret.CurrentRevision)
			if err != nil {
				t.Fatal(err)
			}
			currentMaterial := entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: currentName, SecretKey: "value",
				SecretUID: "10000000-0000-4000-8000-000000000103", SecretResourceVersion: "103", ContentSHA256: runtimeSecretHashB}
			rotatedOperation, err := service.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.rotate"), platformrepo.RuntimeSecretPrepareInput{
				Kind: "ROTATE", SecretRef: secret.Ref, ValueType: secret.ValueType, ExpectedContentSHA256: runtimeSecretHashB,
				Mutation: value.Mutation{IdempotencyKey: "secret-retention-" + mode + "-rotate", ExpectedVersion: &rotationExpectedVersion}})
			if err != nil || rotatedOperation.State != "COMPLETED" {
				t.Fatalf("rotation receipt did not rejoin: %v", err)
			}
			currentRecovery, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverer, platformrepo.RuntimeSecretRecoveryInput{OperationRef: rotatedOperation.OperationRef, Materialization: currentMaterial})
			if err != nil || currentRecovery.Action != "KEEP" {
				t.Fatalf("unreferenced current secret revision was retired before revoke: %v", err)
			}
			foreignProject := execute(command.CreateProject, "foreign-project", nil, command.ProjectInput{Name: "Foreign retention scope " + mode, Language: "en"}).Project
			own := runtimeSecretOwnerPrincipal(owner, "secret.revoke")
			own.ProjectRef = gateTestProjectID(t, ctx, repository, owner, project.Ref)
			foreign := runtimeSecretOwnerPrincipal(owner, "secret.revoke")
			foreign.ProjectRef = gateTestProjectID(t, ctx, repository, owner, foreignProject.Ref)
			if _, err := service.PrepareRuntimeSecretOperation(ctx, foreign, revokeInput("foreign")); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("foreign project reached secret revocation: %v", err)
			}
			stale := revokeInput("stale")
			staleVersion := secret.Version - 1
			stale.Mutation.ExpectedVersion = &staleVersion
			if _, err := service.PrepareRuntimeSecretOperation(ctx, own, stale); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatalf("stale revoke version accepted: %v", err)
			}
			finalInput := revokeInput("final")
			revoke, err := service.PrepareRuntimeSecretOperation(ctx, own, finalInput)
			if err != nil {
				t.Fatalf("deleted history still blocks unused secret: %v", err)
			}
			revokeClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{OperationGrant: revoke.OperationGrant, ClaimantID: "retention-broker-revoke"})
			if err != nil || len(revokeClaim.RevisionDescriptors) != 2 {
				t.Fatalf("revoke lost exact revision descriptors: %v", err)
			}
			revoked, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{
				OperationRef: revoke.OperationRef, ClaimantID: "retention-broker-revoke", ClaimGeneration: revokeClaim.ClaimGeneration})
			if err != nil || revoked.State != "REVOKED" {
				t.Fatalf("complete unused secret revoke: %v", err)
			}
			replay, err := service.PrepareRuntimeSecretOperation(ctx, own, finalInput)
			if err != nil || replay.OperationRef != revoke.OperationRef || replay.TerminalSecret == nil || replay.TerminalSecret.State != "REVOKED" {
				t.Fatalf("terminal revoke receipt did not replay: %v", err)
			}
			if mode == "stale-candidate" {
				releaseCandidate()
				select {
				case result := <-pendingCandidate:
					if result.err != nil || len(result.result.RuntimeItems) != 0 {
						t.Fatalf("stale candidate was materialized or terminally rejected: %v", result.err)
					}
				case <-ctx.Done():
					t.Fatal("stale candidate did not resume within fixture budget")
				}
				var state string
				var states []string
				var version, revisions, leases, events int64
				if err := pool.QueryRow(ctx, secretRetentionClaimReadback, pendingRunRef).Scan(&state, &version, &states, &revisions, &leases, &events); err != nil ||
					state != "RUNNING" || version != pendingVersion || len(states) != 1 || states[0] != "QUEUED" || revisions != 0 || leases != 0 || events != pendingEvents {
					t.Fatal("stale pin changed the root or leaked a runtime revision, lease or event")
				}
				fresh, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
					Mutation: value.Mutation{IdempotencyKey: "secret-retention-stale-candidate-fresh-claim"}, Payload: command.LeaseInput{WorkloadInstance: "secret-retention-fresh-worker", Limit: 10}})
				if err != nil || len(fresh.RuntimeItems) != 1 {
					t.Fatalf("fresh next claim did not recover queued work: %v", err)
				}
				lease := fresh.RuntimeItems[0]
				projections, ok := lease["secretProjections"].([]runtimecontract.RuntimeSecretProjection)
				if !ok || len(projections) != 0 || stringMap(lease, "runRef") != pendingRunRef || stringMap(lease, "runtimeEnvironmentRef") != base.Environment.Ref ||
					stringMap(lease, "runtimeEnvironmentDigest") != base.Environment.CurrentVersion.Digest {
					t.Fatal("fresh next claim reused the revoked historical secret pin")
				}
				completeClaimedExecutionWithSummary(t, ctx, service, worker, lease, "secret-retention-stale-candidate-fresh", false, "Synthetic fresh candidate result")
			}
			if err := pool.QueryRow(ctx, deletedEnvironmentSecretHistory, environment.Ref).Scan(&afterState, &afterVersion, &afterCurrent, &afterHistory); err != nil ||
				afterState != "DELETED" || afterVersion != environment.Version || afterCurrent != beforeCurrent || afterHistory != beforeHistory {
				t.Fatal("secret recovery/revocation rewrote deleted environment history")
			}
		})
	}
}
