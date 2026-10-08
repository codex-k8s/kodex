package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAssistantSessionResumeComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := worker
	warm.Permission = "platform.runtime.warm.report"
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	invoke := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "resume-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	for _, scopeKind := range []string{"SYSTEM", "PROJECT"} {
		t.Run(scopeKind, func(t *testing.T) {
			projectRef := ""
			if scopeKind == "PROJECT" {
				projectRef = invoke(command.CreateProject, owner, "project", command.ProjectInput{Name: "Resume fixture", Language: "en"}).Project.Ref
				invoke(command.CreateProjectAssistant, owner, "profile", command.ProjectAssistantInput{ProjectRef: projectRef, Name: "Project helper", Purpose: "Synthetic session resume", Instructions: "Use approved resources."})
			}
			conversation := invoke(command.CreateAssistantConversation, owner, scopeKind+"-create", command.AssistantConversationInput{AssistantScope: scopeKind, ProjectRef: projectRef}).Conversation
			var restoreWhenQueued func()
			originalJSON := `{"operations":[{"instructions":"` + strings.Repeat("Полный исходный JSON 😀 <>& ", 600) + `"}],"complete":true}`
			claim := func(key string) map[string]any {
				t.Helper()
				content := "Synthetic " + key
				if key == "first" {
					content = originalJSON
				}
				invoke(command.AddAssistantTurn, owner, scopeKind+key+"-turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: content, DeliveryMode: "QUEUE"})
				if restoreWhenQueued != nil {
					restoreWhenQueued()
					restoreWhenQueued = nil
				}
				items := invoke(command.ClaimExecution, worker, scopeKind+key+"-claim", command.LeaseInput{WorkloadInstance: "resume-worker", Limit: 1}).RuntimeItems
				if len(items) != 1 {
					t.Fatalf("expected exact claim, got %d", len(items))
				}
				return items[0]
			}
			first := claim("first")
			if stringMap(first, "codexSessionID") != "" {
				t.Fatal("new conversation resumed a foreign provider thread")
			}
			threadID := "00000000-0000-4000-8000-000000000003"
			if scopeKind == "PROJECT" {
				threadID = "00000000-0000-4000-8000-000000000004"
			}
			payload := command.CompleteExecutionInput{LeaseRef: stringMap(first, "leaseRef"), Fence: stringMap(first, "fence"), Generation: first["generation"].(int64), Success: true, ResultSummary: "Synthetic final", Usage: turnUsageFixture(), CodexSessionID: threadID, ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/04/rollout-2026-10-04T00-00-00-" + threadID + ".jsonl", ArchiveSHA256: strings.Repeat("a", 64), ArchiveSizeBytes: 128}
			invoke(command.CompleteExecution, worker, scopeKind+"-complete", payload)
			invoke(command.CompleteExecution, worker, scopeKind+"-complete", payload)
			var generation int64
			if err := pool.QueryRow(ctx, `SELECT storage.content_generation FROM control_plane.session_storage storage JOIN control_plane.sessions session ON session.id=storage.session_id WHERE session.ref=$1`, conversation.SessionRef).Scan(&generation); err != nil || generation != 1 {
				t.Fatalf("confirmed helper binding missing or duplicated: %d %v", generation, err)
			}
			restoreWhenQueued = prepareAssistantSessionArchiveRoundTrip(t, ctx, r, service, worker, owner, conversation.Ref, conversation.SessionRef, scopeKind)
			second := claim("second")
			messages := runtimeRevisionSessionContext(second["sessionContext"])
			if len(messages) < 2 || messages[0].Role != "USER" || messages[0].Content != originalJSON || !json.Valid([]byte(messages[0].Content)) {
				t.Fatal("next turn received a truncated or foreign original USER JSON")
			}
			var persistedHistory []byte
			if err := pool.QueryRow(ctx, `SELECT safe_snapshot->'sessionContext' FROM control_plane.runtime_revisions WHERE ref=$1`, stringMap(second, "runtimeRevisionRef")).Scan(&persistedHistory); err != nil {
				t.Fatal(err)
			}
			var persistedMessages []map[string]string
			if json.Unmarshal(persistedHistory, &persistedMessages) != nil || len(persistedMessages) < 2 || persistedMessages[0]["content"] != originalJSON {
				t.Fatal("immutable runtime snapshot lost exact original USER JSON")
			}
			if stringMap(second, "codexSessionID") != threadID {
				var raw []byte
				pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE ref=$1`, stringMap(first, "runtimeRevisionRef")).Scan(&raw)
				var before map[string]any
				_ = json.Unmarshal(raw, &before)
				pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE ref=$1`, stringMap(second, "runtimeRevisionRef")).Scan(&raw)
				var after map[string]any
				_ = json.Unmarshal(raw, &after)
				t.Log("decoded compatibility", runtimeSessionResumeCompatible(before, after))
				for _, key := range []string{"organizationRef", "sessionRef", "agentRef", "providerAccountRef", "runtimeModel", "promptTemplateRef", "promptTemplateDigest", "roleRuntimeContractSHA256", "imageManifestDigest", "runtimeEnvironmentDigest"} {
					if stringMap(before, key) == "" {
						t.Log("missing required", key)
					}
				}
				for key, value := range before {
					if other, present := after[key]; present {
						a, _ := json.Marshal(value)
						b, _ := json.Marshal(other)
						if string(a) != string(b) {
							t.Log("changed snapshot key", key)
						}
					}
				}
				t.Fatal("unchanged exact helper session cold-started instead of native resume")
			}
			for _, field := range []string{"runtimeModel", "providerAccountRef", "runtimeRevision", "promptTemplateDigest", "runtimeEnvironmentDigest", "imageManifestDigest", "assistantProfileRef", "sessionRef", "integrationGrants"} {
				changed := map[string]any{}
				for key, value := range second {
					changed[key] = value
				}
				changed[field] = "changed"
				if runtimeSessionResumeCompatible(second, changed) {
					t.Fatalf("changed %s retained native history", field)
				}
			}
			var rawCurrent []byte
			if err := pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE ref=$1`, stringMap(second, "runtimeRevisionRef")).Scan(&rawCurrent); err != nil {
				t.Fatal(err)
			}
			var currentSnapshot map[string]any
			if json.Unmarshal(rawCurrent, &currentSnapshot) != nil || !runtimeSessionResumeCompatible(currentSnapshot, currentSnapshot) {
				t.Fatal("current complete compatibility snapshot rejected")
			}
			var historyOnly map[string]any
			if json.Unmarshal(rawCurrent, &historyOnly) != nil {
				t.Fatal("synthetic history snapshot cannot be decoded")
			}
			historyOnly["task"], historyOnly["turnRef"], historyOnly["sessionContext"] = "Next user message", "trn_next0001", map[string]any{"message_count": 20}
			if !runtimeSessionResumeCompatible(currentSnapshot, historyOnly) {
				t.Fatal("new turn/history reset unchanged native configuration")
			}
			historyOnly["promptSnapshot"].(map[string]any)["variables"].(map[string]any)["user.ref"] = "usr_foreign01"
			if runtimeSessionResumeCompatible(currentSnapshot, historyOnly) {
				t.Fatal("foreign root actor reused native context")
			}
			for _, field := range []string{"runtimeKey", "providerAccountRef", "runtimeEnvironmentDigest", "integrationGrants"} {
				missing := map[string]any{}
				for key, value := range currentSnapshot {
					missing[key] = value
				}
				delete(missing, field)
				if runtimeSessionResumeCompatible(currentSnapshot, missing) {
					t.Fatal("incomplete runtime snapshot resumed")
				}
			}
			unsupported := map[string]any{}
			for key, value := range currentSnapshot {
				unsupported[key] = value
			}
			unsupported["runtimeProvider"] = "unsupported"
			if runtimeSessionResumeCompatible(unsupported, unsupported) {
				t.Fatal("unsupported native provider resumed")
			}
			run, err := service.GetRun(ctx, owner, stringMap(second, "runRef"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "resume-" + scopeKind + "-cancel", ExpectedVersion: &run.Version}, Payload: command.RunCommandInput{RunRef: run.Ref}})
			if err != nil {
				t.Fatal(err)
			}
			payload.LeaseRef, payload.Fence, payload.Generation = stringMap(second, "leaseRef"), stringMap(second, "fence"), second["generation"].(int64)
			_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "resume-" + scopeKind + "-late"}, Payload: payload})
			if !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("late ACK accepted: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT storage.content_generation FROM control_plane.session_storage storage JOIN control_plane.sessions session ON session.id=storage.session_id WHERE session.ref=$1`, conversation.SessionRef).Scan(&generation); err != nil || generation != 1 {
				t.Fatal("cancelled late ACK changed private storage")
			}
			cancelled, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			retryCommand := command.Command{Kind: command.RetryRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "resume-" + scopeKind + "retry", ExpectedVersion: &cancelled.Version}, Payload: command.RunCommandInput{RunRef: cancelled.Ref}}
			retried, err := service.Execute(ctx, retryCommand)
			if err != nil || retried.Run == nil || retried.Run.SessionRef != conversation.SessionRef {
				t.Fatalf("retry changed the exact helper session: %v", err)
			}
			retryItems := invoke(command.ClaimExecution, worker, scopeKind+"retry-claim", command.LeaseInput{WorkloadInstance: "resume-worker", Limit: 1}).RuntimeItems
			if len(retryItems) != 1 || stringMap(retryItems[0], "runRef") != retried.Run.Ref || stringMap(retryItems[0], "codexSessionID") != threadID {
				t.Fatal("retry lost unchanged native context or crossed helper conversation")
			}
			retryRun, err := service.GetRun(ctx, owner, retried.Run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "resume-" + scopeKind + "retry-cancel", ExpectedVersion: &retryRun.Version}, Payload: command.RunCommandInput{RunRef: retryRun.Ref}}); err != nil {
				t.Fatal(err)
			}
			replayed, err := service.Execute(ctx, retryCommand)
			if err != nil || replayed.Run == nil || replayed.Run.Ref != retried.Run.Ref {
				t.Fatal("retry replay created new session work")
			}
			view, err := service.GetAgentRuntimeConfiguration(ctx, owner, stringMap(second, "agentRef"))
			if err != nil {
				t.Fatal(err)
			}
			candidates := append([]entity.ProviderAccountCandidate(nil), view.Configuration.ProviderPolicy.AccountCandidates...)
			for index := range candidates {
				candidates[index].DefaultReasoningEffort, candidates[index].ModelCapabilityDigest = "", ""
			}
			_, err = service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "resume-" + scopeKind + "policy-change", ExpectedVersion: &view.AgentVersion}, Payload: command.AgentRuntimeConfigurationInput{AgentRef: stringMap(second, "agentRef"), RuntimeProfileRef: view.Configuration.RuntimeProfileRef, Model: view.Configuration.Model, ProviderPolicyMode: "LEAST_USED", ProviderAccounts: candidates}})
			if err != nil {
				t.Fatalf("canonical runtime policy change: %v", err)
			}
			if scopeKind == "SYSTEM" {
				updated, err := service.GetSystemAssistant(ctx, owner)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: updated.DesiredRuntimeRevision, State: "READY"}); err != nil {
					t.Fatal(err)
				}
			}
			cold := claim("changed")
			if stringMap(cold, "codexSessionID") != "" {
				t.Fatal("changed canonical runtime policy resumed old native context")
			}
			coldRun, err := service.GetRun(ctx, owner, stringMap(cold, "runRef"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "resume-" + scopeKind + "coldcancel", ExpectedVersion: &coldRun.Version}, Payload: command.RunCommandInput{RunRef: coldRun.Ref}}); err != nil {
				t.Fatal(err)
			}
			// Возвращаем synthetic idle clock, чтобы следующий scope не архивировал
			// уже проверенную сессию повторно по намеренно состаренному fixture.
			if _, err := pool.Exec(ctx, `UPDATE control_plane.session_storage SET idle_since=clock_timestamp() WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, conversation.SessionRef); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Проверяется owner-переход с synthetic tuple. Происхождение байтов rollout
// подтверждает runner; PostgreSQL не умеет независимо проверять произвольный SHA.
func TestAssistantFailedRolloutStorageComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := worker
	warm.Permission = "platform.runtime.warm.report"
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	claimPrincipal := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.tasks.claim")
	snapshotPrincipal := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.snapshot.complete")
	for index, scopeKind := range []string{"SYSTEM", "PROJECT"} {
		t.Run(scopeKind, func(t *testing.T) {
			execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
				t.Helper()
				result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + key}, Payload: payload})
				if err != nil {
					t.Fatalf("%s %s: %v", kind, key, err)
				}
				return result
			}
			projectRef := ""
			if scopeKind == "PROJECT" {
				projectRef = execute(command.CreateProject, owner, "-project", command.ProjectInput{Name: "Failed rollout fixture", Language: "en"}).Project.Ref
				execute(command.CreateProjectAssistant, owner, "-profile", command.ProjectAssistantInput{ProjectRef: projectRef, Name: "Failed rollout helper", Purpose: "Synthetic failed rollout", Instructions: "Use approved resources."})
			}
			conversation := execute(command.CreateAssistantConversation, owner, "-conversation", command.AssistantConversationInput{AssistantScope: scopeKind, ProjectRef: projectRef}).Conversation
			queue := func(key string) {
				execute(command.AddAssistantTurn, owner, key+"-turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic " + key, DeliveryMode: "QUEUE"})
			}
			claim := func(key string) map[string]any {
				t.Helper()
				items := execute(command.ClaimExecution, worker, key+"-claim", command.LeaseInput{WorkloadInstance: "failed-rollout-worker", Limit: 1}).RuntimeItems
				if len(items) != 1 || stringMap(items[0], "sessionRef") != conversation.SessionRef {
					t.Fatal("runtime claim crossed the exact failed rollout session")
				}
				return items[0]
			}
			storage := func() string {
				t.Helper()
				var result string
				if err := pool.QueryRow(ctx, `SELECT to_jsonb(storage)::text FROM control_plane.session_storage storage JOIN control_plane.sessions session ON session.id=storage.session_id WHERE session.ref=$1`, conversation.SessionRef).Scan(&result); err != nil {
					t.Fatal("read exact failed rollout storage")
				}
				return result
			}
			idle := func() {
				t.Helper()
				if _, err := pool.Exec(ctx, `UPDATE control_plane.session_storage SET idle_since=clock_timestamp()-interval '1 hour' WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, conversation.SessionRef); err != nil {
					t.Fatal(err)
				}
			}
			snapshotPayload := func(task map[string]any, size int64) command.SessionArchiveTaskInput {
				payload := claimedSessionArchivePayload(task)
				payload.FormatVersion, payload.ObjectKey = 1, stringMap(task, "objectKey")
				payload.ObjectVersion, payload.ObjectETag = "synthetic-version", "synthetic-etag"
				payload.ObjectDigest, payload.ObjectSizeBytes, payload.SourceSizeBytes = "sha256:"+strings.Repeat("c", 64), size+1024, size
				return payload
			}
			queue("-first")
			first := claim("-first")
			thread := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+20)
			initial := command.CompleteExecutionInput{LeaseRef: stringMap(first, "leaseRef"), Fence: stringMap(first, "fence"), Generation: runtimeRevisionMapInt64(first, "generation"), Success: true, ResultSummary: "Synthetic initial result", Usage: turnUsageFixture(), CodexSessionID: thread, ArchiveRelativePath: ".kodex/state/codex-home/sessions/2026/10/04/rollout-2026-10-04T00-00-00-" + thread + ".jsonl", ArchiveSHA256: strings.Repeat("a", 64), ArchiveSizeBytes: 128}
			execute(command.CompleteExecution, worker, "-initial-complete", initial)
			idle()
			oldSnapshot := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
			oldSnapshotPayload := snapshotPayload(oldSnapshot, initial.ArchiveSizeBytes)
			oldArchive := stringMap(execute(command.CompleteSessionSnapshot, snapshotPrincipal, "-old-snapshot", oldSnapshotPayload).Runtime, "archiveRef")
			var oldDeleteRef string
			if err := pool.QueryRow(ctx, `SELECT ref FROM control_plane.session_archive_tasks WHERE archive_id=(SELECT id FROM control_plane.session_archives WHERE ref=$1) AND kind='DELETE_PVC' AND state='READY'`, oldArchive).Scan(&oldDeleteRef); err != nil {
				t.Fatal("old snapshot did not create exact PVC deletion task")
			}
			oldDeletePayload := command.SessionArchiveTaskInput{TaskRef: oldDeleteRef, LeaseRef: "rls_absent_fixture", Fence: "fnc_absent_fixture", Generation: 1, PVCName: stringMap(oldSnapshot, "pvcName")}
			pvcPrincipal := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.pvc-delete.complete")
			forbiddenDelete := func(key string) {
				t.Helper()
				before := storage()
				if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionPVCDeletion, Principal: pvcPrincipal, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + key}, Payload: oldDeletePayload}); !errors.Is(err, errs.ErrForbidden) || storage() != before {
					t.Fatalf("unleased PVC deletion did not return typed authority rejection: %v", err)
				}
			}
			t.Run("ready-task", func(t *testing.T) { forbiddenDelete("-ready-delete") })
			queue("-failed")
			// Штатный owner materialize отменяет DELETE_PVC из-за queued turn.
			if tasks, err := service.ClaimSessionArchiveTasks(ctx, claimPrincipal, "failed-rollout-archive", 1); err != nil || len(tasks) != 0 {
				t.Fatal("pending turn retained archive work before runtime claim")
			}
			t.Run("cancelled-task", func(t *testing.T) { forbiddenDelete("-cancelled-delete") })
			second := claim("-failed")
			if stringMap(second, "runtimeRevisionRef") == stringMap(first, "runtimeRevisionRef") || stringMap(second, "codexSessionID") != thread {
				t.Fatal("failed turn did not receive a fresh exact native-resume snapshot")
			}
			failed := initial
			failed.LeaseRef, failed.Fence, failed.Generation = stringMap(second, "leaseRef"), stringMap(second, "fence"), runtimeRevisionMapInt64(second, "generation")
			failed.Success, failed.SafeErrorCode, failed.ResultSummary = false, "PROVIDER_RESPONSE_INVALID", "Synthetic failed result"
			failed.ArchiveRelativePath, failed.ArchiveSHA256, failed.ArchiveSizeBytes = ".kodex/state/codex-home/sessions/2026/10/07/rollout-2026-10-07T00-00-00-"+thread+".jsonl", strings.Repeat("b", 64), 193
			before := storage()
			for _, test := range []struct {
				name   string
				mutate func(*command.CompleteExecutionInput)
				want   error
			}{
				{"thread-path", func(p *command.CompleteExecutionInput) { p.CodexSessionID = "00000000-0000-4000-8000-000000000099" }, errs.ErrInvalid},
				{"unknown-lease", func(p *command.CompleteExecutionInput) { p.LeaseRef = "rls_foreign_fixture" }, errs.ErrNotFound},
				{"foreign-fence", func(p *command.CompleteExecutionInput) { p.Fence += "-foreign" }, errs.ErrForbidden},
				{"stale-generation", func(p *command.CompleteExecutionInput) { p.Generation++ }, errs.ErrForbidden},
				{"closed-old-lease", func(p *command.CompleteExecutionInput) {
					p.LeaseRef, p.Fence, p.Generation = initial.LeaseRef, initial.Fence, initial.Generation
				}, errs.ErrForbidden},
			} {
				t.Run(test.name, func(t *testing.T) {
					bad := failed
					test.mutate(&bad)
					_, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + test.name}, Payload: bad})
					if !errors.Is(err, test.want) {
						t.Fatalf("mismatched failed rollout accepted: %v", err)
					}
					if storage() != before {
						t.Fatal("rejected completion changed source pins")
					}
				})
			}
			completion := command.Command{Kind: command.CompleteExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + "-complete"}, Payload: failed}
			func() {
				original := queryCommandsExecuteInsertAuditEventsRefProjectIdAction
				queryCommandsExecuteInsertAuditEventsRefProjectIdAction = queryRuntimeClaimAuditUnavailable
				defer func() { queryCommandsExecuteInsertAuditEventsRefProjectIdAction = original }()
				if _, err := service.Execute(ctx, completion); !errors.Is(err, errs.ErrUnavailable) || storage() != before {
					t.Fatal("failed audit committed rollout tuple or terminal state")
				}
			}()
			result, err := service.Execute(ctx, completion)
			if err != nil || result.Run == nil || result.Run.State != "FAILED" {
				t.Fatalf("failed rollout completion: %v", err)
			}
			var generation, sourceSize int64
			var sourcePath, sourceSHA, codexID, revisionRef, state string
			var currentArchiveNull bool
			if err := pool.QueryRow(ctx, `SELECT storage.content_generation,storage.source_relative_path,storage.source_sha256,storage.source_size_bytes,storage.codex_session_id::text,revision.ref,storage.state,storage.current_archive_id IS NULL FROM control_plane.session_storage storage JOIN control_plane.sessions session ON session.id=storage.session_id JOIN control_plane.runtime_revisions revision ON revision.id=storage.runtime_revision_id WHERE session.ref=$1`, conversation.SessionRef).Scan(&generation, &sourcePath, &sourceSHA, &sourceSize, &codexID, &revisionRef, &state, &currentArchiveNull); err != nil || generation != 2 || sourcePath != failed.ArchiveRelativePath || sourceSHA != failed.ArchiveSHA256 || sourceSize != failed.ArchiveSizeBytes || codexID != failed.CodexSessionID || revisionRef != stringMap(second, "runtimeRevisionRef") || state != "LIVE" || !currentArchiveNull {
				t.Fatal("FAILED completion did not publish exact fresh generation and tuple")
			}
			var oldDeleteState, oldArchiveState string
			var oldDeleteUnleased bool
			if err := pool.QueryRow(ctx, `SELECT task.state,task.lease_ref IS NULL AND task.fence_digest IS NULL AND task.lease_expires_at IS NULL,archive.lifecycle_state FROM control_plane.session_archive_tasks task JOIN control_plane.session_archives archive ON archive.id=task.archive_id WHERE task.ref=$1`, oldDeleteRef).Scan(&oldDeleteState, &oldDeleteUnleased, &oldArchiveState); err != nil || oldDeleteState != "CANCELLED" || !oldDeleteUnleased || oldArchiveState != "SUPERSEDED" {
				t.Fatal("previous snapshot retained PVC deletion authority")
			}
			for _, node := range result.Graph.Nodes {
				if contains([]string{"PLANNED", "QUEUED", "RUNNING", "WAITING"}, node.State) {
					t.Fatal("FAILED completion left an open process node")
				}
			}
			if _, err := service.Execute(ctx, command.Command{Kind: command.RenewExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + "-late-renew"}, Payload: command.LeaseInput{LeaseRef: failed.LeaseRef, Fence: failed.Fence, Generation: failed.Generation}}); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("completed failed lease retained renew authority")
			}
			counts := func() [3]int64 {
				t.Helper()
				var counts [3]int64
				if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM control_plane.audit_events WHERE resource_ref=$1),(SELECT count(*) FROM control_plane.idempotency_receipts WHERE idempotency_key=$2),(SELECT count(*) FROM control_plane.run_events WHERE root_run_id=(SELECT id FROM control_plane.runs WHERE ref=$3))`, stringMap(second, "nodeRef"), completion.Mutation.IdempotencyKey, stringMap(second, "runRef")).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
					t.Fatal("read failed completion durable effects")
				}
				return counts
			}
			durableCounts, completedStorage := counts(), storage()
			if durableCounts[0] < 1 || durableCounts[1] != 1 || durableCounts[2] < 1 {
				t.Fatal("FAILED tuple lacks atomic audit, receipt and events")
			}
			replay, err := service.Execute(ctx, completion)
			if err != nil || replay.Run == nil || replay.Run.Ref != result.Run.Ref || replay.Run.State != "FAILED" || storage() != completedStorage || counts() != durableCounts {
				t.Fatal("lost-ACK FAILED replay changed source generation or durable effects")
			}
			changed := completion
			changedTuple := failed
			changedTuple.ArchiveSHA256 = strings.Repeat("d", 64)
			changed.Payload = changedTuple
			if _, err := service.Execute(ctx, changed); !errors.Is(err, errs.ErrIdempotencyReuse) || storage() != completedStorage || counts() != durableCounts {
				t.Fatal("changed failed tuple reused accepted idempotency key")
			}
			if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotPrincipal, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + "-old-snapshot-late"}, Payload: oldSnapshotPayload}); !errors.Is(err, errs.ErrForbidden) {
				t.Errorf("closed snapshot grant did not return typed authority rejection: %v", err)
			}
			if storage() != completedStorage || counts() != durableCounts {
				t.Fatal("closed snapshot completion changed current source or durable effects")
			}
			idle()
			freshSnapshot := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
			if freshSnapshot["contentGeneration"].(int64) != 2 || stringMap(freshSnapshot, "sourceRelativePath") != failed.ArchiveRelativePath || stringMap(freshSnapshot, "sourceSHA256") != failed.ArchiveSHA256 || freshSnapshot["sourceSizeBytes"].(int64) != failed.ArchiveSizeBytes || stringMap(freshSnapshot, "runtimeRevisionRef") != stringMap(second, "runtimeRevisionRef") || stringMap(freshSnapshot, "inputDigest") == stringMap(oldSnapshot, "inputDigest") {
				t.Fatal("fresh snapshot reused the old failed source tuple or revision")
			}
			freshPayload := snapshotPayload(freshSnapshot, failed.ArchiveSizeBytes)
			// Штатный retry отзывает весь старый claim, не нарушая schema CHECK.
			failPayload := claimedSessionArchivePayload(freshSnapshot)
			failPayload.SafeErrorCode = "SESSION_ARCHIVE_WORKER_FAILED"
			if result := execute(command.FailSessionArchiveTask, sessionArchivePrincipal(t, ctx, r, "platform.session-archive.tasks.fail"), "-revoke-snapshot", failPayload); stringMap(result.Runtime, "state") != "READY" {
				t.Fatal("snapshot failure did not revoke the first claim for retry")
			}
			t.Run("revoked-task", func(t *testing.T) {
				before := storage()
				if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotPrincipal, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + "-revoked-snapshot"}, Payload: freshPayload}); !errors.Is(err, errs.ErrForbidden) || storage() != before {
					t.Fatalf("revoked snapshot claim did not return typed rejection: %v", err)
				}
			})
			cleanup := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_OBJECT")
			cleanupPayload := claimedSessionArchivePayload(cleanup)
			cleanupPayload.ObjectKey, cleanupPayload.ObjectVersion = stringMap(cleanup, "objectKey"), stringMap(cleanup, "objectVersion")
			execute(command.CompleteSessionObjectDeletion, sessionArchivePrincipal(t, ctx, r, "platform.session-archive.object-delete.complete"), "-retry-object-cleanup", cleanupPayload)
			if _, err := pool.Exec(ctx, `UPDATE control_plane.session_archive_tasks SET available_at=clock_timestamp()-interval '1 second' WHERE ref=$1 AND state='READY'`, freshPayload.TaskRef); err != nil {
				t.Fatal("advance exact disposable retry clock")
			}
			reclaimed := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
			if stringMap(reclaimed, "taskRef") != stringMap(freshSnapshot, "taskRef") || reclaimed["contentGeneration"].(int64) != 2 || reclaimed["generation"].(int64) != freshSnapshot["generation"].(int64)+1 || reclaimed["attempt"].(int32) != freshSnapshot["attempt"].(int32)+1 || stringMap(reclaimed, "sourceSHA256") != failed.ArchiveSHA256 || stringMap(reclaimed, "leaseRef") == stringMap(freshSnapshot, "leaseRef") {
				t.Fatal("fresh snapshot retry changed source or reused revoked claim")
			}
			freshPayload = snapshotPayload(reclaimed, failed.ArchiveSizeBytes)
			badSnapshot := freshPayload
			badSnapshot.SourceSizeBytes = initial.ArchiveSizeBytes
			if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionSnapshot, Principal: snapshotPrincipal, Mutation: value.Mutation{IdempotencyKey: "failed-rollout-" + scopeKind + "-old-source-size"}, Payload: badSnapshot}); !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("new snapshot accepted old source size")
			}
			freshArchive := stringMap(execute(command.CompleteSessionSnapshot, snapshotPrincipal, "-fresh-snapshot", freshPayload).Runtime, "archiveRef")
			if err := pool.QueryRow(ctx, `SELECT content_generation,source_relative_path,source_sha256,source_size_bytes FROM control_plane.session_archives WHERE ref=$1`, freshArchive).Scan(&generation, &sourcePath, &sourceSHA, &sourceSize); err != nil || generation != 2 || sourcePath != failed.ArchiveRelativePath || sourceSHA != failed.ArchiveSHA256 || sourceSize != failed.ArchiveSizeBytes {
				t.Fatal("fresh archive receipt did not retain exact FAILED rollout tuple")
			}
			// Потерянный ACK completion не оживляет LIVE после последующей публикации.
			publishedStorage := storage()
			if _, err := service.Execute(ctx, completion); err != nil || storage() != publishedStorage || counts() != durableCounts {
				t.Fatal("late FAILED replay invalidated the fresh archive")
			}
			deletion := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_PVC")
			deletePayload := claimedSessionArchivePayload(deletion)
			deletePayload.PVCName = stringMap(deletion, "pvcName")
			execute(command.CompleteSessionPVCDeletion, sessionArchivePrincipal(t, ctx, r, "platform.session-archive.pvc-delete.complete"), "-fresh-delete", deletePayload)
		})
	}
}

func prepareAssistantSessionArchiveRoundTrip(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, worker, owner value.Principal, conversationRef, sessionRef, suffix string) func() {
	t.Helper()
	if _, err := r.pool.Exec(ctx, `UPDATE control_plane.session_storage SET idle_since=clock_timestamp()-interval '1 hour' WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, sessionRef); err != nil {
		t.Fatal(err)
	}
	claimPrincipal := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.tasks.claim")
	queue := func(key string) {
		t.Helper()
		if _, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "republication-" + suffix + key}, Payload: command.AssistantTurnInput{ConversationRef: conversationRef, Content: "Synthetic archive publication fixture", DeliveryMode: "QUEUE"}}); err != nil {
			t.Fatal("queue archive publication fixture failed")
		}
	}
	cancelQueued := func(key string) {
		t.Helper()
		var runRef string
		if err := r.pool.QueryRow(ctx, `SELECT ref FROM control_plane.runs WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1) AND state IN ('QUEUED','RUNNING') ORDER BY created_at DESC LIMIT 1`, sessionRef).Scan(&runRef); err != nil {
			t.Fatal("queued publication fixture run is missing")
		}
		run, err := service.GetRun(ctx, owner, runRef)
		if err != nil {
			t.Fatal("queued publication fixture run read failed")
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "republication-cancel-" + suffix + key, ExpectedVersion: &run.Version}, Payload: command.RunCommandInput{RunRef: runRef}}); err != nil {
			t.Fatal("cancel queued publication fixture failed")
		}
	}
	complete := func(kind command.Kind, operation, key string, payload command.SessionArchiveTaskInput) {
		t.Helper()
		actor := sessionArchivePrincipal(t, ctx, r, operation)
		if _, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "resume-archive-" + suffix + key}, Payload: payload}); err != nil {
			if kind == command.CompleteSessionSnapshot && errors.Is(err, errs.ErrUnavailable) {
				var taskID, organizationID, sessionID string
				if readErr := r.pool.QueryRow(ctx, `SELECT id::text,organization_id::text,session_id::text FROM control_plane.session_archive_tasks WHERE ref=$1`, payload.TaskRef).Scan(&taskID, &organizationID, &sessionID); readErr != nil {
					t.Fatal("snapshot failure fixture identity read failed")
				}
				_, probeErr := r.pool.Exec(ctx, querySessionArchiveCompleteSnapshot, pgx.StrictNamedArgs{
					"archive_ref": "sar_repeated_publication_fixture", "organization_id": organizationID, "session_id": sessionID,
					"task_id": taskID, "content_generation": snapshotGeneration(t, ctx, r, sessionRef), "format_version": payload.FormatVersion,
					"object_key": payload.ObjectKey, "object_version": payload.ObjectVersion, "object_etag": payload.ObjectETag,
					"object_digest": payload.ObjectDigest, "object_size_bytes": payload.ObjectSizeBytes,
					"active_turn": false, "retention_seconds": int64(sessionArchiveRetention / time.Second), "maximum_attempts": sessionArchiveMaxAttempts,
				})
				var databaseError *pgconn.PgError
				if errors.As(probeErr, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "session_archives_session_id_content_generation_key" {
					t.Fatal("snapshot publication SQLSTATE 23505: session_archives_session_id_content_generation_key")
				}
			}
			t.Fatalf("helper archive %s: %v", key, err)
		}
	}
	snapshot := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
	if stringMap(snapshot, "sessionRef") != sessionRef {
		t.Fatal("archive task crossed helper conversation")
	}
	payload := claimedSessionArchivePayload(snapshot)
	payload.FormatVersion = 1
	payload.ObjectKey = stringMap(snapshot, "objectKey")
	payload.ObjectVersion = "synthetic-version"
	payload.ObjectETag = "synthetic-etag"
	payload.ObjectDigest = "sha256:" + strings.Repeat("b", 64)
	payload.ObjectSizeBytes = 1152
	payload.SourceSizeBytes = 128
	// Имитируем появление нового queued-хода после claim snapshot, но до receipt.
	// После его terminal без нового rollout content generation остаётся прежним.
	queue("snapshot-race")
	complete(command.CompleteSessionSnapshot, "platform.session-archive.snapshot.complete", "active-snapshot", payload)
	cancelQueued("snapshot-race")
	snapshot = claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
	payload = claimedSessionArchivePayload(snapshot)
	payload.FormatVersion = 1
	payload.ObjectKey = stringMap(snapshot, "objectKey")
	payload.ObjectVersion = "synthetic-second-version"
	payload.ObjectETag = "synthetic-second-etag"
	payload.ObjectDigest = "sha256:" + strings.Repeat("b", 64)
	payload.ObjectSizeBytes = 1152
	payload.SourceSizeBytes = 128
	complete(command.CompleteSessionSnapshot, "platform.session-archive.snapshot.complete", "snapshot", payload)
	deletion := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_PVC")
	payload = claimedSessionArchivePayload(deletion)
	payload.PVCName = stringMap(deletion, "pvcName")
	complete(command.CompleteSessionPVCDeletion, "platform.session-archive.pvc-delete.complete", "delete", payload)
	return func() {
		t.Helper()
		blocked, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "resume-archive-" + suffix + "blocked"}, Payload: command.LeaseInput{WorkloadInstance: "resume-blocked", Limit: 1}})
		if err != nil || len(blocked.RuntimeItems) != 0 {
			t.Fatal("runtime started before the exact archive restore receipt")
		}
		restore := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "RESTORE")
		if stringMap(restore, "sessionRef") != sessionRef {
			t.Fatal("restore task crossed helper conversation")
		}
		archive, ok := restore["archive"].(map[string]any)
		if !ok {
			t.Fatal("restore archive missing")
		}
		payload = claimedSessionArchivePayload(restore)
		payload.FormatVersion = uint32(archive["formatVersion"].(int32))
		payload.ObjectKey = stringMap(archive, "objectKey")
		payload.ObjectVersion = stringMap(archive, "objectVersion")
		payload.ObjectETag = stringMap(archive, "objectETag")
		payload.ObjectDigest = stringMap(archive, "objectDigest")
		payload.ObjectSizeBytes = archive["objectSizeBytes"].(int64)
		payload.RestoredSourceSHA256 = stringMap(archive, "sourceSHA256")
		payload.SourceSizeBytes = archive["sourceSizeBytes"].(int64)
		bad := payload
		bad.RestoredSourceSHA256 = strings.Repeat("c", 64)
		actor := sessionArchivePrincipal(t, ctx, r, "platform.session-archive.restore.complete")
		if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteSessionRestore, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "resume-archive-" + suffix + "corrupt"}, Payload: bad}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("corrupt restore accepted: %v", err)
		}
		complete(command.CompleteSessionRestore, "platform.session-archive.restore.complete", "restore", payload)
		complete(command.CompleteSessionRestore, "platform.session-archive.restore.complete", "restore", payload)
		// Restore не меняет содержимое. После GC повторная публикация этого
		// поколения не должна переписывать либо оживлять прежние receipts.
		cancelQueued("restored-before-gc")
		if _, err := r.pool.Exec(ctx, `UPDATE control_plane.session_storage SET idle_since=clock_timestamp() WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, sessionRef); err != nil {
			t.Fatal(err)
		}
		var previousReceipts string
		const immutableReceipts = `SELECT jsonb_agg(to_jsonb(archive)-'lifecycle_state'-'retention_until'-'deleted_at' ORDER BY archive.ref)::text FROM control_plane.session_archives archive WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1) AND ref=ANY($2::text[])`
		rows, err := r.pool.Query(ctx, `SELECT ref FROM control_plane.session_archives WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1) ORDER BY ref`, sessionRef)
		if err != nil {
			t.Fatal(err)
		}
		var previousRefs []string
		for rows.Next() {
			var ref string
			if err := rows.Scan(&ref); err != nil {
				t.Fatal(err)
			}
			previousRefs = append(previousRefs, ref)
		}
		rows.Close()
		if rows.Err() != nil || len(previousRefs) != 2 {
			t.Fatal("repeated snapshot did not retain two immutable receipts")
		}
		if err := r.pool.QueryRow(ctx, immutableReceipts, sessionRef, previousRefs).Scan(&previousReceipts); err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE control_plane.session_archives SET retention_until=clock_timestamp()-interval '1 second' WHERE ref=ANY($1::text[])`, previousRefs); err != nil {
			t.Fatal(err)
		}
		for index := range previousRefs {
			deletion := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_OBJECT")
			deletionPayload := claimedSessionArchivePayload(deletion)
			deletionPayload.ObjectKey = stringMap(deletion, "objectKey")
			deletionPayload.ObjectVersion = stringMap(deletion, "objectVersion")
			complete(command.CompleteSessionObjectDeletion, "platform.session-archive.object-delete.complete", "gc-"+fmt.Sprint(index), deletionPayload)
		}
		if _, err := r.pool.Exec(ctx, `UPDATE control_plane.session_storage SET idle_since=clock_timestamp()-interval '1 hour' WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, sessionRef); err != nil {
			t.Fatal(err)
		}
		republished := claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "SNAPSHOT")
		republishedPayload := claimedSessionArchivePayload(republished)
		republishedPayload.FormatVersion = 1
		republishedPayload.ObjectKey = stringMap(republished, "objectKey")
		republishedPayload.ObjectETag = "synthetic-republished-etag"
		republishedPayload.ObjectDigest = "sha256:" + strings.Repeat("b", 64)
		republishedPayload.ObjectSizeBytes = 1152
		republishedPayload.SourceSizeBytes = 128
		complete(command.CompleteSessionSnapshot, "platform.session-archive.snapshot.complete", "republish", republishedPayload)
		complete(command.CompleteSessionSnapshot, "platform.session-archive.snapshot.complete", "republish", republishedPayload)
		var retainedReceipts string
		var generation int64
		var archivedCount, deletedCount int
		if err := r.pool.QueryRow(ctx, immutableReceipts, sessionRef, previousRefs).Scan(&retainedReceipts); err != nil {
			t.Fatal(err)
		}
		if err := r.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE lifecycle_state='DELETED') FROM control_plane.session_archives WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, sessionRef).Scan(&archivedCount, &deletedCount); err != nil {
			t.Fatal(err)
		}
		generation = snapshotGeneration(t, ctx, r, sessionRef)
		if retainedReceipts != previousReceipts || archivedCount != 3 || deletedCount != 2 || generation != republished["contentGeneration"].(int64) {
			t.Fatal("republication changed immutable receipts, content generation or receipt cardinality")
		}
		deletion = claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "DELETE_PVC")
		payload = claimedSessionArchivePayload(deletion)
		payload.PVCName = stringMap(deletion, "pvcName")
		complete(command.CompleteSessionPVCDeletion, "platform.session-archive.pvc-delete.complete", "republish-delete", payload)
		queue("republished-restore")
		restore = claimSingleSessionArchiveTask(t, ctx, service, claimPrincipal, "RESTORE")
		archive, ok = restore["archive"].(map[string]any)
		if !ok || stringMap(archive, "objectKey") != republishedPayload.ObjectKey {
			t.Fatal("restore did not select the exact current publication")
		}
		payload = claimedSessionArchivePayload(restore)
		payload.FormatVersion = uint32(archive["formatVersion"].(int32))
		payload.ObjectKey = stringMap(archive, "objectKey")
		payload.ObjectVersion = stringMap(archive, "objectVersion")
		payload.ObjectETag = stringMap(archive, "objectETag")
		payload.ObjectDigest = stringMap(archive, "objectDigest")
		payload.ObjectSizeBytes = archive["objectSizeBytes"].(int64)
		payload.RestoredSourceSHA256 = stringMap(archive, "sourceSHA256")
		payload.SourceSizeBytes = archive["sourceSizeBytes"].(int64)
		complete(command.CompleteSessionRestore, "platform.session-archive.restore.complete", "republish-restore", payload)
	}
}

func snapshotGeneration(t *testing.T, ctx context.Context, r *Repository, sessionRef string) int64 {
	t.Helper()
	var generation int64
	if err := r.pool.QueryRow(ctx, `SELECT content_generation FROM control_plane.session_storage WHERE session_id=(SELECT id FROM control_plane.sessions WHERE ref=$1)`, sessionRef).Scan(&generation); err != nil {
		t.Fatal("snapshot generation fixture read failed")
	}
	return generation
}
