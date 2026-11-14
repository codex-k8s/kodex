package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sentryObservabilityComponentConfiguration supplies explicit synthetic scope choices for each shipped definition; it never loads operator target addresses, credentials or qualification files.
// sentryObservabilityComponentConfiguration задаёт explicit synthetic scope choice каждой shipped definition; operator target addresses, credentials и qualification files не загружаются.
func sentryObservabilityComponentConfiguration(definition integrationpackage.Package) map[string]any {
	key := definition.Metadata.Key
	configuration := map[string]any{"base_url": "https://provider.example.invalid"}
	if strings.HasPrefix(key, "sentry") {
		configuration["base_url"] = "https://sentry.io"
		if strings.Contains(key, "self-hosted") {
			configuration["base_url"] = "https://sentry.example.invalid"
		}
		configuration["organization_slug"] = "fixture-org"
		if !strings.HasSuffix(key, "organization") {
			configuration["project_slug"] = "fixture-project"
		}
	}
	if strings.HasPrefix(key, "grafana") {
		configuration["base_url"] = "https://grafana.example.invalid"
		configuration["organization_id"] = "1"
	}
	if strings.HasPrefix(key, "loki") {
		configuration["base_url"] = "https://loki.example.invalid"
	}
	for _, field := range definition.Spec.ConfigurationFields {
		switch field.Key {
		case "scope_mode":
			configuration[field.Key] = field.AllowedValues[0]
		case "datasource_uid":
			configuration[field.Key] = "fixture-prometheus"
		case "dashboard_uid":
			configuration[field.Key] = "fixture-dashboard"
		case "tenant_id":
			configuration[field.Key] = "fixture-tenant"
		case "rule_namespace":
			configuration[field.Key] = "fixture-rules"
		}
	}
	return configuration
}

// sentryObservabilityComponentInput creates package-valid scalar metadata for owner pin/gate/RPC qualification; native provider semantic validation and HTTP effects are covered separately by the gateway matrix and are never invoked here.
// sentryObservabilityComponentInput создаёт package-valid scalar metadata для owner pin/gate/RPC qualification; native provider semantic validation и HTTP effects отдельно покрывает gateway matrix и здесь не вызываются.
func sentryObservabilityComponentInput(capability integrationpackage.Capability) map[string]any {
	input := map[string]any{}
	for _, field := range capability.InputFields {
		switch field.Type {
		case "BOOLEAN":
			input[field.Key] = false
		case "INTEGER":
			input[field.Key] = field.Minimum
		case "STRING":
			text := "synthetic"
			if field.MaximumLength < len(text) {
				text = text[:field.MaximumLength]
			}
			if len(field.AllowedValues) > 0 {
				text = field.AllowedValues[0]
			}
			input[field.Key] = text
		}
	}
	return input
}

