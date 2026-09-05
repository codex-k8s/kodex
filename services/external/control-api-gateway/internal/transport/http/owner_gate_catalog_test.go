package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func gateCatalogFixture() *cp.ListOwnerGatesResponse {
	return &cp.ListOwnerGatesResponse{
		Gates: []*cp.OwnerGate{{Ref: "gate_fixture01", ProjectRef: "prj_fixture01", Version: 3, State: cp.OwnerGateState_OWNER_GATE_STATE_APPROVED}},
		Total: 91,
		Page:  &cp.PageInfo{NextPageToken: "opaque_snapshot_cursor"},
	}
}

func TestOwnerGateCatalogPreservesQueryStatesTotalAndCursor(t *testing.T) {
	client := &catalogRPCRecorder{response: gateCatalogFixture()}
	w := httptest.NewRecorder()
	query := "Проверка %_"
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates?projectRef=prj_fixture01&query="+url.QueryEscape(query)+"&states=APPROVED&states=CHANGES_REQUESTED&states=REJECTED&states=CANCELLED&states=EXPIRED&pageSize=30&pageToken=original_snapshot", nil))
	if w.Code != 200 || client.method != cp.PlatformQueryService_ListOwnerGates_FullMethodName {
		t.Fatalf("catalog request failed: %d", w.Code)
	}
	request := client.request.(*cp.ListOwnerGatesRequest)
	wantStates := []cp.OwnerGateState{cp.OwnerGateState_OWNER_GATE_STATE_APPROVED, cp.OwnerGateState_OWNER_GATE_STATE_CHANGES_REQUESTED, cp.OwnerGateState_OWNER_GATE_STATE_REJECTED, cp.OwnerGateState_OWNER_GATE_STATE_CANCELLED, cp.OwnerGateState_OWNER_GATE_STATE_EXPIRED}
	if request.Query != query || request.ProjectRef != "prj_fixture01" || request.State != cp.OwnerGateState_OWNER_GATE_STATE_UNSPECIFIED || !slices.Equal(request.States, wantStates) || request.Page.PageToken != "original_snapshot" || request.Page.PageSize != 30 {
		t.Fatal("owner query, state set or snapshot cursor changed")
	}
	var body struct {
		Items []json.RawMessage `json:"items"`
		Total int64             `json:"total"`
		Next  string            `json:"nextPageToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Total != 91 || body.Next != "opaque_snapshot_cursor" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authoritative total, items or cursor lost")
	}
	for _, total := range []int64{0, 91} {
		client.response = &cp.ListOwnerGatesResponse{Total: total}
		w = httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates", nil))
		var empty map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &empty); err != nil || w.Code != 200 || empty["total"] != float64(total) || empty["nextPageToken"] != "" || len(empty["items"].([]any)) != 0 {
			t.Fatal("empty page lost required total or terminal cursor")
		}
	}
}

func TestOwnerGateCatalogAcceptsEveryKnownStateInSet(t *testing.T) {
	for value := int32(1); value <= 6; value++ {
		state := cp.OwnerGateState(value)
		response := gateCatalogFixture()
		response.Gates[0].State = state
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates?states="+strings.TrimPrefix(state.String(), "OWNER_GATE_STATE_"), nil))
		if w.Code != 200 || !slices.Equal(client.request.(*cp.ListOwnerGatesRequest).States, []cp.OwnerGateState{state}) {
			t.Fatalf("known state was lost: %s", state)
		}
	}
}

func TestOwnerGateCatalogRejectsInvalidFiltersBeforeRPC(t *testing.T) {
	for _, query := range []string{
		"state=OPEN&states=OPEN", "state=OPEN&states=APPROVED", "states=", "states=UNKNOWN", "states=UNSPECIFIED",
		"states=OPEN&states=OPEN", "states=OPEN,APPROVED", "states=OPEN&states=APPROVED&states=REJECTED&states=CHANGES_REQUESTED&states=CANCELLED&states=EXPIRED&states=OPEN",
		"query=%00", "query=%FF", "query=" + url.QueryEscape(strings.Repeat("я", 201)),
	} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid owner catalog filter reached RPC: %d", w.Code)
		}
	}
}

func TestOwnerGateCatalogRejectsMalformedReadback(t *testing.T) {
	for name, change := range map[string]func(*cp.ListOwnerGatesResponse){
		"negative total":     func(r *cp.ListOwnerGatesResponse) { r.Total = -1 },
		"unsafe total":       func(r *cp.ListOwnerGatesResponse) { r.Total = maximumSafeJSONInteger + 1 },
		"total below page":   func(r *cp.ListOwnerGatesResponse) { r.Total = 0 },
		"nil item":           func(r *cp.ListOwnerGatesResponse) { r.Gates[0] = nil },
		"duplicate":          func(r *cp.ListOwnerGatesResponse) { r.Gates = append(r.Gates, proto.Clone(r.Gates[0]).(*cp.OwnerGate)) },
		"foreign project":    func(r *cp.ListOwnerGatesResponse) { r.Gates[0].ProjectRef = "prj_foreign01" },
		"bad ref":            func(r *cp.ListOwnerGatesResponse) { r.Gates[0].Ref = "bad!" },
		"zero version":       func(r *cp.ListOwnerGatesResponse) { r.Gates[0].Version = 0 },
		"unknown state":      func(r *cp.ListOwnerGatesResponse) { r.Gates[0].State = cp.OwnerGateState(999) },
		"zero state":         func(r *cp.ListOwnerGatesResponse) { r.Gates[0].State = cp.OwnerGateState_OWNER_GATE_STATE_UNSPECIFIED },
		"wrong filter state": func(r *cp.ListOwnerGatesResponse) { r.Gates[0].State = cp.OwnerGateState_OWNER_GATE_STATE_OPEN },
		"oversized cursor":   func(r *cp.ListOwnerGatesResponse) { r.Page.NextPageToken = strings.Repeat("x", 513) },
		"invalid cursor":     func(r *cp.ListOwnerGatesResponse) { r.Page.NextPageToken = string([]byte{0xff}) },
	} {
		t.Run(name, func(t *testing.T) {
			response := gateCatalogFixture()
			change(response)
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates?projectRef=prj_fixture01&states=APPROVED&pageSize=2", nil))
			if w.Code != 502 || !strings.Contains(w.Body.String(), "INVALID_UPSTREAM_RESPONSE") || strings.Contains(w.Body.String(), "gate_fixture01") {
				t.Fatalf("malformed owner page was exposed: %d", w.Code)
			}
		})
	}
	response := gateCatalogFixture()
	response.Gates = append(response.Gates, &cp.OwnerGate{Ref: "gate_fixture02", ProjectRef: "prj_fixture01", Version: 1, State: cp.OwnerGateState_OWNER_GATE_STATE_APPROVED})
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/owner-gates?pageSize=1", nil))
	if w.Code != 502 {
		t.Fatal("oversized page was exposed")
	}
}
