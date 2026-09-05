package httptransport

import (
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

func runtimeDiffFixture() *cp.GetRuntimeRevisionDiffResponse {
	digest := strings.Repeat("a", 64)
	result := &cp.GetRuntimeRevisionDiffResponse{Current: &cp.PublicRuntimeRevisionIdentity{Ref: "rrv_current01", Version: 2, RunRef: "run_fixture01", SessionRef: "ses_fixture01", TurnRef: "turn_fixture01", Attempt: 1, RevisionDigest: digest, CreatedAt: timestamppb.New(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))}}
	for code := int32(1); code <= 11; code++ {
		v := &cp.RuntimeRevisionDiffValue{}
		switch code {
		case 1:
			v.Ref = "openai"
		case 2:
			v.Ref = "gpt-6-astra"
		case 3:
			v.Ref = "developer"
			v.Revision = "release-v1"
		case 4, 5, 6, 7, 8:
			v.Ref = "ref_fixture01"
			v.Version = 2
			v.Digest = digest
		case 9:
			v.Ref = "instruction_v1"
			v.Digest = digest
		case 10:
			v.Digest = digest
		case 11:
			v.Digest = "sha256:" + digest
		}
		result.Changes = append(result.Changes, &cp.RuntimeRevisionDiffChange{Component: cp.RuntimeRevisionDiffComponent(code), Current: v})
	}
	return result
}

func TestRuntimeRevisionDiffTypedProjection(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		response := runtimeDiffFixture()
		path := "/api/v1/runs/run_fixture01/runtime-revision-diff"
		if pinned {
			path += "?currentRevisionRef=rrv_current01"
		}
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || client.method != cp.PlatformQueryService_GetRuntimeRevisionDiff_FullMethodName {
			t.Fatalf("diff route: %d %s", w.Code, w.Body.String())
		}
		q := client.request.(*cp.GetRuntimeRevisionDiffRequest)
		if q.RunRef != "run_fixture01" || (q.CurrentRevisionRef != "") != pinned {
			t.Fatal("exact revision request changed")
		}
		var result generated.RuntimeRevisionDiff
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Changes) != 11 || result.Previous != nil || result.Current.RevisionDigest != response.Current.RevisionDigest || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("safe projection lost")
		}
		if *result.Changes[1].Current.Ref != "gpt-6-astra" || *result.Changes[2].Current.Revision != "release-v1" || *result.Changes[10].Current.Digest != "sha256:"+strings.Repeat("a", 64) {
			t.Fatal("component values changed")
		}
		for _, forbidden := range []string{"snapshot", "prompt", "credential", "locator", "content"} {
			if strings.Contains(w.Body.String(), `"`+forbidden+`"`) {
				t.Fatal("private snapshot field exposed")
			}
		}
	}
}

func TestRuntimeRevisionDiffPreviousAndEmpty(t *testing.T) {
	response := runtimeDiffFixture()
	old := proto.Clone(response.Current).(*cp.PublicRuntimeRevisionIdentity)
	old.Ref, old.RunRef, old.Version, old.TurnRef = "rrv_previous01", "run_previous01", 1, ""
	old.CreatedAt = timestamppb.New(old.CreatedAt.AsTime().Add(-time.Minute))
	response.Previous = old
	response.Changes = []*cp.RuntimeRevisionDiffChange{{Component: cp.RuntimeRevisionDiffComponent_RUNTIME_REVISION_DIFF_COMPONENT_MODEL, Previous: &cp.RuntimeRevisionDiffValue{Ref: "gpt-5.5"}, Current: &cp.RuntimeRevisionDiffValue{Ref: "gpt-6-astra"}}}
	for _, empty := range []bool{false, true} {
		if empty {
			response.Changes = nil
		}
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs/run_fixture01/runtime-revision-diff", nil))
		var result generated.RuntimeRevisionDiff
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Previous == nil || result.Previous.RunRef != "run_previous01" || result.Previous.TurnRef != nil || (len(result.Changes) == 0) != empty {
			t.Fatalf("predecessor lost: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestRuntimeRevisionDiffRejectMalformed(t *testing.T) {
	for _, path := range []string{"/api/v1/runs/bad!/runtime-revision-diff", "/api/v1/runs/run_fixture01/runtime-revision-diff?currentRevisionRef=", "/api/v1/runs/run_fixture01/runtime-revision-diff?currentRevisionRef=bad!"} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid diff reached CP: %d", w.Code)
		}
	}
	for name, mutate := range map[string]func(*cp.GetRuntimeRevisionDiffResponse){
		"missing current":   func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current = nil },
		"wrong run":         func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current.RunRef = "run_foreign01" },
		"wrong pin":         func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current.Ref = "rrv_foreign01" },
		"bad version":       func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current.Version = maximumSafeJSONInteger + 1 },
		"bad attempt":       func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current.Attempt = 0 },
		"bad timestamp":     func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current.CreatedAt = nil },
		"bad digest":        func(r *cp.GetRuntimeRevisionDiffResponse) { r.Current.RevisionDigest = "private" },
		"unknown component": func(r *cp.GetRuntimeRevisionDiffResponse) { r.Changes[0].Component = 99 },
		"duplicate":         func(r *cp.GetRuntimeRevisionDiffResponse) { r.Changes[1] = r.Changes[0] },
		"missing value":     func(r *cp.GetRuntimeRevisionDiffResponse) { r.Changes[0].Current = nil },
		"unexpected previous": func(r *cp.GetRuntimeRevisionDiffResponse) {
			r.Changes[0].Previous = &cp.RuntimeRevisionDiffValue{Ref: "private"}
		},
		"foreign session": func(r *cp.GetRuntimeRevisionDiffResponse) {
			old := proto.Clone(r.Current).(*cp.PublicRuntimeRevisionIdentity)
			old.Ref = "rrv_previous01"
			old.SessionRef = "ses_foreign01"
			r.Previous = old
		},
		"profile digest":     func(r *cp.GetRuntimeRevisionDiffResponse) { r.Changes[2].Current.Digest = strings.Repeat("a", 64) },
		"incomplete binding": func(r *cp.GetRuntimeRevisionDiffResponse) { r.Changes[7].Current.Version = 0 },
		"image locator": func(r *cp.GetRuntimeRevisionDiffResponse) {
			r.Changes[10].Current.Ref = "private-registry.example/image"
		},
		"image digest": func(r *cp.GetRuntimeRevisionDiffResponse) { r.Changes[10].Current.Digest = "latest" },
	} {
		t.Run(name, func(t *testing.T) {
			response := runtimeDiffFixture()
			mutate(response)
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs/run_fixture01/runtime-revision-diff?currentRevisionRef=rrv_current01", nil))
			if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("corrupt diff accepted: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRuntimeRevisionDiffOwnerErrors(t *testing.T) {
	for code, want := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.InvalidArgument: 400, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private upstream detail")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/runs/run_fixture01/runtime-revision-diff", nil))
		if w.Code != want || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("unsafe diff error: %d", w.Code)
		}
	}
}
