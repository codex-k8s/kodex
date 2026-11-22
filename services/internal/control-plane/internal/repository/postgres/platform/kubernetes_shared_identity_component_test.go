package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newKubernetesSharedSetupFixture creates an actual shared Kubernetes Connection with explicit SERVICE custody, proves org.manage denial before allocation, then checks known material and its attachment under the selected deadline.
// newKubernetesSharedSetupFixture создаёт actual shared Kubernetes Connection с explicit SERVICE custody, проверяет отказ без org.manage до allocation, затем проверяет known material и подключение с выбранным сроком.
func newKubernetesSharedSetupFixture(t *testing.T, expireDuringAttachment bool) (kubernetesSetupOwnerFixture, *platformservice.Service, entity.IntegrationConnection, value.ManagedKubernetesIdentityLocator) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	f := newKubernetesSetupOwnerFixture(t, ctx)
	service, err := platformservice.New(f.repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "kube-shared-")
	connection := *execute(command.CreateConnection, f.transport, "create", nil, command.ConnectionInput{DefinitionKey: integrationpackage.KubernetesDefinitionKey, Name: "Disposable explicit service identity", PublicConfiguration: map[string]any{"target_ref": f.candidate.Targets[0].Target.Ref, "service_subject_ref": "sys_platform"}}).Connection
	connection = *execute(command.SetConnectionEnabled, f.transport, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
	policyOwner := f.transport
	policyOwner.Permission = "platform.integrations.credential-policy.set"
	connection = *execute(command.SetIntegrationCredentialPolicy, policyOwner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialProjectShared}).Connection
	t.Run("requires-organization-manage", func(t *testing.T) {
		proof := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000002145", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Disposable KUBE shared manager", CallerWorkload: "control-api-gateway", Operation: kubernetesSetupBeginOperation}
		if _, err := f.repository.ResolveProofAuthority(ctx, proof); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("unbound shared manager admitted", err)
		}
		subjects, _, err := service.ListAccessSubjects(ctx, f.transport, query.Filter{Query: proof.ExternalDisplayName}, "USER")
		if err != nil || len(subjects) != 1 {
			t.Fatal("read explicit manager subject", err)
		}
		receiptAccessBinding(t, ctx, service, f.transport, subjects[0].Ref, "kube-shared-manager", []string{"integration.view", "integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connection.Ref})
		authority, err := f.repository.ResolveProofAuthority(ctx, proof)
		if err != nil {
			t.Fatal("resolve ordinary manager without OwnerClaim", err)
		}
		actor := value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, Permission: proof.Operation, CallerWorkload: proof.CallerWorkload, CorrelationRef: "kube-shared-negative", CredentialRevision: 1}
		admitted, err := service.BeginKubernetesIdentitySetup(ctx, actor, value.Mutation{IdempotencyKey: "kube-shared-denied", ExpectedVersion: &connection.Version}, command.KubernetesIdentitySetupBegin{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version, ContentSHA256: strings.Repeat("c", 64), ContentBytes: 12})
		if !errors.Is(err, errs.ErrForbidden) || admitted.View.Ref != "" || len(admitted.Permit) != 0 {
			t.Fatal("integration manager gained SERVICE setup authority", err)
		}
		var allocated int
		if f.pool.QueryRow(ctx, "SELECT count(*)::int FROM control_plane.kubernetes_identity_setups WHERE organization_id=$1::uuid AND actor_id=$2::uuid", authority.OrganizationID, authority.ActorID).Scan(&allocated) != nil || allocated != 0 {
			t.Fatal("denied manager retained setup intent")
		}
	})
	owner := f.principal
	owner.Permission = kubernetesSetupBeginOperation
	setup, err := f.repository.BeginKubernetesIdentitySetup(ctx, owner, value.Mutation{IdempotencyKey: "kube-shared-positive", ExpectedVersion: &connection.Version}, command.KubernetesIdentitySetupBegin{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version, ContentSHA256: strings.Repeat("c", 64), ContentBytes: 12})
	if err != nil || len(setup.Permit) != 32 {
		t.Fatal("shared SERVICE first setup", err)
	}
	digest := kubernetesHealthFenceDigest(string(setup.Permit))
	clear(setup.Permit)
	validFor := 20 * time.Second
	if expireDuringAttachment {
		validFor = 3500 * time.Millisecond
	}
	finiteUntil, restore := isolateIdentitySetupPermission(t, ctx, f.pool, service, f.transport, f.current, []string{"organization.manage", "integration.view", "integration.manage"}, "organization.manage", validFor)
	defer restore()
	broker := f.principal
	broker.CallerWorkload = "secret-broker"
	broker.Permission = "platform.runtime.kubernetes.setup.resolve"
	snapshot, err := f.repository.ResolveKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadResolve{SetupRef: setup.View.Ref, PermitSHA256: digest})
	if err != nil {
		t.Fatal("resolve shared material intent", err)
	}
	broker.Permission = "platform.runtime.kubernetes.setup.claim"
	admission, err := f.repository.ClaimKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadClaim{SetupRef: setup.View.Ref, PermitSHA256: digest, SnapshotDigest: snapshot.Digest, ContentSHA256: snapshot.Provision.ContentSHA256, WorkloadInstance: "kube-shared-broker"})
	if err != nil || admission.ReconcileOnly {
		t.Fatal("claim shared material write", err)
	}
	if !snapshot.ExpiresAt.Equal(finiteUntil) || admission.ExpiresAt.After(finiteUntil) || !admission.ExpiresAt.After(time.Now()) {
		t.Fatal("Kubernetes preflight or grant outlived fresh SERVICE management permission")
	}
	var durableExpiry time.Time
	if err := f.pool.QueryRow(ctx, "SELECT expires_at FROM control_plane.kubernetes_identity_upload_attempts WHERE ref=$1", admission.AttemptRef).Scan(&durableExpiry); err != nil || !durableExpiry.Equal(finiteUntil) {
		t.Fatal("Kubernetes durable attempt did not retain finite SERVICE management expiry", err)
	}
	name, err := managedkubernetes.IdentitySecretName(snapshot.Provision.IdentityRef, snapshot.Provision.Revision)
	if err != nil {
		t.Fatal(err)
	}
	locator := value.ManagedKubernetesIdentityLocator{IdentityRef: snapshot.Provision.IdentityRef, TargetRef: snapshot.Provision.TargetRef, Revision: snapshot.Provision.Revision, Namespace: snapshot.Provision.Namespace, Name: name, UID: uuid.NewString(), ResourceVersion: "1", ContentSHA256: snapshot.Provision.ContentSHA256}
	broker.Permission = "platform.runtime.kubernetes.setup.complete"
	completion := command.KubernetesIdentityUploadComplete{SetupRef: admission.SetupRef, AttemptRef: admission.AttemptRef, Fence: admission.Fence, Generation: admission.Generation, WorkloadInstance: "kube-shared-broker", Identity: &locator}
	if expireDuringAttachment {
		verifyKubernetesSharedReceiptAfterExpiry(t, ctx, f, broker, completion, finiteUntil)
		return f, service, connection, locator
	}
	completed, err := f.repository.CompleteKubernetesIdentityUpload(ctx, broker, completion)
	if err != nil || completed.State != "COMPLETED" {
		t.Fatal("attach shared SERVICE material", err)
	}
	read := f.transport
	read.Permission = "platform.query.integration-connections.get"
	fresh, err := service.GetIntegrationConnection(ctx, read, connection.Ref)
	if err != nil || fresh.CredentialDelivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || fresh.CredentialDelivery.BrokerPurpose != "KUBERNETES_TARGET" || fresh.CredentialRevision != nil || fresh.CredentialPolicy != entity.IntegrationCredentialProjectShared {
		t.Fatal("read exact shared BI attachment", err)
	}
	var serviceOwned, personalAbsent bool
	if f.pool.QueryRow(ctx, `SELECT subject.kind='SERVICE' AND subject.ref='sys_platform',identity.binding_id IS NULL AND connection.broker_identity_revision_id=identity.id
 FROM control_plane.integration_broker_identity_revisions identity JOIN control_plane.integration_connections connection ON connection.id=identity.connection_id AND connection.organization_id=identity.organization_id JOIN control_plane.subjects subject ON subject.id=identity.subject_id AND subject.organization_id=identity.organization_id
 WHERE identity.organization_id=$1::uuid AND identity.ref=$2`, f.current.organizationID, snapshot.Provision.IdentityRef).Scan(&serviceOwned, &personalAbsent) != nil || !serviceOwned || !personalAbsent {
		t.Fatal("shared custody substituted a personal or administrative identity")
	}
	return f, service, fresh, locator
}

