package integration

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// KubernetesPreflightRejection is a closed unsupported PF profile notification, never proof of a native effect or owner failure.
// KubernetesPreflightRejection — закрытое уведомление unsupported PF profile, не proof native effect либо owner failure.
type KubernetesPreflightRejection struct{}

// Error returns a fixed bounded diagnostic; authoritative state comes from the owner's independently checked completion.
// Error возвращает fixed bounded diagnostic; authoritative state приходит из independently checked owner completion.
func (*KubernetesPreflightRejection) Error() string {
	return managedkubernetes.ReasonPortForwardProfileUnsupported
}

// kubernetesPreflightFromError accepts one exact broker-only empty-metadata detail, never raw diagnostics or unknown protocol states.
// kubernetesPreflightFromError принимает exact broker-only empty-metadata detail, не raw diagnostics либо unknown protocol states.
func kubernetesPreflightFromError(err error) *KubernetesPreflightRejection {
	reply, ok := status.FromError(err)
	if !ok || reply.Code() != codes.FailedPrecondition || len(reply.Proto().ProtoReflect().GetUnknown()) != 0 || len(reply.Proto().Details) != 1 || len(reply.Proto().Details[0].ProtoReflect().GetUnknown()) != 0 {
		return nil
	}
	details := reply.Details()
	if len(details) != 1 {
		return nil
	}
	info, ok := details[0].(*errdetails.ErrorInfo)
	if !ok || info.Domain != "kodex.secret-broker" || info.Reason != managedkubernetes.ReasonPortForwardProfileUnsupported || len(info.Metadata) != 0 || len(info.ProtoReflect().GetUnknown()) != 0 {
		return nil
	}
	return &KubernetesPreflightRejection{}
}

// RejectKubernetesPortForwardPreflight submits one closed hint with original plan pins; only the owner derives whether the selected transport is unsupported and no effects exist.
// An uncertain RPC is never replayed; callers may perform one original-binding metadata Get instead.
// RejectKubernetesPortForwardPreflight передаёт closed hint с original plan pins; только owner выводит unsupported selected transport и отсутствие effects.
// Uncertain RPC не повторяется; caller может выполнить один original-binding metadata Get.
func RejectKubernetesPortForwardPreflight(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, observed *cp.ManagedKubernetesInvocationReceipt, owner cp.ManagedKubernetesOwnerServiceClient) (*cp.ManagedKubernetesInvocationReceipt, error) {
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil || !validKubernetesInvocationReceipt(observed, binding) || observed.PlanRef == "" || observed.CompletionRecorded || observed.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	call, cancel := context.WithTimeout(ctx, kubernetesOwnerTimeout)
	defer cancel()
	response, err := owner.CompleteManagedKubernetesInvocation(call, &cp.CompleteManagedKubernetesInvocationRequest{Binding: wire, PlanRef: observed.PlanRef, PlanRevision: observed.PlanRevision, PlanDigest: observed.PlanDigest, PreflightRejection: cp.ManagedKubernetesPreflightRejection_MANAGED_KUBERNETES_PREFLIGHT_REJECTION_PORT_FORWARD_PROFILE_UNSUPPORTED})
	if err != nil || call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInvocationReceipt(response.Receipt, binding) || !response.Receipt.CompletionRecorded || response.Receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED || response.Receipt.PlanRef != observed.PlanRef || response.Receipt.PlanRevision != observed.PlanRevision || response.Receipt.PlanDigest != observed.PlanDigest || response.Receipt.SnapshotDigest != observed.SnapshotDigest {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return proto.Clone(response.Receipt).(*cp.ManagedKubernetesInvocationReceipt), nil
}
