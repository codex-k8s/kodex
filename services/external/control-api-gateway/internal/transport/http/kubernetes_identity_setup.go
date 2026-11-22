package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// kubernetesSetupBeginBody preserves presence of the explicit zero absent-binding precondition.
// It contains no credential, subject, purpose, target, endpoint or private material selector.
// kubernetesSetupBeginBody сохраняет presence явного нуля отсутствующей binding precondition.
// Не содержит credential, subject, purpose, target, endpoint либо private material selector.
type kubernetesSetupBeginBody struct {
	ExpectedVersion           *int64  `json:"expectedVersion"`
	ExpectedConnectionVersion *int64  `json:"expectedConnectionVersion"`
	ContentSHA256             string  `json:"contentSHA256"`
	ContentBytes              *uint32 `json:"contentBytes"`
}

// BeginManagedKubernetesIdentitySetup submits metadata only and never accepts kubeconfig bytes.
// Its one-shot permit is returned only from the original owner admission under no-store.
// BeginManagedKubernetesIdentitySetup передаёт только metadata и не принимает kubeconfig bytes.
// Одноразовый permit возвращается лишь из исходного owner admission под no-store.
func (server *Server) BeginManagedKubernetesIdentitySetup(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.BeginManagedKubernetesIdentitySetupParams) {
	body, context, ok := decodeKubernetesSetupMutation[kubernetesSetupBeginBody](w, r, params.IdempotencyKey, "", []string{"expectedVersion", "expectedConnectionVersion", "contentSHA256", "contentBytes"})
	if !ok {
		return
	}
	if body.ExpectedVersion == nil || *body.ExpectedVersion < 0 || *body.ExpectedVersion > maximumSafeJSONInteger || body.ExpectedConnectionVersion == nil || !validManagedVersion(*body.ExpectedConnectionVersion) ||
		!validManagedDigest(body.ContentSHA256) || body.ContentBytes == nil || *body.ContentBytes == 0 || *body.ContentBytes > 1<<20 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	context.ExpectedVersion = body.ExpectedVersion
	response, err := server.control.KubernetesIdentities.BeginManagedKubernetesIdentitySetup(r.Context(), &cp.BeginManagedKubernetesIdentitySetupRequest{ConnectionRef: ref, Mutation: context,
		ExpectedConnectionVersion: *body.ExpectedConnectionVersion, ContentSha256: body.ContentSHA256, ContentBytes: *body.ContentBytes})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	defer clear(response.UploadPermit)
	view, valid := kubernetesSetupPublicView(response.GetSetup(), ref)
	if !valid || response.GetReconcileOnly() && (len(response.GetUploadPermit()) != 0 || response.GetEndpoint() != nil) ||
		!response.GetReconcileOnly() && (view.State != generated.KubernetesIdentitySetupViewStatePREPARED || len(response.GetUploadPermit()) != 32 || !kubernetesSetupPublicEndpoint(response.GetEndpoint())) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesIdentitySetupAdmission{Setup: view, ReconcileOnly: response.GetReconcileOnly()}
	if !result.ReconcileOnly {
		permit := response.UploadPermit
		result.UploadPermit = &permit
		endpoint := response.GetEndpoint()
		result.Endpoint = &generated.KubernetesIdentityUploadEndpoint{ServerOrigin: endpoint.GetServerOrigin(), TlsServerName: endpoint.GetTlsServerName(),
			CertificateAuthorityPem: endpoint.GetCertificateAuthorityPem(), CertificateAuthoritySHA256: endpoint.GetCertificateAuthoritySha256(), AddressIPs: endpoint.GetAddressIps()}
	}
	writeJSON(w, http.StatusCreated, result)
}

