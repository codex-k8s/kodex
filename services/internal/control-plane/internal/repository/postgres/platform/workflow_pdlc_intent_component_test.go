package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newWorkflowPDLCFileOwnerFixture creates one isolated graph through owner commands, including
// actual grants and readiness; only established disposable credential metadata is seeded.
// newWorkflowPDLCFileOwnerFixture создаёт isolated graph через owner commands с actual grants
// и readiness; только credential metadata использует штатную disposable оснастку.
func newWorkflowPDLCFileOwnerFixture(t *testing.T, extraResults ...entity.WorkflowResultRequirement) (*workflowEvidenceNativeFixture, map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated PDLC database")
	}
	t.Cleanup(pool.Close)
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, repository: repository, service: service, owner: owner, worker: worker, agents: map[string]string{}}
	fixture := &workflowEvidenceNativeFixture{f: f}
	project := f.execute(command.CreateProject, "pdlc-project", nil, command.ProjectInput{Name: "Журнал обращений: документы", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "pdlc-manager", "Менеджер")
	author := createLifecycleAgent(t, ctx, service, owner, project.Ref, "pdlc-author", "Аналитик")
	check := createLifecycleAgent(t, ctx, service, owner, project.Ref, "pdlc-check", "Архитектор")
	check = *f.execute(command.ChangeAgentCapability, "pdlc-artifact", &check.Version, command.AgentBindingInput{AgentRef: check.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	f.managerRef, f.agents["author"], f.agents["check"] = manager.Ref, author.Ref, check.Ref
	fixture.connection = *f.execute(command.CreateConnection, "pdlc-connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Документы журнала обращений", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, fixture.connection.Ref); err != nil {
		t.Fatal("seed established disposable credential metadata")
	}
	fixture.connection, err = service.GetIntegrationConnection(ctx, owner, fixture.connection.Ref)
	if err != nil {
		t.Fatal("read native PDLC connection")
	}
	operations := []string{"github.repository.content.create", "github.repository.content.update", "github.pull_request.read", "github.repository.content.read"}
	for _, result := range extraResults {
		if result.Kind == "PR_COMMENT" {
			operations = append(operations, "github.pull_request.comment.create")
		} else if result.Kind == "PULL_REQUEST" {
			operations = append(operations, "github.pull_request.ready")
		}
	}
	for _, operation := range operations {
		fixture.connection = *f.execute(command.ChangeIntegrationGrant, fixture.key("pdlc-grant"), &fixture.connection.Version, command.IntegrationGrantInput{ConnectionRef: fixture.connection.Ref, AgentRef: author.Ref, CapabilityKey: operation, ApprovalPolicy: "NONE", Enabled: true}).Connection
	}
	fixture.gateway = resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", ResultEvidenceProfile: workflowservice.GitHubPDLCV1,
		Name: "Документы журнала обращений", Purpose: "Сохранить уточнения требований в ветке существующего PR.", CoordinatorAgentRef: manager.Ref,
		Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{
			{Key: "author", Position: 1, Name: "Требования", Instructions: "Уточнить требования и сохранить документ в ветке PR.", ExpectedResult: "Версия документа", AgentRef: author.Ref, TimeoutSeconds: 120,
				RequiredCapabilityKeys: []string{"github.repository.content.create", "github.repository.content.update", "github.pull_request.read", "github.repository.content.read"},
				ResultRequirements:     []entity.WorkflowResultRequirement{{Key: "document", Kind: "REPOSITORY_FILE", Repository: "fixture/repository", TargetRef: "3", Branch: "feature/journal", FileName: "docs/requirements.md"}}},
			{Key: "check", Position: 2, Name: "Архитектура", Instructions: "Подготовить архитектуру по принятой версии требований.", ExpectedResult: "Архитектура", AgentRef: check.Ref, TimeoutSeconds: 120, DependsOn: []string{"author"},
				RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "architecture", Kind: "ARTIFACT", FileName: "architecture.md", MediaType: "text/markdown"}}},
		}}
	draft.Steps[0].ResultRequirements = append(draft.Steps[0].ResultRequirements, extraResults...)
	draft.Steps[0].RequiredCapabilityKeys = append([]string{}, operations...)
	workflow := f.execute(command.CreateWorkflow, "pdlc-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "pdlc-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "pdlc-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	run := f.execute(command.LaunchRun, "pdlc-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Уточнить требования к журналу обращений.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
	f.runRef = run.Ref
	lease := fixture.claim(author.Ref)
	return fixture, lease
}

