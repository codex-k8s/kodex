package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// systemGrantProjectionCounter counts exact full-connection/grant query projections atomically
// through the pgx tracer. It stores no query text, arguments or results and does not count
// unrelated SQL or prove all read costs.
// systemGrantProjectionCounter атомарно считает точные full-connection/grant query projections
// через pgx tracer. Не сохраняет query text, arguments или results и не считает прочие SQL либо
// полную стоимость чтения.
type systemGrantProjectionCounter struct{ fullReads atomic.Int64 }

// TraceQueryStart increments the counter only for the two exact full projection query strings and
// returns the original context. It records no request data and does not modify query execution or
// authorization.
// TraceQueryStart увеличивает counter только для двух точных строк full projection queries и
// возвращает исходный context. Не сохраняет request data и не меняет query execution или
// authorization.
func (counter *systemGrantProjectionCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef || data.SQL == queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef {
		counter.fullReads.Add(1)
	}
	return ctx
}

// TraceQueryEnd implements the required tracer completion callback without retaining rows, errors,
// arguments or timing. Full-read counting occurs only at TraceQueryStart.
// TraceQueryEnd реализует обязательный tracer completion callback без хранения rows, errors,
// arguments или timing. Full-read counting выполняется только в TraceQueryStart.
func (*systemGrantProjectionCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

// TestSystemAssistantGrantHeaderClosed checks the exact synthetic header and rejects mismatched
// ref, missing name/version/definition key/revision, malformed digest and an unbound requested
// ref. It tests the lexical/header predicate only, not database eligibility or live package
// provenance.
// TestSystemAssistantGrantHeaderClosed проверяет точный synthetic header и отклоняет несовпадающий
// ref, отсутствие name/version/definition key/revision, malformed digest и несвязанный requested
// ref. Проверяет только lexical/header predicate, а не database eligibility или live package
// provenance.
func TestSystemAssistantGrantHeaderClosed(t *testing.T) {
	header := systemAssistantGrantConnectionHeader{ref: "int_owned", name: "Owned", version: 1, definitionKey: "github", definitionVersion: "6.0.0", definitionDigest: strings.Repeat("a", 64)}
	if !validSystemAssistantGrantConnectionHeader(header, header.ref) {
		t.Fatal("exact header rejected")
	}
	for _, field := range []string{"ref", "name", "version", "key", "revision", "digest"} {
		t.Run(field, func(t *testing.T) {
			bad := header
			switch field {
			case "ref":
				bad.ref = "int_foreign"
			case "name":
				bad.name = ""
			case "version":
				bad.version = 0
			case "key":
				bad.definitionKey = ""
			case "revision":
				bad.definitionVersion = ""
			case "digest":
				bad.definitionDigest = "PRIVATE_SENTINEL"
			}
			if validSystemAssistantGrantConnectionHeader(bad, header.ref) {
				t.Fatal("invalid header accepted")
			}
		})
	}
	if validSystemAssistantGrantConnectionHeader(header, "") {
		t.Fatal("unbound header accepted")
	}
}

// TestSystemAssistantGrantHeaderBatchComponent proposes and atomically applies 21 SYSTEM READ
// grants while the tracer requires zero full connection projections during proposal. It preserves
// immutable owner tuples, exact versions and one replay receipt, rejects caller owner pins,
// signed-project/foreign scope and revoked-owner replay. PostgreSQL, warm runtime and integration
// credentials are disposable/synthetic; timing is observational and not a deployed performance
// acceptance.
// TestSystemAssistantGrantHeaderBatchComponent предлагает и атомарно применяет 21 SYSTEM READ
// grant, пока tracer требует ноль full connection projections при proposal. Сохраняет immutable
// owner tuples, exact versions и один replay receipt, отклоняет caller owner pins,
// signed-project/foreign scope и revoked-owner replay. PostgreSQL, warm runtime и integration
// credentials disposable/synthetic; timing является наблюдением, а не развёрнутой performance
// acceptance.
func TestSystemAssistantGrantHeaderBatchComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated SYSTEM header fixture")
	}
	counter := &systemGrantProjectionCounter{}
	config.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open isolated SYSTEM header fixture")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	prepareObservedWarmFixture(t, ctx, r)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner, worker := resolveSystemGrantComponentPrincipals(t, ctx, r)
	s, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := s.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := principal("runtime-controller", "platform.runtime.warm.report", "kodex-system-subject", "kodex-installation")
	if _, err = s.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := s.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "header-system-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("synthetic command %s failed: %v", kind, err)
		}
		return result
	}
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Header repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	conversation := execute(command.CreateAssistantConversation, owner, "conversation", nil, command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
	execute(command.AddAssistantTurn, owner, "turn", nil, command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Prepare exact SYSTEM read grants", DeliveryMode: "QUEUE"})
	leases := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "header-system-worker", Limit: 1}).RuntimeItems
	if len(leases) != 1 {
		t.Fatal("missing exact synthetic lease")
	}
	lease := leases[0]
	candidates, err := s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "", query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	ops := []entity.AssistantPlanOperation{}
	for _, item := range candidates.Items {
		if item.Capability.Risk == "READ" && item.Capability.ApprovalPolicy == "NONE" && len(ops) < 21 {
			ops = append(ops, entity.AssistantPlanOperation{Key: fmt.Sprintf("read%d", len(ops)), Type: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", Title: "SYSTEM read grant", Summary: "Confirm exact read policy", Parameters: map[string]any{"connectionRef": connection.Ref, "capabilityKey": item.Capability.Key, "enabled": true, "approvalPolicy": "NONE"}})
		}
	}
	if len(ops) != 21 {
		t.Fatal("fixture needs21 approved READ capabilities")
	}
	propose := func(key string, operations []entity.AssistantPlanOperation) (command.Result, error) {
		request, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return s.Execute(request, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "header-system-" + key}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: "Confirm21 SYSTEM read capabilities", Operations: operations}})
	}
	bad := ops[0]
	bad.Parameters = cloneAssistantFields(bad.Parameters)
	bad.Parameters["connectionRef"] = "int_missing_fixture"
	if _, err = propose("missing", []entity.AssistantPlanOperation{bad}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("missing connection accepted")
	}
	bad.Parameters["connectionRef"] = connection.Ref
	bad.Parameters["systemAssistantRef"] = "agt_caller"
	if _, err = propose("caller-owner", []entity.AssistantPlanOperation{bad}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("caller SYSTEM owner accepted")
	}
	counter.fullReads.Store(0)
	started := time.Now()
	prepared, err := propose("prepare", ops)
	if err != nil || prepared.Plan == nil || len(prepared.Plan.Operations) != 21 || counter.fullReads.Load() != 0 {
		t.Fatal("SYSTEM21 proposal used full projection or failed")
	}
	t.Logf("operations=21 full_connection_projection_reads=%d elapsed_ms=%d", counter.fullReads.Load(), time.Since(started).Milliseconds())
	for _, op := range prepared.Plan.Operations {
		if op.Target.Ref != connection.Ref || assistantString(op.Parameters, "systemAssistantRef") != assistant.Ref || assistantString(op.Parameters, "scopeKind") != "ORGANIZATION" || assistantString(op.Parameters, "organizationRef") == "" || assistantString(op.Parameters, "grantRef") != "" || op.Before["enabled"] != false || op.After["enabled"] != true {
			t.Fatal("SYSTEM owner/before/after tuple changed")
		}
		if _, exists := op.Parameters["projectRef"]; exists {
			t.Fatal("SYSTEM header invented PROJECT authority")
		}
	}
	plan := execute(command.ValidateAssistantPlan, owner, "validate", &prepared.Plan.Version, command.AssistantPlanInput{PlanRef: prepared.Plan.Ref, Revision: prepared.Plan.Revision}).Plan
	if plan == nil || plan.State != "VALID" {
		t.Fatal("SYSTEM21 plan not VALID")
	}
	immutable, _ := json.Marshal(plan.Operations)
	applied := execute(command.ApplyAssistantPlan, owner, "apply", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
	if applied.Plan == nil || applied.Plan.State != "APPLIED" || applied.PlanReceipt == nil || len(applied.PlanReceipt.Operations) != 21 || !assistantJSONEqual(json.RawMessage(immutable), applied.Plan.Operations) {
		t.Fatal("SYSTEM21 snapshots changed or partial apply")
	}
	replay := execute(command.ApplyAssistantPlan, owner, "apply", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
	if replay.PlanReceipt == nil || replay.PlanReceipt.Ref != applied.PlanReceipt.Ref {
		t.Fatal("SYSTEM21 replay duplicated receipt")
	}
	after, err := s.GetSystemAssistantIntegrationGrantCandidates(ctx, owner, connection.Ref, "", query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	enabled := 0
	for _, item := range after.Items {
		if item.CurrentGrantEnabled {
			enabled++
			if item.CurrentApprovalPolicy != "NONE" || item.CurrentGrantVersion != 1 {
				t.Fatal("SYSTEM21 exact grant snapshot changed")
			}
		}
	}
	if enabled != 21 || after.ConnectionVersion != candidates.ConnectionVersion+21 {
		t.Fatal("SYSTEM21 effect cardinality changed")
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	signed := current
	signed.authorityProjectID = "90000000-0000-4000-8000-000000000099"
	if _, err = r.systemAssistantGrantHeader(ctx, tx, signed, connection.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("signed PROJECT read SYSTEM header")
	}
	foreign := current
	foreign.organizationID = "90000000-0000-4000-8000-000000000099"
	if _, err = r.systemAssistantGrantHeader(ctx, tx, foreign, connection.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign tenant read SYSTEM header")
	}
	_ = tx.Rollback(ctx)
	if _, err = pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
		t.Fatal("revoke synthetic SYSTEM owner")
	}
	if _, err = s.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "header-system-apply", ExpectedVersion: &plan.Version}, Payload: command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}}); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("revoked SYSTEM owner replayed receipt")
	}
}
