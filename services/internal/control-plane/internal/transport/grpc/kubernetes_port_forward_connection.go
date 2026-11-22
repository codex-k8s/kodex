package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesPortForwardConnectionViewToProto validates safe owner coordinates and separate local join facts without inferring remote success.
// kubernetesPortForwardConnectionViewToProto проверяет safe owner coordinates и отдельные local join facts без вывода remote success.
func kubernetesPortForwardConnectionViewToProto(input entity.KubernetesPortForwardConnection) (*cp.ManagedKubernetesPortForwardConnectionView, error) {
	state, known := cp.ManagedKubernetesPortForwardConnectionState_value["MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_"+string(input.State)]
	deadline := managedKubernetesTimestamp(input.HardDeadline)
	active := input.State == "PREPARED" || input.State == "OPENING" || input.State == "OPEN"
	if !known || state == 0 || !managedkubernetes.ValidPortForwardConnectionRef(input.Ref) || !kubernetesInputPublicRef(input.InvocationRef, "inv_") || !kubernetesInputPublicRef(input.StreamRef, "kst_") || !validMaterialPublicVersion(input.Generation) || !validMaterialPublicVersion(input.StreamGeneration) || !validMaterialPublicVersion(input.InputVersion) || input.Slot >= managedkubernetes.MaximumForwardPorts || deadline == nil || input.NativeJoined && active || input.ReasonCode != "" && !managedkubernetes.ValidReasonCode(input.ReasonCode) {
		return nil, errs.ErrUnavailable
	}
	return &cp.ManagedKubernetesPortForwardConnectionView{ConnectionRef: input.Ref, InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, StreamGeneration: input.StreamGeneration, Generation: input.Generation, Slot: input.Slot, State: cp.ManagedKubernetesPortForwardConnectionState(state), InputVersion: input.InputVersion, LocalWriteClosed: input.LocalWriteClosed, ReadEndObserved: input.ReadEndObserved, NativeJoined: input.NativeJoined, HardDeadline: deadline, ReasonCode: input.ReasonCode}, nil
}

// kubernetesPortForwardReadbackToProto rejects mixed parent/incarnation metadata before returning a typed OPEN/CLOSE receipt.
// kubernetesPortForwardReadbackToProto отклоняет mixed parent/incarnation metadata до возврата typed OPEN/CLOSE receipt.
func kubernetesPortForwardReadbackToProto(input entity.KubernetesPortForwardConnectionReadback, inv, stream string) (*cp.ManagedKubernetesPortForwardConnectionView, *cp.ManagedKubernetesStreamInputView, *cp.ManagedKubernetesStreamInputSubmission, error) {
	connection, err := kubernetesPortForwardConnectionViewToProto(input.Connection)
	if err != nil || connection.InvocationRef != inv || connection.StreamRef != stream || input.Input == nil {
		return nil, nil, nil, errs.ErrUnavailable
	}
	parent, submission, err := kubernetesStreamInputReadbackToProto(entity.KubernetesStreamInputReadback{View: *input.Input, Submission: input.Submission}, inv, stream)
	if err != nil || parent.Operation != "kubernetes.port-forward" || parent.Generation != connection.StreamGeneration || parent.Version != connection.InputVersion || !parent.HardDeadline.AsTime().Equal(connection.HardDeadline.AsTime()) || submission != nil && submission.Channel != "PORT_FORWARD" {
		return nil, nil, nil, errs.ErrUnavailable
	}
	return connection, parent, submission, nil
}