// TestSentryObservabilityPersonalProfilesComponent exercises every advertised Sentry/OBS tuple through real owner create/personal verification/grants/launch, native DB profile guard, optional EACH gate, claim and Begin RPC; no external provider effect or live qualification is claimed.
// TestSentryObservabilityPersonalProfilesComponent проверяет каждый advertised Sentry/OBS tuple через real owner create/personal verification/grants/launch, native DB profile guard, optional EACH gate, claim и Begin RPC; external provider effect либо live qualification не утверждаются.
func TestSentryObservabilityPersonalProfilesComponent(t *testing.T) {
	for _, key := range []string{"sentry", "sentry-self-hosted", "sentry-organization", "sentry-self-hosted-organization", "prometheus", "thanos", "grafana-dashboard", "grafana-organization", "grafana-dashboard-annotations", "grafana-organization-rule-delete", "loki", "loki-ruler-namespace-read", "loki-ruler-namespace"} {
		t.Run(key, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repository := newSyntheticRuntimeComponentRepository(t, ctx, pool)
			seedObservedCatalogFixture(t, ctx, repository)
			service, err := platformservice.New(repository, platformservice.WithCredentialMaterializer(&personalCredentialComponentMaterializer{}))
			if err != nil {
				t.Fatal(err)
			}
			owner, runtimeWorker, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			execute := newComponentCommandExecutor(t, ctx, service, "obs-profile-")
			definition := repository.integrationDefinitions[key]
			connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: key, Name: "Synthetic scoped provider " + key, PublicConfiguration: sentryObservabilityComponentConfiguration(definition)}).Connection
			connection = execute(command.SetConnectionEnabled, owner, "disable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: false}).Connection
			owner.Permission = "platform.integrations.credential-policy.set"
			connection = execute(command.SetIntegrationCredentialPolicy, owner, "policy", &connection.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
			owner.Permission = "platform.command.integrations.enable"
			connection = execute(command.SetConnectionEnabled, owner, "enable", &connection.Version, command.ConnectionInput{Ref: connection.Ref, Enabled: true}).Connection
			owner.Permission = "platform.personal-integrations.create"
			binding := execute(command.CreatePersonalIntegrationCredential, owner, "binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}).PersonalIntegrationCredential
			owner.Permission = "platform.personal-integrations.configure"
			configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, value.Mutation{IdempotencyKey: "obs-profile-key", ExpectedVersion: &binding.Version}, connection.Ref, connection.Version, []byte("synthetic-personal-provider-key"))
			if err != nil {
				t.Fatal(err)
			}
			owner.Permission = "platform.personal-integrations.test"
			execute(command.TestPersonalIntegrationCredential, owner, "test", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version})
			verifier := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.personal-integration-tests.claim"}, "integration-gateway")
			tests, err := service.ClaimPersonalIntegrationCredentialTests(ctx, verifier, "obs-profile-verifier", 1)
			if err != nil || len(tests) != 1 {
				t.Fatal("claim typed provider verification", err)
			}
			completion := personalTestCompletionFromClaim(tests[0])
			verifier.Permission = "platform.personal-integration-tests.begin"
			if err = service.BeginPersonalIntegrationCredentialProbe(ctx, verifier, completion); err != nil {
				t.Fatal(err)
			}
			completion.Outcome = entity.PersonalIntegrationCredentialAuthenticated
			completion.ConfirmedCapabilityKeys = []string{definition.Spec.HealthCheck.Operation}
			completion.ObservedPermissionProfiles, _ = integrationpackage.CredentialObservationRequests(definition)
			verifier.Permission = "platform.personal-integration-tests.complete"
			execute(command.CompletePersonalIntegrationCredentialTest, verifier, "verified", nil, completion)
			owner.Permission = "platform.personal-integrations.get"
			current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
			if err != nil || current.State != "READY" || !slices.Equal(current.VerifiedCapabilities, completion.ConfirmedCapabilityKeys) {
				t.Fatal("owner fabricated provider WRITE proof", err)
			}
			project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic scoped provider " + key, Language: "ru"}).Project
			agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "obs-profile-agent", "Synthetic provider consumer")
			for _, capability := range definition.Spec.Capabilities {
				connection = execute(command.ChangeIntegrationGrant, owner, "grant-"+capability.Key, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: capability.Key, ApprovalPolicy: capability.ApprovalPolicy, Enabled: true}).Connection
			}
			run := execute(command.LaunchRun, owner, "run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Synthetic owner/profile qualification only", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			runtime := execute(command.ClaimExecution, runtimeWorker, "runtime", nil, command.LeaseInput{WorkloadInstance: "obs-profile-runtime", Limit: 1}).RuntimeItems
			if len(runtime) != 1 {
				t.Fatal("typed provider launch not claimed")
			}
			rpc := personalRuntimeRPCClient(t, service)
			for _, capability := range definition.Spec.Capabilities {
				contract, exists := integrationpackage.CredentialResourcePermissionContractFor(key, capability.Key)
				if !exists {
					t.Fatal("shipped capability lacks typed owner contract")
				}
				invocation, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(runtime[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": capability.Key, "idempotency_key": "obs-profile-" + capability.Key}, sentryObservabilityComponentInput(capability))
				if err != nil {
					t.Fatal("typed provider immutable pin failed", capability.Key, err)
				}
				expected := "READY"
				if capability.Risk != "READ" {
					expected = "WAITING_APPROVAL"
				}
				if stringMap(invocation, "state") != expected {
					t.Fatal("typed provider gate contract changed", capability.Key)
				}
				for _, wrong := range []string{"", integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1} {
					_, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.integration_personal_invocation_credentials
(invocation_id,organization_id,connection_id,root_run_id,subject_id,binding_id,binding_generation,credential_revision_id,definition_digest,configuration_digest,resource_permission_profile)
SELECT pin.invocation_id,pin.organization_id,pin.connection_id,pin.root_run_id,pin.subject_id,pin.binding_id,pin.binding_generation,pin.credential_revision_id,pin.definition_digest,pin.configuration_digest,$2
FROM control_plane.integration_personal_invocation_credentials pin JOIN control_plane.integration_invocations invocation ON invocation.id=pin.invocation_id WHERE invocation.ref=$1`, stringMap(invocation, "invocationRef"), wrong)
					var rejected *pgconn.PgError
					if !errors.As(err, &rejected) || rejected.Code != "23514" {
						t.Fatal("database accepted a missing/foreign provider profile", capability.Key, err)
					}
				}
				if capability.Risk != "READ" {
					gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
					if err != nil {
						t.Fatal(err)
					}
					execute(command.ResolveOwnerGate, owner, "approve-"+capability.Key, &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
				}
				claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "obs-profile-gateway", 1)
				if err != nil || len(claims) != 1 || stringMap(claims[0], "resourcePermissionProfile") != contract.Profile || claims[0]["credential"] != nil {
					t.Fatal("typed provider claim lost exact profile/key boundary", capability.Key, err)
				}
				claim := claims[0]
				request := &cp.BeginPersonalIntegrationInvocationRequest{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), WorkloadInstance: "obs-profile-gateway"}
				admitted, err := rpc.BeginPersonalIntegrationInvocation(personalRuntimeRPCContext(ctx), request)
				if err != nil || !admitted.GetAdmitted() || admitted.GetCredentialRevision().GetRef() != current.Credential.Ref || admitted.GetResourcePermissionProfile().String() != "INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+contract.Profile {
					t.Fatal("typed provider Begin lost owner key/profile", capability.Key, err)
				}
				execute(command.CompleteIntegrationInvocation, gateway, "closed-"+capability.Key, nil, command.IntegrationInvocationInput{InvocationRef: request.InvocationRef, LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"})
				stored, err := service.GetIntegrationInvocation(ctx, gateway, request.InvocationRef)
				if err != nil || stringMap(stored, "state") != "FAILED" || stringMap(stored, "effectReceiptRef") != "" {
					t.Fatal("synthetic provider refusal fabricated an external effect", err)
				}
			}
			fresh, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Close synthetic typed provider profile case"})
		})
	}
}
