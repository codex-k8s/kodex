package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListInteractionIdentities(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ListInteractionIdentitiesParams) {
	if !opaqueHTTPReference.MatchString(ref) || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListInteractionIdentities(r.Context(), &controlplanev1.ListInteractionIdentitiesRequest{ConnectionRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.GetIdentities()) > 100 || len(response.GetPage().GetNextPageToken()) > 512 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.InteractionIdentityPage{Items: []generated.InteractionIdentity{}, NextPageToken: response.GetPage().GetNextPageToken()}
	seen := make(map[string]bool)
	for _, identity := range response.GetIdentities() {
		item, ok := interactionIdentityView(identity)
		if !ok || item.ConnectionRef != ref || seen[item.Ref] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) BindInteractionIdentity(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.BindInteractionIdentityParams) {
	body, ok := decodeJSON[generated.InteractionIdentityBindInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) || !validInteractionExternalRef(body.ExternalTeamRef) || !validInteractionExternalRef(body.ExternalChannelRef) || !validManagedDigest(body.ExternalUserDigest) || !opaqueHTTPReference.MatchString(body.SubjectRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.BindInteractionIdentity(r.Context(), &controlplanev1.BindInteractionIdentityRequest{Mutation: mutation, ConnectionRef: ref,
		ExternalTeamRef: body.ExternalTeamRef, ExternalChannelRef: body.ExternalChannelRef, ExternalUserDigest: body.ExternalUserDigest, SubjectRef: body.SubjectRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	item, ok := interactionIdentityView(response.GetIdentity())
	if !ok || item.ConnectionRef != ref || item.ConnectionVersion != mutation.GetExpectedVersion() || string(item.State) != "ACTIVE" ||
		item.ExternalTeamRef != body.ExternalTeamRef || item.ExternalChannelRef != body.ExternalChannelRef || item.ExternalUserDigest != body.ExternalUserDigest || item.SubjectRef != body.SubjectRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(item.Version, 10)+"\"")
	writeJSON(w, http.StatusCreated, item)
}

func (server *Server) RevokeInteractionIdentity(w http.ResponseWriter, r *http.Request, ref generated.InteractionIdentityRef, p generated.RevokeInteractionIdentityParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RevokeInteractionIdentity(r.Context(), &controlplanev1.RevokeInteractionIdentityRequest{Mutation: mutation, IdentityRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	item, ok := interactionIdentityView(response.GetIdentity())
	if !ok || item.Ref != ref || string(item.State) != "REVOKED" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(item.Version, 10)+"\"")
	writeJSON(w, http.StatusOK, item)
}

func validInteractionExternalRef(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n")
}

func interactionIdentityView(input *controlplanev1.InteractionIdentity) (generated.InteractionIdentity, bool) {
	if input == nil || !opaqueHTTPReference.MatchString(input.GetRef()) || !opaqueHTTPReference.MatchString(input.GetConnectionRef()) || !opaqueHTTPReference.MatchString(input.GetSubjectRef()) ||
		input.GetVersion() < 1 || input.GetVersion() > maximumSafeJSONInteger || input.GetConnectionVersion() < 1 || input.GetConnectionVersion() > maximumSafeJSONInteger ||
		!validInteractionExternalRef(input.GetExternalTeamRef()) || !validInteractionExternalRef(input.GetExternalChannelRef()) || !validManagedDigest(input.GetExternalUserDigest()) ||
		(input.GetState() != "ACTIVE" && input.GetState() != "REVOKED") {
		return generated.InteractionIdentity{}, false
	}
	return generated.InteractionIdentity{Ref: input.GetRef(), Version: input.GetVersion(), ConnectionRef: input.GetConnectionRef(), ConnectionVersion: input.GetConnectionVersion(),
		ExternalTeamRef: input.GetExternalTeamRef(), ExternalChannelRef: input.GetExternalChannelRef(), ExternalUserDigest: input.GetExternalUserDigest(), SubjectRef: input.GetSubjectRef(), State: generated.InteractionIdentityState(input.GetState())}, true
}
