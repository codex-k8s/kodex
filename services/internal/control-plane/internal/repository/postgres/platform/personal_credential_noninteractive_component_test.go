package platform

import (
	"context"
	"errors"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalNoninteractiveConfiguration checks real schedule create/update/enable commands
// against a READY owner's personal key. Refusals precede revisions, events and execution; a
// formerly shared-only schedule is rechecked after its target gains a personal requirement.
// Disabling, archiving and deleting remain available without borrowing any user's credential.
// assertPersonalNoninteractiveConfiguration проверяет настоящие create/update/enable расписания
// при READY личном ключе владельца. Отказ предшествует revisions, events и исполнению; прежнее
// shared-only расписание проверяется после появления personal requirement у target.
// Отключение, архивация и удаление доступны без заимствования пользовательского ключа.
func assertPersonalNoninteractiveConfiguration(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, personalAgentRef, connectionRef string) {
	t.Helper()
	self := owner
	self.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, self, connectionRef)
	if err != nil || binding.State != "READY" {
		t.Fatal("noninteractive fixture requires the author's own ready key", err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "personal-noninteractive-")
	plain := createLifecycleAgent(t, ctx, service, owner, projectRef, "personal-noninteractive-plain", "Initially shared-only schedule target")
	payload := command.ScheduleInput{ProjectRef: projectRef, Name: "Synthetic noninteractive contract", Target: entity.RunTarget{Type: "AGENT", Ref: plain.Ref}, Preset: "CUSTOM", CronExpression: "0 * * * *", Timezone: "UTC", Input: map[string]any{}, AutomationText: "Read synthetic metadata", PromptInputs: map[string]any{}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"}
	counts := func() [5]int64 {
		t.Helper()
		var result [5]int64
		if err := repository.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM control_plane.schedules),
 (SELECT count(*) FROM control_plane.schedule_revisions),
 (SELECT count(*) FROM control_plane.outbox_events),
 (SELECT count(*) FROM control_plane.sessions),
 (SELECT count(*) FROM control_plane.runs)`).Scan(&result[0], &result[1], &result[2], &result[3], &result[4]); err != nil {
			t.Fatal(err)
		}
		return result
	}
	refuse := func(kind command.Kind, key string, version *int64, input command.ScheduleInput) {
		t.Helper()
		before := counts()
		_, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-noninteractive-" + key, ExpectedVersion: version}, Payload: input})
		var problem *platformservice.IntegrationCredentialPreflightError
		if !errors.As(err, &problem) || len(problem.Issues) != 1 || problem.Issues[0].Problem != entity.IntegrationCredentialInitiatorMissing || problem.Issues[0].ConnectionRef != connectionRef || counts() != before {
			t.Fatal("noninteractive configuration borrowed a personal initiator or changed durable state", err)
		}
	}
	personal := payload
	personal.Target.Ref = personalAgentRef
	refuse(command.CreateSchedule, "create-refused", nil, personal)
	schedule := execute(command.CreateSchedule, owner, "plain-create", nil, payload).Schedule
	if schedule == nil {
		t.Fatal("shared-only schedule configuration was blocked")
	}
	personal.Ref = schedule.Ref
	refuse(command.UpdateSchedule, "update-refused", &schedule.Version, personal)
	schedule = execute(command.SetScheduleEnabled, owner, "plain-disable", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref, Enabled: false}).Schedule
	if schedule == nil || schedule.Enabled {
		t.Fatal("plain schedule did not disable")
	}
	schedule = execute(command.SetScheduleEnabled, owner, "plain-enable", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref, Enabled: true}).Schedule
	schedule = execute(command.SetScheduleEnabled, owner, "plain-disable-again", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref, Enabled: false}).Schedule
	source := execute(command.CreateConnection, owner, "source-create", nil, command.ConnectionInput{DefinitionKey: "mattermost", Name: "Synthetic noninteractive source", PublicConfiguration: map[string]any{"base_url": "https://channel.example.invalid", "team_name": "fixture", "channel_name": "synthetic"}}).Connection
	if source == nil {
		t.Fatal("inbound source configuration missing")
	}
	if _, err := repository.pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, source.Ref); err != nil {
		t.Fatal("prepare synthetic source credential metadata", err)
	}
	currentSource, err := service.GetIntegrationConnection(ctx, owner, source.Ref)
	if err != nil {
		t.Fatal(err)
	}
	source = &currentSource
	refuseInbound := func(key, target string) {
		t.Helper()
		before := counts()
		_, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-noninteractive-" + key, ExpectedVersion: &source.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: source.Ref, AgentRef: target, CapabilityKey: "mattermost.inbound", ApprovalPolicy: "NONE", Enabled: true}})
		var problem *platformservice.IntegrationCredentialPreflightError
		if !errors.As(err, &problem) || len(problem.Issues) != 1 || problem.Issues[0].Problem != entity.IntegrationCredentialInitiatorMissing || problem.Issues[0].ConnectionRef != connectionRef || counts() != before {
			t.Fatal("inbound route configuration borrowed the author's personal identity", err)
		}
		current, err := service.GetIntegrationConnection(ctx, owner, source.Ref)
		if err != nil || current.Version != source.Version {
			t.Fatal("refused inbound route changed its source version", err)
		}
	}
	refuseInbound("source-personal-refused", personalAgentRef)
	source = execute(command.ChangeIntegrationGrant, owner, "source-plain-allowed", &source.Version, command.IntegrationGrantInput{ConnectionRef: source.Ref, AgentRef: plain.Ref, CapabilityKey: "mattermost.inbound", ApprovalPolicy: "NONE", Enabled: true}).Connection
	var sourceGrantRef string
	if err := repository.pool.QueryRow(ctx, `SELECT grant_row.ref FROM control_plane.integration_grants grant_row
JOIN control_plane.integration_connections connection ON connection.id=grant_row.connection_id
WHERE connection.ref=$1 AND grant_row.target_kind='AGENT' AND grant_row.target_ref=$2 AND grant_row.capability_key='mattermost.inbound'`, source.Ref, plain.Ref).Scan(&sourceGrantRef); err != nil {
		t.Fatal(err)
	}
	plain, err = service.GetAgent(ctx, owner, plain.Ref)
	if err != nil {
		t.Fatal(err)
	}
	plain = *execute(command.ChangeAgentGrant, owner, "agent-source-disable", &plain.Version, command.AgentBindingInput{AgentRef: plain.Ref, BindingRef: sourceGrantRef, Enabled: false}).Agent
	plain = *execute(command.ChangeAgentGrant, owner, "agent-source-plain-enable", &plain.Version, command.AgentBindingInput{AgentRef: plain.Ref, BindingRef: sourceGrantRef, Enabled: true}).Agent
	connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.ChangeIntegrationGrant, owner, "target-gains-personal", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connectionRef, AgentRef: plain.Ref, CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true})
	refuseInbound("source-reenable-refused", plain.Ref)
	before := counts()
	_, err = service.Execute(ctx, command.Command{Kind: command.ChangeAgentGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-noninteractive-agent-source-refused", ExpectedVersion: &plain.Version}, Payload: command.AgentBindingInput{AgentRef: plain.Ref, BindingRef: sourceGrantRef, Enabled: true}})
	var problem *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &problem) || len(problem.Issues) != 1 || problem.Issues[0].Problem != entity.IntegrationCredentialInitiatorMissing || counts() != before {
		t.Fatal("agent-card grant enable bypassed noninteractive preflight", err)
	}
	unchanged, err := service.GetAgent(ctx, owner, plain.Ref)
	if err != nil || unchanged.Version != plain.Version {
		t.Fatal("refused agent-card enable changed the agent version", err)
	}
	plain = *execute(command.ChangeAgentGrant, owner, "agent-source-personal-disable", &plain.Version, command.AgentBindingInput{AgentRef: plain.Ref, BindingRef: sourceGrantRef, Enabled: false}).Agent
	source = execute(command.ChangeIntegrationGrant, owner, "source-disable-allowed", &source.Version, command.IntegrationGrantInput{ConnectionRef: source.Ref, AgentRef: plain.Ref, CapabilityKey: "mattermost.inbound", ApprovalPolicy: "NONE", Enabled: false}).Connection
	refuse(command.SetScheduleEnabled, "enable-refused", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref, Enabled: true})
	current, err := service.GetSchedule(ctx, owner, schedule.Ref)
	if err != nil || current.Version != schedule.Version || current.Enabled || current.CurrentRevision.Ref != schedule.CurrentRevision.Ref {
		t.Fatal("rejected enable changed the schedule or revision", err)
	}
	schedule = execute(command.SetScheduleEnabled, owner, "disable-still-allowed", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref, Enabled: false}).Schedule
	schedule = execute(command.ArchiveSchedule, owner, "archive-still-allowed", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref}).Schedule
	if schedule == nil || schedule.State != "ARCHIVED" {
		t.Fatal("personal requirement prevented schedule archival")
	}
	execute(command.DeleteSchedule, owner, "delete-still-allowed", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref})
}
