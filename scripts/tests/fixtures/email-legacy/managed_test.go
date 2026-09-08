package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Этот test-only профиль проверяет прежний producer/caster/new gateway.
// Исторический UI/GIT published snapshot создаётся только в disposable БД;
// UI/Git publication и старый email authorizer здесь не объявляются проверенными.
func testLegacyManagedEmailProducer(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, connection entity.IntegrationConnection, config api.Configuration, origin string) {
	t.Helper()
	connection = testLegacyManagedEmailFixture(t, ctx, repository, owner, connection, origin)
	var configErr error
	config, configErr = repository.EmailConfiguration(ctx)
	if configErr != nil {
		t.Fatal(configErr)
	}
	worker := func(workload, operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject",
			ExternalTenantID: "kodex-installation", CallerWorkload: workload, Operation: operation}, workload)
	}
	gateway := worker("integration-gateway", "platform.runtime.integration-tests.claim")
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-test", ExpectedVersion: &connection.Version},
		Payload:  command.ConnectionInput{Ref: connection.Ref}}); err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "email-managed-"+origin+"-test", 32)
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	for _, claim := range claims {
		if stringMap(claim, "connectionRef") == connection.Ref {
			health = claim
		}
	}
	if health == nil {
		t.Fatal("email health claim missing")
	}
	testLegacyEmailGatewayClaim(t, health, "health")
	binding := entity.EmailExecutionBinding{ConnectionTestRef: stringMap(health, "testRef"), LeaseRef: stringMap(health, "leaseRef"),
		Fence: stringMap(health, "fence"), Generation: health["generation"].(int64), ExpiresAt: health["expiresAt"].(time.Time)}
	complete, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-test-complete"}, Payload: command.IntegrationConnectionTestInput{
			TestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, Success: true}})
	if err != nil || complete.Connection == nil {
		t.Fatalf("complete email health: %v", err)
	}
	connection = *complete.Connection
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-project"}, Payload: command.ProjectInput{Name: "Email managed " + origin, Purpose: "Owner authorization", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create email project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "email-managed-"+origin+"-agent", "Email operator")
	granted, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-grant", ExpectedVersion: &connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "email.message.send", AgentRef: agent.Ref, Enabled: true}})
	if err != nil || granted.Connection == nil {
		t.Fatalf("grant email: %v", err)
	}
	granted, err = service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-read-grant", ExpectedVersion: &granted.Connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "email.message.list", AgentRef: agent.Ref, Enabled: true}})
	if err != nil || granted.Connection == nil {
		t.Fatalf("grant email read: %v", err)
	}
	config.Revision++
	config.Mailboxes[0].Revision++
	for index := range config.Mailboxes[0].Policies {
		if config.Mailboxes[0].Policies[index].Operation == api.OperationList {
			config.Mailboxes[0].Policies[index].Policy = api.HumanGate
		}
		if config.Mailboxes[0].Policies[index].Operation == api.OperationSend {
			config.Mailboxes[0].Policies[index].Policy = api.Allow
		}
	}
	configurationJSON, _ := json.Marshal(config)
	if err := repository.ConfigureEmail(ctx, configurationJSON); err != nil {
		t.Fatal(err)
	}
	run, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref,
			Title: "Email effect", Task: "Send approved message", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}})
	if err != nil || run.Run == nil {
		t.Fatalf("launch email run: %v", err)
	}
	defer func() {
		current, err := service.GetRun(ctx, owner, run.Run.Ref)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-cancel", ExpectedVersion: &current.Version},
			Payload:  command.RunCommandInput{RunRef: current.Ref}}); err != nil {
			t.Error(err)
		}
	}()
	runtime := worker("runtime-controller", "platform.runtime.execution.claim")
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: runtime,
		Mutation: value.Mutation{IdempotencyKey: "email-managed-" + origin + "-execution"}, Payload: command.LeaseInput{WorkloadInstance: "email-managed-" + origin + "-runtime", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim email runtime: %v", err)
	}
	execution := claimed.RuntimeItems[0]
	readInvocation, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": connection.Ref,
		"capability_key": "email.message.list", "idempotency_key": "email-managed-" + origin + "-mailbox-read"}, map[string]any{})
	if err != nil || stringMap(readInvocation, "state") != "WAITING_APPROVAL" || stringMap(readInvocation, "gateRef") == "" {
		t.Fatalf("mailbox Human Gate did not protect READ operation: %v", err)
	}
	bounded := map[string]any{"to": "recipient@example.test", "subject": "Fixture", "body_text": "Bounded test"}
	invocation, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": connection.Ref,
		"capability_key": "email.message.send", "idempotency_key": "email-managed-" + origin + "-send"}, bounded)
	if err != nil || stringMap(invocation, "state") != "READY" || stringMap(invocation, "gateRef") != "" {
		t.Fatalf("mailbox ALLOW unexpectedly required gate: %v", err)
	}
	claims, err = service.ClaimIntegrationInvocations(ctx, gateway, "email-managed-"+origin+"-gateway", 32)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim email effect: %d %v", len(claims), err)
	}
	claim := claims[0]
	testLegacyEmailGatewayClaim(t, claim, "invocation")
}

