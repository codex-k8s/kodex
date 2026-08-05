// Package result реализует защищённую readiness-границу integration-gateway.
package result

import (
	"context"
	"errors"

	integrationgatewayv1 "github.com/codex-k8s/matter-codex/libs/go/integrationgatewayapi/gen/integrationgateway/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	readinessOperation = "integration.result.readiness"
	callerWorkload     = "agent-runner"
	callerSPIFFEID     = "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner"
	targetWorkload     = "integration-gateway"
	targetSPIFFEID     = "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway"
)

type readinessStore interface {
	Check(context.Context) error
}

type ownerReadiness interface {
	Check(context.Context) error
}

type Server struct {
	integrationgatewayv1.UnimplementedIntegrationResultServiceServer
	postgres readinessStore
	control  ownerReadiness
}

func New(postgres readinessStore, control ownerReadiness) (*Server, error) {
	if postgres == nil || control == nil {
		return nil, errors.New("integration gateway readiness dependencies are required")
	}
	return &Server{postgres: postgres, control: control}, nil
}

func (server *Server) CheckReadiness(
	ctx context.Context,
	_ *integrationgatewayv1.IntegrationResultServiceCheckReadinessRequest,
) (*integrationgatewayv1.IntegrationResultServiceCheckReadinessResponse, error) {
	if err := verifiedContext(ctx); err != nil {
		return nil, err
	}
	if err := server.postgres.Check(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "integration gateway dependency unavailable")
	}
	if err := server.control.Check(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "integration gateway owner readiness unavailable")
	}
	return &integrationgatewayv1.IntegrationResultServiceCheckReadinessResponse{
		Ready: true, SchemaVersion: 1, AuthorityReady: true, PostgresReady: true,
	}, nil
}

func verifiedContext(ctx context.Context) error {
	verified, ok := authorityclient.VerifiedAuthorizationContext(ctx)
	if !ok || verified.GetCallerWorkloadId() != callerWorkload || verified.GetCallerSpiffeId() != callerSPIFFEID ||
		verified.GetTargetWorkloadId() != targetWorkload || verified.GetTargetSpiffeId() != targetSPIFFEID ||
		verified.GetOperationId() != readinessOperation {
		return status.Error(codes.PermissionDenied, "verified gateway readiness context rejected")
	}
	return nil
}
