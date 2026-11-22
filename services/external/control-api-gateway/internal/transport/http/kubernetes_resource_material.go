package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	kubevalidation "k8s.io/apimachinery/pkg/util/validation"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// kubernetesMaterialBeginBody preserves required field presence, including intentional empty
// name/generateName/patchType strings. Explicit null cannot become an omitted destination field.
// kubernetesMaterialBeginBody сохраняет required field presence, включая intentional empty
// name/generateName/patchType strings. Explicit null не становится omitted destination field.
type kubernetesMaterialBeginBody struct {
	ExpectedVersion           *int64  `json:"expectedVersion"`
	ExpectedConnectionVersion *int64  `json:"expectedConnectionVersion"`
	Operation                 *string `json:"operation"`
	Namespace                 *string `json:"namespace"`
	Name                      *string `json:"name"`
	GenerateName              *string `json:"generateName"`
	SecretType                *string `json:"secretType"`
	PatchType                 *string `json:"patchType"`
	ContentSHA256             *string `json:"contentSHA256"`
	ContentBytes              *uint32 `json:"contentBytes"`
}

// BeginManagedKubernetesResourceMaterial accepts safe metadata only; private source bytes travel
// directly from an owner-local CLI to the broker. A lost ACK cannot issue another upload permit.
// BeginManagedKubernetesResourceMaterial принимает safe metadata; private source bytes идут
// owner-local CLI прямо broker. Lost ACK не выдаёт другой upload permit.
func (server *Server) BeginManagedKubernetesResourceMaterial(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.BeginManagedKubernetesResourceMaterialParams) {
	body, mutation, ok := decodeKubernetesSetupMutation[kubernetesMaterialBeginBody](w, r, params.IdempotencyKey, "", []string{"expectedVersion", "expectedConnectionVersion", "operation", "namespace", "name", "generateName", "secretType", "patchType", "contentSHA256", "contentBytes"})
	if !ok {
		return
	}
	if body.ExpectedVersion == nil || *body.ExpectedVersion < 0 || *body.ExpectedVersion > maximumSafeJSONInteger || body.ExpectedConnectionVersion == nil || !validManagedVersion(*body.ExpectedConnectionVersion) || body.Operation == nil || body.Namespace == nil || body.Name == nil || body.GenerateName == nil || body.SecretType == nil || body.PatchType == nil || body.ContentSHA256 == nil || !validManagedDigest(*body.ContentSHA256) || body.ContentBytes == nil || *body.ContentBytes == 0 || *body.ContentBytes > 1<<20 ||
		!kubernetesMaterialDestination(*body.Operation, *body.Namespace, *body.Name, *body.GenerateName, *body.SecretType, *body.PatchType) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation.ExpectedVersion = body.ExpectedVersion
	reply, err := server.control.KubernetesMaterials.BeginManagedKubernetesResourceMaterial(r.Context(), &cp.BeginManagedKubernetesResourceMaterialRequest{ConnectionRef: ref, Mutation: mutation, ExpectedConnectionVersion: *body.ExpectedConnectionVersion, Operation: *body.Operation, Namespace: *body.Namespace, Name: *body.Name, GenerateName: *body.GenerateName, SecretType: *body.SecretType, PatchType: *body.PatchType, ContentSha256: *body.ContentSHA256, ContentBytes: *body.ContentBytes})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	response := reply.GetAdmission()
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	defer clear(response.UploadPermit)
	view, valid := kubernetesMaterialPublicView(response.GetMaterial(), ref)
	if !valid || view.Operation != generated.KubernetesResourceMaterialViewOperation(*body.Operation) || view.Namespace != *body.Namespace || view.Name != *body.Name || view.GenerateName != *body.GenerateName || view.SecretType != *body.SecretType || view.PatchType != generated.KubernetesResourceMaterialViewPatchType(*body.PatchType) || response.GetReconcileOnly() && (len(response.GetUploadPermit()) != 0 || response.GetEndpoint() != nil) || !response.GetReconcileOnly() && (view.State != generated.KubernetesResourceMaterialViewStatePREPARED || len(response.GetUploadPermit()) != 32 || !kubernetesSetupPublicEndpoint(response.GetEndpoint()) || len(response.GetEndpoint().ProtoReflect().GetUnknown()) != 0) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.KubernetesResourceMaterialAdmission{Material: view, ReconcileOnly: response.GetReconcileOnly()}
	if !result.ReconcileOnly {
		permit := response.UploadPermit
		result.UploadPermit = &permit
		e := response.GetEndpoint()
		result.Endpoint = &generated.KubernetesIdentityUploadEndpoint{ServerOrigin: e.GetServerOrigin(), TlsServerName: e.GetTlsServerName(), CertificateAuthorityPem: e.GetCertificateAuthorityPem(), CertificateAuthoritySHA256: e.GetCertificateAuthoritySha256(), AddressIPs: e.GetAddressIps()}
	}
	writeJSON(w, http.StatusCreated, result)
}

