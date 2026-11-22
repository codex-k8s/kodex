package httptransport

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

const (
	// sshSetupInvalidInput rejects ambiguous metadata before the dedicated owner RPC without exposing submitted content.
	// sshSetupInvalidInput отклоняет ambiguous metadata до dedicated owner RPC без выдачи submitted content.
	sshSetupInvalidInput = "INVALID_REQUEST"
	// sshSetupInvalidUpstream prevents malformed owner metadata or permit union from becoming a public success response.
	// sshSetupInvalidUpstream не допускает malformed owner metadata либо permit union в public success response.
	sshSetupInvalidUpstream = "INVALID_UPSTREAM_RESPONSE"
)

// sshSetupObject accepts one bounded exact object, rejecting null, duplicate, aliased, unknown and missing fields before typed decoding; private key material is never an allowed property.
// sshSetupObject принимает bounded exact object, отклоняя null, duplicate, aliased, unknown и missing fields до typed decoding; private key material не является допустимым property.
func sshSetupObject(w http.ResponseWriter, r *http.Request, keys ...string) ([]byte, bool) {
	if r.Body == nil || r.URL.RawQuery != "" {
		writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	var fields map[string]json.RawMessage
	if err != nil || len(raw) > 4096 || internalrpcauth.DecodeStrictJSON(raw, &fields) != nil || fields == nil || len(fields) != len(keys) {
		writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
		return nil, false
	}
	for _, key := range keys {
		field, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
			return nil, false
		}
	}
	return raw, true
}

// sshHTTPSetupReference requires the exact owner-assigned public reference representation, rather than accepting an arbitrary opaque identifier carrying the same prefix.
// sshHTTPSetupReference требует exact owner-assigned public reference representation вместо произвольного opaque identifier с таким же prefix.
func sshHTTPSetupReference(ref string) bool {
	if len(ref) != 36 || !strings.HasPrefix(ref, "ssu_") || strings.ToLower(ref) != ref {
		return false
	}
	raw, err := hex.DecodeString(ref[4:])
	return err == nil && len(raw) == 16
}

// sshSetupMutation admits only one canonical bounded ETag, with an explicit zero for first personal allocation, without changing the existing positive-version mutation parser.
// sshSetupMutation допускает canonical bounded ETag с explicit zero для первого personal allocation без изменения existing positive-version mutation parser.
func sshSetupMutation(r *http.Request, key, etag string, allowZero bool) (*cp.MutationContext, bool) {
	if len(r.Header.Values("If-Match")) != 1 || len(r.Header.Values("Idempotency-Key")) != 1 || !validSearchText(key, 8, 128) || len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return nil, false
	}
	version, err := strconv.ParseInt(etag[1:len(etag)-1], 10, 64)
	if err != nil || version < 0 || version > maximumSafeJSONInteger || (!allowZero && version == 0) || strconv.FormatInt(version, 10) != etag[1:len(etag)-1] {
		return nil, false
	}
	return &cp.MutationContext{IdempotencyKey: key, ExpectedVersion: &version}, true
}

