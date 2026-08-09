package httptransport

import (
	"net/http"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	ownerclient "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/clients/owner"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRPCErrorMatrixRejectsMismatch(t *testing.T) {
	current := status.New(codes.Aborted, "safe")
	withDetail, err := current.WithDetails(&controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	problem, expected := mapRPCError(withDetail.Err(), true)
	if !expected || problem.Status != http.StatusPreconditionFailed || problem.Code != "VERSION_MISMATCH" || !problem.Retryable {
		t.Fatalf("mapped problem = %#v, expected=%v", problem, expected)
	}
	mismatch := status.New(codes.InvalidArgument, "safe")
	mismatchDetail, err := mismatch.WithDetails(&controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true})
	if err != nil {
		t.Fatalf("mismatch detail: %v", err)
	}
	problem, expected = mapRPCError(mismatchDetail.Err(), true)
	if expected || problem.Status != http.StatusInternalServerError || problem.Code != "INTERNAL" {
		t.Fatalf("mismatched detail escaped: %#v, expected=%v", problem, expected)
	}
}

func TestDownstreamErrorProfilesKeepTypedProblemSemantics(t *testing.T) {
	correlation := uuid.NewString()
	interactionStatus := status.New(codes.NotFound, "private downstream message")
	interactionWithDetail, err := interactionStatus.WithDetails(&interactiongatewayv1.ErrorDetail{
		Reason: interactiongatewayv1.ErrorReason_ERROR_REASON_NOT_FOUND, Code: "NOT_FOUND", CorrelationId: correlation,
	})
	if err != nil {
		t.Fatal(err)
	}
	problem, expected := mapRPCError(&ownerclient.DownstreamError{Source: ownerclient.RPCSourceInteraction, Method: interactiongatewayv1.AgentMattermostBotIdentityService_GetAgentMattermostBotIdentity_FullMethodName, Err: interactionWithDetail.Err()}, false)
	if !expected || problem.Status != http.StatusNotFound || problem.Code != "NOT_FOUND" || problem.CorrelationId.String() != correlation {
		t.Fatalf("interaction error mapping changed: %#v, expected=%v", problem, expected)
	}
	problem, expected = mapRPCError(&ownerclient.DownstreamError{Source: ownerclient.RPCSourceIntegration, Method: integrationgatewayv1.IntegrationManagementService_GetProviderConnection_FullMethodName,
		NormalizedDetail: &controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE, Code: "UNAVAILABLE", CorrelationId: uuid.NewString(), Retryable: true},
		Err:              status.Error(codes.Unavailable, "private downstream message")}, false)
	if !expected || problem.Status != http.StatusServiceUnavailable || problem.Code != "UNAVAILABLE" || !problem.Retryable {
		t.Fatalf("integration error mapping changed: %#v, expected=%v", problem, expected)
	}
	problem, expected = mapRPCError(status.Error(codes.NotFound, "bare control-plane status"), false)
	if expected || problem.Status != http.StatusInternalServerError || problem.Code != "INTERNAL" {
		t.Fatalf("untyped control-plane error escaped fail-closed mapping: %#v, expected=%v", problem, expected)
	}
}

func TestInteractionDomainErrorCodeKeepsAmbiguousRetrySemantics(t *testing.T) {
	current := status.New(codes.Unavailable, "private downstream message")
	withDetail, err := current.WithDetails(&interactiongatewayv1.ErrorDetail{
		Reason: interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE, Code: "MATTERMOST_BOT_EFFECT_AMBIGUOUS", CorrelationId: uuid.NewString(), Retryable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	problem, expected := mapRPCError(&ownerclient.DownstreamError{Source: ownerclient.RPCSourceInteraction, Method: interactiongatewayv1.AgentMattermostBotIdentityService_RebindAgentMattermostBotIdentity_FullMethodName, Err: withDetail.Err()}, true)
	if !expected || problem.Status != http.StatusServiceUnavailable || problem.Code != "MATTERMOST_BOT_EFFECT_AMBIGUOUS" || !problem.Retryable {
		t.Fatalf("ambiguous interaction semantics changed: %#v, expected=%v", problem, expected)
	}
}

func TestNormalizedLegacyOwnerErrorsKeepConflictAndStaleSemantics(t *testing.T) {
	tests := []struct {
		name       string
		source     ownerclient.RPCSource
		method     string
		grpcCode   codes.Code
		detail     *controlplanev1.ErrorDetail
		mutation   bool
		httpStatus int
	}{
		{name: "interaction state conflict", source: ownerclient.RPCSourceInteraction, method: interactiongatewayv1.MattermostTeamService_LinkMattermostTeam_FullMethodName, grpcCode: codes.FailedPrecondition,
			detail: &controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT, Code: "STATE_CONFLICT", CorrelationId: uuid.NewString()}, mutation: true, httpStatus: http.StatusConflict},
		{name: "integration stale version", source: ownerclient.RPCSourceIntegration, method: integrationgatewayv1.IntegrationManagementService_ConfigureIntegration_FullMethodName, grpcCode: codes.Aborted,
			detail: &controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true}, mutation: true, httpStatus: http.StatusPreconditionFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			problem, expected := mapRPCError(&ownerclient.DownstreamError{Source: test.source, Method: test.method, NormalizedDetail: test.detail, Err: status.Error(test.grpcCode, "private downstream message")}, test.mutation)
			if !expected || problem.Status != test.httpStatus || problem.Code != test.detail.GetCode() || problem.Retryable != test.detail.GetRetryable() {
				t.Fatalf("normalized owner error mapping changed: %#v, expected=%v", problem, expected)
			}
		})
	}
}

func TestRPCErrorMatrixRejectsMutationConflictForRead(t *testing.T) {
	current := status.New(codes.Aborted, "safe")
	withDetail, err := current.WithDetails(&controlplanev1.ErrorDetail{Reason: controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH, Code: "VERSION_MISMATCH", CorrelationId: uuid.NewString(), Retryable: true})
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	problem, expected := mapRPCError(withDetail.Err(), false)
	if expected || problem.Status != http.StatusInternalServerError || problem.Code != "INTERNAL" {
		t.Fatalf("mutation-only detail escaped read profile: %#v, expected=%v", problem, expected)
	}
}

func TestETagParsingIsExact(t *testing.T) {
	for _, invalid := range []string{"1", "W/\"1\"", "\"0\"", "\"01x\"", "*"} {
		if _, ok := parseETag(invalid); ok {
			t.Fatalf("invalid ETag accepted: %q", invalid)
		}
	}
	if value, ok := parseETag("\"42\""); !ok || value != 42 {
		t.Fatalf("valid ETag rejected: %d, %v", value, ok)
	}
}
