package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/runtime_assistant_tool_phase_coordinates.sql
var queryAssistantToolPhaseCoordinates string

//go:embed testdata/sql/runtime_assistant_tool_phase_snapshot_fixture.sql
var queryAssistantToolPhaseSnapshotFixture string

//go:embed testdata/sql/runtime_assistant_tool_phase_allow_snapshot_fixture.sql
var queryAssistantToolPhaseAllowSnapshotFixture string

//go:embed testdata/sql/runtime_assistant_tool_phase_conversation_fixture.sql
var queryAssistantToolPhaseConversationFixture string

//go:embed testdata/sql/runtime_assistant_tool_phase_actor_fixture.sql
var queryAssistantToolPhaseActorFixture string

//go:embed testdata/sql/runtime_assistant_tool_phase_project_fixture.sql
var queryAssistantToolPhaseProjectFixture string

func TestRuntimeAssistantRecordToolCallPhaseComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated tool phase PostgreSQL")
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
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.assistant.turns.add", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	toolActor := principal("runtime-controller", "platform.runtime.tool-call.record", "kodex-system-subject", "kodex-installation")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "tool-phase-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Tool phase project", Language: "en"}).Project
	profile := execute(command.CreateProjectAssistant, owner, "profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Own helper", Purpose: "Synthetic tool phase", Instructions: "Use only approved project operations."}).ProjectAssistant
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: project.Ref}).Conversation
	execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Inspect project configuration", DeliveryMode: "QUEUE"})
	claim := func(key string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, worker, key, nil, command.LeaseInput{WorkloadInstance: "tool-phase-fixture", Limit: 1}).RuntimeItems
		if len(items) != 1 {
			t.Fatalf("%s claim count %d", key, len(items))
		}
		return items[0]
	}
	lease := claim("project-claim")
	if stringMap(lease, "assistantScope") != "PROJECT" || stringMap(lease, "assistantProfileRef") != profile.Ref || stringMap(lease, "agentRef") != profile.AgentRef {
		t.Fatal("claim lost exact project assistant pins")
	}
	callFor := func(item map[string]any, ref, tool, capability string) command.RunToolCallInput {
		return command.RunToolCallInput{LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: runtimeRevisionMapInt64(item, "generation"), CallRef: ref, Tool: tool, CapabilityRef: capability, SafeParameters: map[string]any{}, State: "RUNNING", Revision: 1}
	}
	tools := []struct{ name, capability string }{
		{"get_configuration_catalog", "platform.configuration.read"},
		{"find_platform_resources", "platform.resources.search"},
		{"propose_configuration_plan", "platform.configuration.plan"},
		{"propose_assistant_metadata", "platform.presentation.propose"},
	}
	for index, item := range tools {
		call := callFor(lease, fmt.Sprintf("tcl_project_phase_%d", index), item.name, item.capability)
		started := execute(command.RecordRunToolCall, toolActor, item.name+"-started", nil, call).Event
		if started == nil || started.ToolCall == nil || started.ToolCall.State != "RUNNING" || started.Actor.Kind != "AGENT" || started.Actor.Ref != profile.AgentRef || started.Delta.Execution == nil {
			t.Fatal("project init phase lost activity or ordinary author identity")
		}
		call.State, call.Revision, call.DurationMS, call.SafeResult = "SUCCEEDED", 2, 1, "Safe completed result"
		if index%2 == 1 {
			call.State, call.SafeResult = "FAILED", "Safe synthetic failure"
		}
		finished := execute(command.RecordRunToolCall, toolActor, item.name+"-finished", nil, call).Event
		if finished.ToolCall == nil || finished.ToolCall.State != call.State || finished.ToolCall.Revision != 2 || finished.Actor.Kind != "AGENT" || *finished.Delta.Execution != *started.Delta.Execution {
			t.Fatal("project terminal tool phase changed execution pins or author")
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolActor, Mutation: value.Mutation{IdempotencyKey: "tool-phase-duplicate-" + item.name}, Payload: call}); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("duplicate terminal phase: %v", err)
		}
	}
	// Нативные grants отсутствуют: профиль не расширяет доступ к MCP capabilities.
	wrongGrant := callFor(lease, "tcl_project_ungranted", "invoke_integration", "context7.library.resolve")
	wrongGrant.GrantRef = "igr_synthetic_missing"
	if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolActor, Mutation: value.Mutation{IdempotencyKey: "tool-phase-no-grant"}, Payload: wrongGrant}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("project profile implied an integration grant: %v", err)
	}
	ordinary := createLifecycleAgent(t, ctx, service, owner, project.Ref, "tool-phase-ordinary", "Ordinary operator")
	execute(command.LaunchRun, owner, "ordinary-run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Ordinary fixture", Target: entity.RunTarget{Type: "AGENT", Ref: ordinary.Ref}})
	ordinaryLease := claim("ordinary-claim")
	// Собственный read использует прежний fresh lease, не assistant permission.
	own := callFor(ordinaryLease, "tcl_ordinary_own_snapshot", runtimecontract.ExecutionSnapshotTool, "")
	startedOwn := execute(command.RecordRunToolCall, toolActor, "own-read-start", nil, own).Event
	if startedOwn.ToolCall == nil || startedOwn.ToolCall.CapabilityRef != "" || startedOwn.ToolCall.GrantRef != "" || startedOwn.Actor.Ref != ordinary.Ref {
		t.Fatal("own read invented a capability or changed actor")
	}
	own.State, own.Revision, own.SafeResult = "SUCCEEDED", 2, runtimecontract.ExecutionSnapshotTool+":completed"
	execute(command.RecordRunToolCall, toolActor, "own-read-finish", nil, own)
	for name, change := range map[string]func(*command.RunToolCallInput){
		"stale-fence":      func(v *command.RunToolCallInput) { v.Fence = "stale-fixture" },
		"stale-generation": func(v *command.RunToolCallInput) { v.Generation++ },
	} {
		t.Run("own-read-"+name, func(t *testing.T) {
			call := callFor(ordinaryLease, "tcl_own_"+name, runtimecontract.ExecutionSnapshotTool, "")
			change(&call)
			if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolActor,
				Mutation: value.Mutation{IdempotencyKey: "own-read-" + name}, Payload: call}); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("stale own read lease accepted: %v", err)
			}
		})
	}
	for index, item := range tools {
		call := callFor(ordinaryLease, fmt.Sprintf("tcl_ordinary_phase_%d", index), item.name, item.capability)
		if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolActor, Mutation: value.Mutation{IdempotencyKey: "tool-phase-ordinary-" + item.name}, Payload: call}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("ordinary agent accepted %s: %v", item.name, err)
		}
	}
	prepareObservedWarmFixture(t, ctx, r)
	system, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warmWorker := principal("runtime-controller", "platform.runtime.warm.report", "kodex-system-subject", "kodex-installation")
	if _, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: system.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	systemConversation := execute(command.CreateAssistantConversation, owner, "system-conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	execute(command.AddAssistantTurn, owner, "system-turn", nil, command.AssistantTurnInput{ConversationRef: systemConversation.Ref, Content: "Inspect system configuration", DeliveryMode: "QUEUE"})
	systemLease := claim("system-claim")
	systemCall := callFor(systemLease, "tcl_system_phase", "get_configuration_catalog", "platform.configuration.read")
	systemEvent := execute(command.RecordRunToolCall, toolActor, "system-tool", nil, systemCall).Event
	if systemEvent.Actor.Kind != "SYSTEM_ASSISTANT" || systemEvent.Actor.Ref != systemConversation.AssistantRef {
		t.Fatal("system author identity changed")
	}
	// Невозможные immutable pins проверяются только внутри откатываемой
	// транзакции disposable fixture. Production guard остаётся неизменным.
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	if err != nil {
		t.Fatal("parse isolated fixture administrator")
	}
	adminConfig.Database = pool.Config().ConnConfig.Database
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect isolated fixture administrator")
	}
	defer func() { _ = admin.Close(ctx) }()
	var organizationID, nodeID string
	var generation int64
	if err := pool.QueryRow(ctx, queryAssistantToolPhaseCoordinates, stringMap(lease, "runtimeRevisionRef")).Scan(&organizationID, &nodeID, &generation); err != nil {
		t.Fatal("read immutable fixture coordinates")
	}
	checkEligibility := func(t *testing.T, fixture string, arguments []any, organization string, expected bool) {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated eligibility fixture")
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil {
				t.Error("rollback isolated eligibility fixture")
			}
		}()
		if fixture != "" {
			if fixture == queryAssistantToolPhaseSnapshotFixture {
				if _, err := tx.Exec(ctx, queryAssistantToolPhaseAllowSnapshotFixture); err != nil {
					t.Fatal("enable transaction-local synthetic snapshot fixture")
				}
			}
			if _, err := tx.Exec(ctx, fixture, arguments...); err != nil {
				t.Fatal("materialize isolated negative eligibility fixture")
			}
		}
		var actorRef, actorName string
		var system, eligible, grant bool
		err = tx.QueryRow(ctx, queryRuntimeRecordtoolcallSelectActorAndGrant, pgx.StrictNamedArgs{
			"organization_id": organization, "node_id": nodeID, "generation": generation,
			"grant_ref": "", "capability_ref": "platform.configuration.read", "tool": "get_configuration_catalog", "purpose": "",
		}).Scan(&actorRef, &actorName, &system, &eligible, &grant)
		if organization != organizationID && errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil || eligible != expected || system || !grant {
			t.Fatalf("isolated eligibility mismatch: eligible=%t expected=%t system=%t grant=%t error=%v", eligible, expected, system, grant, err)
		}
	}
	t.Run("active-owner-predicate", func(t *testing.T) { checkEligibility(t, "", nil, organizationID, true) })
	t.Run("foreign-organization", func(t *testing.T) { checkEligibility(t, "", nil, "90000000-0000-4000-8000-000000000001", false) })
	for _, fixture := range []struct{ name, key, value string }{
		{"unknown-scope", "assistantScope", "UNKNOWN"},
		{"ordinary-scope", "assistantScope", "NONE"},
		{"missing-profile", "assistantProfileRef", ""},
		{"unknown-profile", "assistantProfileRef", "asstp_synthetic_unknown"},
		{"foreign-agent", "agentRef", ordinary.Ref},
		{"foreign-project", "projectRef", "prj_synthetic_foreign"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			checkEligibility(t, queryAssistantToolPhaseSnapshotFixture, []any{stringMap(lease, "runtimeRevisionRef"), fixture.key, fixture.value}, organizationID, false)
		})
	}
	for _, fixture := range []struct {
		name, query string
		args        []any
	}{
		{"disabled-agent", queryProjectAssistantArchivedFixture, []any{profile.AgentRef, "DISABLED", false}},
		{"archived-agent", queryProjectAssistantArchivedFixture, []any{profile.AgentRef, "ARCHIVED", true}},
		{"inactive-conversation", queryAssistantToolPhaseConversationFixture, []any{conversation.Ref}},
		{"inactive-root-actor", queryAssistantToolPhaseActorFixture, []any{stringMap(lease, "runtimeRevisionRef")}},
		{"inactive-project", queryAssistantToolPhaseProjectFixture, []any{project.Ref}},
	} {
		t.Run(fixture.name, func(t *testing.T) { checkEligibility(t, fixture.query, fixture.args, organizationID, false) })
	}
	t.Run("rollback-restores-owner-predicate", func(t *testing.T) { checkEligibility(t, "", nil, organizationID, true) })
	run, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel-project", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic phase cleanup"})
	afterTerminal := callFor(lease, "tcl_after_terminal", "get_configuration_catalog", "platform.configuration.read")
	if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolActor, Mutation: value.Mutation{IdempotencyKey: "tool-phase-after-terminal"}, Payload: afterTerminal}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal project lease accepted a tool: %v", err)
	}
	afterTerminal.Tool, afterTerminal.CapabilityRef, afterTerminal.CallRef = runtimecontract.ExecutionSnapshotTool, "", "tcl_own_after_terminal"
	if _, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolActor,
		Mutation: value.Mutation{IdempotencyKey: "own-read-after-terminal"}, Payload: afterTerminal}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal own read lease accepted: %v", err)
	}
}
