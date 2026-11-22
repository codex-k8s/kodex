package httptransport

import (
	"encoding/base64"
	"io"
	"net/http"
	"strconv"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// kubernetesInputHTTPBodyBytes bounds the private base64 envelope before any decoder or owner call.
// kubernetesInputHTTPBodyBytes ограничивает private base64 envelope до decoder либо owner call.
const kubernetesInputHTTPBodyBytes = 64 << 10

// kubernetesInputSubmitBody preserves explicit false/empty EOF fields and refuses null or omitted private input members.
// kubernetesInputSubmitBody сохраняет explicit false/empty EOF fields и отвергает null либо omitted private input members.
type kubernetesInputSubmitBody struct {
	Channel       *string `json:"channel"`
	PayloadBase64 *string `json:"payloadBase64"`
	PayloadDigest *string `json:"payloadDigest"`
	EndOfInput    *bool   `json:"endOfInput"`
}

// kubernetesInputClientAvailable keeps missing composition closed without falling back to a generic artifact or runtime caller.
// kubernetesInputClientAvailable закрывает missing composition без fallback к generic artifact либо runtime caller.
func (server *Server) kubernetesInputClientAvailable(w http.ResponseWriter) bool {
	if server.control == nil || server.control.KubernetesInputs == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return false
	}
	return true
}

// ListManagedKubernetesStreamInputs discovers a bounded exact-node page before terminal invocation receipts exist; common current root visibility and action-specific write authority remain owner resolved.
// ListManagedKubernetesStreamInputs обнаруживает bounded exact-node page до terminal invocation receipts; common current root visibility и action-specific write authority разрешает owner.
func (server *Server) ListManagedKubernetesStreamInputs(w http.ResponseWriter, r *http.Request, runRef generated.RunRef, nodeRef generated.OpaqueRef, params generated.ListManagedKubernetesStreamInputsParams) {
	size := 20
	if params.PageSize != nil {
		size = *params.PageSize
	}
	token := ""
	if params.PageToken != nil {
		token = *params.PageToken
	}
	if !validKubernetesInputHTTPRef(runRef, "run_") || !validKubernetesInputHTTPRef(nodeRef, "nod_") || size < 1 || size > 50 || !validKubernetesInputHTTPToken(token) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesInputClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesInputs.ListManagedKubernetesStreamInputs(r.Context(), &cp.ListManagedKubernetesStreamInputsRequest{RunRef: runRef, NodeRef: nodeRef, Page: &cp.PageRequest{PageSize: int32(size), PageToken: token}})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.Page == nil || len(response.Page.ProtoReflect().GetUnknown()) != 0 || len(response.Items) > size || !validKubernetesInputHTTPToken(response.Page.NextPageToken) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesStreamInputPage{Items: make([]generated.KubernetesStreamInputView, 0, len(response.Items)), NextPageToken: response.Page.NextPageToken}
	seen := make(map[string]bool, len(response.Items))
	for _, raw := range response.Items {
		view, valid := kubernetesInputPublicView(raw)
		if !valid || view.RunRef != runRef || view.NodeRef != nodeRef || seen[view.StreamRef] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[view.StreamRef] = true
		result.Items = append(result.Items, view)
	}
	writeJSON(w, http.StatusOK, result)
}

// GetManagedKubernetesStreamInput reads safe exact metadata, including one original submission key after a lost ACK; it never recovers private bytes or repeats a write.
// GetManagedKubernetesStreamInput читает safe exact metadata, включая original submission key после lost ACK; private bytes не восстанавливаются, write не повторяется.
func (server *Server) GetManagedKubernetesStreamInput(w http.ResponseWriter, r *http.Request, invocationRef generated.IntegrationInvocationRef, streamRef generated.KubernetesStreamInputRef, params generated.GetManagedKubernetesStreamInputParams) {
	key := ""
	if params.SubmissionIdempotencyKey != nil {
		key = *params.SubmissionIdempotencyKey
	}
	if !validKubernetesInputHTTPRef(invocationRef, "inv_") || !validKubernetesInputHTTPRef(streamRef, "kst_") || params.SubmissionIdempotencyKey != nil && !validKubernetesInputHTTPKey(key) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesInputClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesInputs.GetManagedKubernetesStreamInput(r.Context(), &cp.GetManagedKubernetesStreamInputRequest{InvocationRef: invocationRef, StreamRef: streamRef, SubmissionIdempotencyKey: key})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesInputPublicReadback(response.View, response.Submission, invocationRef, streamRef)
	if !valid || key == "" && view.Submission != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(view.View.Version, 10)+"\"")
	writeJSON(w, http.StatusOK, view)
}

