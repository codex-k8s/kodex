package httptransport

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// kubernetesPortForwardOpenBody requires explicit nonnull slot presence; no caller target, generation or native envelope is accepted here.
// kubernetesPortForwardOpenBody требует explicit nonnull slot presence; caller target, generation либо native envelope здесь не принимаются.
type kubernetesPortForwardOpenBody struct {
	Slot *uint32 `json:"slot"`
}

// kubernetesPortForwardClientAvailable closes missing composition before an RPC without substituting a generic tool or artifact caller.
// kubernetesPortForwardClientAvailable закрывает missing composition до RPC без подстановки generic tool либо artifact caller.
func (server *Server) kubernetesPortForwardClientAvailable(w http.ResponseWriter) bool {
	if server.control == nil || server.control.KubernetesPortForward == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return false
	}
	return true
}

// kubernetesPortForwardPublicConnection validates owner coordinates and distinguishes owner closure from local positive join, never remote success.
// kubernetesPortForwardPublicConnection проверяет owner coordinates и отличает owner closure от local positive join, не remote success.
func kubernetesPortForwardPublicConnection(raw *cp.ManagedKubernetesPortForwardConnectionView) (generated.KubernetesPortForwardConnectionView, bool) {
	var empty generated.KubernetesPortForwardConnectionView
	if raw == nil {
		return empty, false
	}
	state := generated.KubernetesPortForwardConnectionViewState(strings.TrimPrefix(raw.State.String(), "MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_"))
	active := state == "PREPARED" || state == "OPENING" || state == "OPEN"
	if len(raw.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidPortForwardConnectionRef(raw.ConnectionRef) || !validKubernetesInputHTTPRef(raw.InvocationRef, "inv_") || !validKubernetesInputHTTPRef(raw.StreamRef, "kst_") || !validManagedVersion(raw.Generation) || !validManagedVersion(raw.StreamGeneration) || !validManagedVersion(raw.InputVersion) || raw.Slot >= managedkubernetes.MaximumForwardPorts || !state.Valid() || raw.NativeJoined && active || raw.HardDeadline == nil || raw.HardDeadline.CheckValid() != nil || len(raw.HardDeadline.ProtoReflect().GetUnknown()) != 0 || raw.ReasonCode != "" && !managedkubernetes.ValidReasonCode(raw.ReasonCode) {
		return empty, false
	}
	return generated.KubernetesPortForwardConnectionView{ConnectionRef: raw.ConnectionRef, InvocationRef: raw.InvocationRef, StreamRef: raw.StreamRef, StreamGeneration: raw.StreamGeneration, Generation: raw.Generation, Slot: int(raw.Slot), State: state, InputVersion: raw.InputVersion, LocalWriteClosed: raw.LocalWriteClosed, ReadEndObserved: raw.ReadEndObserved, NativeJoined: raw.NativeJoined, HardDeadline: raw.HardDeadline.AsTime(), ReasonCode: raw.ReasonCode}, true
}

// kubernetesPortForwardPublicSubmission checks exact original stream metadata and the canonical control/data envelope budget without returning its bytes.
// kubernetesPortForwardPublicSubmission проверяет exact original stream metadata и canonical control/data envelope budget без возврата bytes.
func kubernetesPortForwardPublicSubmission(raw *cp.ManagedKubernetesStreamInputSubmission, connection generated.KubernetesPortForwardConnectionView) (*generated.KubernetesStreamInputSubmission, bool) {
	if raw != nil && (raw.EndOfInput || raw.ByteCount < managedkubernetes.PortForwardConnectionFrameHeaderBytes || raw.Channel != "PORT_FORWARD") {
		return nil, false
	}
	return kubernetesInputPublicSubmissionTuple(raw, connection.InvocationRef, connection.StreamRef, connection.StreamGeneration, managedkubernetes.MaximumFrameBytes, []generated.KubernetesStreamInputChannel{"PORT_FORWARD"})
}

// kubernetesPortForwardPublicMutation binds connection and submission metadata to the real current parent view rather than inferring execution readiness from OPEN alone.
// kubernetesPortForwardPublicMutation связывает connection/submission metadata с real current parent view без вывода execution readiness из одного OPEN.
func kubernetesPortForwardPublicMutation(raw *cp.ManagedKubernetesPortForwardConnectionView, parent *cp.ManagedKubernetesStreamInputView, submission *cp.ManagedKubernetesStreamInputSubmission, inv, stream string) (generated.KubernetesPortForwardConnectionClosed, bool) {
	var empty generated.KubernetesPortForwardConnectionClosed
	connection, ok := kubernetesPortForwardPublicConnection(raw)
	if !ok || connection.InvocationRef != inv || connection.StreamRef != stream {
		return empty, false
	}
	view, ok := kubernetesInputPublicView(parent)
	if !ok || view.InvocationRef != inv || view.StreamRef != stream || view.Operation != "kubernetes.port-forward" || view.Generation != connection.StreamGeneration || view.Version != connection.InputVersion || !view.HardDeadline.Equal(connection.HardDeadline) {
		return empty, false
	}
	stage, ok := kubernetesPortForwardPublicSubmission(submission, connection)
	if !ok {
		return empty, false
	}
	return generated.KubernetesPortForwardConnectionClosed{Connection: connection, Input: view, Submission: stage}, true
}

// kubernetesPortForwardMutation reads one bounded exact body after strict public headers; its result grants no actor or connection authority.
// kubernetesPortForwardMutation читает один bounded exact body после strict public headers; результат не выдаёт actor либо connection authority.
func kubernetesPortForwardMutation(w http.ResponseWriter, r *http.Request, key, etag string, body any) (*cp.MutationContext, bool) {
	if !validKubernetesInputHTTPKey(key) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	mutation, ok := requireMutation(w, key, etag)
	if !ok {
		return nil, false
	}
	if mutation.ExpectedVersion == nil || !validManagedVersion(*mutation.ExpectedVersion) || etag != "\""+strconv.FormatInt(*mutation.ExpectedVersion, 10)+"\"" || r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	defer clear(raw)
	if err != nil || len(raw) > 1024 || internalrpcauth.DecodeExactJSON(raw, body) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return mutation, true
}

// OpenManagedKubernetesPortForwardConnection submits one explicit slot and original OCC key; only the owner chooses the incarnation and protected OPEN bytes.
// OpenManagedKubernetesPortForwardConnection передаёт explicit slot и original OCC key; incarnation и protected OPEN bytes выбирает лишь owner.
func (server *Server) OpenManagedKubernetesPortForwardConnection(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, stream generated.KubernetesStreamInputRef, params generated.OpenManagedKubernetesPortForwardConnectionParams) {
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPRef(stream, "kst_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	var body kubernetesPortForwardOpenBody
	mutation, ok := kubernetesPortForwardMutation(w, r, params.IdempotencyKey, params.IfMatch, &body)
	if !ok {
		return
	}
	if body.Slot == nil || *body.Slot >= managedkubernetes.MaximumForwardPorts {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesPortForwardClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesPortForward.OpenManagedKubernetesPortForwardConnection(r.Context(), &cp.OpenManagedKubernetesPortForwardConnectionRequest{InvocationRef: inv, StreamRef: stream, Slot: body.Slot, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result, ok := kubernetesPortForwardPublicMutation(response.Connection, response.Input, response.Submission, inv, stream)
	if !ok || result.Submission == nil || result.Connection.Slot != int(*body.Slot) || result.Submission.ByteCount != managedkubernetes.PortForwardConnectionFrameHeaderBytes {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.Connection.InputVersion, 10)+"\"")
	writeJSON(w, http.StatusCreated, generated.KubernetesPortForwardConnectionOpened{Connection: result.Connection, Input: result.Input, Submission: *result.Submission})
}

// GetManagedKubernetesPortForwardConnection requires one exact selector and rechecks returned lineage before revealing metadata after lost Open ACK.
// GetManagedKubernetesPortForwardConnection требует exact selector и сверяет returned lineage до выдачи metadata после lost Open ACK.
func (server *Server) GetManagedKubernetesPortForwardConnection(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, stream generated.KubernetesStreamInputRef, params generated.GetManagedKubernetesPortForwardConnectionParams) {
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPRef(stream, "kst_") || (params.ConnectionRef == nil) == (params.OpenIdempotencyKey == nil) || params.ConnectionRef != nil && !managedkubernetes.ValidPortForwardConnectionRef(*params.ConnectionRef) || params.OpenIdempotencyKey != nil && !validKubernetesInputHTTPKey(*params.OpenIdempotencyKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesPortForwardClientAvailable(w) {
		return
	}
	request := &cp.GetManagedKubernetesPortForwardConnectionRequest{InvocationRef: inv, StreamRef: stream}
	if params.ConnectionRef != nil {
		request.Selector = &cp.GetManagedKubernetesPortForwardConnectionRequest_ConnectionRef{ConnectionRef: *params.ConnectionRef}
	} else {
		request.Selector = &cp.GetManagedKubernetesPortForwardConnectionRequest_OpenIdempotencyKey{OpenIdempotencyKey: *params.OpenIdempotencyKey}
	}
	response, err := server.control.KubernetesPortForward.GetManagedKubernetesPortForwardConnection(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	connection, ok := kubernetesPortForwardPublicConnection(response.Connection)
	if !ok || connection.InvocationRef != inv || connection.StreamRef != stream || params.ConnectionRef != nil && connection.ConnectionRef != *params.ConnectionRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	submission, ok := kubernetesPortForwardPublicSubmission(response.Submission, connection)
	if !ok || params.OpenIdempotencyKey != nil && (submission == nil || submission.ByteCount != managedkubernetes.PortForwardConnectionFrameHeaderBytes) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(connection.InputVersion, 10)+"\"")
	writeJSON(w, http.StatusOK, generated.KubernetesPortForwardConnectionReadback{Connection: connection, Submission: submission})
}

// ListManagedKubernetesPortForwardConnections validates bounded original-stream history, deterministic generations and explicit capacity without selecting a first connection for use.
// ListManagedKubernetesPortForwardConnections проверяет bounded original-stream history, deterministic generations и explicit capacity без выбора первого connection для использования.
func (server *Server) ListManagedKubernetesPortForwardConnections(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, stream generated.KubernetesStreamInputRef, params generated.ListManagedKubernetesPortForwardConnectionsParams) {
	size := 16
	token := ""
	if params.PageSize != nil {
		size = *params.PageSize
	}
	if params.PageToken != nil {
		token = *params.PageToken
	}
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPRef(stream, "kst_") || size < 1 || size > 32 || token != "" && !managedkubernetes.ValidPortForwardConnectionRef(token) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesPortForwardClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesPortForward.ListManagedKubernetesPortForwardConnections(r.Context(), &cp.ListManagedKubernetesPortForwardConnectionsRequest{InvocationRef: inv, StreamRef: stream, Page: &cp.PageRequest{PageSize: int32(size), PageToken: token}})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.Page == nil || len(response.Page.ProtoReflect().GetUnknown()) != 0 || response.Profile == nil || len(response.Profile.ProtoReflect().GetUnknown()) != 0 || len(response.Items) > size {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	profile := response.Profile
	protocol := generated.KubernetesPortForwardConnectionProfileProtocol(profile.Protocol)
	reason := profile.AdmissionReasonCode
	if !protocol.Valid() || profile.MaximumActiveConnections != managedkubernetes.MaximumPortForwardConnections || profile.ActiveConnections > profile.MaximumActiveConnections || reason != "" && reason != managedkubernetes.ReasonPortForwardProfileUnsupported && reason != managedkubernetes.ReasonPortForwardCapacity && reason != "KUBERNETES_RECONCILE_REQUIRED" || profile.Protocol == string(managedkubernetes.PortForwardWebSocketV4) && reason != managedkubernetes.ReasonPortForwardProfileUnsupported || profile.Protocol == "" && reason != "KUBERNETES_RECONCILE_REQUIRED" || profile.Protocol == string(managedkubernetes.PortForwardSPDYV1) && reason == managedkubernetes.ReasonPortForwardProfileUnsupported {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	if len(profile.Ports) < 1 || len(profile.Ports) > managedkubernetes.MaximumForwardPorts || profile.TimeoutSeconds < 1 || profile.TimeoutSeconds > 300 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	approved := managedkubernetes.PortForwardProfile{TimeoutSeconds: profile.TimeoutSeconds}
	mappings := make([]generated.KubernetesPortForwardMapping, 0, len(profile.Ports))
	for _, port := range profile.Ports {
		if port == nil || len(port.ProtoReflect().GetUnknown()) != 0 || port.Port < 1 || port.Port > 65535 || port.TargetPort < 1 || port.TargetPort > 65535 {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		approved.Ports = append(approved.Ports, managedkubernetes.ForwardPort{Port: uint16(port.Port), TargetPort: uint16(port.TargetPort)})
		mappings = append(mappings, generated.KubernetesPortForwardMapping{Port: int(port.Port), TargetPort: int(port.TargetPort)})
	}
	if approved.Validate() != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesPortForwardConnectionPage{Items: make([]generated.KubernetesPortForwardConnectionView, 0, len(response.Items)), NextPageToken: response.Page.NextPageToken, Profile: generated.KubernetesPortForwardConnectionProfile{Ports: mappings, TimeoutSeconds: int(profile.TimeoutSeconds), Protocol: protocol, MaximumActiveConnections: int(profile.MaximumActiveConnections), ActiveConnections: int(profile.ActiveConnections), AdmissionReasonCode: reason}}
	seen := make(map[string]bool, len(response.Items))
	var previous, generation int64
	for _, raw := range response.Items {
		item, ok := kubernetesPortForwardPublicConnection(raw)
		if !ok || item.InvocationRef != inv || item.StreamRef != stream || seen[item.ConnectionRef] || item.Slot >= len(profile.Ports) || item.Generation <= previous || generation != 0 && generation != item.StreamGeneration {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.ConnectionRef] = true
		previous, generation = item.Generation, item.StreamGeneration
		result.Items = append(result.Items, item)
	}
	if result.NextPageToken != "" && (len(result.Items) != size || !managedkubernetes.ValidPortForwardConnectionRef(result.NextPageToken) || result.NextPageToken != result.Items[len(result.Items)-1].ConnectionRef) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// CloseManagedKubernetesPortForwardConnection closes only the selected owner incarnation; absent submission/join facts never become a remote stop claim.
// CloseManagedKubernetesPortForwardConnection закрывает лишь selected owner incarnation; отсутствующие submission/join facts не становятся remote stop claim.
func (server *Server) CloseManagedKubernetesPortForwardConnection(w http.ResponseWriter, r *http.Request, inv generated.IntegrationInvocationRef, stream generated.KubernetesStreamInputRef, ref generated.KubernetesPortForwardConnectionRef, params generated.CloseManagedKubernetesPortForwardConnectionParams) {
	if !validKubernetesInputHTTPRef(inv, "inv_") || !validKubernetesInputHTTPRef(stream, "kst_") || !managedkubernetes.ValidPortForwardConnectionRef(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	var body struct{}
	mutation, ok := kubernetesPortForwardMutation(w, r, params.IdempotencyKey, params.IfMatch, &body)
	if !ok {
		return
	}
	if !server.kubernetesPortForwardClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesPortForward.CloseManagedKubernetesPortForwardConnection(r.Context(), &cp.CloseManagedKubernetesPortForwardConnectionRequest{InvocationRef: inv, StreamRef: stream, ConnectionRef: ref, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result, ok := kubernetesPortForwardPublicMutation(response.Connection, response.Input, response.Submission, inv, stream)
	if !ok || result.Connection.ConnectionRef != ref || result.Connection.State == "PREPARED" || result.Connection.State == "OPENING" || result.Connection.State == "OPEN" || result.Submission != nil && result.Submission.ByteCount != managedkubernetes.PortForwardConnectionFrameHeaderBytes {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.Connection.InputVersion, 10)+"\"")
	writeJSON(w, http.StatusOK, result)
}
