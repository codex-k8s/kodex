package platform

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// authenticateKubernetesInvocationFixture uses the actual owner health transitions with synthetic
// broker authentication metadata. It proves no live TLS, signed policy108 or target RBAC admission.
// authenticateKubernetesInvocationFixture использует actual owner health transitions с synthetic
// broker authentication metadata. Не доказывает live TLS, signed policy108 либо target RBAC admission.
func authenticateKubernetesInvocationFixture(t *testing.T, ctx context.Context, fixture kubernetesSetupOwnerFixture, service *platformservice.Service, connection entity.IntegrationConnection, binding entity.PersonalIntegrationCredential) entity.PersonalIntegrationCredential {
	t.Helper()
	owner := fixture.transport
	owner.Permission = "platform.personal-integrations.test"
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-invocation-health-" + strconv.FormatInt(binding.Version, 10), ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}}); err != nil {
		t.Fatal("queue invocation identity health", err)
	}
	worker := fixture.principal
	worker.CallerWorkload, worker.Permission = "integration-gateway", "platform.personal-integration-tests.claim"
	claims, err := fixture.repository.ClaimPersonalIntegrationCredentialTests(ctx, worker, "kube-invocation-health", 1)
	if err != nil || len(claims) != 1 {
		t.Fatal("claim invocation identity health", err)
	}
	claim := claims[0]
	lease := value.ManagedKubernetesBinding{WorkloadInstance: "kube-invocation-health", ConnectionTestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	broker := fixture.principal
	broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.health.begin"
	admission, err := fixture.repository.BeginManagedKubernetesHealthProbe(ctx, broker, lease)
	if err != nil {
		t.Fatal("admit invocation identity health", err)
	}
	broker.Permission = "platform.runtime.kubernetes.health.complete"
	result, err := fixture.repository.CompleteManagedKubernetesHealthProbe(ctx, broker, lease, command.KubernetesIdentityHealthCompletion{AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest, Outcome: entity.PersonalIntegrationCredentialAuthenticated, ObservedSubjectDigest: strings.Repeat("a", 64)})
	if err != nil || !result.Applied {
		t.Fatal("complete invocation identity health", err)
	}
	owner.Permission = "platform.personal-integrations.get"
	current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || current.State != "READY" {
		t.Fatal("read authenticated invocation source", err)
	}
	return current
}

