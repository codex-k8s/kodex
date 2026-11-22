package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesGrantByNamespace resolves a test assertion from the actual public owner projection.
// It rejects duplicate namespaces rather than relying on the order of returned grant rows.
// kubernetesGrantByNamespace разрешает test assertion из actual public owner projection.
// Отклоняет duplicate namespaces вместо зависимости от порядка returned grant rows.
func kubernetesGrantByNamespace(t *testing.T, connection entity.IntegrationConnection, namespace string) entity.IntegrationGrant {
	t.Helper()
	var selected entity.IntegrationGrant
	for _, grant := range connection.Grants {
		if grant.ResourceScope["namespace"] == namespace {
			if selected.Ref != "" {
				t.Fatal("duplicate public namespace scope")
			}
			selected = grant
		}
	}
	if selected.Ref == "" {
		t.Fatal("missing public namespace grant")
	}
	return selected
}

// TestKubernetesGrantOwnerComponent proves two independent namespaces, exact-ref revoke/upsert,
// stale OCC, cross-scope denial and the unchanged non-Kubernetes scalar grant contract.
// It invokes the real public owner service without target effects or policy108 signature claims.
// TestKubernetesGrantOwnerComponent доказывает две независимые namespaces, exact-ref revoke/upsert,
// stale OCC, cross-scope denial и прежний non-Kubernetes scalar grant contract.
// Использует real public owner service без target effects либо policy108 signature claims.
func TestKubernetesGrantOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	fixture, service, connection, _ := kubernetesPersonalHealthFixture(t, ctx)
	repository := fixture.repository
	seedObservedCatalogFixture(t, ctx, repository)
	owner := fixture.transport
	execute := newComponentCommandExecutor(t, ctx, service, "kube-grants-")
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Disposable namespace grants", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "kube-grants-agent", "Namespace reader")
	create := func(namespace, key string) {
		t.Helper()
		connection = *execute(command.ChangeIntegrationGrant, owner, key, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.get", Enabled: true, ApprovalPolicy: "NONE", KubernetesNamespace: namespace}).Connection
	}
	create("alpha", "alpha")
	alpha := kubernetesGrantByNamespace(t, connection, "alpha")
	create("beta", "beta")
	beta := kubernetesGrantByNamespace(t, connection, "beta")
	if alpha.Ref == beta.Ref || len(connection.Grants) != 2 || alpha.ResourceScopeDigest == beta.ResourceScopeDigest {
		t.Fatal("another namespace replaced the first grant")
	}
	create("alpha", "alpha-upsert")
	updated := kubernetesGrantByNamespace(t, connection, "alpha")
	if updated.Ref != alpha.Ref || updated.Version <= alpha.Version || len(connection.Grants) != 2 {
		t.Fatal("same-scope upsert created a duplicate or changed its ref")
	}
	alpha = updated
	before := connection.Version
	wrong := command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.get", Enabled: false, ApprovalPolicy: "NONE", KubernetesNamespace: "beta", KubernetesGrantRef: alpha.Ref}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-grants-cross-scope", ExpectedVersion: &before}, Payload: wrong}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign namespace ref retargeted or revoked a sibling grant", err)
	}
	wrong.KubernetesNamespace = "alpha"
	wrong.KubernetesGrantRef = ""
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-grants-missing-ref", ExpectedVersion: &before}, Payload: wrong}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("revoke selected an arbitrary namespace without its ref", err)
	}
	wrong.KubernetesGrantRef = alpha.Ref
	connection = *execute(command.ChangeIntegrationGrant, owner, "alpha-revoke", &connection.Version, wrong).Connection
	if kubernetesGrantByNamespace(t, connection, "alpha").Enabled || !kubernetesGrantByNamespace(t, connection, "beta").Enabled {
		t.Fatal("exact revoke changed the sibling namespace")
	}
	stale := before
	wrong.KubernetesNamespace = "beta"
	wrong.KubernetesGrantRef = beta.Ref
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-grants-stale", ExpectedVersion: &stale}, Payload: wrong}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("stale Connection OCC revoked a namespace grant", err)
	}
	invalid := command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "kubernetes.get", Enabled: true, ApprovalPolicy: "NONE", KubernetesNamespace: "@all-namespaces"}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-grants-implicit-all", ExpectedVersion: &connection.Version}, Payload: invalid}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("ordinary get inherited all namespaces", err)
	}
	invalid.CapabilityKey = "kubernetes.apply"
	invalid.KubernetesNamespace = "alpha"
	invalid.ApprovalPolicy = "HUMAN_SCOPED"
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-grants-apply-floor", ExpectedVersion: &connection.Version}, Payload: invalid}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("apply inherited a scoped approval policy", err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin scalar-plan refusal check", err)
	}
	_, planErr := repository.hydrateAssistantIntegrationGrant(ctx, tx, fixture.current, project.Ref, entity.AssistantPlanOperation{Type: "CHANGE_INTEGRATION_GRANT", Parameters: map[string]any{"connectionRef": connection.Ref, "capabilityKey": "kubernetes.get", "agentRef": agent.Ref, "enabled": true, "approvalPolicy": "NONE"}})
	if !errors.Is(planErr, errs.ErrKubernetesConfigurationPlanUnsupported) {
		_ = tx.Rollback(ctx)
		t.Fatal("scalar configuration plan selected an arbitrary namespace", planErr)
	}
	var configuration []byte
	var scalarRef string
	var scalarVersion int64
	if tx.QueryRow(ctx, queryIntegrationCandidateScope, pgx.StrictNamedArgs{"organization_id": fixture.current.organizationID, "connection_ref": connection.Ref, "recipient_kind": "AGENT", "recipient_ref": agent.Ref, "capability_key": "kubernetes.get"}).Scan(&configuration, &scalarRef, &scalarVersion) != nil || scalarRef != "" || scalarVersion != 0 {
		_ = tx.Rollback(ctx)
		t.Fatal("candidate collapsed several namespaces to one ref")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("close scalar-plan negative transaction", err)
	}
	ordinary := execute(command.CreateConnection, owner, "ordinary", nil, command.ConnectionInput{DefinitionKey: "gitlab", Name: "Ordinary grant regression", PublicConfiguration: map[string]any{"base_url": "https://git.example.invalid", "scope_kind": "PROJECT", "project_path": "fixture/project"}}).Connection
	ordinary = execute(command.SetConnectionEnabled, owner, "ordinary-disable", &ordinary.Version, command.ConnectionInput{Ref: ordinary.Ref, Enabled: false}).Connection
	policyOwner := owner
	policyOwner.Permission = "platform.integrations.credential-policy.set"
	ordinary = execute(command.SetIntegrationCredentialPolicy, policyOwner, "ordinary-personal", &ordinary.Version, command.IntegrationCredentialPolicyInput{ConnectionRef: ordinary.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}).Connection
	ordinary = execute(command.SetConnectionEnabled, owner, "ordinary-enable", &ordinary.Version, command.ConnectionInput{Ref: ordinary.Ref, Enabled: true}).Connection
	payload := command.IntegrationGrantInput{ConnectionRef: ordinary.Ref, AgentRef: agent.Ref, CapabilityKey: "gitlab.project.metadata.read", Enabled: true, ApprovalPolicy: "NONE"}
	ordinary = execute(command.ChangeIntegrationGrant, owner, "ordinary-grant", &ordinary.Version, payload).Connection
	if len(ordinary.Grants) != 1 {
		t.Fatal("ordinary scalar grant was not created")
	}
	ordinaryRef := ordinary.Grants[0].Ref
	ordinary = execute(command.ChangeIntegrationGrant, owner, "ordinary-upsert", &ordinary.Version, payload).Connection
	if len(ordinary.Grants) != 1 || ordinary.Grants[0].Ref != ordinaryRef {
		t.Fatal("Kubernetes multi-scope changed ordinary uniqueness")
	}
	payload.KubernetesNamespace = "alpha"
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "kube-grants-ordinary-extra", ExpectedVersion: &ordinary.Version}, Payload: payload}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("ordinary adapter accepted a Kubernetes scope field", err)
	}
}
