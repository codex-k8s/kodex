package platform

import (
	"context"
	_ "embed"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// personalProbeDeadlineSetUntil changes only the known synthetic original subject's view binding through the disposable owner fixture.
// personalProbeDeadlineSetUntil меняет лишь известную synthetic view binding исходного subject в disposable owner fixture.
//
//go:embed testdata/sql/personal_probe_deadline__set_until.sql
var personalProbeDeadlineSetUntil string

// personalProbeDeadlineJob reads durable admission and audit without inspecting credentials or provider payloads.
// personalProbeDeadlineJob читает durable admission и audit без credentials либо provider payloads.
//
//go:embed testdata/sql/personal_probe_deadline__job.sql
var personalProbeDeadlineJob string

// TestPersonalIntegrationCredentialProbeDeadlineComponent reuses the complete personal owner setup and stops after probe-only deadline/receipt qualification, before runtime or native assistant work.
// TestPersonalIntegrationCredentialProbeDeadlineComponent переиспользует полный personal owner setup и завершает probe-only deadline/receipt qualification до runtime либо native assistant work.
func TestPersonalIntegrationCredentialProbeDeadlineComponent(t *testing.T) {
	testPersonalCredentialComponent(t, true, true)
}

// assertPersonalProbeFiniteAllowance exercises real original-subject view ACL, version skew, claim and fresh allowance, delayed successful COMMIT and unchanged completion/replay against disposable PostgreSQL.
// assertPersonalProbeFiniteAllowance проверяет real view ACL исходного subject, version skew, claim/fresh allowance, поздний successful COMMIT и прежний completion/replay в disposable PostgreSQL.
func assertPersonalProbeFiniteAllowance(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, user value.Principal, connection entity.IntegrationConnection, binding entity.AccessBinding) entity.PersonalIntegrationCredential {
	t.Helper()
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
	workerService, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	setUntil := func(until any) {
		t.Helper()
		tag, err := repository.pool.Exec(ctx, personalProbeDeadlineSetUntil, until, worker.AuthorityTenant, binding.Ref)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("set exact original actor's view allowance", err)
		}
	}
	defer setUntil(nil)
	read := func() entity.PersonalIntegrationCredential {
		t.Helper()
		p := user
		p.Permission = "platform.personal-integrations.get"
		result, err := service.GetPersonalIntegrationCredential(ctx, p, connection.Ref)
		if err != nil {
			t.Fatal("read finite probe binding", err)
		}
		return result
	}
	for _, scenario := range []string{"lease-cap", "versions", "late-ack", "restored-before-begin", "shortened-before-begin", "late-observation"} {
		setUntil(nil)
		current := read()
		p := user
		p.Permission = "platform.personal-integrations.test"
		if _, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: p,
			Mutation: value.Mutation{IdempotencyKey: "probe-finite-" + scenario, ExpectedVersion: &current.Version},
			Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}}); err != nil {
			t.Fatal("queue exact original actor probe", scenario, err)
		}
		until := time.Now().Add(2 * time.Second).Truncate(time.Microsecond).UTC()
		if scenario == "lease-cap" {
			until = time.Now().Add(time.Minute).Truncate(time.Microsecond).UTC()
		}
		if scenario != "shortened-before-begin" {
			setUntil(until)
		}
		claims, err := workerService.ClaimPersonalIntegrationCredentialTests(ctx, worker, "probe-finite-worker", 1)
		if err != nil || len(claims) != 1 {
			t.Fatal("claim finite original actor probe", scenario, err)
		}
		probe := personalTestCompletionFromClaim(claims[0])
		dispatch, ok := claims[0]["expiresAt"].(time.Time)
		var state string
		var lease time.Time
		var admitted bool
		var audited int
		if err := repository.pool.QueryRow(ctx, personalProbeDeadlineJob, worker.AuthorityTenant, probe.TestRef).Scan(&state, &lease, &admitted, &audited); err != nil || !ok || state != "CLAIMED" || admitted || audited != 0 || !lease.After(time.Now()) || dispatch == dispatch.Round(0) {
			t.Fatal("claim corrupted original durable reconciliation lease", scenario, err)
		}
		if scenario != "shortened-before-begin" && scenario != "lease-cap" && !dispatch.Equal(until) {
			t.Fatal("claim lost original finite view deadline", scenario)
		}
		originalLease := lease
		if scenario == "lease-cap" && (!dispatch.Before(until) || time.Until(dispatch) > 30*time.Second) {
			t.Fatal("native claim lease cap lost its monotonic deadline")
		}
		if scenario == "restored-before-begin" {
			setUntil(nil)
		} else if scenario == "shortened-before-begin" {
			setUntil(until)
		}
		trace := &personalAdmissionACKTrace{admissionSQL: queryPersonalCredentialTokenProbeBegin}
		if scenario == "late-ack" {
			trace.until = until
		}
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
		client := personalRuntimeRPCClient(t, observedService, true)
		request := &cp.BeginPersonalIntegrationCredentialProbeRequest{TestRef: probe.TestRef, LeaseRef: probe.LeaseRef, Fence: probe.Fence, Generation: probe.Generation,
			AdmissionContractVersion: command.PersonalIntegrationCredentialProbeContractVersion}
		if scenario == "versions" {
			for _, version := range []uint32{0, 2} {
				request.AdmissionContractVersion = version
				denied, err := client.BeginPersonalIntegrationCredentialProbe(personalRuntimeRPCContext(ctx), request)
				if err != nil || denied.GetAdmitted() || denied.GetAuthorizedUntil() != nil {
					pool.Close()
					t.Fatal("old or unknown gateway admitted a probe", version, err)
				}
			}
			request.AdmissionContractVersion = command.PersonalIntegrationCredentialProbeContractVersion
		}
		response, err := client.BeginPersonalIntegrationCredentialProbe(personalRuntimeRPCContext(ctx), request)
		pool.Close()
		if scenario == "late-ack" {
			if trace.hits != 1 || ctx.Err() != nil || err != nil || response.GetAdmitted() || response.GetAuthorizedUntil() != nil {
				t.Fatal("expired successful probe COMMIT ACK disclosed fresh admission", trace.hits, err)
			}
		} else if err != nil || !response.GetAdmitted() || response.GetAuthorizedUntil() == nil || (scenario != "restored-before-begin" && response.GetAuthorizedUntil().AsTime().After(until)) {
			t.Fatal("finite fresh probe admission lost its deadline", scenario, err)
		}
		if scenario == "restored-before-begin" && (response.GetAuthorizedUntil() == nil || !response.GetAuthorizedUntil().AsTime().After(dispatch)) {
			t.Fatal("restored permission fixture did not exceed original dispatch ceiling")
		}
		if scenario == "shortened-before-begin" && !dispatch.After(until) {
			t.Fatal("fresh shortening fixture did not shorten the original dispatch")
		}
		if err := repository.pool.QueryRow(ctx, personalProbeDeadlineJob, worker.AuthorityTenant, probe.TestRef).Scan(&state, &lease, &admitted, &audited); err != nil || state != "PROBING" || !admitted || audited != 1 || !lease.Equal(originalLease) {
			t.Fatal("one-shot probe admission or audit was lost", scenario, err)
		}
		setUntil(nil)
		replayed, err := personalRuntimeRPCClient(t, workerService, true).BeginPersonalIntegrationCredentialProbe(personalRuntimeRPCContext(ctx), request)
		if err != nil || replayed.GetAdmitted() || replayed.GetAuthorizedUntil() != nil {
			t.Fatal("restored allowance replayed a consumed probe", scenario, err)
		}
		if scenario == "late-observation" {
			setUntil(until)
			time.Sleep(time.Until(until) + 20*time.Millisecond)
		}
		probe.Outcome = entity.PersonalIntegrationCredentialAuthenticated
		p = worker
		p.Permission = "platform.personal-integration-tests.complete"
		input := command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: p, Mutation: value.Mutation{IdempotencyKey: "probe-finite-complete-" + scenario}, Payload: probe}
		completed, err := workerService.Execute(ctx, input)
		if err != nil || completed.PersonalIntegrationCredentialTest == nil || (scenario == "late-observation" && (completed.PersonalIntegrationCredentialTest.State != "CANCELLED" || completed.PersonalIntegrationCredentialTest.Applied)) {
			t.Fatal("finite dispatch changed the existing completion lifecycle", scenario, err)
		}
		setUntil(nil)
		if _, err := workerService.Execute(ctx, input); err != nil {
			t.Fatal("finite probe completion lost its original receipt", scenario, err)
		}
	}
	return read()
}
