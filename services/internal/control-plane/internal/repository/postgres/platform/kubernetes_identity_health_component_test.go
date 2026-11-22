package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// kubernetesPersonalHealthFixture creates an actual published Kubernetes Connection and attaches
// synthetic broker material metadata through the real public/private setup owner transitions.
// It proves PostgreSQL ownership/lifecycle, not target TLS, account RBAC or policy108 signatures.
// kubernetesPersonalHealthFixture создаёт actual published Kubernetes Connection и привязывает
// synthetic broker material metadata через real public/private setup owner transitions.
// Доказывает PostgreSQL ownership/lifecycle, не target TLS, account RBAC либо policy108 signatures.
func kubernetesPersonalHealthFixture(t *testing.T, ctx context.Context) (kubernetesSetupOwnerFixture, *platformservice.Service, entity.IntegrationConnection, entity.PersonalIntegrationCredential) {
	t.Helper()
	fixture := newKubernetesSetupOwnerFixture(t, ctx)
	service, err := platformservice.New(fixture.repository)
	if err != nil {
		t.Fatal("construct actual Kubernetes owner")
	}
	owner := fixture.transport
	owner.Permission = "platform.command.integrations.create"
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "kube-health-create-connection"}, Payload: command.ConnectionInput{
			DefinitionKey: integrationpackage.KubernetesDefinitionKey, Name: "Disposable Kubernetes identity",
			PublicConfiguration: map[string]any{"target_ref": fixture.candidate.Targets[0].Target.Ref}}})
	if err != nil || created.Connection == nil {
		t.Fatalf("create actual Kubernetes Connection: %v", err)
	}
	connection := *created.Connection
	owner.Permission = "platform.command.integrations.enable"
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "kube-health-disable", ExpectedVersion: &connection.Version},
		Payload:  command.ConnectionInput{Ref: connection.Ref, Enabled: false}})
	if err != nil || disabled.Connection == nil {
		t.Fatalf("disable Kubernetes Connection: %v", err)
	}
	connection = *disabled.Connection
	owner.Permission = "platform.integrations.credential-policy.set"
	personal, err := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "kube-health-personal-policy", ExpectedVersion: &connection.Version},
		Payload:  command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}})
	if err != nil || personal.Connection == nil {
		t.Fatalf("select explicit personal Kubernetes policy: %v", err)
	}
	connection = *personal.Connection
	owner.Permission = "platform.command.integrations.enable"
	enabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "kube-health-enable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: true}})
	if err != nil || enabled.Connection == nil {
		t.Fatalf("enable personal Kubernetes Connection: %v", err)
	}
	connection = *enabled.Connection
	assertKubernetesPreBeginRequirements(t, ctx, fixture, service, connection)
	setupOwner := fixture.principal
	setupOwner.Permission = "platform.integrations.kubernetes.setup.begin"
	zero := int64(0)
	setup, err := fixture.repository.BeginKubernetesIdentitySetup(ctx, setupOwner, value.Mutation{IdempotencyKey: "kube-health-first-setup", ExpectedVersion: &zero},
		command.KubernetesIdentitySetupBegin{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version,
			ContentSHA256: strings.Repeat("f", 64), ContentBytes: 12})
	if err != nil || len(setup.Permit) != 32 {
		t.Fatalf("admit actual Kubernetes package setup: %v", err)
	}
	if setup.View.CanonicalContext != fixture.candidate.Targets[0].Target.EmbeddedContext {
		t.Fatal("setup context was not pinned to the original operator target")
	}
	permitDigest := kubernetesHealthFenceDigest(string(setup.Permit))
	clear(setup.Permit)
	broker := fixture.principal
	broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.setup.resolve"
	snapshot, err := fixture.repository.ResolveKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadResolve{SetupRef: setup.View.Ref, PermitSHA256: permitDigest})
	if err != nil {
		t.Fatalf("resolve exact actual package setup: %v", err)
	}
	broker.Permission = "platform.runtime.kubernetes.setup.claim"
	admission, err := fixture.repository.ClaimKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadClaim{
		SetupRef: setup.View.Ref, PermitSHA256: permitDigest, SnapshotDigest: snapshot.Digest, ContentSHA256: snapshot.Provision.ContentSHA256, WorkloadInstance: "synthetic-broker"})
	if err != nil || admission.ReconcileOnly {
		t.Fatalf("claim first actual package setup write: %v", err)
	}
	name, err := managedkubernetes.IdentitySecretName(snapshot.Provision.IdentityRef, snapshot.Provision.Revision)
	if err != nil {
		t.Fatal("derive exact disposable purpose name")
	}
	locator := value.ManagedKubernetesIdentityLocator{IdentityRef: snapshot.Provision.IdentityRef, TargetRef: snapshot.Provision.TargetRef,
		Revision: snapshot.Provision.Revision, Namespace: snapshot.Provision.Namespace, Name: name, UID: uuid.NewString(),
		ResourceVersion: "1", ContentSHA256: snapshot.Provision.ContentSHA256}
	broker.Permission = "platform.runtime.kubernetes.setup.complete"
	completed, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadComplete{
		SetupRef: admission.SetupRef, AttemptRef: admission.AttemptRef, Fence: admission.Fence, Generation: admission.Generation,
		WorkloadInstance: "synthetic-broker", Identity: &locator})
	if err != nil || completed.State != "COMPLETED" {
		t.Fatalf("attach exact actual package purpose fact: %v", err)
	}
	owner.Permission = "platform.personal-integrations.get"
	binding, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || binding.Kind != entity.IntegrationCredentialBrokerIdentity || binding.Credential != nil || binding.BrokerIdentity == nil || binding.State != "CHECK_REQUIRED" {
		t.Fatalf("read actual package BI attachment: %v", err)
	}
	return fixture, service, connection, binding
}