// sshSetupPublicView constructs only the safe public shape, checks exact addressed Connection/setup and finite enums, and refuses JavaScript-unsafe integers or mixed personal/shared metadata.
// sshSetupPublicView создаёт лишь safe public shape, проверяет exact addressed Connection/setup и finite enums; JavaScript-unsafe integers и mixed personal/shared metadata отклоняются.
func sshSetupPublicView(item *cp.ManagedSSHIdentitySetupView, connection, setup string) (generated.SSHIdentitySetup, bool) {
	var view generated.SSHIdentitySetup
	if item == nil || item.GetConnectionRef() != connection || !opaqueHTTPReference.MatchString(connection) || !sshHTTPSetupReference(item.GetRef()) || (setup != "" && item.GetRef() != setup) || !managedssh.OwnerReference(item.GetIdentityRef()) || !managedssh.OwnerReference(item.GetTargetRef()) || !validManagedVersion(item.GetVersion()) || !validManagedVersion(item.GetConnectionVersion()) || !validManagedVersion(item.GetIdentityRevision()) || item.GetExpiresAt() == nil || item.GetExpiresAt().CheckValid() != nil || item.GetExpiresAt().AsTime().IsZero() {
		return view, false
	}
	policy := ""
	switch item.GetPolicy() {
	case cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED:
		if !validManagedVersion(item.GetBindingVersion()) || !validManagedVersion(item.GetBindingGeneration()) {
			return view, false
		}
		policy = "INITIATOR_REQUIRED"
	case cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED:
		if item.GetBindingVersion() != 0 || item.GetBindingGeneration() != 0 {
			return view, false
		}
		policy = "PROJECT_SHARED"
	default:
		return view, false
	}
	state := strings.TrimPrefix(item.GetState().String(), "MANAGED_SSH_IDENTITY_SETUP_STATE_")
	switch state {
	case "PREPARED", "ATTEMPTING", "UNKNOWN", "UNREPORTED", "ACK_REJECTED", "COMPLETED", "CANCELLED", "EXPIRED", "QUARANTINED":
	default:
		return view, false
	}
	switch item.GetReasonCode() {
	case "PREPARED", "ATTEMPTING", "MATERIAL_UNKNOWN", "MISSING_COMPLETION", "ACK_REJECTED", "COMPLETED", "CANCELLED", "EXPIRED", "STALE_OWNER", "MATERIAL_RECORDED_NO_ATTACHMENT", "QUARANTINED", "SETUP_EXPIRED", "SETUP_EXPIRED_WITH_MATERIAL_UNRESOLVED", "OWNER_REVOKED", "OWNER_REPLACED":
	default:
		return view, false
	}
	view = generated.SSHIdentitySetup{Ref: item.GetRef(), ConnectionRef: connection, IdentityRef: item.GetIdentityRef(), TargetRef: item.GetTargetRef(), Version: item.GetVersion(), ConnectionVersion: item.GetConnectionVersion(), BindingVersion: item.GetBindingVersion(), BindingGeneration: item.GetBindingGeneration(), IdentityRevision: item.GetIdentityRevision(), Policy: generated.SSHIdentitySetupPolicy(policy), State: generated.SSHIdentitySetupState(state), ReasonCode: generated.SSHIdentitySetupReasonCode(item.GetReasonCode()), ExpiresAt: item.GetExpiresAt().AsTime().UTC()}
	return view, true
}

// BeginSSHIdentitySetup sends bounded content metadata and independent OCC to the owner, then returns a one-time direct broker capability only for a valid fresh admission; replay never reissues a permit.
// BeginSSHIdentitySetup передаёт owner bounded content metadata и independent OCC, затем выдаёт direct broker capability лишь для valid fresh admission; replay не перевыдаёт permit.
func (server *Server) BeginSSHIdentitySetup(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.BeginSSHIdentitySetupParams) {
	raw, ok := sshSetupObject(w, r, "expectedConnectionVersion", "contentSHA256", "contentBytes")
	if !ok {
		return
	}
	var body generated.SSHIdentitySetupBeginInput
	mutation, valid := sshSetupMutation(r, params.IdempotencyKey, params.IfMatch, true)
	if !valid || internalrpcauth.DecodeStrictJSON(raw, &body) != nil || !opaqueHTTPReference.MatchString(ref) || !validManagedVersion(body.ExpectedConnectionVersion) || !validManagedDigest(body.ContentSHA256) || body.ContentBytes < 1 || body.ContentBytes > 16384 {
		writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
		return
	}
	response, err := server.control.SSHIdentities.BeginSSHIdentitySetup(r.Context(), &cp.BeginSSHIdentitySetupRequest{ConnectionRef: ref, Mutation: mutation, ExpectedConnectionVersion: body.ExpectedConnectionVersion, ContentSha256: body.ContentSHA256, ContentBytes: uint32(body.ContentBytes)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, sshSetupInvalidUpstream, false)
		return
	}
	defer clear(response.UploadPermit)
	view, ok := sshSetupPublicView(response.GetSetup(), ref, "")
	if !ok || view.ConnectionVersion != body.ExpectedConnectionVersion {
		writeLocalProblem(w, http.StatusBadGateway, sshSetupInvalidUpstream, false)
		return
	}
	if response.GetReconcileOnly() {
		if len(response.GetUploadPermit()) != 0 || response.GetUploadOrigin() != "" || response.GetUploadTlsServerName() != "" {
			writeLocalProblem(w, http.StatusBadGateway, sshSetupInvalidUpstream, false)
			return
		}
		writeJSON(w, http.StatusOK, generated.SSHIdentitySetupAdmission1{Setup: view, ReconcileOnly: true})
		return
	}
	endpoint := managedssh.IdentityUploadEndpoint{Origin: response.GetUploadOrigin(), TLSServerName: response.GetUploadTlsServerName()}
	if len(response.GetUploadPermit()) != 32 || !endpoint.Valid() || view.State != "PREPARED" || view.Version != 1 {
		writeLocalProblem(w, http.StatusBadGateway, sshSetupInvalidUpstream, false)
		return
	}
	output := generated.SSHIdentitySetupAdmission0{Setup: view, ReconcileOnly: false}
	output.Upload.Origin = endpoint.Origin
	output.Upload.TlsServerName = endpoint.TLSServerName
	permit := base64.RawURLEncoding.EncodeToString(response.GetUploadPermit())
	output.Upload.Permit = &permit
	writeJSON(w, http.StatusOK, output)
}