// GetManagedKubernetesResourceMaterial reads one actor-owned locator or original Begin key.
// Both selectors together are refused; no raw permit is recovered from safe history.
// GetManagedKubernetesResourceMaterial читает actor-owned locator либо original Begin key.
// Оба selector вместе запрещены; raw permit не восстанавливается из safe history.
func (server *Server) GetManagedKubernetesResourceMaterial(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.GetManagedKubernetesResourceMaterialParams) {
	input := &cp.GetManagedKubernetesResourceMaterialRequest{ConnectionRef: ref}
	if (params.MaterialRef == nil) == (params.BeginIdempotencyKey == nil) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if params.MaterialRef != nil {
		if !validMaterialHTTPRef(*params.MaterialRef) {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		input.Selector = &cp.GetManagedKubernetesResourceMaterialRequest_MaterialRef{MaterialRef: *params.MaterialRef}
	} else {
		key := *params.BeginIdempotencyKey
		if !utf8.ValidString(key) || len(key) < 8 || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		input.Selector = &cp.GetManagedKubernetesResourceMaterialRequest_BeginIdempotencyKey{BeginIdempotencyKey: key}
	}
	response, err := server.control.KubernetesMaterials.GetManagedKubernetesResourceMaterial(r.Context(), input)
	if err == nil && (response == nil || len(response.ProtoReflect().GetUnknown()) != 0) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	if err == nil && params.MaterialRef != nil && response.GetMaterial().GetMaterialRef() != *params.MaterialRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeKubernetesMaterialPublicView(w, response.GetMaterial(), err, ref)
}

// CancelManagedKubernetesResourceMaterial preserves exact artifact OCC and future-use closure.
// It does not pretend an already unknown or consumed target Secret effect was undone.
// CancelManagedKubernetesResourceMaterial сохраняет exact artifact OCC и future-use closure.
// Не утверждает отмену уже unknown либо consumed target Secret effect.
func (server *Server) CancelManagedKubernetesResourceMaterial(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, materialRef string, params generated.CancelManagedKubernetesResourceMaterialParams) {
	body, mutation, ok := decodeKubernetesSetupMutation[generated.PersonalIntegrationCredentialRevokeInput](w, r, params.IdempotencyKey, params.IfMatch, nil)
	if !ok {
		return
	}
	if len(body) != 0 || !validMaterialHTTPRef(materialRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.KubernetesMaterials.CancelManagedKubernetesResourceMaterial(r.Context(), &cp.CancelManagedKubernetesResourceMaterialRequest{ConnectionRef: ref, MaterialRef: materialRef, Mutation: mutation})
	if err == nil && (response == nil || len(response.ProtoReflect().GetUnknown()) != 0) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	if err == nil && response.GetMaterial().GetMaterialRef() != materialRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeKubernetesMaterialPublicView(w, response.GetMaterial(), err, ref)
}

// kubernetesMaterialDestination validates typed public coordinates, never target eligibility.
// Exact current ownership/policy/identity and material use are separately resolved by the owner.
// kubernetesMaterialDestination проверяет typed public coordinates, не target eligibility.
// Exact current ownership/policy/identity и material use отдельно разрешает owner.
func kubernetesMaterialDestination(operation, namespace, name, generateName, secretType, patchType string) bool {
	address := managedkubernetes.ResourceAddress{Version: "v1", Resource: "secrets", Namespace: namespace, Namespaced: true, Name: name}
	if address.Validate() != nil || name != "" && len(kubevalidation.IsDNS1123Subdomain(name)) != 0 || !utf8.ValidString(secretType) || len(secretType) == 0 || len(secretType) > 253 || strings.ContainsAny(secretType, "\x00\r\n") || secretType == "kubernetes.io/service-account-token" {
		return false
	}
	if operation == "kubernetes.create" {
		prefixName := generateName
		if strings.HasSuffix(prefixName, "-") {
			prefixName = strings.TrimSuffix(prefixName, "-") + "a"
		}
		return (name == "") != (generateName == "") && patchType == "" && (generateName == "" || len(generateName) <= 253 && len(kubevalidation.IsDNS1123Subdomain(prefixName)) == 0)
	}
	if name == "" || generateName != "" {
		return false
	}
	switch operation {
	case "kubernetes.replace", "kubernetes.apply", "kubernetes.edit":
		return patchType == ""
	case "kubernetes.patch":
		return patchType == "JSON" || patchType == "MERGE" || patchType == "STRATEGIC"
	default:
		return false
	}
}

// validMaterialHTTPRef recognizes only the closed actor-owned artifact locator syntax.
// validMaterialHTTPRef распознаёт лишь closed actor-owned artifact locator syntax.
func validMaterialHTTPRef(ref string) bool {
	return opaqueHTTPReference.MatchString(ref) && strings.HasPrefix(ref, "krs_") && len(ref) >= 12
}

// kubernetesMaterialPublicView refuses partial/future owner metadata and private storage fields.
// AVAILABLE stays material availability rather than target effect or RBAC success.
// kubernetesMaterialPublicView отклоняет partial/future owner metadata/private storage fields.
// AVAILABLE остаётся material availability, не target effect либо RBAC success.
func kubernetesMaterialPublicView(input *cp.ManagedKubernetesResourceMaterialView, connection string) (generated.KubernetesResourceMaterialView, bool) {
	var empty generated.KubernetesResourceMaterialView
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.GetConnectionRef() != connection || !validMaterialHTTPRef(input.GetMaterialRef()) || !opaqueHTTPReference.MatchString(input.GetIdentityRef()) || !opaqueHTTPReference.MatchString(input.GetTargetRef()) || !validManagedVersion(input.GetVersion()) || !validManagedVersion(input.GetConnectionVersion()) || !validManagedVersion(input.GetIdentityRevision()) || input.GetExpiresAt() == nil || input.GetExpiresAt().CheckValid() != nil || len(input.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 || input.GetExpiresAt().AsTime().IsZero() || !kubernetesMaterialDestination(input.GetOperation(), input.GetNamespace(), input.GetName(), input.GetGenerateName(), input.GetSecretType(), input.GetPatchType()) {
		return empty, false
	}
	state := generated.KubernetesResourceMaterialViewState(strings.TrimPrefix(input.GetState().String(), "MANAGED_KUBERNETES_RESOURCE_MATERIAL_STATE_"))
	policy := generated.KubernetesResourceMaterialViewCredentialPolicy(strings.TrimPrefix(input.GetCredentialPolicy().String(), "INTEGRATION_CREDENTIAL_POLICY_"))
	reason := generated.KubernetesResourceMaterialViewReasonCode(input.GetReasonCode())
	if !state.Valid() || !policy.Valid() || !reason.Valid() || policy == generated.KubernetesResourceMaterialViewCredentialPolicyINITIATORREQUIRED && !validManagedVersion(input.GetBindingVersion()) || policy == generated.KubernetesResourceMaterialViewCredentialPolicyPROJECTSHARED && input.GetBindingVersion() != 0 {
		return empty, false
	}
	return generated.KubernetesResourceMaterialView{MaterialRef: input.GetMaterialRef(), ConnectionRef: connection, ConnectionVersion: input.GetConnectionVersion(), IdentityRef: input.GetIdentityRef(), IdentityRevision: input.GetIdentityRevision(), TargetRef: input.GetTargetRef(), Namespace: input.GetNamespace(), Name: input.GetName(), GenerateName: input.GetGenerateName(), Operation: generated.KubernetesResourceMaterialViewOperation(input.GetOperation()), SecretType: input.GetSecretType(), PatchType: generated.KubernetesResourceMaterialViewPatchType(input.GetPatchType()), State: state, ExpiresAt: input.GetExpiresAt().AsTime(), Version: input.GetVersion(), ReasonCode: reason, CredentialPolicy: policy, BindingVersion: input.GetBindingVersion()}, true
}

// writeKubernetesMaterialPublicView returns safe metadata and the exact artifact ETag under no-store.
// writeKubernetesMaterialPublicView выдаёт safe metadata и exact artifact ETag под no-store.
func writeKubernetesMaterialPublicView(w http.ResponseWriter, input *cp.ManagedKubernetesResourceMaterialView, err error, connection string) {
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, valid := kubernetesMaterialPublicView(input, connection)
	if !valid {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(view.Version, 10)+`"`)
	writeJSON(w, http.StatusOK, view)
}
