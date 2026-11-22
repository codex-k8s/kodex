package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// kubernetesBindingInvalid rejects malformed owner locators without private diagnostic detail.
	// kubernetesBindingInvalid отклоняет malformed owner locators без private diagnostic detail.
	kubernetesBindingInvalid = "managed Kubernetes binding is invalid"
	// kubernetesStreamRequestInvalid rejects a malformed stream envelope before owner or native I/O.
	// kubernetesStreamRequestInvalid отклоняет malformed stream envelope до owner либо native I/O.
	kubernetesStreamRequestInvalid = "managed Kubernetes stream request is invalid"

	// kubernetesResultUnavailable hides private execution diagnostics from RPC clients.
	// kubernetesResultUnavailable скрывает private execution diagnostics от RPC clients.
	kubernetesResultUnavailable = "managed Kubernetes result unavailable"
	// kubernetesOperationPathUnavailable is the closed refusal for an absent or failed consumer.
	// kubernetesOperationPathUnavailable закрыто отклоняет absent либо failed consumer.
	kubernetesOperationPathUnavailable = "managed Kubernetes operation path is unavailable"
)

// KubernetesOperations accepts only an original owner binding; selectors and credentials stay server-side.
// KubernetesOperations принимает лишь original owner binding; selectors и credentials остаются server-side.
type KubernetesOperations interface {
	// Execute returns exact owner-admitted result bytes or typed pending metadata without replay.
	// Execute возвращает exact owner-admitted result bytes либо typed pending metadata без replay.
	Execute(context.Context, managedkubernetes.Binding) (targetrepo.OperationExecution, error)
}

// WithKubernetesOperations connects the managed operation consumer without altering health or ordinary Secrets.
// WithKubernetesOperations подключает managed operation consumer без изменения health либо ordinary Secrets.
func WithKubernetesOperations(operations KubernetesOperations) Option {
	return func(server *Server) { server.kubernetesOperations = operations }
}

// KubernetesHealth admits an existing owner health claim without a credential selector in transport.
// KubernetesHealth допускает existing owner health claim без credential selector в transport.
type KubernetesHealth interface {
	// Execute joins owner completion before returning the safe current authentication result.
	// Execute завершает owner completion до выдачи safe current authentication result.
	Execute(context.Context, managedkubernetes.Binding) (targetrepo.IdentityHealthReceipt, error)
}

// WithKubernetesHealth installs the purpose-specific consumer without altering ordinary Secret paths.
// WithKubernetesHealth подключает purpose-specific consumer без изменения ordinary Secret paths.
func WithKubernetesHealth(health KubernetesHealth) Option {
	return func(server *Server) { server.kubernetesHealth = health }
}

// managedKubernetesAuthority requires the exact admitted gateway method independently of owner locators.
// mTLS, bearer and replay verification remain mandatory middleware prerequisites.
// managedKubernetesAuthority требует exact admitted gateway method независимо от owner locators.
// mTLS, bearer и replay verification остаются обязательными middleware prerequisites.
func managedKubernetesAuthority(ctx context.Context, method, operation string) error {
	const gatewaySPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"
	if admission, ok := serviceidentity.FromContext(ctx); ok && admission.RPCProfile == transportprofile.TrustedCluster {
		if admission.Peer.SPIFFEID == gatewaySPIFFE && admission.TargetSPIFFEID == secretBrokerSPIFFEID &&
			admission.FullMethod == method && admission.OperationID == operation && admission.Permission == operation {
			return nil
		}
		return status.Error(codes.PermissionDenied, "managed Kubernetes caller rejected")
	}
	_, _, err := verifiedProjectionAuthority(ctx, "integration-gateway", gatewaySPIFFE, method, operation)
	return err
}

