package agentmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

func TestActivityRouteRecordsPreToolUseSafeRequest(t *testing.T) {
	t.Parallel()

	recorder := &recordingActivityRecorder{}
	route := NewActivityRoute(recorder)
	event := validActivityEvent(hookenum.HookEventPreToolUse)

	if err := route.DispatchSafeHookEvent(context.Background(), event); err != nil {
		t.Fatalf("DispatchSafeHookEvent(): %v", err)
	}
	request := recorder.onlyRequest(t)
	if request.GetActivityKind() != agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_USE {
		t.Fatalf("activity kind = %s, want tool use", request.GetActivityKind())
	}
	if request.GetStatus() != agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_STARTED {
		t.Fatalf("status = %s, want started", request.GetStatus())
	}
	if request.GetSessionId() != event.RunContext.SessionID || request.GetRunId() != event.RunContext.RunID.String() ||
		request.GetTurnId() != *event.RunContext.TurnID || request.GetToolUseId() != event.ToolContext.ToolUseID {
		t.Fatalf("request refs = %+v, want run/session/turn/tool refs", request)
	}
	if request.GetToolName() != event.ToolContext.ToolName || request.GetToolCategory() != string(event.ToolContext.ToolCategory) {
		t.Fatalf("tool fields = %q/%q", request.GetToolName(), request.GetToolCategory())
	}
	if request.GetSafeSummary() != event.SafeSummary || request.GetPayloadDigest() != event.PayloadDigest {
		t.Fatalf("safe summary/digest = %q/%q", request.GetSafeSummary(), request.GetPayloadDigest())
	}
	if request.GetFinishedAt() != "" || request.DurationMs != nil {
		t.Fatalf("pre-tool request has finish fields: %+v", request)
	}
	if request.GetMeta().GetActor().GetType() != activityRouteActorType || request.GetMeta().GetActor().GetId() != activityRouteActorID {
		t.Fatalf("meta actor = %+v", request.GetMeta().GetActor())
	}
	assertContainsAll(
		t,
		request.GetSafeRefsJson(),
		"hook-event:",
		"session:",
		"run:",
		"slot:",
		"tool-use:",
		`"capability_context_ref":"capability-context:run-5555:guidance"`,
		`"capability_digest_ref":"`+digest("q")+`"`,
		`"capability_selection_ref":"agent-manager:capability-selection:123"`,
		`"capability_materialization_ref":"runtime-manager:materialization:456"`,
		`"source_kind":"package"`,
		`"source_ref":"package-source:go-guidelines"`,
		`"package_ref":"package:go-guidelines"`,
		`"package_version_ref":"package-version:go-guidelines:v1"`,
		`"manifest_digest_ref":"`+digest("m")+`"`,
		`"capability_ref":"capability:guidance:go-guidelines"`,
		`"policy_summary_digest_ref":"`+digest("p")+`"`,
	)
	assertContainsAll(t, request.GetSafeDetailsJson(), `"hook_event_name":"PreToolUse"`, `"risk_class":"low"`, `"tool_category":"shell"`)
}

func TestActivityRouteRecordsPostToolUseSafeFailureWithoutRawLeak(t *testing.T) {
	t.Parallel()

	recorder := &recordingActivityRecorder{}
	route := NewActivityRoute(recorder)
	event := validActivityEvent(hookenum.HookEventPostToolUse)
	event.SafeSummary = "Command finished with a bounded error digest."
	*event.ExitStatus = 1
	event.BoundedError = &value.BoundedError{
		ErrorClass:  "command_failed",
		SafeMessage: "raw stdout secret-value /home/s/projects-second/kodex-agent-2 must not leave ingress",
		Truncated:   true,
		ErrorDigest: stringPtr(digest("e")),
	}

	if err := route.DispatchSafeHookEvent(context.Background(), event); err != nil {
		t.Fatalf("DispatchSafeHookEvent(): %v", err)
	}
	request := recorder.onlyRequest(t)
	if request.GetActivityKind() != agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_RESULT {
		t.Fatalf("activity kind = %s, want tool result", request.GetActivityKind())
	}
	if request.GetStatus() != agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_FAILED {
		t.Fatalf("status = %s, want failed", request.GetStatus())
	}
	if request.GetFinishedAt() == "" || request.GetDurationMs() != 0 {
		t.Fatalf("post-tool finish fields = finished_at %q duration %d", request.GetFinishedAt(), request.GetDurationMs())
	}
	rendered := fmt.Sprintf("%+v", request)
	for _, raw := range []string{"raw stdout", "secret-value", "/home/s/projects-second", "tool_input", "tool_response"} {
		if strings.Contains(rendered, raw) {
			t.Fatalf("RecordAgentActivity request leaked raw marker %q: %s", raw, rendered)
		}
	}
	assertContainsAll(t, request.GetBoundedError(), "class=command_failed", "digest="+digest("e"), "truncated=true")
	assertContainsAll(t, request.GetSafeDetailsJson(), `"exit_status":1`, `"output_digest":"`+digest("o")+`"`)
}

