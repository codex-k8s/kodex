package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// personalAdmissionACKTrace delays only a successful native admission COMMIT acknowledgement; neither SQL execution nor its durable outcome is simulated.
// personalAdmissionACKTrace задерживает только успешный ACK native COMMIT допуска; SQL и его durable outcome не имитируются.
type personalAdmissionACKTrace struct {
	until        time.Time
	admissionSQL string
	armed        bool
	hits         int
}

// personalAdmissionTraceKey separates the exact admission and commit observations from unrelated pool traffic.
// personalAdmissionTraceKey отделяет exact admission и commit от прочих pool queries.
type personalAdmissionTraceKey struct{}

// TraceQueryStart marks only the actual production admission INSERT and native COMMIT.
// TraceQueryStart отмечает лишь production admission INSERT и native COMMIT.
func (trace *personalAdmissionACKTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	phase := 0
	admission := trace.admissionSQL
	if admission == "" {
		admission = queryPersonalInvocationAdmit
	}
	if data.SQL == admission {
		phase = 1
	} else if strings.EqualFold(data.SQL, "commit") {
		phase = 2
	}
	return context.WithValue(ctx, personalAdmissionTraceKey{}, phase)
}

// TraceQueryEnd preserves the real successful transaction before delaying its acknowledgement beyond the finite allowance.
// TraceQueryEnd сохраняет реальную успешную транзакцию до задержки её ACK за конечный allowance.
func (trace *personalAdmissionACKTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		return
	}
	phase, _ := ctx.Value(personalAdmissionTraceKey{}).(int)
	if phase == 1 {
		trace.armed = true
	}
	if phase == 2 && trace.armed {
		trace.armed = false
		trace.hits++
		time.Sleep(time.Until(trace.until) + 20*time.Millisecond)
	}
}

