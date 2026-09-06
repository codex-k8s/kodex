package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const schedulePromptBody = `{"preset":"HOURLY","timezone":"UTC","dstGapPolicy":"SHIFT_FORWARD","dstFoldPolicy":"RUN_ONCE_EARLIEST","misfirePolicy":"COALESCE","overlapPolicy":"FORBID","materialization":{"projectRef":"prj_fixture01","targetType":"AGENT","targetRef":"agent_fixture01","name":"Automation","task":"i18n:literal task","input":{},"promptInputs":{},"sessionPolicy":"NEW_EACH_RUN","notificationPolicy":"CONTROL_CENTER_ONLY"}}`

func schedulePromptResponseFixture(current bool) *cp.PreviewScheduleResponse {
	now := timestamppb.New(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	response := &cp.PreviewScheduleResponse{NormalizedCronExpression: "0 * * * *", Occurrences: []*timestamppb.Timestamp{now}, DstGapPolicy: "SHIFT_FORWARD", DstFoldPolicy: "RUN_ONCE_EARLIEST", MisfirePolicy: "COALESCE", OverlapPolicy: "FORBID", MaterializedPrompt: promptPreviewFixture(),
		MaterializationPin: &cp.SchedulePromptPreviewPin{ScheduledFor: now, Timezone: "UTC", Mode: cp.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_DRAFT, ExecutionActorRef: "actor_fixture01"}}
	response.MaterializedPrompt.SafePreview = "i18n:literal preview"
	for _, name := range []string{"ref", "name", "task", "scheduled_at", "timezone", "revision"} {
		v := &cp.TemplateVariable{Name: "automation." + name, ValueType: "string", Source: "AUTOMATION", Available: true, Reason: cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE}
		if name == "revision" {
			v.Available = false
			v.Reason = cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_REVISION_NOT_SAVED
		}
		if name == "ref" {
			v.Available = false
			v.Reason = cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_NOT_MATERIALIZED
		}
		response.AutomationVariables = append(response.AutomationVariables, v)
	}
	if current {
		pin := response.MaterializationPin
		pin.Mode = cp.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_CURRENT_REVISION
		pin.ScheduleRef = "sch_fixture01"
		pin.ScheduleVersion = 4
		pin.RevisionAvailable = true
		pin.RevisionRef = "scr_fixture01"
		pin.RevisionDigest = strings.Repeat("a", 64)
		pin.BaseRevisionRef = pin.RevisionRef
		pin.BaseRevisionDigest = pin.RevisionDigest
		response.AutomationVariables[5].Available = true
		response.AutomationVariables[5].Reason = cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE
		response.AutomationVariables[0].Available = true
		response.AutomationVariables[0].Reason = cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE
	}
	return response
}

func TestSchedulePromptPreviewPreservesDraftAndCurrentProvenance(t *testing.T) {
	for _, current := range []bool{false, true} {
		body := schedulePromptBody
		if current {
			body = strings.Replace(body, `"sessionPolicy":`, `"mode":"CURRENT_REVISION","scheduleRef":"sch_fixture01","expectedScheduleVersion":4,"sessionPolicy":`, 1)
		}
		client := &catalogRPCRecorder{response: schedulePromptResponseFixture(current)}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/schedules/preview", body))
		if w.Code != 200 {
			t.Fatalf("materialized preview status %d", w.Code)
		}
		request := client.request.(*cp.PreviewScheduleRequest)
		if request.Materialization.Task != "i18n:literal task" || request.Materialization.Target.GetAgentRef() != "agent_fixture01" {
			t.Fatal("task or target changed")
		}
		var output map[string]any
		if json.Unmarshal(w.Body.Bytes(), &output) != nil {
			t.Fatal("invalid output")
		}
		pin := output["materializationPin"].(map[string]any)
		if pin["revisionAvailable"] != current || pin["continuation"] != false || output["materializedPrompt"].(map[string]any)["safePreview"] != "i18n:literal preview" {
			t.Fatal("provenance or literal content changed")
		}
		if !current && pin["scheduleVersion"] != float64(0) {
			t.Fatal("new draft version invented")
		}
	}
}

func TestSchedulePromptPreviewRejectsForgedInputsAndCrossPins(t *testing.T) {
	for _, body := range []string{
		strings.Replace(schedulePromptBody, `"task":`, `"executionActorRef":"actor_forged01","task":`, 1),
		strings.Replace(schedulePromptBody, `"task":`, `"mode":"CURRENT_REVISION","task":`, 1),
		strings.Replace(schedulePromptBody, `"task":`, `"scheduleRef":"sch_fixture01","task":`, 1),
	} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/schedules/preview", body))
		if w.Code != 400 || client.request != nil {
			t.Fatal("forged materialization input reached owner")
		}
	}
	for _, mutate := range []func(*cp.PreviewScheduleResponse){
		func(r *cp.PreviewScheduleResponse) { r.MaterializationPin = nil },
		func(r *cp.PreviewScheduleResponse) { r.MaterializationPin.RevisionAvailable = true },
		func(r *cp.PreviewScheduleResponse) { r.MaterializationPin.ScheduledFor = timestamppb.New(time.Now()) },
		func(r *cp.PreviewScheduleResponse) { r.MaterializedPrompt.ContextPin.AgentRef = "agent_other01" },
		func(r *cp.PreviewScheduleResponse) {
			r.AutomationVariables[5].Reason = cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE
		},
		func(r *cp.PreviewScheduleResponse) { r.MaterializedPrompt.FullMaterializedPrompt = "private full text" },
	} {
		r := schedulePromptResponseFixture(false)
		mutate(r)
		client := &catalogRPCRecorder{response: r}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/schedules/preview", schedulePromptBody))
		if w.Code != 502 || strings.Contains(w.Body.String(), "private full text") {
			t.Fatalf("malformed prompt status %d", w.Code)
		}
	}
}