// OpenManagedKubernetesPortForwardConnection requires explicit slot presence and delegates owner-assigned incarnation/idempotency to the existing original-root boundary.
// OpenManagedKubernetesPortForwardConnection требует explicit slot presence и передаёт owner-assigned incarnation/idempotency existing original-root boundary.
func (server *Server) OpenManagedKubernetesPortForwardConnection(ctx context.Context, request *cp.OpenManagedKubernetesPortForwardConnectionRequest) (*cp.OpenManagedKubernetesPortForwardConnectionResponse, error) {
	if !managedKubernetesWireValid(request) || request.Slot == nil || request.GetSlot() >= managedkubernetes.MaximumForwardPorts {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesPortForwardConnectionService_OpenManagedKubernetesPortForwardConnection_FullMethodName, func(p value.Principal) (*cp.OpenManagedKubernetesPortForwardConnectionResponse, error) {
		result, err := server.service.OpenKubernetesPortForwardConnection(ctx, p, mutation(request.GetMutation()), command.KubernetesPortForwardConnectionOpen{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef(), Slot: request.GetSlot()})
		if err != nil {
			return nil, err
		}
		connection, parent, submission, err := kubernetesPortForwardReadbackToProto(result, request.GetInvocationRef(), request.GetStreamRef())
		if err != nil || submission == nil || connection.Slot != request.GetSlot() {
			return nil, errs.ErrUnavailable
		}
		return &cp.OpenManagedKubernetesPortForwardConnectionResponse{Connection: connection, Input: parent, Submission: submission}, nil
	})
}

// GetManagedKubernetesPortForwardConnection reconciles either one exact locator or the original Open key without reissuing any private bytes.
// GetManagedKubernetesPortForwardConnection сверяет exact locator либо original Open key без повторной выдачи private bytes.
func (server *Server) GetManagedKubernetesPortForwardConnection(ctx context.Context, request *cp.GetManagedKubernetesPortForwardConnectionRequest) (*cp.GetManagedKubernetesPortForwardConnectionResponse, error) {
	if !managedKubernetesWireValid(request) || request.GetSelector() == nil {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesPortForwardConnectionService_GetManagedKubernetesPortForwardConnection_FullMethodName, func(p value.Principal) (*cp.GetManagedKubernetesPortForwardConnectionResponse, error) {
		result, err := server.service.GetKubernetesPortForwardConnection(ctx, p, query.KubernetesPortForwardConnectionRead{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef(), ConnectionRef: request.GetConnectionRef(), OpenIdempotencyKey: request.GetOpenIdempotencyKey()})
		if err != nil {
			return nil, err
		}
		connection, _, submission, err := kubernetesPortForwardReadbackToProto(result, request.GetInvocationRef(), request.GetStreamRef())
		if err != nil || request.GetConnectionRef() != "" && request.GetConnectionRef() != connection.ConnectionRef {
			return nil, errs.ErrUnavailable
		}
		return &cp.GetManagedKubernetesPortForwardConnectionResponse{Connection: connection, Submission: submission}, nil
	})
}

