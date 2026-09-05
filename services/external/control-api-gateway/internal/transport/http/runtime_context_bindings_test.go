package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func runtimeContextFixture() *controlplanev1.AgentRuntimeConfigurationView {
	_, _, skill := contextFixtures()
	memory := proto.Clone(skill).(*controlplanev1.AgentContextBinding)
	memory.Ref, memory.ResourceRef, memory.RevisionRef = "bind_memory01", "mem_fixture01", "memv_fixture01"
	return &controlplanev1.AgentRuntimeConfigurationView{
		OverlaySchema: runtimeOverlayFixture(),
		Configuration: &controlplanev1.AgentRuntimeConfiguration{AgentRef: "agt_fixture01"}, AgentVersion: 9,
		SkillBindings: []*controlplanev1.AgentContextBinding{skill}, MemoryBindings: []*controlplanev1.AgentContextBinding{memory},
	}
}

func TestRuntimeContextBindingsEveryResponse(t *testing.T) {
	for _, tc := range []struct {
		method, suffix, body string
		status               int
	}{
		{"GET", "/runtime-configuration", "", 200},
		{"PUT", "/runtime-configuration", runtimePublishFixture, 200},
		{"POST", "/config-overlay-drafts", `{"content":""}`, 201},
		{"POST", "/config-overlay-drafts/validation", "", 200},
		{"POST", "/config-overlay-drafts/publication", "", 200},
		{"POST", "/config-overlay-rollbacks", `{"publishedOverlayRef":"ovr_fixture01"}`, 200},
		{"PUT", "/runtime-environment-binding", `{"environmentRef":"renv_fixture01"}`, 200},
	} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			for _, empty := range []bool{false, true} {
				view := runtimeContextFixture()
				if empty {
					view.SkillBindings, view.MemoryBindings = nil, nil
				}
				client := &contextRPCRecorder{corrupt: func(response proto.Message) {
					message := response.ProtoReflect()
					field := message.Descriptor().Fields().ByName("runtime_configuration")
					if field == nil {
						t.Fatal("missing runtime response field")
					}
					message.Set(field, protoreflect.ValueOfMessage(view.ProtoReflect()))
				}}
				w := httptest.NewRecorder()
				contextHandler(client).ServeHTTP(w, managedTestRequest(tc.method, "/api/v1/agents/agt_fixture01"+tc.suffix, tc.body))
				if w.Code != tc.status || w.Header().Get("ETag") != `"9"` {
					t.Fatalf("status=%d etag=%s", w.Code, w.Header().Get("ETag"))
				}
				var got generated.AgentRuntimeConfigurationView
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.SkillBindings == nil || got.MemoryBindings == nil || got.AgentVersion != 9 {
					t.Fatal("missing binding arrays or agent version")
				}
				if empty {
					if len(got.SkillBindings)+len(got.MemoryBindings) != 0 {
						t.Fatal("unexpected bindings")
					}
				} else if len(got.SkillBindings) != 1 || len(got.MemoryBindings) != 1 || got.SkillBindings[0].Version != 3 || got.MemoryBindings[0].RevisionRef != "memv_fixture01" {
					t.Fatal("binding revision lost")
				}
			}
		})
	}
}

func TestRuntimeContextBindingsRejectCorruptResponse(t *testing.T) {
	for name, mutate := range map[string]func(*controlplanev1.AgentRuntimeConfigurationView){
		"agent":           func(v *controlplanev1.AgentRuntimeConfigurationView) { v.Configuration.AgentRef = "agt_other01" },
		"agent-version":   func(v *controlplanev1.AgentRuntimeConfigurationView) { v.AgentVersion = maximumSafeJSONInteger + 1 },
		"binding-agent":   func(v *controlplanev1.AgentRuntimeConfigurationView) { v.SkillBindings[0].AgentRef = "agt_other01" },
		"binding-version": func(v *controlplanev1.AgentRuntimeConfigurationView) { v.SkillBindings[0].Version = 0 },
		"revision":        func(v *controlplanev1.AgentRuntimeConfigurationView) { v.MemoryBindings[0].RevisionRef = "" },
		"digest":          func(v *controlplanev1.AgentRuntimeConfigurationView) { v.MemoryBindings[0].Digest = "invalid" },
		"nil":             func(v *controlplanev1.AgentRuntimeConfigurationView) { v.SkillBindings[0] = nil },
		"duplicate-ref": func(v *controlplanev1.AgentRuntimeConfigurationView) {
			v.MemoryBindings[0].Ref = v.SkillBindings[0].Ref
		},
		"duplicate-resource": func(v *controlplanev1.AgentRuntimeConfigurationView) {
			v.MemoryBindings[0].ResourceRef = v.SkillBindings[0].ResourceRef
		},
		"total": func(v *controlplanev1.AgentRuntimeConfigurationView) {
			for i := 1; i < 128; i++ {
				b := proto.Clone(v.SkillBindings[0]).(*controlplanev1.AgentContextBinding)
				b.Ref = "bind_fixture" + strconv.Itoa(i)
				b.ResourceRef = "skl_fixture" + strconv.Itoa(i)
				v.SkillBindings = append(v.SkillBindings, b)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			view := runtimeContextFixture()
			mutate(view)
			w := httptest.NewRecorder()
			writeAgentRuntimeConfiguration(w, http.StatusOK, view, "agt_fixture01")
			if w.Code != 502 || strings.Contains(w.Body.String(), "skillBindings") || w.Header().Get("ETag") != "" {
				t.Fatalf("corrupt snapshot escaped: %d", w.Code)
			}
		})
	}
}