// SubmitManagedKubernetesStreamInput forwards one exact bounded private body under owner OCC, clearing bytes after the synchronous RPC and never retrying payload transmission.
// SubmitManagedKubernetesStreamInput передаёт один exact bounded private body под owner OCC, очищает bytes после synchronous RPC и не повторяет передачу payload.
func (server *Server) SubmitManagedKubernetesStreamInput(w http.ResponseWriter, r *http.Request, invocationRef generated.IntegrationInvocationRef, streamRef generated.KubernetesStreamInputRef, params generated.SubmitManagedKubernetesStreamInputParams) {
	if !validKubernetesInputHTTPRef(invocationRef, "inv_") || !validKubernetesInputHTTPRef(streamRef, "kst_") || !validKubernetesInputHTTPKey(params.IdempotencyKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if mutation.ExpectedVersion == nil || !validManagedVersion(*mutation.ExpectedVersion) || params.IfMatch != "\""+strconv.FormatInt(*mutation.ExpectedVersion, 10)+"\"" {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, kubernetesInputHTTPBodyBytes+1))
	defer clear(raw)
	var body kubernetesInputSubmitBody
	if err != nil || len(raw) > kubernetesInputHTTPBodyBytes || internalrpcauth.DecodeExactJSON(raw, &body) != nil || body.Channel == nil || body.PayloadBase64 == nil || body.PayloadDigest == nil || body.EndOfInput == nil || len(*body.PayloadBase64) > 43692 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(*body.PayloadBase64)
	defer clear(payload)
	if err != nil || base64.StdEncoding.EncodeToString(payload) != *body.PayloadBase64 || managedkubernetes.ValidatePublicStreamInput(*body.Channel, payload, *body.PayloadDigest, *body.EndOfInput) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesInputClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesInputs.SubmitManagedKubernetesStreamInput(r.Context(), &cp.SubmitManagedKubernetesStreamInputRequest{InvocationRef: invocationRef, StreamRef: streamRef, Mutation: mutation, Channel: *body.Channel, Payload: payload, PayloadDigest: *body.PayloadDigest, EndOfInput: *body.EndOfInput})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result, valid := kubernetesInputPublicReadback(response.View, response.Submission, invocationRef, streamRef)
	if !valid || result.Submission == nil || string(result.Submission.Channel) != *body.Channel || result.Submission.PayloadDigest != *body.PayloadDigest || result.Submission.ByteCount != int64(len(payload)) || result.Submission.EndOfInput != *body.EndOfInput {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(result.View.Version, 10)+"\"")
	writeJSON(w, http.StatusCreated, result)
}

// StopManagedKubernetesStream closes one original tool through actor-scoped owner OCC; an empty body cannot carry a fence, actor, whole-Run selector or remote-stop assertion.
// StopManagedKubernetesStream закрывает один original tool через actor-scoped owner OCC; empty body не переносит fence, actor, whole-Run selector либо утверждение remote stop.
func (server *Server) StopManagedKubernetesStream(w http.ResponseWriter, r *http.Request, invocationRef generated.IntegrationInvocationRef, streamRef generated.KubernetesStreamInputRef, params generated.StopManagedKubernetesStreamParams) {
	if !validKubernetesInputHTTPRef(invocationRef, "inv_") || !validKubernetesInputHTTPRef(streamRef, "kst_") || !validKubernetesInputHTTPKey(params.IdempotencyKey) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if mutation.ExpectedVersion == nil || !validManagedVersion(*mutation.ExpectedVersion) || params.IfMatch != "\""+strconv.FormatInt(*mutation.ExpectedVersion, 10)+"\"" || r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	defer clear(raw)
	var body struct{}
	if err != nil || len(raw) > 1024 || internalrpcauth.DecodeExactJSON(raw, &body) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if !server.kubernetesInputClientAvailable(w) {
		return
	}
	response, err := server.control.KubernetesInputs.StopManagedKubernetesStream(r.Context(), &cp.StopManagedKubernetesStreamRequest{InvocationRef: invocationRef, StreamRef: streamRef, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	view, valid := kubernetesInputPublicView(response.View)
	if !valid || view.InvocationRef != invocationRef || view.StreamRef != streamRef || view.State == "ACTIVE" || view.State == "PREPARED" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(view.Version, 10)+"\"")
	writeJSON(w, http.StatusOK, view)
}