func testLegacyManagedEmailFixture(t *testing.T, ctx context.Context, repository *Repository, owner value.Principal, connection entity.IntegrationConnection, origin string) entity.IntegrationConnection {
	t.Helper()
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	definition := repository.integrationDefinitions["email"]
	// Клонирование через JSON исключает изменение in-memory shipped catalog.
	raw, _ := json.Marshal(definition)
	definition, err = integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	definition.Metadata.Origin = origin
	definition.Spec.HealthCheck.TimeoutSeconds--
	raw, _ = json.Marshal(definition)
	definition, err = integrationpackage.Parse(raw)
	if err != nil || integrationpackage.ValidateExecutableRevision(definition, repository.integrationDefinitions["email"]) != nil {
		t.Fatal("legacy fixture narrowing invalid")
	}
	source, sourceRevision := "control-center", ""
	if origin == "GIT" {
		source, sourceRevision = "https://example.invalid/configuration.git", strings.Repeat("a", 40)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var configurationID, revisionID string
	err = tx.QueryRow(ctx, `INSERT INTO control_plane.managed_configuration_sets
 (ref,organization_id,kind,name,managed_by,source,source_revision,created_by)
 VALUES ($1,$2,'INTEGRATION_DEFINITION',$3,$4,$5,$6,$7) RETURNING id`,
		"mcfg_legacy_email_"+origin, current.organizationID, "Legacy email "+origin, origin, source, sourceRevision, current.actorID).Scan(&configurationID)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO control_plane.managed_configuration_revisions
 (ref,organization_id,configuration_set_id,revision,state,content_format,content,digest,created_by,validated_at,published_at)
 VALUES ($1,$2,$3,1,'PUBLISHED','JSON',$4,$5,$6,clock_timestamp(),clock_timestamp()) RETURNING id`,
		"mrev_legacy_email_"+origin, current.organizationID, configurationID, string(raw), definition.Digest, current.actorID).Scan(&revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET current_revision_id=$2 WHERE id=$1`, configurationID, revisionID); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO control_plane.managed_configuration_bindings
 (ref,organization_id,configuration_set_id,configuration_revision_id,configuration_kind,consumer_kind,consumer_ref,rebound_by)
 VALUES ($1,$2,$3,$4,'INTEGRATION_DEFINITION','INTEGRATION_CONNECTION',$5,$6)
 ON CONFLICT (organization_id,configuration_kind,consumer_kind,consumer_ref) DO UPDATE SET
 configuration_set_id=EXCLUDED.configuration_set_id,configuration_revision_id=EXCLUDED.configuration_revision_id,version=managed_configuration_bindings.version+1`,
		"mcbind_legacy_email_"+origin, current.organizationID, configurationID, revisionID, connection.Ref, current.actorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, queryIntegrationPackageBindConnection, current.organizationID, connection.Ref, definition.Metadata.Version, definition.Digest); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	connection, err = readConnection(ctx, repository.pool, current, connection.Ref)
	if err != nil || connection.DefinitionDigest != definition.Digest {
		t.Fatal("managed legacy fixture readback invalid")
	}
	return connection
}