// ExecuteManagedKubernetesOperation dispatches only an owner-issued binding and never reads agent selectors.
// Health completion is final in its owner; resource effects use the separate operation consumer.
// ExecuteManagedKubernetesOperation принимает лишь owner-issued binding без agent selectors.
// Health completion окончателен в owner; resource effects используют отдельный operation consumer.
func (server *Server) ExecuteManagedKubernetesOperation(ctx context.Context, request *sb.ExecuteManagedKubernetesOperationRequest) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	if err := managedKubernetesAuthority(ctx, sb.ManagedKubernetesService_ExecuteManagedKubernetesOperation_FullMethodName, "kubernetes.execute"); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || request.GetBinding() == nil || len(request.GetBinding().ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, kubernetesBindingInvalid)
	}
	input := request.GetBinding()
	binding := managedkubernetes.Binding{WorkloadInstance: input.GetWorkloadInstance(), InvocationRef: input.GetInvocationRef(), ConnectionTestRef: input.GetConnectionTestRef(),
		LeaseRef: input.GetLeaseRef(), Fence: input.GetFence(), Generation: input.GetGeneration(), InputDigest: input.GetInputDigest()}
	if binding.Validate() != nil {
		return nil, status.Error(codes.InvalidArgument, kubernetesBindingInvalid)
	}
	if binding.InvocationRef != "" {
		if server.kubernetesOperations == nil {
			return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
		}
		result, err := server.kubernetesOperations.Execute(ctx, binding)
		if result.Effect.Result != nil {
			defer clear(result.Effect.Result.ResultJSON)
		}
		if err != nil {
			if errors.Is(err, targetrepo.ErrPortForwardProfileUnsupported) {
				rejected := status.New(codes.FailedPrecondition, "managed Kubernetes operation preflight rejected")
				detailed, detailErr := rejected.WithDetails(&errdetails.ErrorInfo{Domain: "kodex.secret-broker", Reason: managedkubernetes.ReasonPortForwardProfileUnsupported})
				if detailErr == nil {
					return nil, detailed.Err()
				}
			}
			return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
		}
		if result.Stream != nil {
			return managedKubernetesStreamResponse(ctx, binding, result)
		}
		return managedKubernetesResourceResponse(ctx, binding, result.Effect)
	}
	if server.kubernetesHealth == nil {
		return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
	}
	receipt, err := server.kubernetesHealth.Execute(ctx, binding)
	if err != nil || !receipt.Applied || receipt.TestRef != binding.ConnectionTestRef || (receipt.State != "SUCCEEDED" && receipt.State != "FAILED") {
		return nil, status.Error(codes.Unavailable, "managed Kubernetes health unavailable")
	}
	result, _ := json.Marshal(struct {
		Authenticated            bool   `json:"authenticated"`
		ResourcePermissionChecks string `json:"resourcePermissionChecks"`
	}{Authenticated: receipt.State == "SUCCEEDED", ResourcePermissionChecks: "PER_OPERATION_REQUIRED"})
	digest := sha256.Sum256(result)
	completed := &sb.ManagedKubernetesSafeResult{EffectRef: receipt.TestRef, ResultDigest: hex.EncodeToString(digest[:]), ResultJson: result,
		RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED}
	if receipt.State != "SUCCEEDED" {
		completed.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED
		completed.ReasonCode = "HEALTH_CHECK_FAILED"
	}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Completed{Completed: completed}}, nil
}

// managedKubernetesResourceResponse refuses stale data even after domain execution returned successfully.
// Pending gates preserve only exact plan/child metadata and never masquerade as completed effects.
// managedKubernetesResourceResponse отклоняет stale data даже после успешного domain execution.
// Pending gates сохраняют лишь exact plan/child metadata и не выглядят completed effects.
func managedKubernetesResourceResponse(ctx context.Context, binding managedkubernetes.Binding, result targetrepo.ResourceExecution) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	if ctx.Err() != nil || !managedkubernetes.ValidLocator(result.PlanRef) || !managedkubernetes.ValidLocator(result.ChildRef) {
		return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
	}
	if result.GateRef != "" {
		if !managedkubernetes.ValidLocator(result.GateRef) || result.Result != nil || result.EffectRef != "" || result.EffectGeneration != 0 || result.ReconcileOnly || !result.AuthorizedUntil.IsZero() {
			return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
		}
		rejection := status.New(codes.FailedPrecondition, "managed Kubernetes approval required")
		withDetails, err := rejection.WithDetails(&errdetails.ErrorInfo{Reason: "KUBERNETES_APPROVAL_REQUIRED", Domain: "kodex.control-plane", Metadata: map[string]string{"gateRef": result.GateRef, "planRef": result.PlanRef, "childRef": result.ChildRef}})
		if err != nil {
			return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
		}
		return nil, withDetails.Err()
	}
	if !managedkubernetes.ValidLocator(result.EffectRef) || result.EffectGeneration < 1 {
		return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
	}
	if result.Result == nil {
		if result.ReconcileOnly && result.AuthorizedUntil.IsZero() {
			rejection := status.New(codes.FailedPrecondition, "managed Kubernetes effect reconciliation required")
			withDetails, err := rejection.WithDetails(&errdetails.ErrorInfo{Reason: "KUBERNETES_RECONCILIATION_REQUIRED", Domain: "kodex.control-plane", Metadata: map[string]string{
				"planRef": result.PlanRef, "childRef": result.ChildRef, "effectRef": result.EffectRef, "effectGeneration": strconv.FormatInt(result.EffectGeneration, 10),
			}})
			if err != nil {
				return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
			}
			return nil, withDetails.Err()
		}
		return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
	}
	accepted := result.Result
	now := time.Now()
	if !result.AuthorizedUntil.After(now) || result.AuthorizedUntil.After(now.Add(30*time.Second)) || managedkubernetes.ValidateEffectResult(accepted.ResultJSON, accepted.ResultDigest, accepted.Outcome, accepted.ReasonCode) != nil {
		return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
	}
	outcome := sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN
	switch accepted.Outcome {
	case "SUCCEEDED":
		outcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED
	case "FAILED":
		outcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED
	}
	completed := &sb.ManagedKubernetesSafeResult{EffectRef: result.EffectRef, EffectGeneration: result.EffectGeneration, InvocationGeneration: binding.Generation, ResultDigest: accepted.ResultDigest, ResultJson: bytes.Clone(accepted.ResultJSON), RemoteOutcome: outcome, ReasonCode: accepted.ReasonCode, AuthorizedUntil: timestamppb.New(result.AuthorizedUntil)}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Completed{Completed: completed}}, nil
}