// kubernetesSetupAttachmentDeadlineTrace delays only the authorized attachment statement in a disposable database.
// kubernetesSetupAttachmentDeadlineTrace задерживает только запрос подключения в одноразовой БД.
type kubernetesSetupAttachmentDeadlineTrace struct {
	until   time.Time
	reached bool
}

// TraceQueryStart keeps the receipt transaction open until the selected finite deadline expires.
// TraceQueryStart удерживает транзакцию receipt до истечения выбранного конечного срока.
func (trace *kubernetesSetupAttachmentDeadlineTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryBrokerIdentityAttachShared {
		trace.reached = true
		timer := time.NewTimer(time.Until(trace.until) + 10*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	return ctx
}

// TraceQueryEnd leaves completion diagnostics to the repository under test.
// TraceQueryEnd оставляет обработку результата проверяемому репозиторию.
func (*kubernetesSetupAttachmentDeadlineTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

// verifyKubernetesSharedReceiptAfterExpiry proves a known observation remains durable when authority expires at the attachment statement.
// verifyKubernetesSharedReceiptAfterExpiry проверяет сохранность known observation при истечении права на последнем запросе подключения.
func verifyKubernetesSharedReceiptAfterExpiry(t *testing.T, ctx context.Context, fixture kubernetesSetupOwnerFixture, broker value.Principal, input command.KubernetesIdentityUploadComplete, until time.Time) {
	t.Helper()
	trace := &kubernetesSetupAttachmentDeadlineTrace{until: until}
	config := fixture.pool.Config()
	config.ConnConfig.Tracer = trace
	delayedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("connect disposable attachment probe", err)
	}
	defer delayedPool.Close()
	repository := *fixture.repository
	repository.pool = delayedPool
	result, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, input)
	if !trace.reached || err != nil || result.State != "CANCELLED" {
		t.Fatal("known material did not stop at the expired attachment", err)
	}
	if replay, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, input); err != nil || replay != result {
		t.Fatal("late known receipt was lost after attachment denial", err)
	}
	var recorded, attached bool
	if err := fixture.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM control_plane.kubernetes_identity_upload_observations observation
 JOIN control_plane.kubernetes_identity_upload_attempts attempt ON attempt.id=observation.attempt_id
 WHERE attempt.ref=$1 AND observation.outcome='KNOWN'),
 EXISTS(SELECT 1 FROM control_plane.integration_connections connection
 JOIN control_plane.kubernetes_identity_setups setup ON setup.connection_id=connection.id
 WHERE setup.ref=$2 AND connection.broker_identity_revision_id=setup.identity_revision_id)`, input.AttemptRef, input.SetupRef).Scan(&recorded, &attached); err != nil || !recorded || attached {
		t.Fatal("known receipt and rejected credential attachment were not separated", err)
	}
}

// TestKubernetesSharedIdentityOwnerComponent follows actual shared setup, typed queue and purpose health admission; a late completion retains facts without reconnecting a revoked Connection.
// TestKubernetesSharedIdentityOwnerComponent проходит actual shared setup, typed queue и purpose health admission; late completion сохраняет facts без восстановления revoked Connection.
func TestKubernetesSharedIdentityOwnerComponent(t *testing.T) {
	t.Run("expiry-at-attachment", func(t *testing.T) {
		newKubernetesSharedSetupFixture(t, true)
	})
	for _, scenario := range []string{"authenticated", "revoke-before-result"} {
		t.Run(scenario, func(t *testing.T) {
			f, service, connection, locator := newKubernetesSharedSetupFixture(t, false)
			runctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
			defer cancel()
			execute := newComponentCommandExecutor(t, runctx, service, "kube-shared-health-")
			connection = *execute(command.SetConnectionEnabled, f.transport, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
			var projectRef, agentRef string
			if scenario == "authenticated" {
				seedObservedCatalogFixture(t, runctx, f.repository)
				project := execute(command.CreateProject, f.transport, "project", nil, command.ProjectInput{Name: "Disposable SERVICE Kubernetes execution", Language: "ru"}).Project
				agent := createLifecycleAgent(t, runctx, service, f.transport, project.Ref, "kube-shared-agent", "Shared Kubernetes reader")
				projectRef, agentRef = project.Ref, agent.Ref
			}
			owner := f.transport
			owner.Permission = "platform.command.integrations.test"
			started, err := service.Execute(runctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-shared-health-test", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref}})
			if err != nil || started.Connection == nil {
				t.Fatal("queue actual shared Kubernetes health", err)
			}
			connection = *started.Connection
			gateway := f.principal
			gateway.CallerWorkload = "integration-gateway"
			gateway.Permission = "platform.runtime.integration-tests.claim"
			claims, err := f.repository.ClaimIntegrationConnectionTests(runctx, gateway, "kube-shared-health-worker", 16)
			if err != nil {
				t.Fatal("claim actual shared health", err)
			}
			var claim map[string]any
			for _, c := range claims {
				if stringMap(c, "connectionRef") == connection.Ref {
					claim = c
				}
			}
			if claim == nil || claim["credentialDeliveryKind"] != "BROKER_IDENTITY" || claim["brokerIdentityPurpose"] != "KUBERNETES_TARGET" || claim["credential"] != nil || claim["brokerIdentityRef"] != locator.IdentityRef {
				t.Fatal("shared Kubernetes health lost exact BI metadata")
			}
			binding := value.ManagedKubernetesBinding{WorkloadInstance: "kube-shared-health-worker", ConnectionTestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
			broker := f.principal
			broker.CallerWorkload = "secret-broker"
			broker.Permission = "platform.runtime.kubernetes.health.begin"
			wrong := binding
			wrong.WorkloadInstance = "foreign-shared-worker"
			if _, err := f.repository.BeginManagedKubernetesHealthProbe(runctx, broker, wrong); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("foreign worker consumed shared health", err)
			}
			admitted, err := f.repository.BeginManagedKubernetesHealthProbe(runctx, broker, binding)
			if err != nil || admitted.Identity.IdentityRef != locator.IdentityRef {
				t.Fatal("admit actual shared Kubernetes health", err)
			}
			if scenario == "revoke-before-result" {
				connection = *execute(command.SetConnectionEnabled, f.transport, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
			}
			completion := command.KubernetesIdentityHealthCompletion{AttemptRef: admitted.AttemptRef, SnapshotDigest: admitted.SnapshotDigest, Outcome: entity.PersonalIntegrationCredentialAuthenticated, ObservedSubjectDigest: strings.Repeat("a", 64)}
			broker.Permission = "platform.runtime.kubernetes.health.complete"
			if scenario == "authenticated" {
				before := kubernetesSharedHealthOwnerSnapshot(t, runctx, f)
				for _, item := range []struct {
					sql string
					oid uint32
				}{{queryKubernetesHealthSharedSource, pgtype.BoolOID}, {queryKubernetesHealthRead, pgtype.TimestamptzOID}} {
					broken, fault := kubernetesAuthorityFaultRepository(t, runctx, f.repository, item.sql, item.oid)
					if _, err := broken.CompleteManagedKubernetesHealthProbe(runctx, broker, binding, completion); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
						t.Fatal("shared health Scan failure became semantic closure", err)
					}
					if kubernetesSharedHealthOwnerSnapshot(t, runctx, f) != before {
						t.Fatal("shared health Scan failure changed owner rows or receipt")
					}
				}
			}
			receipt, err := f.repository.CompleteManagedKubernetesHealthProbe(runctx, broker, binding, completion)
			if err != nil || receipt.Applied != (scenario == "authenticated") {
				t.Fatal("retain shared health fact with current owner eligibility", err)
			}
			replay, err := f.repository.CompleteManagedKubernetesHealthProbe(runctx, broker, binding, completion)
			if err != nil || replay != receipt {
				t.Fatal("lost shared completion ACK rewrote receipt", err)
			}
			owner.Permission = "platform.query.integration-connections.get"
			fresh, err := service.GetIntegrationConnection(runctx, owner, connection.Ref)
			if err != nil || scenario == "authenticated" && fresh.State != "CONNECTED" || scenario != "authenticated" && fresh.State == "CONNECTED" {
				t.Fatal("shared health published stale or missing readiness", err)
			}
			if scenario == "authenticated" {
				connection = *execute(command.ChangeIntegrationGrant, f.transport, "grant", &fresh.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agentRef, CapabilityKey: "kubernetes.get", ApprovalPolicy: "NONE", Enabled: true, KubernetesNamespace: "review"}).Connection
				run := execute(command.LaunchRun, f.transport, "launch", nil, command.LaunchRunInput{ProjectRef: projectRef, Target: entity.RunTarget{Type: "AGENT", Ref: agentRef}, Task: "Read only a selected disposable Kubernetes ConfigMap"}).Run
				worker := resolvedTestPrincipal(t, runctx, f.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
				runtime := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "kube-shared-runtime", Limit: 1}).RuntimeItems
				if len(runtime) != 1 || stringMap(runtime[0], "runRef") != run.Ref {
					t.Fatal("claim actual shared RuntimeRevision")
				}
				resolved, err := service.ResolveIntegrationInvocation(runctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "kubernetes.get", "idempotency_key": "kube-shared-get"}, map[string]any{"group": "", "version": "v1", "resource": "configmaps", "namespace": "review", "name": "settings"})
				if err != nil || stringMap(resolved, "state") != "READY" {
					t.Fatal("resolve actual SERVICE-backed Kubernetes invocation", err)
				}
				_, _, caller := resolveIntegrationEffectTestPrincipals(t, runctx, f.repository)
				for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
					if method == cp.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName {
						caller.Permission = operation
					}
				}
				selected, err := service.ClaimIntegrationInvocationsWithCapacity(runctx, caller, "kube-shared-gateway", 1, value.IntegrationInvocationDeliveryCapacity{Kubernetes: 1})
				if err != nil || len(selected) != 1 || stringMap(selected[0], "invocationRef") != stringMap(resolved, "invocationRef") || stringMap(selected[0], "brokerIdentityRef") != locator.IdentityRef || selected[0]["credential"] != nil {
					t.Fatal("claim exact shared SERVICE identity without personal fallback", err)
				}
				claimed := selected[0]
				lease := value.ManagedKubernetesBinding{WorkloadInstance: "kube-shared-gateway", InvocationRef: stringMap(claimed, "invocationRef"), LeaseRef: stringMap(claimed, "leaseRef"), Fence: stringMap(claimed, "fence"), Generation: runtimeRevisionMapInt64(claimed, "generation"), InputDigest: stringMap(claimed, "inputDigest")}
				broker.Permission = "platform.runtime.kubernetes.resolve"
				operation, err := f.repository.ResolveManagedKubernetesOperation(runctx, broker, lease)
				if err != nil || operation.IdentityRef != locator.IdentityRef || operation.SubjectDigest != strings.Repeat("a", 64) {
					t.Fatal("resolve shared operation from exact qualified health fact", err)
				}
			}

		})
	}
}

// kubernetesSharedHealthOwnerSnapshot hashes the shared Connection/job/attempt/completion graph without exposing material, proving rollback after successful SQL with driver decode failure.
// kubernetesSharedHealthOwnerSnapshot хэширует shared Connection/job/attempt/completion graph без раскрытия material, доказывая rollback после successful SQL и driver decode failure.
func kubernetesSharedHealthOwnerSnapshot(t *testing.T, ctx context.Context, f kubernetesSetupOwnerFixture) string {
	t.Helper()
	var digest string
	if err := f.pool.QueryRow(ctx, `SELECT md5(jsonb_build_object(
 'connections',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_connections v WHERE organization_id=$1::uuid),
 'tests',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_connection_tests v WHERE organization_id=$1::uuid),
 'attempts',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.kubernetes_identity_health_attempts v WHERE organization_id=$1::uuid),
 'completions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.attempt_id) FROM control_plane.kubernetes_identity_health_completions v JOIN control_plane.kubernetes_identity_health_attempts a ON a.id=v.attempt_id WHERE a.organization_id=$1::uuid)
 )::text)`, f.current.organizationID).Scan(&digest); err != nil {
		t.Fatal("snapshot shared health graph", err)
	}
	return digest
}
