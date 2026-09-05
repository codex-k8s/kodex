package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testEmailProducer(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, connection entity.IntegrationConnection, config api.Configuration) {
	t.Helper()
	worker := func(workload, operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject",
			ExternalTenantID: "kodex-installation", CallerWorkload: workload, Operation: operation}, workload)
	}
	email := worker("email-bridge", "platform.email.authorization.resolve")
	gateway := worker("integration-gateway", "platform.runtime.integration-tests.claim")
	configured, err := service.Execute(ctx, command.Command{Kind: command.ConfigureConnectionCredential, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-credential", ExpectedVersion: &connection.Version},
		Payload: command.ConnectionInput{Ref: connection.Ref, MaterializationRef: "email-producer-credential",
			CredentialRevision: &entity.IntegrationCredentialRevision{SecretRef: "kodex-system/kodex-integration-credentials#email-test",
				SecretUID: "60000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}}})
	if err != nil || configured.Connection == nil {
		t.Fatalf("configure email credential: %v", err)
	}
	connection = *configured.Connection
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-test", ExpectedVersion: &connection.Version},
		Payload:  command.ConnectionInput{Ref: connection.Ref}}); err != nil {
		t.Fatal(err)
	}
	claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "email-producer-test", 32)
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
	binding := entity.EmailExecutionBinding{ConnectionTestRef: stringMap(health, "testRef"), LeaseRef: stringMap(health, "leaseRef"),
		Fence: stringMap(health, "fence"), Generation: health["generation"].(int64), ExpiresAt: health["expiresAt"].(time.Time)}
	mailbox := config.Mailboxes[0]
	semantic, err := api.CommandForIntegration("email.delivery.health.read", mailbox.Id, mailbox.Sender, "", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	input := query.EmailAuthorization{Binding: binding, MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision,
		Operation: "health", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(semantic)}
	decision, err := service.ResolveEmailAuthorization(ctx, email, input)
	if err != nil || !decision.Allowed || decision.AgentRef != "" || decision.AgentScope != nil || decision.CredentialGeneration != mailbox.CredentialGeneration {
		t.Fatalf("authorize health: %v", err)
	}
	for _, kind := range []string{"worker", "fence", "digest", "mailbox", "revision", "sender", "expiry"} {
		bad, actor := input, email
		switch kind {
		case "worker":
			actor = gateway
		case "fence":
			bad.Binding.Fence += "wrong"
		case "digest":
			bad.SemanticInputDigest = strings.Repeat("e", 64)
		case "mailbox":
			bad.MailboxRef = "foreign-mailbox"
		case "revision":
			bad.ConfigurationRevision++
		case "sender":
			bad.Sender = "foreign@example.test"
		case "expiry":
			bad.Binding.ExpiresAt = bad.Binding.ExpiresAt.Add(time.Second)
		}
		if _, err := service.ResolveEmailAuthorization(ctx, actor, bad); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("authorize wrong %s: %v", kind, err)
		}
	}
	complete, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-test-complete"}, Payload: command.IntegrationConnectionTestInput{
			TestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, Success: true}})
	if err != nil || complete.Connection == nil {
		t.Fatalf("complete email health: %v", err)
	}
	connection = *complete.Connection
	if _, err := service.ResolveEmailAuthorization(ctx, email, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal test authorized: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-project"}, Payload: command.ProjectInput{Name: "Email producer", Purpose: "Owner authorization", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create email project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "email-producer-agent", "Email operator")
	granted, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-grant", ExpectedVersion: &connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "email.message.send", AgentRef: agent.Ref, Enabled: true}})
	if err != nil || granted.Connection == nil {
		t.Fatalf("grant email: %v", err)
	}
	granted, err = service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-read-grant", ExpectedVersion: &granted.Connection.Version},
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
	mailbox = config.Mailboxes[0]
	run, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref,
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
			Mutation: value.Mutation{IdempotencyKey: "email-producer-cancel", ExpectedVersion: &current.Version},
			Payload:  command.RunCommandInput{RunRef: current.Ref}}); err != nil {
			t.Error(err)
		}
	}()
	runtime := worker("runtime-controller", "platform.runtime.execution.claim")
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: runtime,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-execution"}, Payload: command.LeaseInput{WorkloadInstance: "email-producer-runtime", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim email runtime: %v", err)
	}
	execution := claimed.RuntimeItems[0]
	readInvocation, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": connection.Ref,
		"capability_key": "email.message.list", "idempotency_key": "email-producer-mailbox-read"}, map[string]any{})
	if err != nil || stringMap(readInvocation, "state") != "WAITING_APPROVAL" || stringMap(readInvocation, "gateRef") == "" {
		t.Fatalf("mailbox Human Gate did not protect READ operation: %v", err)
	}
	bounded := map[string]any{"to": "recipient@example.test", "subject": "Fixture", "body_text": "Bounded test"}
	invocation, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": connection.Ref,
		"capability_key": "email.message.send", "idempotency_key": "email-producer-send"}, bounded)
	if err != nil || stringMap(invocation, "state") != "WAITING_APPROVAL" {
		t.Fatalf("resolve email gate: %v", err)
	}
	if claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-producer-gateway", 32); err != nil || len(claims) != 0 {
		t.Fatalf("claimed before gate: %d %v", len(claims), err)
	}
	version := int64(1)
	if _, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-approve", ExpectedVersion: &version},
		Payload:  command.GateResolutionInput{GateRef: stringMap(invocation, "gateRef"), Decision: "APPROVE", Comment: "Exact fixture"}}); err != nil {
		t.Fatal(err)
	}
	claims, err = service.ClaimIntegrationInvocations(ctx, gateway, "email-producer-gateway", 32)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim email effect: %d %v", len(claims), err)
	}
	claim := claims[0]
	binding = entity.EmailExecutionBinding{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
		Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), ExpiresAt: claim["expiresAt"].(time.Time)}
	encoded, _ := json.Marshal(bounded)
	semantic, err = api.CommandForIntegration("email.message.send", mailbox.Id, mailbox.Sender, stringMap(claim, "effectKey"), encoded)
	if err != nil {
		t.Fatal(err)
	}
	input = query.EmailAuthorization{Binding: binding, MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision,
		Operation: "send", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(semantic), EffectKey: semantic.EffectKey}
	decision, err = service.ResolveEmailAuthorization(ctx, email, input)
	if err != nil || !decision.Allowed || !decision.GateApproved || decision.Policy != "human_gate" || decision.AgentRef != agent.Ref || decision.ProjectRef != project.Project.Ref {
		t.Fatalf("authorize approved send: %v", err)
	}
	report := command.Command{Kind: command.ReportEmailEffect, Principal: worker("email-bridge", "platform.email.effect-receipts.report"),
		Mutation: value.Mutation{IdempotencyKey: "email-producer-unknown"}, Payload: command.EmailEffectReportInput{
			Binding: binding, ExternalReceiptRef: strings.Repeat("f", 32), ExternalReceiptDigest: strings.Repeat("c", 64),
			SemanticInputDigest: input.SemanticInputDigest, Outcome: "UNKNOWN_OUTCOME"}}
	first, err := service.Execute(ctx, report)
	if err != nil || first.EmailReceipt == nil || first.EmailReceipt.Version != 1 {
		t.Fatalf("report before write: %v", err)
	}
	replay, err := service.Execute(ctx, report)
	if err != nil || replay.EmailReceipt == nil || replay.EmailReceipt.Ref != first.EmailReceipt.Ref {
		t.Fatalf("recover report response: %v", err)
	}
	badReport := report
	badPayload := report.Payload.(command.EmailEffectReportInput)
	badPayload.ExternalReceiptDigest = strings.Repeat("d", 64)
	badReport.Payload = badPayload
	if _, err := service.Execute(ctx, badReport); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("mismatched receipt commitment: %v", err)
	}
	confirmed := report
	confirmed.Mutation.IdempotencyKey = "email-producer-confirmed"
	confirmedPayload := report.Payload.(command.EmailEffectReportInput)
	confirmedPayload.Outcome = "EFFECT_CONFIRMED"
	confirmed.Payload = confirmedPayload
	updated, err := service.Execute(ctx, confirmed)
	if err != nil || updated.EmailReceipt == nil || updated.EmailReceipt.Version != 2 {
		t.Fatalf("report confirmed effect: %v", err)
	}
	replay, err = service.Execute(ctx, report)
	if err != nil || replay.EmailReceipt == nil || replay.EmailReceipt.Version != 1 {
		t.Fatalf("immutable original report replay: %v", err)
	}
	revoked, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-revoke", ExpectedVersion: &granted.Connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "email.message.send", AgentRef: agent.Ref, Enabled: false}})
	if err != nil || revoked.Connection == nil {
		t.Fatalf("revoke email grant: %v", err)
	}
	if _, err := service.ResolveEmailAuthorization(ctx, email, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked grant authorized: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-producer-regrant", ExpectedVersion: &revoked.Connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "email.message.send", AgentRef: agent.Ref, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveEmailAuthorization(ctx, email, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("new grant version revived old runtime: %v", err)
	}
	replay, err = service.Execute(ctx, report)
	if err != nil || replay.EmailReceipt == nil || replay.EmailReceipt.Ref != first.EmailReceipt.Ref {
		t.Fatalf("revocation destroyed immutable receipt read: %v", err)
	}
}