// TestKubernetesInvocationOwnerComponent qualifies actual launch/RR→intent→dedicated claim/renew
// and no-replay expiry. Public grant mutation is real; cloned queue metadata is fixture-only.
// Actual broker effects, signed policy108 and stream transport are not claimed by this test.
// TestKubernetesInvocationOwnerComponent проверяет actual launch/RR→intent→dedicated claim/renew
// и no-replay expiry. Public grant mutation настоящий; cloned queue metadata служит fixture.
// Actual broker effects, signed policy108 и stream transport этим test не подтверждаются.
func TestKubernetesInvocationOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancel()
	fixture, service, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
	binding = authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, binding)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	owner := fixture.transport
	execute := newComponentCommandExecutor(t, ctx, service, "kube-invocation-")
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Disposable Kubernetes invocation", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "kube-invocation-agent", "Kubernetes reader")
	connection = *execute(command.ChangeIntegrationGrant, owner, "grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.identity", ApprovalPolicy: "NONE", Enabled: true, KubernetesNamespace: "@cluster"}).Connection
	connection = *execute(command.ChangeIntegrationGrant, owner, "logs-grant", &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.logs", ApprovalPolicy: "NONE", Enabled: true, KubernetesNamespace: "review"}).Connection
	run := execute(command.LaunchRun, owner, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Observe selected synthetic Kubernetes identity"}).Run
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-invocation-runtime", Limit: 1}).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
		t.Fatal("claim actual Kubernetes runtime revision")
	}
	invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(leases[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "kubernetes.identity", "idempotency_key": "kube-invocation-read"}, map[string]any{})
	if err != nil || stringMap(invocation, "state") != "READY" {
		t.Fatal("resolve exact Kubernetes owner invocation", err)
	}
	var originalHealth string
	if err := repository.pool.QueryRow(ctx, `SELECT pin.health_attempt_id::text FROM control_plane.kubernetes_invocation_identity_bindings pin JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef")).Scan(&originalHealth); err != nil {
		t.Fatal("read original immutable invocation health", err)
	}
	binding = authenticateKubernetesInvocationFixture(t, ctx, fixture, service, connection, binding)
	var latestHealth string
	if err := repository.pool.QueryRow(ctx, `SELECT attempt.id::text FROM control_plane.kubernetes_identity_health_attempts attempt JOIN control_plane.kubernetes_identity_health_completions completion ON completion.attempt_id=attempt.id WHERE attempt.organization_id=$1::uuid ORDER BY completion.created_at DESC LIMIT 1`, fixture.current.organizationID).Scan(&latestHealth); err != nil || latestHealth == originalHealth {
		t.Fatal("recheck fixture did not create a fresh health observation", err)
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
	ordinary, err := service.ClaimIntegrationInvocations(ctx, gateway, "kube-ordinary-refused", 1)
	if err != nil || len(ordinary) != 0 {
		t.Fatal("ordinary worker claimed a Kubernetes invocation", err)
	}
	resolvedGateway, err := repository.ResolvePrincipal(ctx, gateway)
	if err != nil {
		t.Fatal("resolve Kubernetes gateway", err)
	}
	resolvedGateway.Permission = "platform.runtime.kubernetes.claim"
	claim := assertKubernetesClaimFairness(t, ctx, repository, resolvedGateway, stringMap(invocation, "invocationRef"))
	var retainedHealth string
	if err := repository.pool.QueryRow(ctx, `SELECT pin.health_attempt_id::text FROM control_plane.kubernetes_invocation_identity_bindings pin JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef")).Scan(&retainedHealth); err != nil || retainedHealth != originalHealth {
		t.Fatal("healthy recheck replaced original invocation proof", err)
	}
	if claim["credential"] != nil || claim["credentialAdmissionRequired"] != false || claim["resourcePermissionProfile"] != integrationpackage.CredentialResourcePermissionKubernetesScopedV1 {
		t.Fatal("typed lane leaked TOKEN or omitted resource checks")
	}
	lease := value.ManagedKubernetesBinding{WorkloadInstance: "kube-dedicated-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
	broker := fixture.principal
	broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.resolve"
	privateSnapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, lease)
	if err != nil || privateSnapshot.IdentityRef != binding.BrokerIdentity.Ref || privateSnapshot.IdentityLocator.IdentityRef != privateSnapshot.IdentityRef ||
		privateSnapshot.Operation != "kubernetes.identity" || privateSnapshot.SubjectDigest != strings.Repeat("a", 64) || privateSnapshot.Resource != (value.ManagedKubernetesResourceAddress{}) || privateSnapshot.Discovery != nil || privateSnapshot.NodeTransport != nil {
		t.Fatal("private owner snapshot changed source or invented an effect address", err)
	}
	resolvedGateway.Permission = "platform.runtime.kubernetes.renew"
	for _, query := range []string{queryKubernetesInvocationLease, queryKubernetesInvocationRenew} {
		var before, after string
		if repository.pool.QueryRow(ctx, "SELECT md5(to_jsonb(invocation)::text) FROM control_plane.integration_invocations invocation WHERE ref=$1", lease.InvocationRef).Scan(&before) != nil {
			t.Fatal("capture native invocation before renewal fault")
		}
		faultRepository, fault := kubernetesAuthorityFaultRepository(t, ctx, repository, query, pgtype.TimestamptzOID)
		if _, err := faultRepository.RenewManagedKubernetesInvocation(ctx, resolvedGateway, lease); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
			t.Fatal("native renewal Scan failure became authority denial", err)
		}
		if repository.pool.QueryRow(ctx, "SELECT md5(to_jsonb(invocation)::text) FROM control_plane.integration_invocations invocation WHERE ref=$1", lease.InvocationRef).Scan(&after) != nil || after != before {
			t.Fatal("native renewal failure changed lease or invocation state")
		}
	}
	wrong := lease
	wrong.WorkloadInstance = "foreign-gateway"
	if _, err := repository.RenewManagedKubernetesInvocation(ctx, resolvedGateway, wrong); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign instance renewed Kubernetes credit", err)
	}
	gatewayRenew := gateway
	gatewayRenew.Permission = ""
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName {
			if gatewayRenew.Permission != "" {
				t.Fatal("duplicate production gateway renewal mapping")
			}
			gatewayRenew.Permission = operation
		}
	}
	if gatewayRenew.Permission == "" {
		t.Fatal("renewal missing from production gateway registry")
	}
	renewed, err := service.RenewManagedKubernetesInvocation(ctx, gatewayRenew, lease)
	if err != nil || renewed.Ref != lease.LeaseRef || renewed.Generation != lease.Generation {
		t.Fatal("renew exact current Kubernetes credit", err)
	}
	rejoined, err := repository.ResolveManagedKubernetesOperation(ctx, broker, lease)
	if err != nil || rejoined.Digest != privateSnapshot.Digest || rejoined.IdentityLocator != privateSnapshot.IdentityLocator || !rejoined.ExpiresAt.After(privateSnapshot.ExpiresAt) {
		t.Fatal("owner renewal replaced immutable operation snapshot or source", err)
	}
	assertKubernetesActiveStreamReadback(t, ctx, repository, broker, lease, privateSnapshot.Digest)
	completion := command.IntegrationInvocationInput{InvocationRef: lease.InvocationRef, LeaseRef: lease.LeaseRef, Fence: lease.Fence, Generation: lease.Generation, SafeErrorCode: "INTEGRATION_OUTCOME_UNKNOWN", UnknownOutcome: true}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "kube-ordinary-completion-refused"}, Payload: completion}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("ordinary completion bypassed Kubernetes effect graph", err)
	}
	if _, err := repository.pool.Exec(ctx, "UPDATE control_plane.integration_invocations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1", lease.InvocationRef); err != nil {
		t.Fatal("expire exact synthetic lease", err)
	}
	if _, err := repository.RenewManagedKubernetesInvocation(ctx, resolvedGateway, lease); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("expired Kubernetes credit was revived", err)
	}
	if _, err := repository.ResolveManagedKubernetesOperation(ctx, broker, lease); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("expired outer credit obtained a private operation snapshot", err)
	}
	resolvedGateway.Permission = "platform.runtime.kubernetes.claim"
	repeated, err := repository.ClaimManagedKubernetesInvocations(ctx, resolvedGateway, "kube-after-expiry", 1)
	if err != nil || len(repeated) != 0 {
		t.Fatal("Kubernetes expiry reissued an attempt", err)
	}
	var state string
	var generation int64
	if repository.pool.QueryRow(ctx, "SELECT state,generation FROM control_plane.integration_invocations WHERE ref=$1", lease.InvocationRef).Scan(&state, &generation) != nil || state != "UNKNOWN_OUTCOME" || generation != 1 {
		t.Fatal("expired Kubernetes read lost its one-attempt unknown outcome")
	}
}

