package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgtype"
)

// assertKubernetesOuterCompletion crosses the production registry/service/real owner boundary and simulates
// lost completion ACK by reading and replaying the exact stored receipt without another claim or effect.
// assertKubernetesOuterCompletion проходит production registry/service/real owner boundary и имитирует
// lost completion ACK чтением и повтором exact stored receipt без нового claim либо effect.
func assertKubernetesOuterCompletion(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, gateway, broker value.Principal, claim func(string, string, map[string]any) value.ManagedKubernetesBinding, candidate managedkubernetes.ChildCandidate) value.ManagedKubernetesBinding {
	t.Helper()
	// registeredGateway uses the actual operation map, never fixture-invented permission strings.
	// registeredGateway использует actual operation map, не придуманные fixture permission strings.
	registeredGateway := func(method string) value.Principal {
		p := gateway
		p.Permission = ""
		for operation, registered := range controlplaneclient.IntegrationGatewayOperations() {
			if registered == method {
				if p.Permission != "" {
					t.Fatal("duplicate owner operation")
				}
				p.Permission = operation
			}
		}
		if p.Permission == "" {
			t.Fatal("owner method absent from production registry")
		}
		return p
	}
	readCaller := registeredGateway(cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName)
	completeCaller := registeredGateway(cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName)
	input := map[string]any{"group": "", "version": "v1", "resource": "configmaps", "namespace": "review", "name": "settings"}
	binding := claim("kubernetes.get", "kube-outer-read", input)
	broker.Permission = "platform.runtime.kubernetes.resolve"
	snapshot, err := repository.ResolveManagedKubernetesOperation(ctx, broker, binding)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := managedkubernetes.ChildProposalDigest(snapshot.Digest, []managedkubernetes.ChildCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	broker.Permission = "platform.runtime.kubernetes.children.prepare"
	plan, err := repository.PrepareManagedKubernetesChildren(ctx, broker, binding, command.ManagedKubernetesPrepareChildren{SnapshotDigest: snapshot.Digest, ProposalDigest: proposal, Candidates: []managedkubernetes.ChildCandidate{candidate}})
	if err != nil {
		t.Fatal(err)
	}
	finish := command.ManagedKubernetesCompleteInvocation{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest}
	pending, err := service.CompleteManagedKubernetesInvocation(ctx, completeCaller, binding, finish)
	if err != nil || pending.State != enum.KubernetesInvocationRunning || pending.CompletionRecorded {
		t.Fatal("known pending plan was finalized", pending, err)
	}
	child := plan.Children[0]
	broker.Permission = "platform.runtime.kubernetes.effect.begin"
	attempt, err := repository.BeginManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesBeginEffect{PlanRef: plan.Ref, PlanRevision: plan.Revision, PlanDigest: plan.Digest, ChildRef: child.Ref, ChildInputDigest: child.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"metadata":{"name":"settings"},"data":{"mode":"bounded"}}`)
	sum := sha256.Sum256(raw)
	broker.Permission = "platform.runtime.kubernetes.effect.complete"
	_, err = repository.CompleteManagedKubernetesEffect(ctx, broker, binding, command.ManagedKubernetesCompleteEffect{EffectRef: attempt.Ref, EffectGeneration: attempt.Generation, ChildInputDigest: child.InputDigest, Outcome: enum.KubernetesEffectSucceeded, ResultJSON: raw, ResultDigest: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.GetManagedKubernetesInvocation(ctx, readCaller, binding)
	if err != nil || before.State != enum.KubernetesInvocationRunning || before.PlanRef != plan.Ref {
		t.Fatal("read owner-assigned plan", before, err)
	}
	wrong := finish
	wrong.PlanDigest = string(make([]byte, 64))
	if _, err = service.CompleteManagedKubernetesInvocation(ctx, completeCaller, binding, wrong); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("invalid complete pins accepted", err)
	}
	var versionBefore int64
	if repository.pool.QueryRow(ctx, "SELECT version FROM control_plane.integration_invocations WHERE ref=$1", binding.InvocationRef).Scan(&versionBefore) != nil {
		t.Fatal("read original version")
	}
	broken, fault := kubernetesAuthorityFaultRepository(t, ctx, repository, queryKubernetesInvocationCompletionChildren, pgtype.BoolOID)
	brokenService, err := platformservice.New(broken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = brokenService.CompleteManagedKubernetesInvocation(ctx, completeCaller, binding, finish); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
		t.Fatal("completion infrastructure failure became terminal", err)
	}
	var unchanged bool
	if repository.pool.QueryRow(ctx, "SELECT state='RUNNING' AND version=$2 AND effect_receipt_id IS NULL FROM control_plane.integration_invocations WHERE ref=$1", binding.InvocationRef, versionBefore).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("completion infrastructure failure changed owner state/version")
	}
	receipt, err := service.CompleteManagedKubernetesInvocation(ctx, completeCaller, binding, finish)
	if err != nil || receipt.State != enum.KubernetesInvocationSucceeded || !receipt.CompletionRecorded || receipt.ResultDigest != hex.EncodeToString(sum[:]) {
		t.Fatal("server-derived useful completion", receipt, err)
	}
	recovered, err := service.GetManagedKubernetesInvocation(ctx, readCaller, binding)
	if err != nil || recovered != receipt {
		t.Fatal("lost ACK readback changed stored completion", recovered, err)
	}
	replay, err := service.CompleteManagedKubernetesInvocation(ctx, completeCaller, binding, finish)
	if err != nil || replay != receipt {
		t.Fatal("exact terminal ACK was not idempotent", replay, err)
	}
	var stored string
	if repository.pool.QueryRow(ctx, "SELECT result_summary FROM control_plane.integration_invocations WHERE ref=$1", binding.InvocationRef).Scan(&stored) != nil || stored != string(raw) {
		t.Fatal("useful safe GET bytes lost in outer summary")
	}
	if _, err = repository.pool.Exec(ctx, "UPDATE control_plane.integration_invocations SET kubernetes_claimed_instance='foreign-gateway' WHERE ref=$1", binding.InvocationRef); err == nil {
		t.Fatal("runtime replaced durable original instance")
	}
	foreign := binding
	foreign.WorkloadInstance = "foreign-gateway"
	if _, err = service.GetManagedKubernetesInvocation(ctx, readCaller, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("terminal read accepted foreign original instance", err)
	}
	noPlan := claim("kubernetes.get", "kube-outer-no-plan", input)
	failed, err := service.CompleteManagedKubernetesInvocation(ctx, completeCaller, noPlan, command.ManagedKubernetesCompleteInvocation{})
	if err != nil || failed.State != enum.KubernetesInvocationFailed || !failed.CompletionRecorded || failed.PlanRef != "" {
		t.Fatal("zero-effect noPlan closure", failed, err)
	}
	return binding
}

// assertKubernetesTerminalReadbackAfterRevocation proves metadata recovery survives loss of delivery authority.
// assertKubernetesTerminalReadbackAfterRevocation доказывает metadata recovery после потери delivery authority.
func assertKubernetesTerminalReadbackAfterRevocation(t *testing.T, ctx context.Context, service *platformservice.Service, gateway value.Principal, binding value.ManagedKubernetesBinding) {
	t.Helper()
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName {
			gateway.Permission = operation
		}
	}
	receipt, err := service.GetManagedKubernetesInvocation(ctx, gateway, binding)
	if err != nil || receipt.State != enum.KubernetesInvocationSucceeded || !receipt.CompletionRecorded {
		t.Fatal("metadata ACK recovery was lost after revoke", receipt, err)
	}
}

// assertKubernetesOwnerUnrecordedClosure keeps pending/expired metadata distinct from recorded business success.
// assertKubernetesOwnerUnrecordedClosure отделяет pending/expired metadata от recorded business success.
func assertKubernetesOwnerUnrecordedClosure(t *testing.T, ctx context.Context, service *platformservice.Service, gateway value.Principal, binding value.ManagedKubernetesBinding, expected enum.ManagedKubernetesInvocationState) {
	t.Helper()
	for operation, method := range controlplaneclient.IntegrationGatewayOperations() {
		if method == cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName {
			gateway.Permission = operation
		}
	}
	receipt, err := service.GetManagedKubernetesInvocation(ctx, gateway, binding)
	if err != nil || receipt.State != expected || receipt.CompletionRecorded || receipt.ReceiptRef != "" || receipt.ResultDigest != "" {
		t.Fatal("unrecorded owner state became business success", receipt, err)
	}
}
