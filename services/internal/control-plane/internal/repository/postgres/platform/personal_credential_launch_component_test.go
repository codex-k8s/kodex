package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalGrantConfiguration qualifies real owner grant creation with an unverified personal
// Connection. Configuration readiness does not manufacture global CONNECTED or a shared credential;
// configuration-only USE does not grant per-initiator call admission.
// assertPersonalGrantConfiguration проверяет реальную выдачу owner grant для непроверенного личного
// Connection. Готовность конфигурации не создаёт общий CONNECTED либо shared credential;
// configuration-only USE не выдаёт per-initiator call admission.
func assertPersonalGrantConfiguration(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, secondUser value.Principal, connection entity.IntegrationConnection) {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, r)
	projectResult, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-preflight-project-one"}, Payload: command.ProjectInput{Name: "Personal launch fixture", Purpose: "Bounded personal credential launch qualification", Language: "ru"}})
	if err != nil || projectResult.Project == nil {
		t.Fatal("create personal launch project", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, projectResult.Project.Ref, "personal-preflight-agent-one", "Personal launch agent")
	var ready bool
	var state string
	var shared bool
	if err := r.pool.QueryRow(ctx, `SELECT control_plane.integration_connection_configuration_ready(id),state,credential_revision_id IS NOT NULL
FROM control_plane.integration_connections WHERE ref=$1`, connection.Ref).Scan(&ready, &state, &shared); err != nil || !ready || state == "CONNECTED" || shared {
		t.Fatal("personal configuration readiness fabricated global credentials or health", err)
	}
	result, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-preflight-grant-one", ExpectedVersion: &connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true}})
	if err != nil || result.Connection == nil {
		t.Fatal("grant personal connection without a shared key", err)
	}
	if result.Connection.State == "CONNECTED" || result.Connection.CredentialRevision != nil || result.Connection.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired {
		t.Fatal("personal grant made a common authenticated connection")
	}
	var reason string
	if err := r.pool.QueryRow(ctx, `SELECT reason FROM control_plane.integration_grant_admission($1::uuid,$2::uuid,NULL,$3,$4,'AGENT',$5,'gitlab.project.metadata.read','USE',NULL)`,
		owner.AuthorityTenant, owner.ActorID, connection.Ref, projectResult.Project.Ref, agent.Ref).Scan(&reason); err != nil || reason != "READY" {
		t.Fatal("personal configuration-only USE incorrectly required a shared credential", err)
	}
	connection = *result.Connection
	second, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-launch-second-connection"}, Payload: command.ConnectionInput{DefinitionKey: "gitlab", Name: "Personal launch missing key", PublicConfiguration: map[string]any{"base_url": "https://git.example.invalid", "project_path": "fixture/launch"}}})
	if err != nil || second.Connection == nil {
		t.Fatal("create missing launch Connection", err)
	}
	missing := *second.Connection
	for _, enabled := range []bool{false, true} {
		owner.Permission = "platform.command.integrations.enable"
		changed, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner, Mutation: value.Mutation{IdempotencyKey: map[bool]string{false: "personal-launch-disable", true: "personal-launch-enable"}[enabled], ExpectedVersion: &missing.Version}, Payload: command.ConnectionInput{Ref: missing.Ref, Enabled: enabled}})
		if err != nil || changed.Connection == nil {
			t.Fatal("change launch Connection availability", err)
		}
		missing = *changed.Connection
		if !enabled {
			owner.Permission = "platform.integrations.credential-policy.set"
			changed, err = service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-launch-policy", ExpectedVersion: &missing.Version}, Payload: command.IntegrationCredentialPolicyInput{ConnectionRef: missing.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}})
			if err != nil || changed.Connection == nil {
				t.Fatal("set launch personal policy", err)
			}
			missing = *changed.Connection
		}
	}
	grant := func(target entity.Agent) {
		t.Helper()
		changed, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-launch-grant-" + target.Ref, ExpectedVersion: &missing.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: missing.Ref, AgentRef: target.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true}})
		if err != nil || changed.Connection == nil {
			t.Fatal("grant missing personal Connection", err)
		}
		missing = *changed.Connection
	}
	grant(agent)
	otherProject, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-launch-project-two"}, Payload: command.ProjectInput{Name: "Other personal launch project", Purpose: "Prove scoped requirement enumeration", Language: "ru"}})
	if err != nil || otherProject.Project == nil {
		t.Fatal("create second personal launch project", err)
	}
	otherAgent := createLifecycleAgent(t, ctx, service, owner, otherProject.Project.Ref, "personal-launch-agent-two", "Other personal launch agent")
	grant(otherAgent)
	for _, test := range []struct {
		project  string
		agent    entity.Agent
		expected int
		source   string
	}{
		{projectResult.Project.Ref, agent, 2, "CONTROL_CENTER"}, {otherProject.Project.Ref, otherAgent, 1, "CONTROL_CENTER"}, {projectResult.Project.Ref, agent, 2, "SCHEDULE"},
		{projectResult.Project.Ref, agent, 2, "INTEGRATION"},
	} {
		var sessionsBefore, runsBefore, sessionsAfter, runsAfter int
		if err := r.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)").Scan(&sessionsBefore, &runsBefore); err != nil {
			t.Fatal(err)
		}
		_, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-blocked-launch-" + test.project + test.source}, Payload: command.LaunchRunInput{ProjectRef: test.project, Target: entity.RunTarget{Type: "AGENT", Ref: test.agent.Ref}, Task: "Synthetic blocked personal launch", Source: test.source}})
		var failure *platformservice.IntegrationCredentialPreflightError
		if !errors.As(err, &failure) || !errors.Is(err, errs.ErrConflict) || len(failure.Issues) != test.expected {
			t.Fatalf("launch did not collect its full personal problem list: error=%v", err)
		}
		for _, issue := range failure.Issues {
			if issue.ConnectionName == "" {
				t.Fatal("visible connection lost its safe display name")
			}
			if issue.ConnectionRef == connection.Ref && test.source == "CONTROL_CENTER" && issue.Problem != entity.IntegrationCredentialRevoked {
				t.Fatal("revoked personal key was not identified before launch")
			}
			if issue.ConnectionRef == missing.Ref && test.source == "CONTROL_CENTER" && issue.Problem != entity.IntegrationCredentialMissing {
				t.Fatal("absent personal binding was not reported missing")
			}
			if (test.source == "SCHEDULE" || test.source == "INTEGRATION") && issue.Problem != entity.IntegrationCredentialInitiatorMissing {
				t.Fatal("schedule silently used its owner's personal key")
			}
			if test.project == otherProject.Project.Ref && issue.ConnectionRef != missing.Ref {
				t.Fatal("preflight included another project's grant")
			}
		}
		if err := r.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)").Scan(&sessionsAfter, &runsAfter); err != nil || sessionsBefore != sessionsAfter || runsBefore != runsAfter {
			t.Fatal("blocked preflight persisted a session or run", err)
		}
	}
	plainAgent := createLifecycleAgent(t, ctx, service, owner, otherProject.Project.Ref, "personal-launch-legacy-agent", "Legacy launch without personal requirements")
	plain, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-launch-legacy-positive"}, Payload: command.LaunchRunInput{ProjectRef: otherProject.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: plainAgent.Ref}, Task: "Synthetic legacy launch remains available"}})
	if err != nil || plain.Run == nil {
		t.Fatal("personal preflight changed launch without personal requirements", err)
	}
	assertPersonalInvocationCredentialPin(t, ctx, r, owner, connection, plain.Run.Ref)
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-launch-legacy-cleanup", ExpectedVersion: &plain.Run.Version}, Payload: command.RunCommandInput{RunRef: plain.Run.Ref}}); err != nil {
		t.Fatal("cancel synthetic legacy launch", err)
	}
	// This isolated session metadata fixture tests ownership refusal, not a live second-user login.
	// Эта isolated session metadata fixture проверяет отказ по владельцу, не live second-user login.
	if _, err := r.pool.Exec(ctx, `INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,provider_account_id,state,created_by)
SELECT 'ses_personalforeign',source.organization_id,(SELECT id FROM control_plane.projects WHERE ref=$1),'AGENT',$2,source.provider_account_id,'ACTIVE',$3::uuid
FROM control_plane.sessions source WHERE source.ref=$4`, projectResult.Project.Ref, agent.Ref, secondUser.ActorID, plain.Run.SessionRef); err != nil {
		t.Fatal("prepare isolated foreign-session metadata fixture", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-launch-foreign-session"}, Payload: command.LaunchRunInput{ProjectRef: projectResult.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, SessionRef: "ses_personalforeign", Task: "Synthetic refused cross-owner continuation"}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("target launch rights replaced the stored session initiator", err)
	}
	assertPersonalPublishedWorkflowPreflight(t, ctx, r, service, owner, connection.Ref, projectResult.Project.Ref, agent.Ref)
	// A published step's explicit requirement remains visible after its grant is disabled.
	// Явное требование опубликованного этапа остаётся видимым после отключения grant.
	disabled, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-launch-disable-required-grant", ExpectedVersion: &missing.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: missing.Ref, AgentRef: agent.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: false}})
	if err != nil || disabled.Connection == nil {
		t.Fatal("disable explicit personal requirement", err)
	}
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve fixture owner identity", err)
	}
	current, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal("resolve fixture owner scope", err)
	}
	var projectID string
	if err := r.pool.QueryRow(ctx, "SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2", current.organizationID, projectResult.Project.Ref).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	issues, err := r.personalLaunchCredentialIssues(ctx, tx, current, projectID,
		[]personalLaunchTarget{{AgentRef: agent.Ref, Capabilities: []string{"gitlab.project.metadata.read"}}}, personalLaunchContext{source: "CONTROL_CENTER"})
	if err != nil || len(issues) != 2 {
		t.Fatal("explicit workflow-shaped requirement disappeared after grant disable", err)
	}
	found := false
	for _, issue := range issues {
		if issue.ConnectionRef == missing.Ref {
			found = issue.Problem == entity.IntegrationCredentialConnectionUnavailable
		}
	}
	if !found {
		t.Fatal("disabled required grant did not report unavailable Connection")
	}
}
