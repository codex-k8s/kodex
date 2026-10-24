package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// ListInteractionIdentities validates connection/page selectors, queries the owner and rejects
// invalid, duplicate or foreign-connection identity projections before returning the page. It does
// not treat selected external identities as authenticated caller claims.
// ListInteractionIdentities проверяет connection/page selectors, запрашивает владельца и отклоняет
// неверные, повторные либо чужие connection проекции identity до выдачи страницы. Выбранные
// внешние identity не становятся claims аутентифицированного caller.
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

// BindInteractionIdentity validates external coordinates, user digest and subject locator under
// versioned mutation headers, then delegates binding to the owner. Success must echo exact
// requested identity fields, connection version and ACTIVE state before returning 201 with the
// binding ETag.
// BindInteractionIdentity проверяет внешние координаты, digest пользователя и адрес субъекта с
// versioned mutation заголовками, затем поручает привязку владельцу. Для 201 с ETag привязки ответ
// обязан повторить точные identity поля, версию connection и ACTIVE.
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

// RevokeInteractionIdentity delegates exact identity revocation with versioned mutation context
// and accepts only the same returned ref in REVOKED state. It returns the resulting binding ETag
// without changing external provider identity state itself.
// RevokeInteractionIdentity поручает отзыв точной identity с versioned mutation context и
// принимает только тот же ref в REVOKED. Возвращается итоговый ETag привязки без самостоятельного
// изменения identity у внешнего провайдера.
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

// validInteractionExternalRef requires bounded nonempty external text without surrounding
// whitespace, NUL or line breaks. It preserves provider-specific syntax and does not establish
// ownership or verify an external account.
// validInteractionExternalRef требует ограниченный непустой внешний текст без внешних пробелов,
// NUL и переводов строк. Синтаксис провайдера сохраняется; владение и внешний аккаунт не
// проверяются.
func validInteractionExternalRef(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n")
}

// interactionIdentityView projects only present identities with valid public refs, safe positive
// identity/connection versions, bounded external coordinates, a user digest and ACTIVE/REVOKED
// state. It emits metadata rather than external credentials.
// interactionIdentityView проецирует только имеющуюся identity с корректными публичными refs,
// безопасными положительными версиями identity/connection, ограниченными внешними координатами,
// digest пользователя и ACTIVE/REVOKED. Выдаются метаданные, а не внешние credentials.
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
