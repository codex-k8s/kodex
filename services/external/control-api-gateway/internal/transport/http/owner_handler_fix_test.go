package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type healthControlPlane struct{ ControlPlane }

func (*healthControlPlane) GetDiagnostics(context.Context, *controlplanev1.GetDiagnosticsRequest, ...grpc.CallOption) (*controlplanev1.GetDiagnosticsResponse, error) {
	return &controlplanev1.GetDiagnosticsResponse{SchemaVersion: 7, PendingOutboxEvents: 2}, nil
}

type healthInteraction struct {
	interactiongatewayv1.MattermostTeamServiceClient
}

func (*healthInteraction) CheckReadiness(context.Context, *interactiongatewayv1.MattermostTeamServiceCheckReadinessRequest, ...grpc.CallOption) (*interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse, error) {
	return &interactiongatewayv1.MattermostTeamServiceCheckReadinessResponse{Ready: false, SchemaVersion: 4}, nil
}

type healthIntegration struct {
	integrationgatewayv1.IntegrationManagementServiceClient
}

func (*healthIntegration) GetManagementDiagnostics(context.Context, *integrationgatewayv1.GetManagementDiagnosticsRequest, ...grpc.CallOption) (*integrationgatewayv1.GetManagementDiagnosticsResponse, error) {
	return &integrationgatewayv1.GetManagementDiagnosticsResponse{Status: "DEGRADED", Dependencies: []*integrationgatewayv1.ManagementDependencyStatus{{Dependency: "provider", Status: "UNAVAILABLE", Version: 2, CheckedAt: timestamppb.Now()}}}, nil
}

