package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimManagedKubernetesInvocations projects only the dedicated owner's long-operation claims.
// The ordinary adapter worker cannot acquire these leases or interpret a stream as a completed tool.
// ClaimManagedKubernetesInvocations проецирует лишь dedicated owner long-operation claims.
// Ordinary adapter worker не получает эти leases и не принимает stream за completed tool.
func (server *Server) ClaimManagedKubernetesInvocations(ctx context.Context, request *cp.ClaimManagedKubernetesInvocationsRequest) (*cp.ClaimManagedKubernetesInvocationsResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInvocations_FullMethodName, func(p value.Principal) (*cp.ClaimManagedKubernetesInvocationsResponse, error) {
		items, err := server.service.ClaimManagedKubernetesInvocations(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
		if err != nil {
			return nil, err
		}
		result := &cp.ClaimManagedKubernetesInvocationsResponse{}
		for _, item := range items {
			claim := CastIntegrationInvocationClaim(item)
			if claim.GetDefinitionKey() != "kubernetes-managed" || claim.GetCredentialRevision() != nil || claim.GetCredentialMaterializationRef() != "" || claim.GetCredentialAdmissionRequired() || claim.GetLease() == nil {
				return nil, errs.ErrUnavailable
			}
			result.Claims = append(result.Claims, claim)
		}
		return result, nil
	})
}

// RenewManagedKubernetesInvocation returns only the same generation after fresh owner checks.
// A response never issues a new attempt, changes input or silently restarts an external stream.
// RenewManagedKubernetesInvocation возвращает лишь то же generation после fresh owner checks.
// Response не создаёт новую attempt, не меняет input и не перезапускает external stream.
func (server *Server) RenewManagedKubernetesInvocation(ctx context.Context, request *cp.RenewManagedKubernetesInvocationRequest) (*cp.RenewManagedKubernetesInvocationResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName, func(p value.Principal) (*cp.RenewManagedKubernetesInvocationResponse, error) {
		result, err := server.service.RenewManagedKubernetesInvocation(ctx, p, managedKubernetesBindingFromProto(request.GetBinding()))
		if err != nil {
			return nil, err
		}
		expires := managedKubernetesTimestamp(result.ExpiresAt)
		if !managedkubernetes.ValidLocator(result.Ref) || result.Ref != request.GetBinding().GetLeaseRef() || result.Generation != request.GetBinding().GetGeneration() || expires == nil {
			return nil, errs.ErrUnavailable
		}
		return &cp.RenewManagedKubernetesInvocationResponse{LeaseRef: result.Ref, Generation: result.Generation, ExpiresAt: expires}, nil
	})
}