// assertKubernetesClaimFairness covers a full stale prefix, identical timestamps, a durable cursor
// across a new repository instance, concurrent claims, wrap and a valid insertion before the cursor.
// Metadata-only cloned intents model historical owner rows; no clone executes an external request.
// assertKubernetesClaimFairness проверяет полный stale prefix, identical timestamps, durable cursor
// через новый repository instance, concurrent claims, wrap и valid insertion перед cursor.
// Metadata-only cloned intents моделируют historical owner rows; внешние requests не исполняются.
func assertKubernetesClaimFairness(t *testing.T, ctx context.Context, repository *Repository, gateway value.Principal, originalRef string) map[string]any {
	t.Helper()
	current, scopeErr := repository.resolveScope(ctx, gateway)
	if scopeErr != nil {
		t.Fatal("resolve internal queue tenant", scopeErr)
	}
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_invocations
 SELECT (jsonb_populate_record(NULL::control_plane.integration_invocations,to_jsonb(original)||jsonb_build_object(
 'id',gen_random_uuid(),'ref','inv_kubeprefix'||lpad(index::text,4,'0'),'idempotency_key','kube-prefix-'||index,
 'effect_key','kube-prefix-effect-'||index,'grant_version',original.grant_version+1,'created_at',original.created_at-interval '1 second'))).*
 FROM control_plane.integration_invocations original CROSS JOIN generate_series(0,127) index WHERE original.ref=$1`, originalRef); err != nil {
		t.Fatal("seed bounded obsolete invocation prefix", err)
	}
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.kubernetes_invocation_identity_bindings
 SELECT (jsonb_populate_record(NULL::control_plane.kubernetes_invocation_identity_bindings,to_jsonb(pin)||jsonb_build_object('invocation_id',prefix.id))).*
 FROM control_plane.kubernetes_invocation_identity_bindings pin
 JOIN control_plane.integration_invocations original ON original.id=pin.invocation_id
 JOIN control_plane.integration_invocations prefix ON prefix.ref LIKE 'inv_kubeprefix%' AND right(prefix.ref,4)::integer<64
 WHERE original.ref=$1`, originalRef); err != nil {
		t.Fatal("pin known old prefix source", err)
	}
	config := repository.pool.Config()
	config.ConnConfig.Tracer = kubernetesInvocationQueryTrace{t: t}
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated query trace pool", err)
	}
	t.Cleanup(tracedPool.Close)
	claimant, err := New(tracedPool, "openai-codex", "gpt-5", repository.objects)
	if err != nil {
		t.Fatal("construct isolated traced claimant", err)
	}
	var cursor string
	for page := 0; page < 4; page++ {
		if page > 0 {
			claimant, err = New(tracedPool, "openai-codex", "gpt-5", repository.objects)
			if err != nil {
				t.Fatal("restart claimant between bounded pages", err)
			}
		}
		first, scanErr := claimant.ClaimManagedKubernetesInvocations(ctx, gateway, "kube-dedicated-gateway", 1)
		if scanErr != nil || len(first) != 0 {
			t.Fatal("bounded scan did not retain denied prefix", page, scanErr)
		}
		expected := fmt.Sprintf("inv_kubeprefix%04d", (page+1)*32-1)
		if repository.pool.QueryRow(ctx, "SELECT after_ref FROM control_plane.kubernetes_invocation_claim_cursors WHERE organization_id=$1::uuid", current.organizationID).Scan(&cursor) != nil || cursor != expected {
			t.Fatal("bounded poll did not persist exact timestamp/ref progress", page, cursor)
		}
	}

	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_invocations
 SELECT (jsonb_populate_record(NULL::control_plane.integration_invocations,to_jsonb(original)||jsonb_build_object(
 'id',gen_random_uuid(),'ref','inv_kubenewbeforecursor','idempotency_key','kube-before-cursor',
 'effect_key','kube-before-effect','created_at',original.created_at-interval '2 seconds'))).*
 FROM control_plane.integration_invocations original WHERE original.ref=$1`, originalRef); err != nil {
		t.Fatal("insert valid older discovery position", err)
	}
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.kubernetes_invocation_identity_bindings
 SELECT (jsonb_populate_record(NULL::control_plane.kubernetes_invocation_identity_bindings,to_jsonb(pin)||jsonb_build_object('invocation_id',earlier.id))).*
 FROM control_plane.kubernetes_invocation_identity_bindings pin
 JOIN control_plane.integration_invocations original ON original.id=pin.invocation_id
 JOIN control_plane.integration_invocations earlier ON earlier.ref='inv_kubenewbeforecursor'
 WHERE original.ref=$1`, originalRef); err != nil {
		t.Fatal("pin earlier valid invocation", err)
	}
	restarted, err := New(repository.pool, "openai-codex", "gpt-5", repository.objects)
	if err != nil {
		t.Fatal("construct restarted claimant", err)
	}
	// claimResult joins both actual concurrent claim transactions before any assertion.
	// claimResult завершает обе actual concurrent claim transactions до assertions.
	type result struct {
		items []map[string]any
		err   error
	}
	results := make(chan result, 2)
	var joined sync.WaitGroup
	for range 2 {
		joined.Go(func() {
			items, err := restarted.ClaimManagedKubernetesInvocations(ctx, gateway, "kube-dedicated-gateway", 1)
			results <- result{items, err}
		})
	}
	joined.Wait()
	close(results)
	found := map[string]map[string]any{}
	consume := func(items []map[string]any, err error) {
		t.Helper()
		if err != nil && !errors.Is(err, errs.ErrUnavailable) {
			t.Fatal("concurrent claimant returned unclassified result", err)
		}
		for _, item := range items {
			ref := stringMap(item, "invocationRef")
			if ref != originalRef && ref != "inv_kubenewbeforecursor" || found[ref] != nil {
				t.Fatal("stale candidate or duplicate credit claimed")
			}
			found[ref] = item
		}
	}
	for result := range results {
		consume(result.items, result.err)
	}
	for attempt := 0; attempt < 5 && len(found) < 2; attempt++ {
		items, err := restarted.ClaimManagedKubernetesInvocations(ctx, gateway, "kube-dedicated-gateway", 1)
		consume(items, err)
	}
	if len(found) != 2 {
		t.Fatal("persisted cursor starved a valid newer or earlier invocation")
	}
	var failed, unattempted int
	if repository.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='FAILED' AND generation=0 AND lease_ref IS NULL),count(*) FILTER(WHERE state='READY' AND generation=0 AND lease_ref IS NULL) FROM control_plane.integration_invocations WHERE ref LIKE 'inv_kubeprefix%'`).Scan(&failed, &unattempted) != nil || failed != 64 || unattempted != 64 {
		t.Fatal("source denial or canonical denial granted credit or lost closed outcome")
	}
	wrapped := false
	for poll := 0; poll < 4; poll++ {
		items, pollErr := restarted.ClaimManagedKubernetesInvocations(ctx, gateway, "kube-dedicated-gateway", 1)
		if pollErr != nil || len(items) != 0 {
			t.Fatal("cursor wrap replayed already claimed work", pollErr)
		}
		if repository.pool.QueryRow(ctx, "SELECT after_ref FROM control_plane.kubernetes_invocation_claim_cursors WHERE organization_id=$1::uuid", current.organizationID).Scan(&cursor) != nil {
			t.Fatal("read bounded wrap progress")
		}
		if cursor == "" {
			wrapped = true
			break
		}
	}
	if !wrapped {
		t.Fatal("fair discovery did not wrap at bounded EOF")
	}

	return found[originalRef]
}

// kubernetesInvocationTraceKey carries only an embedded query name, never SQL arguments.
// kubernetesInvocationTraceKey хранит лишь embedded query name, не SQL arguments.
type kubernetesInvocationTraceKey struct{}

// kubernetesInvocationQueryTrace exposes fixed query/error class diagnostics for native failures.
// It deliberately excludes PostgreSQL messages/details, arguments, SQL bodies and private metadata.
// kubernetesInvocationQueryTrace выдаёт fixed query/error class diagnostics native failures.
// Намеренно исключает PostgreSQL messages/details, arguments, SQL bodies и private metadata.
type kubernetesInvocationQueryTrace struct{ t *testing.T }

// TraceQueryStart carries just the first owned SQL name marker to the completion observer.
// TraceQueryStart передаёт лишь первый owned SQL name marker completion observer.
func (trace kubernetesInvocationQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	name := "TRANSACTION"
	if strings.HasPrefix(data.SQL, "-- name: ") {
		name = strings.SplitN(strings.TrimPrefix(data.SQL, "-- name: "), " ", 2)[0]
	}
	return context.WithValue(ctx, kubernetesInvocationTraceKey{}, name)
}

// TraceQueryEnd logs no data-bearing error text when a native query fails.
// TraceQueryEnd не выводит data-bearing error text при native query failure.
func (trace kubernetesInvocationQueryTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil || errors.Is(data.Err, pgx.ErrNoRows) {
		return
	}
	var database *pgconn.PgError
	if errors.As(data.Err, &database) {
		trace.t.Logf("owner query %s failed with PostgreSQL code %s at %d", ctx.Value(kubernetesInvocationTraceKey{}), database.Code, database.Position)
	} else {
		trace.t.Logf("owner query %s failed with class %T", ctx.Value(kubernetesInvocationTraceKey{}), data.Err)
	}
}