// ListManagedKubernetesPortForwardConnections preserves bounded original-stream discovery/history and explicit capacity without a scalar-first selector.
// ListManagedKubernetesPortForwardConnections сохраняет bounded original-stream discovery/history и explicit capacity без scalar-first selector.
func (server *Server) ListManagedKubernetesPortForwardConnections(ctx context.Context, request *cp.ListManagedKubernetesPortForwardConnectionsRequest) (*cp.ListManagedKubernetesPortForwardConnectionsResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	size := int32(16)
	token := ""
	if request.GetPage() != nil {
		size = request.Page.PageSize
		token = request.Page.PageToken
		if size == 0 {
			size = 16
		}
	}
	return queryResponse(ctx, cp.ManagedKubernetesPortForwardConnectionService_ListManagedKubernetesPortForwardConnections_FullMethodName, func(p value.Principal) (*cp.ListManagedKubernetesPortForwardConnectionsResponse, error) {
		result, err := server.service.ListKubernetesPortForwardConnections(ctx, p, query.KubernetesPortForwardConnectionList{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef(), Page: query.Page{Size: size, Token: token}})
		if err != nil {
			return nil, err
		}
		profile := result.Profile
		if size < 1 || size > 32 || len(result.Items) > int(size) || result.Next != "" && !managedkubernetes.ValidPortForwardConnectionRef(result.Next) || profile.MaximumActiveConnections != managedkubernetes.MaximumPortForwardConnections || profile.ActiveConnections > profile.MaximumActiveConnections || profile.AdmissionReasonCode != "" && !managedkubernetes.ValidReasonCode(profile.AdmissionReasonCode) || profile.Protocol != "" && profile.Protocol != string(managedkubernetes.PortForwardSPDYV1) && profile.Protocol != string(managedkubernetes.PortForwardWebSocketV4) || profile.Protocol != string(managedkubernetes.PortForwardSPDYV1) && profile.AdmissionReasonCode == "" {
			return nil, errs.ErrUnavailable
		}
		response := &cp.ListManagedKubernetesPortForwardConnectionsResponse{Items: make([]*cp.ManagedKubernetesPortForwardConnectionView, 0, len(result.Items)), Page: &cp.PageInfo{NextPageToken: result.Next}, Profile: &cp.ManagedKubernetesPortForwardConnectionProfile{Protocol: profile.Protocol, MaximumActiveConnections: profile.MaximumActiveConnections, ActiveConnections: profile.ActiveConnections, AdmissionReasonCode: profile.AdmissionReasonCode}}
		approved := managedkubernetes.PortForwardProfile{Ports: profile.Ports, TimeoutSeconds: profile.TimeoutSeconds}
		if approved.Validate() != nil {
			return nil, errs.ErrUnavailable
		}
		response.Profile.Ports = make([]*cp.ManagedKubernetesPortForwardMapping, 0, len(profile.Ports))
		response.Profile.TimeoutSeconds = profile.TimeoutSeconds
		for _, port := range profile.Ports {
			response.Profile.Ports = append(response.Profile.Ports, &cp.ManagedKubernetesPortForwardMapping{Port: uint32(port.Port), TargetPort: uint32(port.TargetPort)})
		}
		seen := make(map[string]bool, len(result.Items))
		for _, item := range result.Items {
			view, err := kubernetesPortForwardConnectionViewToProto(item)
			if err != nil || view.InvocationRef != request.GetInvocationRef() || view.StreamRef != request.GetStreamRef() || seen[view.ConnectionRef] || int(view.Slot) >= len(profile.Ports) {
				return nil, errs.ErrUnavailable
			}
			seen[view.ConnectionRef] = true
			response.Items = append(response.Items, view)
		}
		return response, nil
	})
}

// CloseManagedKubernetesPortForwardConnection permits exact owner closure but never manufactures native join or cancels sibling connections.
// CloseManagedKubernetesPortForwardConnection разрешает exact owner closure, не выдумывая native join и не отменяя sibling connections.
func (server *Server) CloseManagedKubernetesPortForwardConnection(ctx context.Context, request *cp.CloseManagedKubernetesPortForwardConnectionRequest) (*cp.CloseManagedKubernetesPortForwardConnectionResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesPortForwardConnectionService_CloseManagedKubernetesPortForwardConnection_FullMethodName, func(p value.Principal) (*cp.CloseManagedKubernetesPortForwardConnectionResponse, error) {
		result, err := server.service.CloseKubernetesPortForwardConnection(ctx, p, mutation(request.GetMutation()), command.KubernetesPortForwardConnectionClose{InvocationRef: request.GetInvocationRef(), StreamRef: request.GetStreamRef(), ConnectionRef: request.GetConnectionRef()})
		if err != nil {
			return nil, err
		}
		connection, parent, submission, err := kubernetesPortForwardReadbackToProto(result, request.GetInvocationRef(), request.GetStreamRef())
		if err != nil || connection.ConnectionRef != request.GetConnectionRef() || connection.State == cp.ManagedKubernetesPortForwardConnectionState_MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_PREPARED || connection.State == cp.ManagedKubernetesPortForwardConnectionState_MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_OPENING || connection.State == cp.ManagedKubernetesPortForwardConnectionState_MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_OPEN {
			return nil, errs.ErrUnavailable
		}
		return &cp.CloseManagedKubernetesPortForwardConnectionResponse{Connection: connection, Input: parent, Submission: submission}, nil
	})
}
