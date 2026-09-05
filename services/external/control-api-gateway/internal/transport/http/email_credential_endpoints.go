package httptransport

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ConfigureEmailMailboxCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ConfigureEmailMailboxCredentialParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.EmailMailboxCredentialInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	value := []byte(stringValue(body.Value))
	body.Value = nil
	defer clear(value)
	kind := cp.EmailMailboxCredentialKind(cp.EmailMailboxCredentialKind_value["EMAIL_MAILBOX_CREDENTIAL_KIND_"+string(body.Kind)])
	limit := 0
	switch kind {
	case cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_CA_CERTIFICATE:
		limit = 65536
	case cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_USERNAME:
		limit = 320
	case cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_AUTH_SECRET:
		limit = 16384
	}
	if len(value) == 0 || len(value) > limit || !utf8.Valid(value) || (kind != cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_CA_CERTIFICATE && strings.ContainsAny(string(value), "\x00\r\n")) || m.GetExpectedVersion() >= maximumSafeJSONInteger {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Command.ConfigureEmailMailboxCredential(r.Context(), &cp.ConfigureEmailMailboxCredentialRequest{Mutation: m, ConnectionRef: ref, Kind: kind, CredentialValue: value})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	v := response.GetCredential()
	if v == nil || v.GetConnectionRef() != ref || v.GetKind() != kind || v.GetConnectionVersion() != m.GetExpectedVersion()+1 || v.GetGeneration() != v.GetConnectionVersion() || !validManagedVersion(v.GetGeneration()) || len(v.GetName()) > 128 || !opaqueHTTPReference.MatchString(v.GetName()) {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", v.GetConnectionVersion()))
	writeJSON(w, http.StatusOK, generated.EmailMailboxCredential{Name: v.GetName(), Generation: v.GetGeneration(), Kind: body.Kind, ConnectionRef: ref, ConnectionVersion: v.GetConnectionVersion()})
}
