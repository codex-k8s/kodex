package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/usertext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const cardActivityTime = "2026-09-06T10:00:00Z"

func catalogCardFixtures() (*cp.Project, *cp.Agent, *cp.Workflow) {
	activity := timestamppb.New(time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC))
	project := &cp.Project{Ref: "prj_fixture01", Version: 4, IntegrationState: "NONE", LastActivityAt: activity,
		UpdatedAt: timestamppb.New(activity.AsTime().Add(-time.Hour))}
	agent := &cp.Agent{Ref: "agt_fixture01", Version: 4, ProjectRef: project.Ref, CurrentRunRef: "TYPE_literal_run01"}
	workflow := &cp.Workflow{Ref: "wfl_fixture01", Version: 4, ProjectRef: project.Ref,
		DraftVersion: &cp.WorkflowVersion{Ref: "wfv_fixture01", Version: 2, Revision: 2, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT}}
	workflowReadinessFixture(workflow)
	// Числа намеренно отличаются от длины локальных steps: это owner projection.
	workflow.CardSummary = &cp.WorkflowCardSummary{StageCount: 17, UniqueAgentCount: 11, ParallelGroupCount: 7,
		HasHumanGate: true, ActiveRunCount: 13, PendingGateCount: 5, LastActivityAt: activity}
	return project, agent, workflow
}

func assertCatalogCard(t *testing.T, name protoreflect.FullName, value map[string]any, empty bool) {
	t.Helper()
	switch name {
	case "controlplane.v1.Project":
		if value["integrationState"] != "NONE" {
			t.Fatal("integration state changed")
		}
		for _, key := range []string{"agentCount", "workflowCount", "activeRunCount", "pendingGateCount"} {
			if value[key] != float64(0) {
				t.Fatalf("required project zero lost: %s", key)
			}
		}
	case "controlplane.v1.Agent":
		if empty {
			if _, exists := value["currentRunRef"]; exists {
				t.Fatal("empty current run reference was fabricated")
			}
		} else if value["currentRunRef"] != "TYPE_literal_run01" {
			t.Fatal("literal owner run reference changed")
		}
		return
	case "controlplane.v1.Workflow":
		var ok bool
		value, ok = value["cardSummary"].(map[string]any)
		if !ok {
			t.Fatal("required summary lost")
		}
		for key, expected := range map[string]float64{"stageCount": 17, "uniqueAgentCount": 11, "parallelGroupCount": 7, "activeRunCount": 13, "pendingGateCount": 5} {
			if empty {
				expected = 0
			}
			if value[key] != expected {
				t.Fatalf("owner count changed: %s", key)
			}
		}
		if value["hasHumanGate"] != !empty {
			t.Fatal("owner gate flag changed")
		}
	}
	if empty {
		if _, exists := value["lastActivityAt"]; exists {
			t.Fatal("missing activity became metadata or epoch")
		}
	} else if value["lastActivityAt"] != cardActivityTime {
		t.Fatal("owner activity timestamp changed")
	}
}

func TestCatalogCardsPreserveEveryProducerResponseEnvelope(t *testing.T) {
	counts := map[protoreflect.FullName]int{}
	// Перебираются generated Go wrappers, включая команды и replay, а не копия списка RPC.
	protoregistry.GlobalTypes.RangeMessages(func(kind protoreflect.MessageType) bool {
		descriptor := kind.Descriptor()
		if !strings.HasPrefix(string(descriptor.FullName()), "controlplane.v1.") || !strings.HasSuffix(string(descriptor.Name()), "Response") {
			return true
		}
		for index := 0; index < descriptor.Fields().Len(); index++ {
			field := descriptor.Fields().Get(index)
			if field.Message() == nil || field.IsMap() {
				continue
			}
			name := field.Message().FullName()
			if name != "controlplane.v1.Project" && name != "controlplane.v1.Agent" && name != "controlplane.v1.Workflow" {
				continue
			}
			counts[name]++
			for _, empty := range []bool{false, true} {
				t.Run(string(descriptor.Name())+map[bool]string{false: "/active", true: "/empty"}[empty], func(t *testing.T) {
					project, agent, workflow := catalogCardFixtures()
					if empty {
						project.LastActivityAt = nil
						agent.CurrentRunRef = ""
						workflow.CardSummary = &cp.WorkflowCardSummary{}
					}
					card := map[protoreflect.FullName]proto.Message{"controlplane.v1.Project": project, "controlplane.v1.Agent": agent, "controlplane.v1.Workflow": workflow}[name]
					envelope := kind.New()
					if field.IsList() {
						envelope.Mutable(field).List().Append(protoreflect.ValueOfMessage(card.ProtoReflect()))
					} else {
						envelope.Set(field, protoreflect.ValueOfMessage(card.ProtoReflect()))
					}
					result, err := messageMap(envelope.Interface())
					if err != nil {
						t.Fatal(err)
					}
					value := result[field.JSONName()]
					if field.IsList() {
						value = value.([]any)[0]
					}
					assertCatalogCard(t, name, value.(map[string]any), empty)
				})
			}
		}
		return true
	})
	for name, expected := range map[protoreflect.FullName]int{"controlplane.v1.Project": 4, "controlplane.v1.Agent": 16, "controlplane.v1.Workflow": 7} {
		if counts[name] != expected {
			t.Fatalf("response envelope inventory changed for %s: %d", name, counts[name])
		}
	}
}

