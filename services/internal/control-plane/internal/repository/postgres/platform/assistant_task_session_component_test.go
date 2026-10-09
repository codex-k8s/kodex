package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/assistant_task_session_component__execution.sql
var queryTaskSessionComponentExecution string

//go:embed testdata/sql/assistant_task_session_component__restore_execution.sql
var queryTaskSessionComponentRestoreExecution string

//go:embed testdata/sql/assistant_task_session_component__source_version.sql
var queryTaskSessionComponentSourceVersion string

func TestAssistantTaskSessionReadComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated task session PostgreSQL")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	principal := func(workload, operation string) value.Principal {
		actor, tenant := "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002"
		if workload == "runtime-controller" {
			actor, tenant = "kodex-system-subject", "kodex-installation"
		}
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.runs.launch")
	worker := principal("runtime-controller", "platform.runtime.execution.claim")
	progress := principal("runtime-controller", "platform.runtime.execution.progress")
	complete := principal("runtime-controller", "platform.runtime.execution.complete")
	reader := principal("runtime-controller", "platform.runtime.assistant.resources.search")
	service, _ := serviceplatform.New(r)
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "task-session-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return result
	}
	claim := func(key string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, worker, key, nil, command.LeaseInput{WorkloadInstance: "task-session-fixture", Limit: 1}).RuntimeItems
		if len(items) != 1 {
			t.Fatal("missing exact fixture lease")
		}
		return items[0]
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Task session read", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "task-session-agent", "Task reader")
	first := execute(command.LaunchRun, owner, "first", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "FIRST_PUBLIC_USER_INPUT", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	firstLease := claim("first-claim")
	execute(command.CompleteExecution, complete, "first-complete", nil, command.CompleteExecutionInput{LeaseRef: stringMap(firstLease, "leaseRef"), Fence: stringMap(firstLease, "fence"), Generation: runtimeRevisionMapInt64(firstLease, "generation"), Success: true, ResultSummary: "FIRST_PUBLIC_RESULT", Usage: turnUsageFixture()})
	second := execute(command.AddSessionTurn, owner, "second", nil, command.SessionTurnInput{SessionRef: first.SessionRef, RunRef: first.Ref, Task: "SECOND_PUBLIC_USER_INPUT"}).Run
	secondLease := claim("second-claim")
	latestEvent := ""
	for index := range 13 {
		phase := "COMMENTARY"
		if index == 12 {
			phase = "FINAL"
		}
		event := execute(command.ReportExecutionProgress, progress, fmt.Sprintf("message-%d", index), nil, command.LeaseInput{LeaseRef: stringMap(secondLease, "leaseRef"), Fence: stringMap(secondLease, "fence"), Generation: runtimeRevisionMapInt64(secondLease, "generation"), Message: &entity.RunMessage{Ref: fmt.Sprintf("msg_task_session_%02d", index), Revision: 1, Phase: phase, Text: fmt.Sprintf("PUBLIC_MESSAGE_%02d", index)}}).Event
		latestEvent = event.Ref
	}
	execute(command.CompleteExecution, complete, "second-failed", nil, command.CompleteExecutionInput{LeaseRef: stringMap(secondLease, "leaseRef"), Fence: stringMap(secondLease, "fence"), Generation: runtimeRevisionMapInt64(secondLease, "generation"), Success: false, ResultSummary: "FAILED_PUBLIC_RESULT", SafeErrorCode: "PROVIDER_RESPONSE_INVALID", Usage: turnUsageFixture()})
	profile := execute(command.CreateProjectAssistant, owner, "profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Task helper", Purpose: "Read public result", Instructions: "Only read published task results."}).ProjectAssistant
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	execute(command.AddAssistantTurn, owner, "helper-turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Find the previous task result.", DeliveryMode: "QUEUE"})
	helper := claim("helper-claim")
	read := func(ref, cursor string) (query.AssistantTaskSessionPage, error) {
		return service.ReadAssistantTaskSession(ctx, reader, stringMap(helper, "leaseRef"), stringMap(helper, "fence"), runtimeRevisionMapInt64(helper, "generation"), query.AssistantTaskSessionRead{RunRef: ref, Cursor: cursor})
	}
	resolved, _ := r.ResolvePrincipal(ctx, owner)
	scope, _ := r.resolveScope(ctx, resolved)
	var before, after string
	if pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, scope.organizationID).Scan(&before) != nil {
		t.Fatal("read fixture effects")
	}
	page, err := read(second.Ref, "")
	if err != nil || page.State != "FAILED" || page.ResultSummary != "FAILED_PUBLIC_RESULT" || page.SessionRef != first.SessionRef || len(page.Messages) != 10 || !page.Truncated || page.Messages[0].Phase != "FINAL" {
		t.Fatalf("failed published result unavailable: %v messages=%d", err, len(page.Messages))
	}
	next, err := read(second.Ref, page.NextCursor)
	if err != nil || next.Truncated || len(next.Messages) != 5 || next.SourceSHA256 != page.SourceSHA256 {
		t.Fatalf("stable history page failed: %v messages=%d", err, len(next.Messages))
	}
	seen := map[string]bool{}
	for _, message := range append(page.Messages, next.Messages...) {
		if seen[message.EventRef] || message.SessionRef != first.SessionRef || message.SourceRunVersion < 1 {
			t.Fatal("history skipped canonical source pins")
		}
		seen[message.EventRef] = true
	}
	if pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, scope.organizationID).Scan(&after) != nil || before != after {
		t.Fatal("read wrote business state/audit/receipt/outbox")
	}
	// Смена source version не позволяет склеить две разные snapshots.
	if _, err = pool.Exec(ctx, queryTaskSessionComponentSourceVersion, second.Ref); err != nil {
		t.Fatal("advance isolated source version")
	}
	if _, err = read(second.Ref, page.NextCursor); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("changed source reused old cursor")
	}
	fresh, err := read(second.Ref, "")
	if err != nil || fresh.SourceSHA256 == page.SourceSHA256 {
		t.Fatal("fresh source read retained stale commitment")
	}
	if _, err = service.ReadAssistantTaskSession(ctx, reader, stringMap(secondLease, "leaseRef"), stringMap(secondLease, "fence"), runtimeRevisionMapInt64(secondLease, "generation"), query.AssistantTaskSessionRead{RunRef: second.Ref}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("ordinary or terminal source lease gained helper authority")
	}
	for _, bad := range []struct {
		fence      string
		generation int64
	}{{"wrong-fence", runtimeRevisionMapInt64(helper, "generation")}, {stringMap(helper, "fence"), runtimeRevisionMapInt64(helper, "generation") + 1}} {
		if _, err = service.ReadAssistantTaskSession(ctx, reader, stringMap(helper, "leaseRef"), bad.fence, bad.generation, query.AssistantTaskSessionRead{RunRef: second.Ref}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("stale lease accepted")
		}
	}
	var previous []byte
	if pool.QueryRow(ctx, queryTaskSessionComponentExecution, latestEvent, "ses_foreign123").Scan(&previous) != nil {
		t.Fatal("prepare corrupted fixture lineage")
	}
	if _, err = read(second.Ref, ""); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("foreign message session lineage accepted")
	}
	if _, err = pool.Exec(ctx, queryTaskSessionComponentRestoreExecution, latestEvent, previous); err != nil {
		t.Fatal("restore isolated fixture")
	}
	otherProject := execute(command.CreateProject, owner, "other-project", nil, command.ProjectInput{Name: "Other scope", Language: "en"}).Project
	otherAgent := createLifecycleAgent(t, ctx, service, owner, otherProject.Ref, "task-session-other-agent", "Other task")
	otherRun := execute(command.LaunchRun, owner, "other-run", nil, command.LaunchRunInput{ProjectRef: otherProject.Ref, Task: "FOREIGN_PROJECT_TEXT", Target: entity.RunTarget{Type: "AGENT", Ref: otherAgent.Ref}}).Run
	if _, err = read(otherRun.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("PROJECT helper crossed project boundary")
	}
	execute(command.CancelRun, owner, "other-cancel", &otherRun.Version, command.RunCommandInput{RunRef: otherRun.Ref})
	if _, err = pool.Exec(ctx, queryAssistantToolPhaseProjectFixture, otherProject.Ref); err != nil {
		t.Fatal("archive isolated project")
	}
	if _, err = read(otherRun.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign archived project became readable")
	}
	var oldExpiry time.Time
	if pool.QueryRow(ctx, queryAssistantCurrentConfigurationExpire, stringMap(helper, "leaseRef")).Scan(&oldExpiry) != nil {
		t.Fatal("expire fixture lease")
	}
	if _, err = read(second.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("expired helper lease accepted")
	}
	if _, err = pool.Exec(ctx, queryAssistantCurrentConfigurationRestoreExpiry, stringMap(helper, "leaseRef"), oldExpiry); err != nil {
		t.Fatal("restore fixture lease")
	}
	helperRun, err := service.GetRun(ctx, owner, stringMap(helper, "runRef"))
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "helper-cancel", &helperRun.Version, command.RunCommandInput{RunRef: helperRun.Ref})
	if _, err = read(second.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("terminal helper remained readable")
	}
	// Новая assistant lease принадлежит ограниченному USER: run.view дан только
	// anchor второго Run. Общая Session не даёт доступа к первому source Run.
	limited := contextProjectReader(t, ctx, r, service, owner, project.Ref, "TASK_SESSION")
	limitedResolved, err := r.ResolvePrincipal(ctx, limited)
	if err != nil {
		t.Fatal(err)
	}
	role := execute(command.CreateAccessRole, owner, "view-anchor-role", nil, command.AccessRoleInput{Name: "Task anchor only", PermissionKeys: []string{"run.view"}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Synthetic history eligibility"}).AccessRole
	binding := execute(command.CreateAccessBinding, owner, "view-anchor-binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: limitedResolved.ActorID, RoleVersionRef: role.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "RUN", ResourceRef: second.Ref}}).AccessBinding
	launchRole := execute(command.CreateAccessRole, owner, "helper-launch-role", nil, command.AccessRoleInput{Name: "Task helper launch only", PermissionKeys: []string{"agent.launch"}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Synthetic exact helper launch"}).AccessRole
	execute(command.CreateAccessBinding, owner, "helper-launch-binding", nil, command.AccessBindingInput{SubjectKind: "USER", SubjectRef: limitedResolved.ActorID, RoleVersionRef: launchRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "AGENT", ResourceRef: profile.AgentRef}})
	limitedConversation := execute(command.CreateAssistantConversation, limited, "limited-conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	execute(command.AddAssistantTurn, limited, "limited-helper-turn", nil, command.AssistantTurnInput{ConversationRef: limitedConversation.Ref, Content: "Read only the permitted previous task.", DeliveryMode: "QUEUE"})
	helper = claim("limited-helper-claim")
	claimedRun, err := service.GetRun(ctx, owner, stringMap(helper, "runRef"))
	if err != nil || claimedRun.SessionRef != limitedConversation.SessionRef {
		t.Fatal("fixture claimed another pending execution")
	}
	limitedPage, err := read(second.Ref, "")
	if err != nil {
		t.Fatalf("exact anchor-only reader failed: %v", err)
	}
	all := append([]query.AssistantTaskPublishedMessage{}, limitedPage.Messages...)
	if limitedPage.Truncated {
		next, err := read(second.Ref, limitedPage.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, next.Messages...)
	}
	if len(all) != 14 {
		t.Fatalf("hidden source appeared or public source missing: %d", len(all))
	}
	for _, message := range all {
		if message.SourceRunRef != second.Ref || strings.Contains(message.Text, "FIRST_PUBLIC_USER_INPUT") {
			t.Fatal("Session locator leaked inaccessible source Run")
		}
	}
	if _, err = read(first.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("limited root USER gained source view")
	}
	execute(command.RevokeAccessBinding, owner, "revoke-anchor", &binding.Version, command.AccessBindingInput{BindingRef: binding.Ref})
	if _, err = read(second.Ref, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("read reused revoked source authority")
	}
}