// GetManagedKubernetesIdentitySetup reads one exact self-owned setup or permanent Begin key.
// It cannot recover the raw permit and does not silently pick between two supplied selectors.
// GetManagedKubernetesIdentitySetup читает точный собственный setup либо постоянный Begin key.
// Не восстанавливает raw permit и не выбирает молча один из двух переданных selectors.
func (server *Server) GetManagedKubernetesIdentitySetup(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.GetManagedKubernetesIdentitySetupParams) {
	input := &cp.GetManagedKubernetesIdentitySetupRequest{ConnectionRef: ref}
	if (params.SetupRef == nil) == (params.BeginIdempotencyKey == nil) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if params.SetupRef != nil {
		if !opaqueHTTPReference.MatchString(*params.SetupRef) || !strings.HasPrefix(*params.SetupRef, "ksu_") {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		input.Selector = &cp.GetManagedKubernetesIdentitySetupRequest_SetupRef{SetupRef: *params.SetupRef}
	} else {
		key := *params.BeginIdempotencyKey
		if len(key) < 8 || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		input.Selector = &cp.GetManagedKubernetesIdentitySetupRequest_BeginIdempotencyKey{BeginIdempotencyKey: key}
	}
	response, err := server.control.KubernetesIdentities.GetManagedKubernetesIdentitySetup(r.Context(), input)
	if response != nil && len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeKubernetesSetupPublicView(w, response.GetSetup(), err, ref)
}

// CancelManagedKubernetesIdentitySetup uses only the addressed setup and its exact OCC version.
// Extra owner/material fields are rejected before the immutable owner command is sent.
// CancelManagedKubernetesIdentitySetup использует только адресованный setup и его exact OCC version.
// Лишние owner/material fields отклоняются до передачи immutable owner command.
func (server *Server) CancelManagedKubernetesIdentitySetup(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, setupRef string, params generated.CancelManagedKubernetesIdentitySetupParams) {
	body, mutation, ok := decodeKubernetesSetupMutation[generated.PersonalIntegrationCredentialRevokeInput](w, r, params.IdempotencyKey, params.IfMatch, nil)
	if !ok {
		return
	}
	if len(body) != 0 || !opaqueHTTPReference.MatchString(setupRef) || !strings.HasPrefix(setupRef, "ksu_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.KubernetesIdentities.CancelManagedKubernetesIdentitySetup(r.Context(), &cp.CancelManagedKubernetesIdentitySetupRequest{ConnectionRef: ref, SetupRef: setupRef, Mutation: mutation})
	if response != nil && len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeKubernetesSetupPublicView(w, response.GetSetup(), err, ref)
}

// decodeKubernetesSetupMutation rejects duplicate, aliased and oversized metadata before command construction.
// This bounded setup-only decoder never handles kubeconfig or other credential material.
// decodeKubernetesSetupMutation отклоняет duplicate, aliased и oversized metadata до сборки command.
// Ограниченный setup decoder никогда не обрабатывает kubeconfig либо иные credential material.
func decodeKubernetesSetupMutation[T any](w http.ResponseWriter, r *http.Request, key, etag string, allowed []string) (T, *cp.MutationContext, bool) {
	var body T
	if r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	defer clear(raw)
	var fields map[string]json.RawMessage
	if err != nil || len(raw) > 4096 || !utf8.Valid(raw) || internalrpcauth.DecodeStrictJSON(raw, &fields) != nil || fields == nil || len(fields) != len(allowed) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	for _, name := range allowed {
		if _, present := fields[name]; !present {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return body, nil, false
		}
	}
	if internalrpcauth.DecodeStrictJSON(raw, &body) != nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	mutation, valid := requireMutation(w, key, etag)
	return body, mutation, valid
}

// kubernetesSetupPublicView constructs only the finite public DTO from exact owner metadata.
// Current account/RBAC readiness is not inferred from completed setup history.
// kubernetesSetupPublicView создаёт лишь конечный public DTO из exact owner metadata.
// Current account/RBAC readiness не выводится из completed setup history.
func kubernetesSetupPublicView(input *cp.ManagedKubernetesIdentitySetupView, connection string) (generated.KubernetesIdentitySetupView, bool) {
	var result generated.KubernetesIdentitySetupView
	if input == nil || input.GetConnectionRef() != connection || !opaqueHTTPReference.MatchString(input.GetSetupRef()) || !strings.HasPrefix(input.GetSetupRef(), "ksu_") ||
		!opaqueHTTPReference.MatchString(input.GetIdentityRef()) || !opaqueHTTPReference.MatchString(input.GetTargetRef()) || !validManagedVersion(input.GetVersion()) || !validManagedVersion(input.GetConnectionVersion()) ||
		!validManagedVersion(input.GetIdentityRevision()) || !validKubernetesCanonicalContext(input.GetCanonicalContext()) || input.GetExpiresAt() == nil || input.GetExpiresAt().CheckValid() != nil || input.GetExpiresAt().AsTime().IsZero() {
		return result, false
	}
	state := generated.KubernetesIdentitySetupViewState(strings.TrimPrefix(input.GetState().String(), "MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_"))
	policy := generated.KubernetesIdentitySetupViewCredentialPolicy(strings.TrimPrefix(input.GetCredentialPolicy().String(), "INTEGRATION_CREDENTIAL_POLICY_"))
	reason := generated.KubernetesIdentitySetupViewReasonCode(input.GetReasonCode())
	if !state.Valid() || !policy.Valid() || !reason.Valid() ||
		policy == generated.KubernetesIdentitySetupViewCredentialPolicyINITIATORREQUIRED && (!validManagedVersion(input.GetBindingVersion()) || !validManagedVersion(input.GetBindingGeneration())) ||
		policy == generated.KubernetesIdentitySetupViewCredentialPolicyPROJECTSHARED && (input.GetBindingVersion() != 0 || input.GetBindingGeneration() != 0) {
		return result, false
	}
	return generated.KubernetesIdentitySetupView{SetupRef: input.GetSetupRef(), ConnectionRef: connection, ConnectionVersion: input.GetConnectionVersion(), BindingVersion: input.GetBindingVersion(),
		BindingGeneration: input.GetBindingGeneration(), IdentityRef: input.GetIdentityRef(), IdentityRevision: input.GetIdentityRevision(), TargetRef: input.GetTargetRef(), State: state, CredentialPolicy: policy,
		CanonicalContext: input.GetCanonicalContext(), ExpiresAt: input.GetExpiresAt().AsTime(), Version: input.GetVersion(), ReasonCode: reason}, true
}

// kubernetesSetupPublicEndpoint bounds operator TLS metadata before handing it to the owner CLI.
// This projection performs no dial; the CLI independently verifies the full CA and route contract.
// kubernetesSetupPublicEndpoint ограничивает operator TLS metadata до передачи owner CLI.
// Projection не делает dial; CLI независимо проверяет полный CA и route contract.
func kubernetesSetupPublicEndpoint(input *cp.ManagedKubernetesIdentityUploadEndpoint) bool {
	if input == nil || len(input.GetServerOrigin()) > 2048 || len(input.GetTlsServerName()) == 0 || len(input.GetTlsServerName()) > 253 ||
		len(input.GetCertificateAuthorityPem()) == 0 || len(input.GetCertificateAuthorityPem()) > 64<<10 || !validManagedDigest(input.GetCertificateAuthoritySha256()) || len(input.GetAddressIps()) == 0 || len(input.GetAddressIps()) > 16 {
		return false
	}
	origin, err := url.Parse(input.GetServerOrigin())
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawPath != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || origin.Opaque != "" {
		return false
	}
	hash := sha256.Sum256(input.GetCertificateAuthorityPem())
	if hex.EncodeToString(hash[:]) != input.GetCertificateAuthoritySha256() {
		return false
	}
	for _, raw := range input.GetAddressIps() {
		address, err := netip.ParseAddr(raw)
		if err != nil || address.IsUnspecified() || address.Zone() != "" || address.String() != raw {
			return false
		}
	}
	return true
}

// writeKubernetesSetupPublicView emits exact safe metadata and the setup version ETag under no-store.
// writeKubernetesSetupPublicView выдаёт exact safe metadata и setup version ETag под no-store.
func writeKubernetesSetupPublicView(w http.ResponseWriter, input *cp.ManagedKubernetesIdentitySetupView, err error, connection string) {
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, valid := kubernetesSetupPublicView(input, connection)
	if !valid {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(view.Version, 10)+`"`)
	writeJSON(w, http.StatusOK, view)
}

// validKubernetesCanonicalContext bounds safe opaque context metadata without treating it as authority.
// validKubernetesCanonicalContext ограничивает safe opaque context metadata без выдачи authority.
func validKubernetesCanonicalContext(input string) bool {
	return managedkubernetes.ValidLocator(input)
}