// GetSSHIdentitySetupByBeginKey recovers safe current metadata for the exact original Begin key; duplicate or unknown query selectors cannot reach the owner.
// GetSSHIdentitySetupByBeginKey получает safe current metadata exact original Begin key; duplicate либо unknown query selectors не достигают owner.
func (server *Server) GetSSHIdentitySetupByBeginKey(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.GetSSHIdentitySetupByBeginKeyParams) {
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil || len(query) != 1 || len(query["beginIdempotencyKey"]) != 1 || !validSearchText(params.BeginIdempotencyKey, 8, 128) || !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
		return
	}
	server.readSSHIdentitySetup(w, r, ref, "", &cp.GetSSHIdentitySetupRequest{ConnectionRef: ref, Selector: &cp.GetSSHIdentitySetupRequest_BeginIdempotencyKey{BeginIdempotencyKey: params.BeginIdempotencyKey}})
}

// GetSSHIdentitySetup reads only one addressed setup through the same dedicated current-owner RPC, without returning physical material coordinates or a reusable permit.
// GetSSHIdentitySetup читает addressed setup через тот же dedicated current-owner RPC без physical material coordinates либо reusable permit.
func (server *Server) GetSSHIdentitySetup(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, setup generated.SSHIdentitySetupRef) {
	if r.URL.RawQuery != "" || !opaqueHTTPReference.MatchString(ref) || !sshHTTPSetupReference(setup) {
		writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
		return
	}
	server.readSSHIdentitySetup(w, r, ref, setup, &cp.GetSSHIdentitySetupRequest{ConnectionRef: ref, Selector: &cp.GetSSHIdentitySetupRequest_SetupRef{SetupRef: setup}})
}

// readSSHIdentitySetup validates the returned addressed safe projection after the authoritative read; it does not infer material absence, authentication readiness or mutation authority from setup state.
// readSSHIdentitySetup проверяет returned addressed safe projection после authoritative read; setup state не доказывает material absence, authentication readiness либо mutation authority.
func (server *Server) readSSHIdentitySetup(w http.ResponseWriter, r *http.Request, ref, setup string, request *cp.GetSSHIdentitySetupRequest) {
	response, err := server.control.SSHIdentities.GetSSHIdentitySetup(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, ok := sshSetupPublicView(response.GetSetup(), ref, setup)
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, sshSetupInvalidUpstream, false)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// CancelSSHIdentitySetup sends only the original addressed setup and exact setup version; closing authority is never presented as remote material deletion or a safe new write.
// CancelSSHIdentitySetup передаёт лишь original addressed setup и exact setup version; closing authority не считается remote material deletion либо safe new write.
func (server *Server) CancelSSHIdentitySetup(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, setup generated.SSHIdentitySetupRef, params generated.CancelSSHIdentitySetupParams) {
	if _, ok := sshSetupObject(w, r); !ok {
		return
	}
	mutation, ok := sshSetupMutation(r, params.IdempotencyKey, params.IfMatch, false)
	if !ok || !opaqueHTTPReference.MatchString(ref) || !sshHTTPSetupReference(setup) {
		writeLocalProblem(w, http.StatusBadRequest, sshSetupInvalidInput, false)
		return
	}
	response, err := server.control.SSHIdentities.CancelSSHIdentitySetup(r.Context(), &cp.CancelSSHIdentitySetupRequest{ConnectionRef: ref, SetupRef: setup, Mutation: mutation})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, ok := sshSetupPublicView(response.GetSetup(), ref, setup)
	if !ok || (view.State != "CANCELLED" && view.State != "EXPIRED" && view.State != "UNREPORTED") {
		writeLocalProblem(w, http.StatusBadGateway, sshSetupInvalidUpstream, false)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
