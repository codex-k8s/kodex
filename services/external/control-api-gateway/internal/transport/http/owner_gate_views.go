package httptransport

import (
	"net/http"
	"slices"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func writeOwnerGatePage(w http.ResponseWriter, response *cp.ListOwnerGatesResponse, project string, state cp.OwnerGateState, states []cp.OwnerGateState, limit int) {
	next := response.GetPage().GetNextPageToken()
	if response == nil || response.Total < int64(len(response.Gates)) || response.Total > maximumSafeJSONInteger || len(response.Gates) > limit || len(next) > 512 || !utf8.ValidString(next) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	seen := map[string]bool{}
	for _, gate := range response.Gates {
		if gate == nil || !opaqueHTTPReference.MatchString(gate.Ref) || !opaqueHTTPReference.MatchString(gate.ProjectRef) ||
			project != "" && gate.ProjectRef != project || gate.Version < 1 || gate.Version > maximumSafeJSONInteger ||
			gate.State < cp.OwnerGateState_OWNER_GATE_STATE_OPEN || gate.State > cp.OwnerGateState_OWNER_GATE_STATE_EXPIRED ||
			state != cp.OwnerGateState_OWNER_GATE_STATE_UNSPECIFIED && gate.State != state ||
			len(states) != 0 && !slices.Contains(states, gate.State) || seen[gate.Ref] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[gate.Ref] = true
	}
	value, err := messageMap(response)
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	items, _ := value["gates"].([]any)
	if items == nil {
		items = []any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": response.Total, "nextPageToken": next})
}
