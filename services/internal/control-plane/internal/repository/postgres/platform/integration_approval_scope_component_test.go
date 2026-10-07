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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
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
			modified.Spec.Capabilities[index].AllowedApprovalPolicies = []string{"HUMAN_SCOPED"}
		}
	}
	raw, err := json.Marshal(modified)
	if err != nil {
		t.Fatal(err)
	}
	// Эта disposable-фикстура изолирует owner-owned scoped lifecycle от
	// внешнего OpenAPI transport и его DNS-допуска.
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
		Payload: command.IntegrationGrantInput{ApprovalPolicy: "HUMAN_SCOPED", ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: true,
			ApprovalScopePaths: []string{"/action", "/action"}},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("duplicate scope path was accepted: %v", err)
	}
	granted, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-grant", ExpectedVersion: &connectionVersion},
		Payload: command.IntegrationGrantInput{ApprovalPolicy: "HUMAN_SCOPED", ConnectionRef: connection.Connection.Ref,
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
	const privatePreviewValue = "scoped-private-preview-first"
	first, err := call("scoped-first", "UPDATE", privatePreviewValue)
	if err != nil || stringMap(first, "state") != "WAITING_APPROVAL" || stringMap(first, "gateRef") == "" {
		t.Fatalf("first scoped effect must wait: state=%q err=%v", stringMap(first, "state"), err)
	}
	gate, err := service.GetOwnerGate(ctx, owner, stringMap(first, "gateRef"))
	if err != nil || gate.IntegrationIntent == nil {
		t.Fatalf("read scoped approval preview: %v", err)
	}
	assertEffectPreview := func(current entity.OwnerGate, visible bool) {
		t.Helper()
		if current.IntegrationIntent == nil {
			t.Fatal("scoped intent is missing")
		}
		effect := current.IntegrationIntent.EffectPreview
		fields, ok := effect["fields"].([]any)
		if !ok || effect["contentComplete"] != visible {
			t.Fatal("scoped effect preview has incorrect completeness")
		}
		if !visible {
			encoded, err := json.Marshal(effect)
			if err != nil || len(fields) != 0 || strings.Contains(string(encoded), privatePreviewValue) {
				t.Fatal("scoped input leaked without decision permission")
			}
			return
		}
		values := make(map[string]any)
		for _, item := range fields {
			field, ok := item.(map[string]any)
			if !ok || field["opaque"] != false || field["truncated"] != false {
				t.Fatal("scoped effect lost safe field descriptors")
			}
			key, ok := field["key"].(string)
			if !ok {
				t.Fatal("scoped effect field key is missing")
			}
			values[key] = field["value"]
		}
		if len(values) != 2 || values["action"] != "UPDATE" || values["value"] != privatePreviewValue {
			t.Fatal("owner lost bounded immutable effect inputs")
		}
	}
	assertEffectPreview(gate, true)
	listed, _, _, err := service.ListOwnerGates(ctx, owner, query.Filter{ProjectRef: project.Project.Ref})
	if err != nil || len(listed) != 1 || listed[0].Ref != gate.Ref {
		t.Fatal("owner gate list lost exact scoped gate", err)
	}
	assertEffectPreview(listed[0], true)
	viewerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009331", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Scoped preview viewer", CallerWorkload: "control-api-gateway", Operation: "platform.query.bootstrap",
	}
	if _, err := repository.ResolveProofAuthority(ctx, viewerInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatal("unbound scoped viewer was accepted", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: viewerInput.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("scoped viewer subject is missing", err)
	}
	viewerOrganization, err := service.Execute(ctx, command.Command{Kind: command.AddPlatformMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-viewer-organization"}, Payload: command.PlatformMembershipInput{
			UserRef: subjects[0].Ref, Role: "MEMBER", Active: true,
		}})
	if err != nil || viewerOrganization.Membership == nil {
		t.Fatal("scoped viewer organization membership is missing", err)
	}
	viewerMember, err := service.Execute(ctx, command.Command{Kind: command.AddMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-viewer-membership"}, Payload: command.MembershipInput{
			ProjectRef: project.Project.Ref, UserRef: subjects[0].Ref, Permissions: []string{"VIEW"}, Active: true,
		}})
	if err != nil || viewerMember.Membership == nil {
		t.Fatal("view-only scoped membership is missing", err)
	}
	viewer := resolvedTestPrincipal(t, ctx, repository, viewerInput, "control-api-gateway")
	viewerGate, err := service.GetOwnerGate(ctx, viewer, gate.Ref)
	if err != nil {
		t.Fatal("view-only scoped gate read failed", err)
	}
	assertEffectPreview(viewerGate, false)
	if len(viewerGate.NextActions) != 0 {
		t.Fatal("view-only scoped gate gained decision actions")
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
	assertEffectPreview(eventGate, false)
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
	assertEffectPreview(*approved.Gate, true)
	historical, err := service.GetOwnerGate(ctx, owner, gate.Ref)
	if err != nil || historical.State != "APPROVED" || len(historical.NextActions) != 0 {
		t.Fatal("terminal scoped history revived decision actions", err)
	}
	assertEffectPreview(historical, true)
	viewerHistorical, err := service.GetOwnerGate(ctx, viewer, gate.Ref)
	if err != nil {
		t.Fatal("view-only terminal scoped history read failed", err)
	}
	assertEffectPreview(viewerHistorical, false)
	var persistedGates, leakedGates int64
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE position($2 in safe_delta::text)>0)
FROM control_plane.run_events WHERE gate_ref=$1`, gate.Ref, privatePreviewValue).Scan(&persistedGates, &leakedGates); err != nil || persistedGates < 2 || leakedGates != 0 {
		t.Fatal("persisted scoped events exposed private preview", err)
	}
	var outboxEvents, leakedOutbox int64
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE position($2 in convert_from(payload,'UTF8'))>0)
FROM control_plane.outbox_events WHERE ordering_key=$1`, "run:"+run.Run.Ref, privatePreviewValue).Scan(&outboxEvents, &leakedOutbox); err != nil || outboxEvents == 0 || leakedOutbox != 0 {
		t.Fatal("scoped outbox exposed private preview", err)
	}
	firstClaim, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-gateway", 1)
	if err != nil || len(firstClaim) != 1 || stringMap(firstClaim[0], "invocationRef") != stringMap(first, "invocationRef") {
		t.Fatalf("claim first approved effect: count=%d err=%v", len(firstClaim), err)
	}
	completeFailure := func(key string, claim map[string]any) {
		t.Helper()
		completion := command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.IntegrationInvocationInput{
				InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
				Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64),
				SafeErrorCode: "INTEGRATION_REQUEST_REJECTED",
			}}
		completed, completeErr := service.Execute(ctx, completion)
		if completeErr != nil {
			t.Fatalf("complete failed fixture effect: %v", completeErr)
		}
		if completed.Event == nil || completed.Event.Delta.IntegrationInvocationRef != stringMap(claim, "invocationRef") {
			t.Fatal("owner completion lost exact invocation binding")
		}
		events, sequence, _, readErr := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: completed.Event.RunRef})
		if readErr != nil {
			t.Fatal(readErr)
		}
		found := false
		for _, event := range events {
			if event.Ref == completed.Event.Ref {
				found = event.Delta.IntegrationInvocationRef == stringMap(claim, "invocationRef")
			}
		}
		if !found {
			t.Fatal("event read lost persisted invocation binding")
		}
		var outboxBinding string
		if err := pool.QueryRow(ctx, `SELECT convert_from(payload,'UTF8')::jsonb->'data'->>'integrationInvocationRef' FROM control_plane.outbox_events WHERE ordering_key=$1 AND sequence=$2`, "run:"+completed.Event.RunRef, completed.Event.Sequence).Scan(&outboxBinding); err != nil || outboxBinding != stringMap(claim, "invocationRef") {
			t.Fatal("outbox lost exact owner binding", err)
		}
		replayed, replayErr := service.Execute(ctx, completion)
		if replayErr != nil || replayed.Event == nil || replayed.Event.Sequence != completed.Event.Sequence || replayed.Event.Delta.IntegrationInvocationRef != outboxBinding {
			t.Fatal("completion replay changed event cardinality or binding", replayErr)
		}
		_, replaySequence, _, replayReadErr := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: completed.Event.RunRef})
		if replayReadErr != nil || replaySequence != sequence {
			t.Fatal("completion replay created another event", replayReadErr)
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
	rejected, err := call("scoped-rejected", "DELETE", "rejected")
	if err != nil || stringMap(rejected, "state") != "WAITING_APPROVAL" {
		t.Fatalf("prepare rejected scoped history: %v", err)
	}
	rejection, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-history-reject", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: stringMap(rejected, "gateRef"), Decision: "REJECT"}})
	if err != nil || rejection.Gate == nil || rejection.Gate.State != "REJECTED" {
		t.Fatalf("reject scoped history: %v", err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-change-paths", ExpectedVersion: &granted.Connection.Version},
		Payload: command.IntegrationGrantInput{ApprovalPolicy: "HUMAN_SCOPED", ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: true,
			ApprovalScopePaths: []string{"/value"}}})
	if !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("active scoped grant path change was accepted: %v", err)
	}
	revoked, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-revoke", ExpectedVersion: &granted.Connection.Version},
		Payload: command.IntegrationGrantInput{ApprovalPolicy: "HUMAN_SCOPED", ConnectionRef: connection.Connection.Ref,
			CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: false}})
	if err != nil || revoked.Connection == nil {
		t.Fatalf("revoke scoped grant: %v", err)
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
	claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "scoped-gateway", 10)
	if err != nil || len(claims) != 0 {
		t.Fatalf("revoked scope still claimed effect: count=%d err=%v", len(claims), err)
	}
	next := modified
	next.Metadata.Version = "100.0.0"
	content, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	nextDigest := sha256.Sum256(content)
	next.Digest = hex.EncodeToString(nextDigest[:])
	t.Run("retired-open-guard", func(t *testing.T) {
		repository.integrationDefinitions["synthetic"] = next
		defer func() { repository.integrationDefinitions["synthetic"] = modified }()
		if _, readErr := service.GetOwnerGate(ctx, owner, stringMap(changed, "gateRef")); !errors.Is(readErr, domainerrs.ErrForbidden) {
			t.Fatal("retired OPEN package preview was accepted", readErr)
		}
		if _, _, _, listErr := service.ListOwnerGates(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, State: "OPEN"}); !errors.Is(listErr, domainerrs.ErrForbidden) {
			t.Fatal("retired OPEN catalog preview was accepted", listErr)
		}
		if _, approveErr := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "scoped-retired-open-approve", ExpectedVersion: &gateVersion},
			Payload:  command.GateResolutionInput{GateRef: stringMap(changed, "gateRef"), Decision: "APPROVE"}}); !errors.Is(approveErr, domainerrs.ErrConflict) {
			t.Fatal("retired/revoked OPEN gate approval was accepted", approveErr)
		}
	})
	currentRun, err := service.GetRun(ctx, owner, run.Run.Ref)
	if err != nil {
		t.Fatal("read current run before terminal history", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "scoped-terminal-history-cancel", ExpectedVersion: &currentRun.Version},
		Payload:  command.RunCommandInput{RunRef: currentRun.Ref}}); err != nil {
		t.Fatal("close active graph before terminal history", err)
	}
	t.Run("retired-terminal-history", func(t *testing.T) {
		// Смена поставленного пакета не переиздаёт immutable invocation pins.
		// Проверяем тот же owner read path после штатного reconciliation.
		repository.integrationDefinitions["synthetic"] = next
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if reconcileErr := repository.reconcileIntegrationDefinitions(ctx, tx); reconcileErr != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("reconcile current shipped package", reconcileErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			t.Fatal(commitErr)
		}
		readTx, beginErr := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		_, retiredErr := repository.integrationPackage(ctx, readTx, ownerScope.organizationID,
			connection.Connection.Ref, "synthetic", modified.Metadata.Version, modified.Digest)
		_ = readTx.Rollback(ctx)
		if !errors.Is(retiredErr, domainerrs.ErrForbidden) {
			t.Fatal("retired executable package was accepted", retiredErr)
		}
		listed, _, _, listErr := service.ListOwnerGates(ctx, owner, query.Filter{ProjectRef: project.Project.Ref})
		if listErr != nil || len(listed) != 4 {
			t.Fatal("retired terminal package poisoned owner gate catalog", listErr)
		}
		for _, historical := range listed {
			if historical.State == "OPEN" || len(historical.NextActions) != 0 || historical.IntegrationIntent == nil {
				t.Fatal("history gained active decision authority")
			}
			effect := historical.IntegrationIntent.EffectPreview
			fields, known := effect["fields"].([]any)
			if !known || len(fields) != 0 || effect["contentComplete"] != false || effect["approvalScope"] != nil || effect["inputDigest"] == "" {
				t.Fatal("retired schema expanded historical input")
			}
			single, singleErr := service.GetOwnerGate(ctx, owner, historical.Ref)
			if singleErr != nil || single.Ref != historical.Ref || single.State != historical.State || single.Version != historical.Version {
				t.Fatal("retired terminal single read lost owner snapshot", singleErr)
			}
		}
	})
}
