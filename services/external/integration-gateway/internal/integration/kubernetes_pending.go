package integration

import (
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// KubernetesPending preserves owner-issued continuation coordinates without granting a replay or output credit.
// GateRef and EffectRef are mutually exclusive; the original invocation binding remains with its consumer.
// KubernetesPending сохраняет owner-issued continuation coordinates без replay либо output credit.
// GateRef и EffectRef взаимоисключающие; original invocation binding остаётся у consumer.
type KubernetesPending struct {
	PlanRef, ChildRef, GateRef, EffectRef string
	EffectGeneration                      int64
}

// Error returns a fixed diagnostic without exposing private owner coordinates or the upstream message.
// Error возвращает fixed diagnostic без private owner coordinates либо upstream message.
func (*KubernetesPending) Error() string { return "managed Kubernetes invocation is pending" }

// kubernetesPendingFromError decodes only one exact typed owner detail; malformed or future shapes remain uncertain failures.
// It does not infer pending state from gRPC code or message text and never performs another request.
// kubernetesPendingFromError принимает лишь один exact typed owner detail; malformed/future shapes остаются uncertain failures.
// Pending не выводится из gRPC code либо message text; новый request не выполняется.
func kubernetesPendingFromError(err error) *KubernetesPending {
	reply, ok := status.FromError(err)
	if !ok || reply.Code() != codes.FailedPrecondition || len(reply.Proto().ProtoReflect().GetUnknown()) != 0 {
		return nil
	}
	details := reply.Details()
	if len(details) != 1 {
		return nil
	}
	info, ok := details[0].(*errdetails.ErrorInfo)
	if !ok || info.Domain != "kodex.control-plane" || len(info.ProtoReflect().GetUnknown()) != 0 ||
		len(reply.Proto().Details[0].ProtoReflect().GetUnknown()) != 0 {
		return nil
	}
	pending := &KubernetesPending{PlanRef: info.Metadata["planRef"], ChildRef: info.Metadata["childRef"]}
	if !managedkubernetes.ValidLocator(pending.PlanRef) || !managedkubernetes.ValidLocator(pending.ChildRef) {
		return nil
	}
	switch info.Reason {
	case "KUBERNETES_APPROVAL_REQUIRED":
		pending.GateRef = info.Metadata["gateRef"]
		if len(info.Metadata) != 3 || !managedkubernetes.ValidLocator(pending.GateRef) {
			return nil
		}
	case "KUBERNETES_RECONCILIATION_REQUIRED":
		pending.EffectRef = info.Metadata["effectRef"]
		generation := info.Metadata["effectGeneration"]
		var parseErr error
		pending.EffectGeneration, parseErr = strconv.ParseInt(generation, 10, 64)
		if len(info.Metadata) != 4 || !managedkubernetes.ValidLocator(pending.EffectRef) || parseErr != nil ||
			pending.EffectGeneration < 1 || strconv.FormatInt(pending.EffectGeneration, 10) != generation {
			return nil
		}
	default:
		return nil
	}
	return pending
}
