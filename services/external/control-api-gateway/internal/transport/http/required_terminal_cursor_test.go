package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"net/http/httptest"
	"testing"
)

func TestRequiredTerminalCursorPages(t *testing.T) {
	for _, next := range []string{"", "next-fixture"} {
		page := &cp.PageInfo{NextPageToken: next}
		for _, tc := range []struct {
			name, field string
			message     proto.Message
		}{
			{"connections", "connections", &cp.ListIntegrationConnectionsResponse{Page: page}},
			{"schedules", "schedules", &cp.ListSchedulesResponse{Page: page}},
			{"audit", "events", &cp.ListAuditEventsResponse{Page: page}},
			{"schedule-revisions", "revisions", &cp.ListScheduleRevisionsResponse{Page: page}},
			{"schedule-runs", "occurrences", &cp.ListScheduleRunsResponse{Page: page}},
			{"image-revisions", "revisions", &cp.ListRoleImageRecipeRevisionsResponse{Page: page}},
			{"provider-definitions", "definitions", &cp.ListProviderDefinitionsResponse{Page: page}},
		} {
			t.Run(tc.name+"/"+next, func(t *testing.T) {
				w := httptest.NewRecorder()
				writeMessage(w, 200, tc.message, "", tc.field)
				var result map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
					t.Fatal("invalid page response")
				}
				if cursor, ok := result["nextPageToken"].(string); !ok || cursor != next {
					t.Fatal("required cursor missing or changed")
				}
				if items, ok := result["items"].([]any); !ok || len(items) != 0 {
					t.Fatal("empty page must contain an array")
				}
			})
		}
	}
}

func TestIntegrationConnectionsEndpointTerminalCursor(t *testing.T) {
	client := &catalogRPCRecorder{response: &cp.ListIntegrationConnectionsResponse{}}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client)}})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/integration-connections?pageSize=40", nil))
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
		t.Fatal("invalid endpoint response")
	}
	if value, ok := result["nextPageToken"].(string); !ok || value != "" {
		t.Fatal("terminal endpoint cursor must be present and empty")
	}
}

func TestOptionalCursorPagePreservesOmission(t *testing.T) {
	w := httptest.NewRecorder()
	writeMessage(w, 200, &cp.ListProjectsResponse{}, "", "projects")
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, present := result["nextPageToken"]; present {
		t.Fatal("optional page gained a cursor")
	}
}