// assertPersonalInvocationFiniteAllowance covers the real original actor, claim envelope, late successful COMMIT ACK, grant restoration and durable completion without any provider request.
// assertPersonalInvocationFiniteAllowance проверяет реального исходного actor, claim envelope, late COMMIT ACK, восстановление grant и durable completion без provider requests.
func assertPersonalInvocationFiniteAllowance(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, worker, gateway value.Principal, runRef, nodeRef, connectionRef string) {
	t.Helper()
	setUntil := func(until any) {
		t.Helper()
		tag, err := repository.pool.Exec(ctx, `UPDATE control_plane.access_bindings binding SET valid_until=$1
 FROM control_plane.application_role_versions role_version,control_plane.subjects subject
 WHERE binding.role_version_id=role_version.id AND binding.subject_id=subject.id
 AND subject.ref='usr_personal_second' AND binding.organization_id=subject.organization_id
 AND binding.organization_id=$2::uuid AND binding.state='ACTIVE'
 AND role_version.permission_keys @> ARRAY['integration.manage']::text[]`, until, worker.AuthorityTenant)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("set only second actor's exact synthetic manage allowance", err)
		}
	}
	defer setUntil(nil)
	workerService, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"late-ack", "late-effect-receipt", "restored-before-begin", "shortened-before-begin"} {
		until := time.Now().Add(3 * time.Second).Truncate(time.Microsecond).UTC()
		setUntil(until)
		if scenario == "shortened-before-begin" {
			setUntil(nil)
		}
		invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": runRef, "node_ref": nodeRef, "connection_ref": connectionRef, "capability_key": "gitlab.project.metadata.read", "idempotency_key": "personal-finite-" + scenario}, map[string]any{})
		if err != nil {
			t.Fatal("resolve finite-allowance personal intent", scenario, err)
		}
		claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-runtime-gateway", 1)
		if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(invocation, "invocationRef") {
			t.Fatal("claim finite personal intent", scenario, err)
		}
		work := claims[0]
		dispatchUntil, ok := work["expiresAt"].(time.Time)
		if !ok {
			t.Fatal("missing dispatch envelope deadline")
		}
		var ownerLease time.Time
		if err := repository.pool.QueryRow(ctx, "SELECT lease_expires_at FROM control_plane.integration_invocations WHERE ref=$1", stringMap(work, "invocationRef")).Scan(&ownerLease); err != nil {
			t.Fatal(err)
		}
		trace := &personalAdmissionACKTrace{until: until}
		config := repository.pool.Config()
		config.ConnConfig.Tracer = trace
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		traced := *repository
		traced.pool = pool
		observedService, err := platformservice.New(&traced)
		if err != nil {
			pool.Close()
			t.Fatal(err)
		}
		request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), WorkloadInstance: "personal-runtime-gateway"}
		if scenario != "late-ack" {
			trace.until = time.Time{}
		}
		if scenario == "restored-before-begin" {
			setUntil(nil)
		}
		if scenario == "shortened-before-begin" {
			setUntil(until)
		}
		response, err := personalRuntimeRPCClient(t, observedService).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
		pool.Close()
		if scenario == "late-ack" {
			if trace.hits != 1 || ctx.Err() != nil || err != nil || response.GetAdmitted() || response.GetCredentialRevision() != nil || response.GetAuthorizedUntil() != nil {
				t.Fatal("expired successful COMMIT ACK disclosed personal credential", trace.hits, err)
			}
			var admissions int
			if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.integration_personal_invocation_admissions WHERE invocation_id=(SELECT id FROM control_plane.integration_invocations WHERE ref=$1)", request.InvocationRef).Scan(&admissions); err != nil || admissions != 1 {
				t.Fatal("late ACK erased committed one-shot admission", err)
			}
			setUntil(nil)
			repeated, err := personalRuntimeRPCClient(t, workerService).BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
			if err != nil || repeated.GetAdmitted() || repeated.GetCredentialRevision() != nil {
				t.Fatal("restored grant replayed consumed admission", err)
			}
		} else {
			if err != nil || !response.GetAdmitted() || response.GetAuthorizedUntil() == nil {
				t.Fatal("fresh finite personal admission failed", scenario, err)
			}
			if scenario == "restored-before-begin" {
				if !dispatchUntil.Equal(until) || !response.GetAuthorizedUntil().AsTime().After(dispatchUntil) {
					t.Fatal("restoration lost original conservative claim envelope")
				}
			} else if scenario == "shortened-before-begin" {
				if !dispatchUntil.After(until) || response.GetAuthorizedUntil().AsTime().After(until) {
					t.Fatal("fresh shortened ACL was lost after claim")
				}
			} else {
				if response.GetAuthorizedUntil().AsTime().After(until) {
					t.Fatal("Begin extended finite allowance")
				}
				time.Sleep(time.Until(until) + 20*time.Millisecond)
				summary := `{"id":2,"path_with_namespace":"fixture/second-project","default_branch":"main","visibility":"private","archived":false}`
				if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "personal-finite-complete"}, Payload: command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, Success: true, EffectKey: stringMap(work, "effectKey"), InputDigest: stringMap(work, "inputDigest"), ProviderEffectRef: "gitlab-project:2", ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}); err != nil {
					t.Fatal("finite dispatch expiry discarded an admitted durable effect", err)
				}
			}
		}
		if (scenario != "shortened-before-begin" && !dispatchUntil.Equal(until)) || !ownerLease.After(until) {
			t.Fatal("personal dispatch allowance replaced reconciliation lease", scenario)
		}
	}
}

// TestPersonalIntegrationInvocationDeadlineComponent runs the unchanged personal credential setup and invocation owner assertions without claiming assistant/process resume acceptance.
// TestPersonalIntegrationInvocationDeadlineComponent выполняет прежний personal credential setup и invocation owner assertions, не заявляя приёмку assistant/process resume.
func TestPersonalIntegrationInvocationDeadlineComponent(t *testing.T) {
	testPersonalCredentialComponent(t, true)
}