func TestSchedulePromptPreviewWorkflowAndContinuationUseExactRuntimePins(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		body := strings.Replace(schedulePromptBody, `"sessionPolicy":"NEW_EACH_RUN"`, `"scheduleRef":"sch_fixture01","expectedScheduleVersion":4,"mode":"CURRENT_REVISION","sessionPolicy":"CONTINUE_ONE"`, 1)
		response := schedulePromptResponseFixture(true)
		response.MaterializationPin.Continuation = true
		response.MaterializationPin.SessionRef = "session_fixture01"
		response.MaterializedPrompt.ContextPin.PreviousRuntimeRevisionRef = "runtime_previous01"
		response.MaterializedPrompt.RuntimeDiff = &cp.PromptRuntimeDiff{PreviousRevisionRef: "runtime_previous01", SessionRef: "session_fixture01", Digest: strings.Repeat("b", 64)}
		if workflow {
			body = strings.Replace(body, `"targetType":"AGENT","targetRef":"agent_fixture01"`, `"targetType":"WORKFLOW","targetRef":"wfl_fixture01"`, 1)
			pin := response.MaterializedPrompt.ContextPin
			pin.WorkflowRef = "wfl_fixture01"
			pin.WorkflowVersion = 4
			pin.WorkflowRevisionRef = "wfv_published01"
			pin.WorkflowStageKey = "workflow.coordinator.initial"
		}
		for _, invalid := range []int{0, 1, 2} {
			response.MaterializedPrompt.RuntimeDiff.PreviousRevisionRef = "runtime_previous01"
			if invalid == 1 {
				response.MaterializedPrompt.RuntimeDiff.PreviousRevisionRef = "runtime_wrong01"
			}
			if invalid == 2 {
				response.MaterializedPrompt.ContextPin.WorkflowRef = "wfl_fixture01"
				response.MaterializedPrompt.ContextPin.WorkflowVersion = 4
				response.MaterializedPrompt.ContextPin.WorkflowRevisionRef = "wfv_published01"
				response.MaterializedPrompt.ContextPin.WorkflowStageKey = "stage_not_coordinator"
			}
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/schedules/preview", body))
			want := 200
			if invalid != 0 {
				want = 502
			}
			if w.Code != want {
				t.Fatalf("continuation status %d, expected %d", w.Code, want)
			}
		}
	}
}

func TestAutomationVariableDescriptionsLocalizeWithoutChangingExamples(t *testing.T) {
	for _, name := range []string{"NAME", "TASK", "SCHEDULED_AT", "TIMEZONE", "REVISION"} {
		v := &cp.TemplateVariable{Name: "automation." + strings.ToLower(name), ValueType: "string", Source: "AUTOMATION", Description: "i18n:AUTOMATION_VARIABLE_" + name, Example: "i18n:literal example", Available: true, Reason: cp.TemplateVariableAvailabilityReason_TEMPLATE_VARIABLE_AVAILABILITY_REASON_AVAILABLE}
		item, ok := templateVariableView(v)
		if !ok {
			t.Fatal("variable fixture invalid")
		}
		localizeTemplateVariable(&localizingRecorder{httptest.NewRecorder()}, &item)
		if item.Description != "localized:AUTOMATION_VARIABLE_"+name || item.Example != v.Example {
			t.Fatal("description localization changed literal variable data")
		}
	}
}