func TestCatalogCardHTTPProjectGlobalAndSingleKeepOwnerProjection(t *testing.T) {
	project, agent, workflow := catalogCardFixtures()
	page := &cp.PageInfo{NextPageToken: "owner_cursor"}
	for _, test := range []struct {
		path, kind string
		response   proto.Message
		paged      bool
	}{
		{"/projects", "Project", &cp.ListProjectsResponse{Projects: []*cp.Project{project}, Page: page}, true},
		{"/projects/prj_fixture01", "Project", &cp.GetProjectResponse{Project: project}, false},
		{"/agents", "Agent", &cp.ListAgentsResponse{Agents: []*cp.Agent{agent}, Page: page}, true},
		{"/projects/prj_fixture01/agents", "Agent", &cp.ListAgentsResponse{Agents: []*cp.Agent{agent}, Page: page}, true},
		{"/agents/agt_fixture01", "Agent", &cp.GetAgentResponse{Agent: agent}, false},
		{"/workflows", "Workflow", &cp.ListWorkflowsResponse{Workflows: []*cp.Workflow{workflow}, Page: page}, true},
		{"/projects/prj_fixture01/workflows", "Workflow", &cp.ListWorkflowsResponse{Workflows: []*cp.Workflow{workflow}, Page: page}, true},
		{"/workflows/wfl_fixture01", "Workflow", &cp.GetWorkflowResponse{Workflow: workflow}, false},
	} {
		t.Run(test.path, func(t *testing.T) {
			client := &catalogRPCRecorder{response: test.response}
			writer := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(writer, managedTestRequest("GET", "/api/v1"+test.path, ""))
			if writer.Code != http.StatusOK {
				t.Fatalf("card status: %d", writer.Code)
			}
			var result map[string]any
			if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if test.paged {
				if result["nextPageToken"] != "owner_cursor" {
					t.Fatal("owner cursor lost")
				}
				result = result["items"].([]any)[0].(map[string]any)
			} else if writer.Header().Get("ETag") != `"4"` {
				t.Fatal("owner version ETag changed")
			}
			assertCatalogCard(t, protoreflect.FullName("controlplane.v1."+test.kind), result, false)
		})
	}
}

func TestCatalogCardsRejectMalformedProjectionBeforeHTTPResponse(t *testing.T) {
	for _, state := range []string{"NONE", "READY", "DEGRADED", "UNKNOWN"} {
		project, _, _ := catalogCardFixtures()
		project.IntegrationState = state
		if _, err := messageMap(&cp.GetProjectResponse{Project: project}); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutate := range []func(*cp.Project, *cp.Agent, *cp.Workflow){
		func(p *cp.Project, _ *cp.Agent, _ *cp.Workflow) { p.IntegrationState = "" },
		func(p *cp.Project, _ *cp.Agent, _ *cp.Workflow) { p.IntegrationState = "untrusted_owner_value" },
		func(p *cp.Project, _ *cp.Agent, _ *cp.Workflow) { p.AgentCount = -1 },
		func(p *cp.Project, _ *cp.Agent, _ *cp.Workflow) {
			p.LastActivityAt = &timestamppb.Timestamp{Seconds: 1 << 62}
		},
		func(_ *cp.Project, a *cp.Agent, _ *cp.Workflow) { a.CurrentRunRef = "untrusted/owner/value" },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) { w.CardSummary = nil },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) { w.CardSummary.StageCount = -1 },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) { w.CardSummary.UniqueAgentCount = -1 },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) { w.CardSummary.ParallelGroupCount = -1 },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) { w.CardSummary.ActiveRunCount = -1 },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) { w.CardSummary.PendingGateCount = -1 },
		func(_ *cp.Project, _ *cp.Agent, w *cp.Workflow) {
			w.CardSummary.LastActivityAt = &timestamppb.Timestamp{Seconds: 1 << 62}
		},
	} {
		project, agent, workflow := catalogCardFixtures()
		mutate(project, agent, workflow)
		rejected := 0
		for _, response := range []proto.Message{&cp.GetProjectResponse{Project: project}, &cp.GetAgentResponse{Agent: agent}, &cp.GetWorkflowResponse{Workflow: workflow}} {
			writer := httptest.NewRecorder()
			writeMessage(writer, http.StatusOK, response, "", "")
			if writer.Code == http.StatusBadGateway {
				rejected++
				if strings.Contains(writer.Body.String(), "untrusted") || strings.Contains(writer.Body.String(), "cardSummary") {
					t.Fatal("malformed projection escaped in error body")
				}
			}
		}
		if rejected != 1 {
			t.Fatalf("malformed card rejection count: %d", rejected)
		}
	}
}

func TestRuntimeLeaseExpiredSummaryUsesBothHTTPMessageCatalogs(t *testing.T) {
	texts, err := usertext.New()
	if err != nil {
		t.Fatal(err)
	}
	translations := map[string]string{}
	for _, locale := range []string{"ru", "en"} {
		value, err := ProtoMap(&cp.AuditEvent{SafeSummary: "i18n:RUNTIME_LEASE_EXPIRED"})
		if err != nil {
			t.Fatal(err)
		}
		LocalizeSafeErrors(value, func(id string) string { return texts.Localize(locale, id, nil) })
		text, _ := value["safeSummary"].(string)
		if text == "" || strings.Contains(text, "RUNTIME_LEASE_EXPIRED") || strings.Contains(text, "i18n:") {
			t.Fatal("runtime expiry message unresolved")
		}
		translations[locale] = text
	}
	if translations["ru"] == translations["en"] {
		t.Fatal("runtime expiry locale was ignored")
	}
}