func TestActivityRouteRecordsPostToolUseFailedExitStatusWithoutBoundedError(t *testing.T) {
	t.Parallel()

	recorder := &recordingActivityRecorder{}
	route := NewActivityRoute(recorder)
	event := validActivityEvent(hookenum.HookEventPostToolUse)
	*event.ExitStatus = 2
	event.BoundedError = nil

	if err := route.DispatchSafeHookEvent(context.Background(), event); err != nil {
		t.Fatalf("DispatchSafeHookEvent(): %v", err)
	}
	request := recorder.onlyRequest(t)
	if request.GetStatus() != agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_FAILED {
		t.Fatalf("status = %s, want failed for non-zero exit_status", request.GetStatus())
	}
	if request.GetBoundedError() != "" {
		t.Fatalf("bounded_error = %q, want empty when sanitizer did not provide bounded_error", request.GetBoundedError())
	}
	assertContainsAll(t, request.GetSafeDetailsJson(), `"exit_status":2`, `"output_digest":"`+digest("o")+`"`)
}

func TestActivityRouteRejectsPostToolUseWithoutRequiredSafeResultParts(t *testing.T) {
	t.Parallel()

	route := NewActivityRoute(&recordingActivityRecorder{})
	event := validActivityEvent(hookenum.HookEventPostToolUse)
	event.ExitStatus = nil

	err := route.DispatchSafeHookEvent(context.Background(), event)
	if !errors.Is(err, hookerrs.ErrInvalidArgument) {
		t.Fatalf("DispatchSafeHookEvent() error = %v, want ErrInvalidArgument", err)
	}
}

func TestActivityRouteSkipsNonActivityHooks(t *testing.T) {
	t.Parallel()

	recorder := &recordingActivityRecorder{}
	route := NewActivityRoute(recorder)
	event := validActivityEvent(hookenum.HookEventSessionStart)
	event.ToolContext = nil

	if err := route.DispatchSafeHookEvent(context.Background(), event); err != nil {
		t.Fatalf("DispatchSafeHookEvent(): %v", err)
	}
	if len(recorder.requests) != 0 {
		t.Fatalf("RecordAgentActivity calls = %d, want 0", len(recorder.requests))
	}
}

func TestActivityRouteUnavailableRecorderFailsSafely(t *testing.T) {
	t.Parallel()

	route := NewActivityRoute(UnavailableRecorder{})
	err := route.DispatchSafeHookEvent(context.Background(), validActivityEvent(hookenum.HookEventPreToolUse))
	if !errors.Is(err, hookerrs.ErrOwnerUnavailable) {
		t.Fatalf("DispatchSafeHookEvent() error = %v, want ErrOwnerUnavailable", err)
	}
}

