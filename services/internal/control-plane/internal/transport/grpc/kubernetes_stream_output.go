package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReadManagedKubernetesStreamOutput exposes one exact owner-disclosed frame and rejects unknown request fields.
// ReadManagedKubernetesStreamOutput выдаёт один exact owner-disclosed frame и отклоняет неизвестные поля запроса.
func (s *Server) ReadManagedKubernetesStreamOutput(ctx context.Context, req *cp.ReadManagedKubernetesStreamOutputRequest) (*cp.ReadManagedKubernetesStreamOutputResponse, error) {
	if !managedKubernetesWireValid(req) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesStreamOutputService_ReadManagedKubernetesStreamOutput_FullMethodName, func(p value.Principal) (*cp.ReadManagedKubernetesStreamOutputResponse, error) {
		v, err := s.service.ReadKubernetesStreamOutput(ctx, p, query.KubernetesStreamOutputRead{InvocationRef: req.GetInvocationRef(), StreamRef: req.GetStreamRef(), Generation: req.GetGeneration(), AfterSequence: req.GetAfterSequence()})
		if err != nil {
			return nil, err
		}
		status, known := cp.ManagedKubernetesStreamOutputStatus_value["MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_"+v.Status]
		if !known || status == 0 || !v.Valid() {
			return nil, errs.ErrUnavailable
		}
		result := &cp.ReadManagedKubernetesStreamOutputResponse{InvocationRef: v.InvocationRef, StreamRef: v.StreamRef, Generation: v.Generation, AfterSequence: v.AfterSequence, OutputWatermark: v.OutputWatermark, Status: cp.ManagedKubernetesStreamOutputStatus(status), AuthorizedUntil: timestamppb.New(v.AuthorizedUntil), NativeTerminalKnown: v.NativeTerminalKnown, TransportJoined: v.TransportJoined, DroppedBytes: v.DroppedBytes}
		if v.Frame != nil {
			result.Frame = &cp.ManagedKubernetesStreamOutputFrame{StageRef: v.Frame.StageRef, Sequence: v.Frame.Sequence, Channel: v.Frame.Channel, PayloadSha256: v.Frame.Digest, Payload: v.Frame.Payload}
		}
		return result, nil
	})
}