// TestKubernetesIdentityHealthOwnerComponent proves positive BI setup→test→claim→private health
// completion, closed generic-token paths, lost-response replay and revoke-before-result fencing.
// TestKubernetesIdentityHealthOwnerComponent доказывает positive BI setup→test→claim→private health
// completion, closed generic-token paths, lost-response replay и revoke-before-result fencing.
func TestKubernetesIdentityHealthOwnerComponent(t *testing.T) {
	for _, scenario := range []string{"authenticated", "revoke-before-result"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
			defer cancel()
			fixture, service, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
			owner := fixture.transport
			owner.Permission = "platform.personal-integrations.test"
			queued, err := service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: "kube-native-test-queue", ExpectedVersion: &binding.Version},
				Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}})
			if err != nil || queued.PersonalIntegrationCredential == nil {
				t.Fatalf("queue actual BI verification: %v", err)
			}
			binding = *queued.PersonalIntegrationCredential
			worker := fixture.principal
			worker.CallerWorkload, worker.Permission = "integration-gateway", "platform.personal-integration-tests.claim"
			claims, err := fixture.repository.ClaimPersonalIntegrationCredentialTests(ctx, worker, "synthetic-kube-gateway", 1)
			if err != nil || len(claims) != 1 {
				t.Fatalf("claim exact BI health metadata: %v", err)
			}
			claim := claims[0]
			if claim["credential"] != nil || claim["credentialDeliveryKind"] != "BROKER_IDENTITY" || claim["brokerIdentityRef"] != binding.BrokerIdentity.Ref {
				t.Fatal("BI test used a generic credential descriptor")
			}
			lease := value.ManagedKubernetesBinding{WorkloadInstance: "synthetic-kube-gateway", ConnectionTestRef: claim["testRef"].(string),
				LeaseRef: claim["leaseRef"].(string), Fence: claim["fence"].(string), Generation: claim["generation"].(int64), InputDigest: claim["inputDigest"].(string)}
			worker.Permission = "platform.personal-integration-tests.begin"
			if admission, err := fixture.repository.BeginPersonalIntegrationCredentialProbe(ctx, worker, command.PersonalIntegrationCredentialProbeAdmission{
				TestRef: lease.ConnectionTestRef, LeaseRef: lease.LeaseRef, Fence: lease.Fence, Generation: lease.Generation,
				ContractVersion: command.PersonalIntegrationCredentialProbeContractVersion}); !errors.Is(err, errs.ErrForbidden) || !admission.AuthorizedUntil.IsZero() {
				t.Fatal("generic token admission consumed a broker identity test")
			}
			broker := fixture.principal
			broker.CallerWorkload, broker.Permission = "secret-broker", "platform.runtime.kubernetes.health.begin"
			wrong := lease
			wrong.WorkloadInstance = "foreign-gateway"
			if _, err := fixture.repository.BeginManagedKubernetesHealthProbe(ctx, broker, wrong); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("another gateway borrowed test credit")
			}
			admission, err := fixture.repository.BeginManagedKubernetesHealthProbe(ctx, broker, lease)
			if err != nil || admission.Identity.IdentityRef != binding.BrokerIdentity.Ref || admission.Target.Ref != binding.BrokerIdentity.TargetRef {
				t.Fatalf("admit exact private BI health: %v", err)
			}
			if _, err := fixture.repository.BeginManagedKubernetesHealthProbe(ctx, broker, lease); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("lost admission response repeated the health probe")
			}
			if scenario == "revoke-before-result" {
				owner.Permission = "platform.personal-integrations.revoke"
				if _, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "kube-native-revoke", ExpectedVersion: &binding.Version}, Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}}); err != nil {
					t.Fatalf("revoke exact admitted BI: %v", err)
				}
			}
			completion := command.KubernetesIdentityHealthCompletion{AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest,
				Outcome: entity.PersonalIntegrationCredentialAuthenticated, ObservedSubjectDigest: strings.Repeat("a", 64)}
			broker.Permission = "platform.runtime.kubernetes.health.complete"
			receipt, err := fixture.repository.CompleteManagedKubernetesHealthProbe(ctx, broker, lease, completion)
			if err != nil || receipt.Applied != (scenario == "authenticated") {
				t.Fatalf("retain current or late authentication fact: %v", err)
			}
			replayed, err := fixture.repository.CompleteManagedKubernetesHealthProbe(ctx, broker, lease, completion)
			if err != nil || replayed != receipt {
				t.Fatal("lost completion response rewrote the immutable health receipt")
			}
			changed := completion
			changed.ObservedSubjectDigest = strings.Repeat("b", 64)
			if _, err := fixture.repository.CompleteManagedKubernetesHealthProbe(ctx, broker, lease, changed); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("health replay substituted another authenticated subject")
			}
			owner.Permission = "platform.personal-integrations.get"
			current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
			if err != nil || (scenario == "authenticated" && (current.State != "READY" || current.VerifiedBrokerIdentityRef != binding.BrokerIdentity.Ref || len(current.VerifiedCapabilities) != 1 || current.VerifiedCapabilities[0] != "kubernetes.identity")) ||
				(scenario != "authenticated" && current.State != "REVOKED") {
				t.Fatal("health completion forged resource permissions or revived a revoked BI")
			}
		})
	}
}
