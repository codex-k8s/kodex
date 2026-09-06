package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) ListProviderAccountBlockers(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.ListProviderAccountBlockersParams) {
	query := stringValue(p.Query)
	kind := cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_UNSPECIFIED
	if p.Kind != nil {
		kind = cp.ProviderAccountBlockerKind(cp.ProviderAccountBlockerKind_value["PROVIDER_ACCOUNT_BLOCKER_KIND_"+string(*p.Kind)])
	}
	if !effectiveCapabilityRef(ref) || !utf8.ValidString(query) || utf8.RuneCountInString(query) > 200 || strings.ContainsRune(query, 0) ||
		p.Kind != nil && !validProviderBlockerKind(kind) || p.PageSize != nil && (*p.PageSize < 1 || *p.PageSize > 100) ||
		len(stringValue(p.PageToken)) > 2048 {
		writeLocalProblem(w, http.StatusBadRequest, "INPUT_INVALID", false)
		return
	}
	response, err := server.control.Query.ListProviderAccountBlockers(r.Context(), &cp.ListProviderAccountBlockersRequest{AccountRef: ref, Kind: kind, Query: query, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validProviderBlockerPage(response, kind, p.PageSize, stringValue(p.PageToken)) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	value, err := messageMap(response)
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	delete(value, "page")
	if token := response.GetPage().GetNextPageToken(); token != "" {
		value["nextPageToken"] = token
	}
	setVersionETag(w, uint64(response.AccountVersion))
	writeJSON(w, http.StatusOK, value)
}

func validProviderBlockerKind(kind cp.ProviderAccountBlockerKind) bool {
	return kind >= cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_AGENT && kind <= cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_WARM_RUNTIME
}

func validProviderBlockerPage(p *cp.ListProviderAccountBlockersResponse, kind cp.ProviderAccountBlockerKind, size *int, cursor string) bool {
	if p == nil || !validManagedVersion(p.AccountVersion) || p.DeletionIntentVersion < 0 || p.DeletionIntentVersion > maximumSafeJSONInteger ||
		p.Total < int64(len(p.Items)) || p.Total > maximumSafeJSONInteger || p.HiddenCount < 0 || p.HiddenCount > maximumSafeJSONInteger ||
		!modelCatalogDigest.MatchString(p.ContextDigest) || len(p.Items) > 100 || size != nil && len(p.Items) > *size {
		return false
	}
	seen := map[string]bool{}
	for _, item := range p.Items {
		if item == nil || !validProviderBlockerKind(item.Kind) || kind != 0 && item.Kind != kind || !effectiveCapabilityRef(item.Ref) ||
			!validManagedVersion(item.Version) || !utf8.ValidString(item.Name) || utf8.RuneCountInString(item.Name) > 300 || strings.ContainsRune(item.Name, 0) ||
			item.ProjectRef != "" && !effectiveCapabilityRef(item.ProjectRef) || item.CanCancel && item.Kind != cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_QUEUED_TURN {
			return false
		}
		key := item.Kind.String() + ":" + item.Ref
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	token := p.GetPage().GetNextPageToken()
	return len(token) <= 2048 && (token == "" || token != cursor && len(p.Items) != 0)
}

func (server *Server) CancelProviderAccountQueuedWork(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.CancelProviderAccountQueuedWorkParams) {
	body, ok := decodeJSON[generated.ProviderAccountQueuedWorkCancellationInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	refs := make([]string, 0, len(body.SelectedRunRefs))
	seen := map[string]bool{}
	for _, selected := range body.SelectedRunRefs {
		value := string(selected)
		if !effectiveCapabilityRef(value) || seen[value] {
			writeLocalProblem(w, http.StatusBadRequest, "INPUT_INVALID", false)
			return
		}
		seen[value] = true
		refs = append(refs, value)
	}
	if !effectiveCapabilityRef(ref) || len(refs) < 1 || len(refs) > 64 || !modelCatalogDigest.MatchString(body.BlockersDigest) {
		writeLocalProblem(w, http.StatusBadRequest, "INPUT_INVALID", false)
		return
	}
	response, err := server.control.Command.CancelProviderAccountQueuedWork(r.Context(), &cp.CancelProviderAccountQueuedWorkRequest{Mutation: mutation, AccountRef: ref, SelectedRunRefs: refs, BlockersDigest: body.BlockersDigest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetAccount() == nil || response.Account.Ref != ref || !validManagedVersion(response.Account.Version) || len(response.Outcomes) != len(refs) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for index, outcome := range response.Outcomes {
		if outcome == nil || outcome.RunRef != refs[index] || outcome.Outcome < cp.ProviderAccountQueuedWorkOutcome_PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_CANCELLED || outcome.Outcome > cp.ProviderAccountQueuedWorkOutcome_PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_NOT_FOUND {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	setVersionETag(w, uint64(response.Account.Version))
	writeMessage(w, http.StatusOK, response, "", "")
}