func validActivityEvent(eventName hookenum.HookEventName) value.SafeHookEvent {
	turnID := "turn-12"
	commandDigest := digest("c")
	mcpToolName := "functions.exec_command"
	versionRef := "skill-version:go-guidelines@v1"
	packageRef := "package-installation:guidance-1"
	sourceRef := "package-source:go-guidelines"
	packageVersionRef := "package-version:go-guidelines:v1"
	packageEntryRef := "package:go-guidelines"
	manifestDigest := digest("m")
	capabilityRef := "capability:guidance:go-guidelines"
	capabilityKind := "guidance"
	packageSlug := "go-guidelines"
	packageVersionLabel := "v1"
	invocationPolicyRef := "policy:skill:go-guidelines:default"
	policySummaryDigest := digest("p")
	event := value.SafeHookEvent{
		EventID:       uuid.MustParse("11111111-2222-4111-8111-111111111111"),
		HookEventName: eventName,
		EventTime:     time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		Owner:         hookenum.DownstreamOwnerAgentManager,
		DeliveryMode:  hookenum.DeliveryModeAsync,
		SafeParts:     []string{"source_context", "run_context", "tool_context", "safe_summary", "payload_digest", "correlation_id"},
		SourceContext: &value.SourceContext{
			SourceRef:      "hook-emitter:slot-7",
			SourceKind:     hookenum.SourceKindHookEmitter,
			ActorRef:       "agent-manager:run-worker",
			OrganizationID: uuid.MustParse("22222222-2222-4222-8222-222222222222"),
			ProjectID:      uuid.MustParse("33333333-3333-4333-8333-333333333333"),
			EmitterVersion: "0.1.0",
			TrustLevel:     hookenum.TrustLevelManaged,
		},
		RunContext: &value.RunContext{
			RunID:     uuid.MustParse("55555555-5555-4555-8555-555555555555"),
			SessionID: "77777777-7777-4777-8777-777777777777",
			SlotID:    uuid.MustParse("66666666-6666-4666-8666-666666666666"),
			TurnID:    &turnID,
		},
		ToolContext: &value.ToolContext{
			ToolName:      "Bash",
			ToolCategory:  hookenum.ToolCategoryShell,
			ToolUseID:     "toolu-001",
			CommandDigest: &commandDigest,
			PathCategory:  "repository",
			MCPToolName:   &mcpToolName,
		},
		CapabilityContext: &value.CapabilityContext{
			CapabilityContextID:  uuid.MustParse("88888888-8888-4888-8888-888888888888"),
			CapabilityContextRef: "capability-context:run-5555:guidance",
			CapabilityDigest:     digest("q"),
			SelectedByRef:        "agent-manager:capability-selection:123",
			MaterializedByRef:    "runtime-manager:materialization:456",
			ScopeKind:            "run",
			SkillRefs: []value.SkillRef{{
				SourceKind:             "package",
				SkillRef:               "skill:go-guidelines",
				VersionRef:             &versionRef,
				SourceRef:              &sourceRef,
				PackageRef:             &packageEntryRef,
				PackageInstallationRef: &packageRef,
				PackageVersionRef:      &packageVersionRef,
				ManifestDigest:         &manifestDigest,
				CapabilityRef:          &capabilityRef,
				CapabilityKind:         &capabilityKind,
				PackageSlug:            &packageSlug,
				PackageVersionLabel:    &packageVersionLabel,
				InvocationPolicyRef:    &invocationPolicyRef,
				PolicySummaryDigest:    &policySummaryDigest,
				Digest:                 digest("d"),
			}},
		},
		SafeSummary:   "Agent intends to run a documentation check command.",
		RiskClass:     "low",
		PayloadDigest: digest("a"),
		CorrelationID: "run-5555:pre-tool-use:toolu-001",
	}
	if eventName == hookenum.HookEventPostToolUse {
		exitStatus := 0
		event.ExitStatus = &exitStatus
		event.OutputDigest = digest("o")
		event.CorrelationID = "run-5555:post-tool-use:toolu-001"
	}
	return event
}

func assertContainsAll(t *testing.T, value string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(value, part) {
			t.Fatalf("%q does not contain %q", value, part)
		}
	}
}

func digest(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}

func stringPtr(value string) *string {
	return &value
}

type recordingActivityRecorder struct {
	requests []*agentsv1.RecordAgentActivityRequest
	err      error
}

func (recorder *recordingActivityRecorder) RecordAgentActivity(
	_ context.Context,
	request *agentsv1.RecordAgentActivityRequest,
	_ ...grpc.CallOption,
) (*agentsv1.AgentActivityResponse, error) {
	recorder.requests = append(recorder.requests, request)
	if recorder.err != nil {
		return nil, recorder.err
	}
	return &agentsv1.AgentActivityResponse{}, nil
}

func (recorder *recordingActivityRecorder) onlyRequest(t *testing.T) *agentsv1.RecordAgentActivityRequest {
	t.Helper()
	if len(recorder.requests) != 1 {
		t.Fatalf("RecordAgentActivity calls = %d, want 1", len(recorder.requests))
	}
	return recorder.requests[0]
}
