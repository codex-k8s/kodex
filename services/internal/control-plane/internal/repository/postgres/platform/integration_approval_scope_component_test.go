package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScopedIntegrationApprovalComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute,
		MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey:       []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap attempt %d: %v", attempt+1, err)
		}
	}
	seedObservedCatalogFixture(t, ctx, repository)
	testScopedIntegrationApproval(t, ctx, repository, pool)
}

func testScopedIntegrationApproval(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	original := repository.integrationDefinitions["synthetic"]
	modified := original
	modified.Metadata.Version = "99.0.0"
	modified.Spec.Capabilities = append([]integrationpackage.Capability(nil), original.Spec.Capabilities...)
	for index := range modified.Spec.Capabilities {
		if modified.Spec.Capabilities[index].Key == "synthetic.journal.write" {
			modified.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_SCOPED"
		}
	}
	raw, err := json.Marshal(modified)
	if err != nil {
		t.Fatal(err)
	}
	// Исполняемый OPENAPI_MCP пока закрыт сетевым admission. Только disposable
	// fixture обходит adapter registry, чтобы проверить owner-owned lifecycle.
	digest := sha256.Sum256(raw)
	modified.Digest = hex.EncodeToString(digest[:])
	capability, ok := modified.Capability("synthetic.journal.write")
	if !ok {
		t.Fatal("scoped capability is missing")
	}
	if _, err := capability.ResolveApprovalScope([]string{"/action"}, []byte(`{"action":"UPDATE","value":"first"}`)); err != nil {
		t.Fatalf("fixture scope resolution failed: %v", err)
	}
	repository.integrationDefinitions["synthetic"] = modified
	defer func() { repository.integrationDefinitions["synthetic"] = original }()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.reconcileIntegrationDefinitions(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("publish scoped test definition: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create",
	}, "control-api-gateway")
	runtimeWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim",
	}, "integration-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-connection"}, Payload: command.ConnectionInput{
			DefinitionKey: "synthetic", Name: "Scoped journal", PublicConfiguration: map[string]any{"journal": "scoped-fixture"},
		}})
	if err != nil || connection.Connection == nil {
		t.Fatalf("create scoped connection: %v", err)
	}
	var connectionVersion int64
	if err := pool.QueryRow(ctx, bootstrapComponentConnectIntegrationQuery, connection.Connection.Ref).Scan(&connectionVersion); err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-project"},
		Payload:  command.ProjectInput{Name: "Scoped approval fixture", Purpose: "Verify reusable typed approval", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create scoped project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "scoped-agent", "Scoped operator")
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-invalid-grant", ExpectedVersion: &connectionVersion},
		Payload: command.IntegrationGrantInput{ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: true,
			ApprovalScopePaths: []string{"/action", "/action"}},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("duplicate scope path was accepted: %v", err)
	}
	granted, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-grant", ExpectedVersion: &connectionVersion},
		Payload: command.IntegrationGrantInput{ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: true,
			ApprovalScopePaths: []string{"/action"}},
	})
	if err != nil || granted.Connection == nil || len(granted.Connection.Grants) != 1 ||
		granted.Connection.Grants[0].ApprovalPolicy != "HUMAN_SCOPED" {
		t.Fatalf("create scoped grant: %v", err)
	}
	run, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-run"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Scoped effects", Task: "Test typed scope reuse", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
		}})
	if err != nil || run.Run == nil {
		t.Fatalf("launch scoped run: %v", err)
	}
	execution, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: runtimeWorker,
		Mutation: value.Mutation{IdempotencyKey: "scoped-runtime-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: "scoped-runtime", Limit: 1}})
	if err != nil || len(execution.RuntimeItems) != 1 {
		t.Fatalf("claim scoped run: %d %v", len(execution.RuntimeItems), err)
	}
	if stringMap(execution.RuntimeItems[0], "runRef") != run.Run.Ref {
		t.Fatalf("claimed runtime does not match scoped run")
	}
	grants, ok := execution.RuntimeItems[0]["integrationGrants"].([]map[string]string)
	if !ok || len(grants) != 1 || grants[0]["capabilityKey"] != "synthetic.journal.write" ||
		grants[0]["grantVersion"] != "1" {
		t.Fatalf("scoped runtime does not include pinned grant: count=%d", len(grants))
	}
	call := func(key, action, value string) (map[string]any, error) {
		return service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
			"run_ref":        stringMap(execution.RuntimeItems[0], "runRef"),
			"node_ref":       stringMap(execution.RuntimeItems[0], "nodeRef"),
			"connection_ref": connection.Connection.Ref,
			"capability_key": "synthetic.journal.write", "idempotency_key": key,
		}, map[string]any{"action": action, "value": value})
	}
	first, err := call("scoped-first", "UPDATE", "first")
	if err != nil || stringMap(first, "state") != "WAITING_APPROVAL" || stringMap(first, "gateRef") == "" {
		t.Fatalf("first scoped effect must wait: state=%q err=%v", stringMap(first, "state"), err)
	}
	gate, err := service.GetOwnerGate(ctx, owner, stringMap(first, "gateRef"))
	if err != nil || gate.IntegrationIntent == nil {
		t.Fatalf("read scoped approval preview: %v", err)
	}
	preview, ok := gate.IntegrationIntent.EffectPreview["approvalScope"].(map[string]any)
	if !ok {
		t.Fatal("scoped approval preview is missing")
	}
	selected, ok := preview["selected"].([]integrationpackage.ApprovalScopeValue)
	if !ok || len(selected) != 1 || selected[0].Path != "/action" || selected[0].Type != "string" || selected[0].Value != "UPDATE" {
		t.Fatal("scoped approval preview lost selected typed value")
	}
	projectionTx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		_ = projectionTx.Rollback(ctx)
		t.Fatal(err)
	}
	ownerScope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		_ = projectionTx.Rollback(ctx)
		t.Fatal(err)
	}
	eventGate := gate
	if err := repository.projectGateIntent(ctx, projectionTx, ownerScope, &eventGate, false); err != nil {
		_ = projectionTx.Rollback(ctx)
		t.Fatalf("redact event projection: %v", err)
	}
	if err := projectionTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	eventPreview := eventGate.IntegrationIntent.EffectPreview
	eventScope := eventPreview["approvalScope"].(map[string]any)
	redactedSelected, ok := eventScope["selected"].([]map[string]string)
	if !ok || len(redactedSelected) != 1 || redactedSelected[0]["path"] != "/action" || len(eventPreview["fields"].([]any)) != 0 {
		t.Fatal("event projection exposed scoped input values")
	}
	mutable, ok := preview["mutablePaths"].([]string)
	if !ok || len(mutable) == 0 {
		t.Fatal("scoped approval preview lost mutable fields")
	}
	gateVersion := int64(1)
	approved, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-approve", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: stringMap(first, "gateRef"), Decision: "APPROVE"}})
	if err != nil || approved.Gate == nil || approved.Gate.State != "APPROVED" {
		t.Fatalf("approve scoped effect: %v", err)
	}
	firstClaim, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-gateway", 1)
	if err != nil || len(firstClaim) != 1 || stringMap(firstClaim[0], "invocationRef") != stringMap(first, "invocationRef") {
		t.Fatalf("claim first approved effect: count=%d err=%v", len(firstClaim), err)
	}
	completeFailure := func(key string, claim map[string]any) {
		t.Helper()
		_, completeErr := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.IntegrationInvocationInput{
				InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
				Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64),
				SafeErrorCode: "INTEGRATION_REQUEST_REJECTED",
			}})
		if completeErr != nil {
			t.Fatalf("complete failed fixture effect: %v", completeErr)
		}
	}
	completeFailure("scoped-first-complete", firstClaim[0])
	second, err := call("scoped-second", "UPDATE", "second")
	if err != nil || stringMap(second, "state") != "READY" || stringMap(second, "gateRef") != "" ||
		stringMap(second, "invocationRef") == stringMap(first, "invocationRef") {
		t.Fatalf("same typed scope did not reuse approval: state=%q err=%v", stringMap(second, "state"), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_approval_scopes
SET reserved_effects=max_effects WHERE origin_gate_id=(SELECT id FROM control_plane.owner_gates WHERE ref=$1)`,
		stringMap(first, "gateRef")); err != nil {
		t.Fatalf("exhaust scoped approval fixture: %v", err)
	}
	exhausted, err := call("scoped-exhausted", "UPDATE", "fourth")
	if err != nil || stringMap(exhausted, "state") != "WAITING_APPROVAL" || stringMap(exhausted, "gateRef") == "" {
		t.Fatalf("exhausted scope skipped a new gate: state=%q err=%v", stringMap(exhausted, "state"), err)
	}
	changed, err := call("scoped-changed", "DELETE", "third")
	if err != nil || stringMap(changed, "state") != "WAITING_APPROVAL" || stringMap(changed, "gateRef") == "" {
		t.Fatalf("changed typed scope skipped gate: state=%q err=%v", stringMap(changed, "state"), err)
	}
	reconfigured, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-change-paths", ExpectedVersion: &granted.Connection.Version},
		Payload: command.IntegrationGrantInput{ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: true,
			ApprovalScopePaths: []string{"/value"}}})
	if err != nil || reconfigured.Connection == nil {
		t.Fatalf("change scoped grant paths: %v", err)
	}
	staleGate, err := service.GetOwnerGate(ctx, owner, stringMap(changed, "gateRef"))
	if err != nil || staleGate.IntegrationIntent == nil {
		t.Fatalf("read pinned gate after grant change: %v", err)
	}
	stalePreview, ok := staleGate.IntegrationIntent.EffectPreview["approvalScope"].(map[string]any)
	if !ok {
		t.Fatal("pinned gate preview is missing")
	}
	staleSelected, ok := stalePreview["selected"].([]integrationpackage.ApprovalScopeValue)
	if !ok || len(staleSelected) != 1 || staleSelected[0].Path != "/action" || staleSelected[0].Value != "DELETE" {
		t.Fatal("gate preview changed with a later grant revision")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-stale-approve", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: stringMap(changed, "gateRef"), Decision: "APPROVE"}}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("stale grant gate approval was accepted: %v", err)
	}
	claimsBeforeRevoke, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-gateway", 10)
	if err != nil || len(claimsBeforeRevoke) != 0 {
		t.Fatalf("grant path change did not revoke a queued scoped effect: count=%d err=%v", len(claimsBeforeRevoke), err)
	}
	revoked, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-revoke", ExpectedVersion: &reconfigured.Connection.Version},
		Payload: command.IntegrationGrantInput{ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: false}})
	if err != nil || revoked.Connection == nil {
		t.Fatalf("revoke scoped grant: %v", err)
	}
	claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-gateway", 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("revoked scope still claimed effect: count=%d err=%v", len(claims), err)
	}
}
