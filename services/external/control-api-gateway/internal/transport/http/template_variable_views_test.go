package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func variableFixture() *cp.TemplateVariable {
	return &cp.TemplateVariable{Name: "agent.name", ValueType: "string", Source: "AGENT", Reason: cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AGENT_CONTEXT_REQUIRED}
}

func TestTemplateVariablesProducerVocabulary(t *testing.T) {
	for _, path := range []string{"/api/v1/projects/prj_fixture01/template-variables", "/api/v1/prompt-templates/catalog"} {
		for _, source := range []string{"AGENT", "AUTOMATION", "RUNTIME", "GATE", "INPUT", "RUN", "ORGANIZATION", "PROJECT", "SESSION", "USER", "WORKFLOW"} {
			for _, kind := range []struct{ producer, public string }{{"string", "STRING"}, {"reference", "OPAQUE_REF"}, {"integer", "INTEGER"}, {"collection", "COLLECTION"}} {
				for _, descriptor := range []struct{ producer, public string }{{"file_descriptor", "FILE_DESCRIPTOR"}, {"tool_descriptor", "TOOL_DESCRIPTOR"}} {
					v := variableFixture()
					v.ValueType, v.Source = kind.producer, source
					if kind.producer == "collection" {
						v.Collection, v.ItemValueType = true, descriptor.producer
						v.ItemFields = []*cp.TemplateVariableField{{Name: "artifact_ref", ValueType: "reference"}, {Name: "name", ValueType: "string"}, {Name: "size", ValueType: "integer"}}
					}
					client := &catalogRPCRecorder{response: &cp.ListTemplateVariablesResponse{Variables: []*cp.TemplateVariable{v}, Total: 1}}
					w := httptest.NewRecorder()
					catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
					if w.Code != 200 || !strings.Contains(w.Body.String(), `"valueType":"`+kind.public+`"`) || !strings.Contains(w.Body.String(), `"source":"`+source+`"`) {
						t.Fatalf("%s/%s: %d %s", source, kind.producer, w.Code, w.Body.String())
					}
					if v.Collection && (!strings.Contains(w.Body.String(), `"itemValueType":"`+descriptor.public+`"`) || !strings.Contains(w.Body.String(), `"name":"artifact_ref","valueType":"OPAQUE_REF"`)) {
						var body struct {
							Items []struct {
								ItemFields    []struct{ Name, ValueType string }
								ItemValueType string
							}
						}
						if json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Items) != 1 || body.Items[0].ItemValueType != descriptor.public || len(body.Items[0].ItemFields) != 3 || body.Items[0].ItemFields[0].Name != "artifact_ref" || body.Items[0].ItemFields[0].ValueType != "OPAQUE_REF" {
							t.Fatalf("descriptor lost: %s", w.Body.String())
						}
					}
				}
			}
		}
	}
}

func TestTemplateVariablesOwnerContext(t *testing.T) {
	for _, path := range []string{"/api/v1/projects/prj_fixture01/template-variables?", "/api/v1/prompt-templates/catalog?projectRef=prj_fixture01&"} {
		for reason := int32(1); reason <= 5; reason++ {
			v := variableFixture()
			v.Reason, v.Available = cp.TemplateVariableAvailabilityReason(reason), reason == 1
			client := &catalogRPCRecorder{response: &cp.ListTemplateVariablesResponse{Variables: []*cp.TemplateVariable{v}, Total: 3, Page: &cp.PageInfo{NextPageToken: "next"}}}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path+"agentRef=agt_fixture01&runtimeRevisionRef=rrv_fixture01&query=agent&pageSize=10&pageToken=first", nil))
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			q := client.request.(*cp.ListTemplateVariablesRequest)
			if q.ProjectRef != "prj_fixture01" || q.AgentRef != "agt_fixture01" || q.RuntimeRevisionRef != "rrv_fixture01" || q.Query != "agent" || q.Page.PageSize != 10 || q.Page.PageToken != "first" {
				t.Fatalf("wrong context: %v", q)
			}
			var body struct {
				Items         []map[string]any
				Total         int64
				NextPageToken string
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Items) != 1 || body.Total != 3 || body.NextPageToken != "next" || body.Items[0]["available"] != v.Available || body.Items[0]["reason"] != strings.TrimPrefix(v.Reason.String(), "TEMPLATE_VARIABLE_AVAILABILITY_REASON_") {
				t.Fatalf("wrong view: %s", w.Body.String())
			}
		}
	}
	client := &catalogRPCRecorder{response: &cp.ListTemplateVariablesResponse{}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/prompt-templates/catalog", nil))
	if w.Code != 200 || client.request.(*cp.ListTemplateVariablesRequest).ProjectRef != "" || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("global: %d %s", w.Code, w.Body.String())
	}
}

func TestTemplateVariablesRejectMalformed(t *testing.T) {
	for _, path := range []string{"/api/v1/projects/prj_fixture01/template-variables", "/api/v1/prompt-templates/catalog"} {
		for _, query := range []string{"agentRef=bad!", "runtimeRevisionRef=bad!", "query=%00", "pageSize=101", "pageSize=0", "pageToken=" + strings.Repeat("x", 513)} {
			client := &catalogRPCRecorder{}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path+"?"+query, nil))
			if w.Code != 400 || client.method != "" {
				t.Fatalf("%s: %d, RPC %s", query, w.Code, client.method)
			}
		}
	}
	for _, mutate := range []func(*cp.ListTemplateVariablesResponse){
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].ValueType = "STRING" },
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].ValueType = "unknown" },
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].ItemValueType = "unknown_descriptor" },
		func(r *cp.ListTemplateVariablesResponse) {
			r.Variables[0].ItemFields = []*cp.TemplateVariableField{{Name: "name", ValueType: "unknown"}}
		},
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].Reason = 0 },
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].Reason = 99 },
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].Available = true },
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].Reason = 1 },
		func(r *cp.ListTemplateVariablesResponse) { r.Total = 0 },
		func(r *cp.ListTemplateVariablesResponse) { r.Total = maximumSafeJSONInteger + 1 },
		func(r *cp.ListTemplateVariablesResponse) {
			r.Page = &cp.PageInfo{NextPageToken: strings.Repeat("x", 513)}
		},
		func(r *cp.ListTemplateVariablesResponse) { r.Variables[0].Source = "UNKNOWN" },
		func(r *cp.ListTemplateVariablesResponse) {
			r.Variables = append(r.Variables, variableFixture())
			r.Total = 2
		},
	} {
		r := &cp.ListTemplateVariablesResponse{Variables: []*cp.TemplateVariable{variableFixture()}, Total: 1}
		mutate(r)
		client := &catalogRPCRecorder{response: r}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/prompt-templates/catalog?pageSize=1", nil))
		if w.Code != 502 {
			t.Fatalf("upstream accepted: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestTemplateVariablesOwnerErrors(t *testing.T) {
	for code, expected := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.InvalidArgument: 400, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private upstream detail")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/prompt-templates/catalog", nil))
		if w.Code != expected || strings.Contains(w.Body.String(), "private upstream detail") {
			t.Fatalf("%s: %d %s", code, w.Code, w.Body.String())
		}
	}
}
