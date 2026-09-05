package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func validCountedCatalogPage(total int64, count int, paging *cp.PageInfo) bool {
	return total >= int64(count) && total <= maximumSafeJSONInteger && count <= 100 && len(paging.GetNextPageToken()) <= 512 && utf8.ValidString(paging.GetNextPageToken())
}
func validPublishedOverlay(item *cp.ConfigOverlayVersion) bool {
	if item == nil || !opaqueHTTPReference.MatchString(item.GetRef()) || !strings.HasPrefix(item.GetRef(), "cov_") ||
		!validManagedVersion(item.GetRevision()) || item.GetVersion() != item.GetRevision() ||
		item.GetState() != "PUBLISHED" && item.GetState() != "SUPERSEDED" ||
		item.GetCreatedAt() == nil || item.GetCreatedAt().CheckValid() != nil ||
		item.GetPublishedAt() == nil || item.GetPublishedAt().CheckValid() != nil ||
		item.GetPublishedAt().AsTime().Before(item.GetCreatedAt().AsTime()) ||
		len(item.GetContent()) > 65536 || !utf8.ValidString(item.GetContent()) || strings.ContainsRune(item.GetContent(), '\x00') {
		return false
	}
	digest := sha256.Sum256([]byte(item.GetContent()))
	return item.GetDigest() == hex.EncodeToString(digest[:])
}
func (server *Server) ListConfigOverlayRevisions(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, p generated.ListConfigOverlayRevisionsParams) {
	r, ok := catalogRequest(w, r, nil, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(agentRef) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListConfigOverlayRevisions(r.Context(), &cp.ListConfigOverlayRevisionsRequest{AgentRef: agentRef, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validCountedCatalogPage(response.GetTotal(), len(response.GetRevisions()), response.GetPage()) || len(response.GetRevisions()) > 20 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	seen := map[string]bool{}
	previous := int64(maximumSafeJSONInteger)
	for index, item := range response.GetRevisions() {
		if !validPublishedOverlay(item) || seen[item.GetRef()] || index > 0 && item.GetRevision() >= previous {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[item.GetRef()] = true
		previous = item.GetRevision()
	}
	writeMessage(w, http.StatusOK, response, "", "revisions")
}
func (server *Server) GetConfigOverlayRevision(w http.ResponseWriter, r *http.Request, agentRef generated.AgentRef, revisionRef generated.OpaqueRef) {
	if !opaqueHTTPReference.MatchString(agentRef) || !opaqueHTTPReference.MatchString(revisionRef) || !strings.HasPrefix(revisionRef, "cov_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetConfigOverlayRevision(r.Context(), &cp.GetConfigOverlayRevisionRequest{AgentRef: agentRef, RevisionRef: revisionRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validPublishedOverlay(response.GetRevision()) || response.GetRevision().GetRef() != revisionRef {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "revision", "")
}