// TestWorkflowPDLCFileIntentOwnerComponent verifies the real file intent journal and native leases,
// without claiming that trusted fixture gateway facts prove external GitHub delivery.
// TestWorkflowPDLCFileIntentOwnerComponent проверяет настоящий file intent journal и native leases,
// не выдавая trusted fixture gateway facts за внешнюю GitHub delivery.
func TestWorkflowPDLCFileIntentOwnerComponent(t *testing.T) {
	fixture, lease := newWorkflowPDLCFileOwnerFixture(t)
	f, ctx, owner, worker := fixture.f, fixture.f.ctx, fixture.f.owner, fixture.f.worker
	input := func(text, sha string) map[string]any {
		request := map[string]any{"path": "docs/requirements.md", "branch": "feature/journal", "message": "Уточнить требования", "content_base64": base64.StdEncoding.EncodeToString([]byte(text))}
		if sha != "" {
			request["sha"] = sha
		}
		return request
	}
	rejected := func(key, operation string, request map[string]any) {
		t.Helper()
		_, err := fixture.resolve(lease, key, operation, request)
		if !errors.Is(err, errs.ErrConflict) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("PDLC file drift was not rejected", completionOutcomeClass(err))
		}
	}
	foreign := input("Требования A", "")
	foreign["branch"] = "foreign/branch"
	rejected("pdlc-foreign-branch", "github.repository.content.create", foreign)
	var invocations []map[string]any
	for index, text := range []string{"Требования A", "Требования B", "Требования A"} {
		operation, sha := "github.repository.content.create", ""
		if index > 0 {
			operation, sha = "github.repository.content.update", strings.Repeat("b", 40)
		}
		key := "pdlc-native-file-" + leftPad(index, 3)
		request := input(text, sha)
		invocation, err := fixture.resolve(lease, key, operation, request)
		if err != nil || stringMap(invocation, "state") != "READY" || stringMap(invocation, "gateRef") != "" {
			t.Fatal("native PDLC file write was not admitted without Gate", index, completionOutcomeClass(err))
		}
		for _, previous := range invocations {
			if stringMap(previous, "invocationRef") == stringMap(invocation, "invocationRef") {
				t.Fatal("deliberate file revision reused an older WRITE")
			}
		}
		receipt := fixture.finishEffect(invocation, "github-commit:"+strings.Repeat("a", 40), `{"path":"docs/requirements.md","sha":"`+strings.Repeat("a", 40)+`"}`)
		invocations = append(invocations, invocation)
		for _, replayKey := range []string{key, key + "-same-bytes"} {
			replay, err := fixture.resolve(lease, replayKey, operation, request)
			if err != nil || stringMap(replay, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(replay, "receiptRef") != receipt {
				t.Fatal("known file success did not rejoin exact native receipt", index, completionOutcomeClass(err))
			}
		}
		if work, err := f.service.ClaimIntegrationInvocations(ctx, fixture.gateway, "pdlc-no-repeat", 1); err != nil || len(work) != 0 {
			t.Fatal("file replay created a second external WRITE")
		}
	}
	original, err := fixture.resolve(lease, "pdlc-native-file-000", "github.repository.content.create", input("Требования A", ""))
	if err != nil || stringMap(original, "invocationRef") != stringMap(invocations[0], "invocationRef") {
		t.Fatal("exact historical file call lost its original receipt", completionOutcomeClass(err))
	}
	rejected("pdlc-native-file-000", "github.repository.content.create", input("Подмена старого намерения", ""))
	pending, err := fixture.resolve(lease, "pdlc-pending-file", "github.repository.content.update", input("Требования C", strings.Repeat("c", 40)))
	if err != nil || stringMap(pending, "state") != "READY" {
		t.Fatal("new linked file revision was not admitted", completionOutcomeClass(err))
	}
	rejected("pdlc-uncertain-successor", "github.repository.content.update", input("Требования D", strings.Repeat("d", 40)))
	current, err := f.service.GetRun(ctx, owner, f.runRef)
	if err != nil {
		t.Fatal("read native PDLC root before cancellation")
	}
	f.execute(command.CancelRun, "pdlc-cancel", &current.Version, command.RunCommandInput{RunRef: current.Ref, Reason: "Завершить проверку file intent без заявления приёмки DAG."})
	state, err := f.service.GetIntegrationInvocation(ctx, worker, stringMap(pending, "invocationRef"))
	if err != nil || stringMap(state, "state") != "CANCELLED" || stringMap(state, "effectReceiptRef") != "" {
		t.Fatal("cancellation revived unresolved file effect or fabricated receipt")
	}
	if work, err := f.service.ClaimIntegrationInvocations(ctx, fixture.gateway, "pdlc-after-cancel", 1); err != nil || len(work) != 0 {
		t.Fatal("cancelled file graph remained claimable")
	}
}
