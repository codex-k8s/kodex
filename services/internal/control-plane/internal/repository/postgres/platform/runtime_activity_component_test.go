package platform

import (
	"context"
	"errors"
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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRuntimeActivityMessagesAndToolLifecycleComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
	seedObservedCatalogFixture(t, ctx, r)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.runs.launch", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	progress := principal("runtime-controller", "platform.runtime.execution.progress", "kodex-system-subject", "kodex-installation")
	tool := principal("runtime-controller", "platform.runtime.tool-call.record", "kodex-system-subject", "kodex-installation")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "activity-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Activity isolation", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "activity-agent", "Activity operator")
	run := execute(command.LaunchRun, owner, "run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Owner input must remain visible.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	claims := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "activity-runtime", Limit: 1}).RuntimeItems
	if len(claims) != 1 {
		t.Fatal("execution missing")
	}
	lease := command.LeaseInput{LeaseRef: stringMap(claims[0], "leaseRef"), Fence: stringMap(claims[0], "fence"), Generation: runtimeRevisionMapInt64(claims[0], "generation")}
	message := entity.RunMessage{Ref: "msg_activity_commentary", Phase: "COMMENTARY", Revision: 1, Text: "Проверяю текущую задачу.\n" + strings.Repeat("Длинный опубликованный текст. ", 200), Source: entity.MessageSource{Origin: "CALLBACK_CONTINUATION"}}
	lease.Message = &message
	first := execute(command.ReportExecutionProgress, progress, "message", nil, lease).Event
	if first.Delta.Execution == nil || first.Delta.Execution.SessionRef == "" || first.Delta.Execution.TurnNumber != 1 || first.Delta.Execution.Attempt != 1 || first.Delta.Message == nil || first.Delta.Message.Text != message.Text {
		t.Fatal("published message lost exact pins or text")
	}
	if first.Delta.Message.Source.Origin != "ORDINARY" {
		t.Fatal("provider supplied source changed public message origin")
	}
	replay := execute(command.ReportExecutionProgress, progress, "message-replay", nil, lease).Event
	if replay.Ref != first.Ref || replay.Sequence != first.Sequence {
		t.Fatal("message replay appended duplicate")
	}
	message.Text = "changed immutable message"
	if _, err := service.Execute(ctx, command.Command{Kind: command.ReportExecutionProgress, Principal: progress, Mutation: value.Mutation{IdempotencyKey: "activity-message-mismatch"}, Payload: lease}); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatalf("message mutation accepted: %v", err)
	}
	call := command.RunToolCallInput{LeaseRef: lease.LeaseRef, Fence: lease.Fence, Generation: lease.Generation,
		CallRef: "tcl_activity_pending", Tool: runtimecontract.NativeToolKindSleep, State: "RUNNING", Revision: 1,
		SafeParameters: map[string]any{"requested_duration_ms": 50}}
	started := execute(command.RecordRunToolCall, tool, "tool-started", nil, call).Event
	if started.ToolCall == nil || started.ToolCall.State != "RUNNING" || started.Delta.Execution == nil {
		t.Fatal("started tool projection missing")
	}
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Synthetic activity cleanup"})
	events, _, _, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: run.Ref, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	var inputFound, messageFound, closed bool
	for _, event := range events {
		if event.Delta.Message != nil {
			if event.Delta.Message.Source.Origin != "ORDINARY" {
				t.Fatal("ordinary owner or provider message was classified as callback")
			}
			inputFound = inputFound || event.Delta.Message.Phase == "USER" && event.Delta.Message.Text == "Owner input must remain visible."
			messageFound = messageFound || event.Ref == first.Ref && event.Delta.Message.Text == first.Delta.Message.Text
		}
		if event.ToolCall != nil && event.ToolCall.Ref == call.CallRef && event.ToolCall.State == "CANCELLED" {
			closed = event.ToolCall.Revision == 2 && event.Delta.Execution != nil && *event.Delta.Execution == *started.Delta.Execution
		}
	}
	if !inputFound || !messageFound || !closed {
		t.Fatalf("history incomplete: input=%t message=%t toolClosed=%t", inputFound, messageFound, closed)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ReportExecutionProgress, Principal: progress, Mutation: value.Mutation{IdempotencyKey: "activity-message-after-terminal"}, Payload: lease}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal lease accepted message: %v", err)
	}
}