func TestHealthSeriesKeepsValidDegradedReadback(t *testing.T) {
	server := &Server{control: &healthControlPlane{}, interaction: &healthInteraction{}, integration: &healthIntegration{}}
	writer := httptest.NewRecorder()
	server.GetHealthSeries(writer, httptest.NewRequest(http.MethodGet, "/health-series", nil))
	if writer.Code != http.StatusOK {
		t.Fatalf("degraded health became Problem: status=%d body=%s", writer.Code, writer.Body.String())
	}
	body := writer.Body.String()
	for _, expected := range []string{`"component":"mattermost_team_working_path"`, `"component":"overall"`, `"component":"provider"`, `"component":"pending_outbox"`, `"status":"DEGRADED"`, `"status":"UNAVAILABLE"`, `"status":"UNKNOWN"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("authoritative degraded observation is absent: %q in %s", expected, body)
		}
	}
}

type auditContinuationControl struct {
	ControlPlane
	calls int
}

func (control *auditContinuationControl) ListAuditEvents(context.Context, *controlplanev1.ListAuditEventsRequest, ...grpc.CallOption) (*controlplanev1.ListAuditEventsResponse, error) {
	control.calls++
	events := make([]*controlplanev1.AuditEvent, maximumPageSize)
	for index := range events {
		events[index] = &controlplanev1.AuditEvent{Id: "event-" + strconv.Itoa(control.calls) + "-" + strconv.Itoa(index+1)}
	}
	return &controlplanev1.ListAuditEventsResponse{Events: events, NextPageToken: "page-" + strconv.Itoa(control.calls)}, nil
}

func TestAuditExportRejectsContinuationBeforeCSVHeaders(t *testing.T) {
	control := &auditContinuationControl{}
	server := &Server{control: control}
	writer := httptest.NewRecorder()
	server.ExportAudit(writer, httptest.NewRequest(http.MethodGet, "/audit/export", nil), generated.ExportAuditParams{})
	if writer.Code != http.StatusServiceUnavailable || !strings.Contains(writer.Body.String(), `"code":"EXPORT_TRUNCATED"`) {
		t.Fatalf("continued audit export did not fail closed: status=%d body=%s", writer.Code, writer.Body.String())
	}
	if writer.Header().Get("Content-Disposition") != "" || strings.Contains(writer.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("partial CSV headers escaped: %#v", writer.Header())
	}
}

type configurationDiffControl struct{ ControlPlane }

func (*configurationDiffControl) CompareInstructionSetVersions(context.Context, *controlplanev1.CompareInstructionSetVersionsRequest, ...grpc.CallOption) (*controlplanev1.CompareInstructionSetVersionsResponse, error) {
	digest := strings.Repeat("a", 64)
	return &controlplanev1.CompareInstructionSetVersionsResponse{
		LeftVersionRef:  &controlplanev1.ConfigurationVersionRef{Version: 1, ContentSha256: digest, SnapshotSha256: digest},
		RightVersionRef: &controlplanev1.ConfigurationVersionRef{Version: 2, ContentSha256: digest, SnapshotSha256: digest},
		Changes:         []*controlplanev1.ConfigurationChange{{Kind: controlplanev1.ConfigurationChangeKind_CONFIGURATION_CHANGE_KIND_CHANGED, Path: "provider.authorization", Display: controlplanev1.ConfigurationChangeDisplay_CONFIGURATION_CHANGE_DISPLAY_REDACTED, Before: "[REDACTED]", After: "[REDACTED]"}},
	}, nil
}

func TestConfigurationDiffReturnsBoundedRedactedChanges(t *testing.T) {
	server := &Server{control: &configurationDiffControl{}}
	writer := httptest.NewRecorder()
	server.GetConfigurationDiff(writer, httptest.NewRequest(http.MethodGet, "/configuration-diff", nil), generated.GetConfigurationDiffParams{InstructionSetRef: "instruction-safe", LeftVersion: 1, RightVersion: 2})
	if writer.Code != http.StatusOK || !strings.Contains(writer.Body.String(), `"changes":[`) || !strings.Contains(writer.Body.String(), `"display":"REDACTED"`) {
		t.Fatalf("typed configuration diff is absent: status=%d body=%s", writer.Code, writer.Body.String())
	}
}

type agentProjectionControl struct{ ControlPlane }

func (*agentProjectionControl) ListAgents(context.Context, *controlplanev1.ListAgentsRequest, ...grpc.CallOption) (*controlplanev1.ListAgentsResponse, error) {
	digest := strings.Repeat("a", 64)
	return &controlplanev1.ListAgentsResponse{Projections: []*controlplanev1.AgentOwnerProjection{{
		AgentRef: "agent-safe", DisplayName: "Developer", StableKey: "developer", Version: 2,
		State: controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, Enabled: true,
		BotIdentity: &controlplanev1.AgentBotIdentityProjection{
			Status: controlplanev1.AgentBotIdentityStatus_AGENT_BOT_IDENTITY_STATUS_UNBOUND,
		},
		RuntimeSelection: &controlplanev1.AgentRuntimeSelectionProjection{
			SelectionKey: "developer-default", DisplayName: "Developer default",
			RoleDefinitionVersion: 1, RoleDefinitionSha256: digest,
			RuntimeProfileVersion: 1, RuntimeProfileSha256: digest,
			Status: controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_PRESENT,
		},
		InstructionSelection: &controlplanev1.OwnerSafeSelection{
			Status: controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_UNAVAILABLE,
		},
		ProviderPoolSelection: &controlplanev1.OwnerSafeSelection{
			Status: controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_UNAVAILABLE,
		},
	}}}, nil
}

func TestListAgentsAcceptsSafeProjectionWithoutLegacyResource(t *testing.T) {
	server := &Server{control: &agentProjectionControl{}}
	writer := httptest.NewRecorder()
	server.ListAgents(writer, httptest.NewRequest(http.MethodGet, "/agents", nil), generated.ListAgentsParams{})
	if writer.Code != http.StatusOK || !strings.Contains(writer.Body.String(), `"agentRef":"agent-safe"`) {
		t.Fatalf("safe projection page was rejected: status=%d body=%s", writer.Code, writer.Body.String())
	}
	if strings.Contains(writer.Body.String(), `"spec"`) {
		t.Fatalf("legacy private resource escaped: %s", writer.Body.String())
	}
}
