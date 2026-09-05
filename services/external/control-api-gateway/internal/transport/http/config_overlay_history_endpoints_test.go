package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func historyOverlayFixture() *cp.ConfigOverlayVersion {
	content := "personality = \"friendly\"\n"
	digest := sha256.Sum256([]byte(content))
	at := timestamppb.New(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	return &cp.ConfigOverlayVersion{Ref: "cov_fixture01", Version: 3, Revision: 3, State: "SUPERSEDED", Content: content, Digest: hex.EncodeToString(digest[:]), CreatedAt: at, PublishedAt: at}
}
func TestConfigOverlayHistoryHTTP(t *testing.T) {
	item := historyOverlayFixture()
	client := &catalogRPCRecorder{response: &cp.ListConfigOverlayRevisionsResponse{Revisions: []*cp.ConfigOverlayVersion{item}, Total: 7, Page: &cp.PageInfo{NextPageToken: "next"}}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/config-overlay/revisions?query=cov&pageSize=2&pageToken=cursor", nil))
	var page generated.ConfigOverlayRevisionPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Total != 7 || len(page.Items) != 1 || page.Items[0].Content != item.Content {
		t.Fatalf("history page mapping: %d %s", w.Code, w.Body.String())
	}
	request := client.request.(*cp.ListConfigOverlayRevisionsRequest)
	if request.AgentRef != "agt_fixture01" || request.Query != "cov" || request.Page.PageSize != 2 || request.Page.PageToken != "cursor" {
		t.Fatal("history request binding lost")
	}
	client = &catalogRPCRecorder{response: &cp.GetConfigOverlayRevisionResponse{Revision: item}}
	w = httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/config-overlay/revisions/cov_fixture01", nil))
	if w.Code != 200 || w.Header().Get("ETag") != "\"3\"" {
		t.Fatalf("history exact read: %d", w.Code)
	}
	exact := client.request.(*cp.GetConfigOverlayRevisionRequest)
	if exact.AgentRef != "agt_fixture01" || exact.RevisionRef != "cov_fixture01" {
		t.Fatal("exact history refs lost")
	}
}
func TestConfigOverlayHistoryRejectsUnsafeProducer(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ConfigOverlayVersion){
		"digest":              func(v *cp.ConfigOverlayVersion) { v.Digest = strings.Repeat("a", 64) },
		"unpublished":         func(v *cp.ConfigOverlayVersion) { v.State = "VALID" },
		"missing publication": func(v *cp.ConfigOverlayVersion) { v.PublishedAt = nil },
		"version mismatch":    func(v *cp.ConfigOverlayVersion) { v.Version++ },
		"diagnostic": func(v *cp.ConfigOverlayVersion) {
			v.Diagnostics = []*cp.ConfigOverlayDiagnostic{{Code: "RAW", Message: "private detail"}}
		},
		"foreign ref": func(v *cp.ConfigOverlayVersion) { v.Ref = "cov_other001" },
	} {
		t.Run(name, func(t *testing.T) {
			item := historyOverlayFixture()
			mutate(item)
			client := &catalogRPCRecorder{response: &cp.GetConfigOverlayRevisionResponse{Revision: item}}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/config-overlay/revisions/cov_fixture01", nil))
			if w.Code != 502 || strings.Contains(w.Body.String(), "private detail") {
				t.Fatalf("unsafe history response: %d", w.Code)
			}
		})
	}
	for _, response := range []*cp.ListConfigOverlayRevisionsResponse{
		{Total: -1}, {Total: maximumSafeJSONInteger + 1},
		{Revisions: []*cp.ConfigOverlayVersion{historyOverlayFixture()}, Total: 0},
		{Revisions: []*cp.ConfigOverlayVersion{historyOverlayFixture(), historyOverlayFixture()}, Total: 2},
	} {
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/config-overlay/revisions", nil))
		if w.Code != 502 {
			t.Fatalf("invalid history page accepted: %d", w.Code)
		}
	}
	for code, want := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.InvalidArgument: 400, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private detail")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/config-overlay/revisions", nil))
		if w.Code != want || strings.Contains(w.Body.String(), "private detail") {
			t.Fatalf("history owner error: %d", w.Code)
		}
	}
}
func TestHomeCatalogTotalsHTTP(t *testing.T) {
	for _, test := range []struct {
		path     string
		response proto.Message
	}{
		{"runs?states=RUNNING", &cp.ListRunsResponse{Total: 7}},
		{"artifacts?query=shared&type=TEXT&scanState=CLEAN&sourceKind=CONTROL_CENTER", &cp.ListArtifactsResponse{Total: 7}},
		{"projects/prj_fixture01/artifacts", &cp.ListArtifactsResponse{Total: 7}},
	} {
		client := &catalogRPCRecorder{response: test.response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+test.path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "\"total\":7") {
			t.Fatalf("count lost: %s %d", test.path, w.Code)
		}
		if strings.HasPrefix(test.path, "artifacts?") {
			q := client.request.(*cp.ListArtifactsRequest)
			if q.ProjectRef != "" || q.Query != "shared" || q.Type != cp.ArtifactType_ARTIFACT_TYPE_TEXT || q.ScanState != cp.ArtifactScanState_ARTIFACT_SCAN_STATE_CLEAN {
				t.Fatal("global artifact request changed owner filtering")
			}
		}
	}
	for _, total := range []int64{-1, maximumSafeJSONInteger + 1} {
		for _, path := range []string{"runs", "artifacts"} {
			var response proto.Message = &cp.ListRunsResponse{Total: total}
			if path == "artifacts" {
				response = &cp.ListArtifactsResponse{Total: total}
			}
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+path, nil))
			if w.Code != 502 {
				t.Fatalf("invalid count accepted: %d", w.Code)
			}
		}
	}
	for _, path := range []string{"runs", "artifacts"} {
		var response proto.Message = &cp.ListRunsResponse{}
		if path == "artifacts" {
			response = &cp.ListArtifactsResponse{}
		}
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/"+path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "\"total\":0") {
			t.Fatalf("zero count omitted: %d", w.Code)
		}
	}
}
func TestEnvironmentDraftBaseAndSavedAtHTTP(t *testing.T) {
	at := timestamppb.New(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	client := &environmentDraftRecorder{mutate: func(v *cp.RuntimeEnvironmentDraft) {
		v.EnvironmentRef = "renv_fixture01"
		v.ExpectedEnvironmentVersion = 7
		v.BaseVersionRef = "renvv_fixture01"
		v.BaseRevision = 3
		v.SavedAt = at
	}}
	w := httptest.NewRecorder()
	draftTestHandler(client).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""))
	var result generated.RuntimeEnvironmentDraft
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.BaseRevision == nil || *result.BaseRevision != 3 || result.BaseVersionRef == nil || *result.BaseVersionRef != "renvv_fixture01" || result.SavedAt == nil || !result.SavedAt.Equal(at.AsTime()) {
		t.Fatalf("draft base/read time lost: %d", w.Code)
	}
	for _, mutate := range []func(*cp.RuntimeEnvironmentDraft){
		func(v *cp.RuntimeEnvironmentDraft) { v.BaseVersionRef = "renvv_fixture01" },
		func(v *cp.RuntimeEnvironmentDraft) { v.BaseRevision = 1 },
		func(v *cp.RuntimeEnvironmentDraft) { v.BaseRevision = -1 },
		func(v *cp.RuntimeEnvironmentDraft) { v.SavedAt = &timestamppb.Timestamp{Seconds: 1 << 62} },
	} {
		client := &environmentDraftRecorder{mutate: mutate}
		w := httptest.NewRecorder()
		draftTestHandler(client).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-environment-drafts/renvd_fixture01", ""))
		if w.Code != 502 {
			t.Fatalf("invalid base/time accepted: %d", w.Code)
		}
	}
}
