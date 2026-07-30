package authoritygrpc

import (
	"context"

	"github.com/codex-k8s/matter-codex/libs/go/grpcserver"
	internalrpcauthorityv1 "github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/application"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DatabaseCredentialLifecycleServer адаптирует согласование поколений к gRPC.
type DatabaseCredentialLifecycleServer struct {
	internalrpcauthorityv1.UnimplementedDatabaseCredentialLifecycleServiceServer
	application *application.DatabaseCredentialLifecycle
}

// NewDatabaseCredentialLifecycleServer создаёт сервер жизненного цикла.
func NewDatabaseCredentialLifecycleServer(
	applicationValue *application.DatabaseCredentialLifecycle,
) *DatabaseCredentialLifecycleServer {
	return &DatabaseCredentialLifecycleServer{application: applicationValue}
}

// ReconcileDatabaseCredentials атомарно согласует зарегистрированный набор.
func (server *DatabaseCredentialLifecycleServer) ReconcileDatabaseCredentials(
	ctx context.Context,
	request *internalrpcauthorityv1.ReconcileDatabaseCredentialsRequest,
) (*internalrpcauthorityv1.ReconcileDatabaseCredentialsResponse, error) {
	if request == nil ||
		grpcserver.HasMalformedProto(request) ||
		request.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "malformed database credential request")
	}
	result, err := server.application.Reconcile(ctx, request.GetIdempotencyKey())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "database credential reconciliation failed")
	}
	return &internalrpcauthorityv1.ReconcileDatabaseCredentialsResponse{
		Generations:                  castDatabaseCredentialGenerations(result.Generations),
		ReceiptId:                    result.ReceiptID,
		CanonicalRequestDigestSha256: result.CanonicalDigest,
	}, nil
}

// CheckReadiness проверяет аренду и фактически обслуживаемые поколения.
func (server *DatabaseCredentialLifecycleServer) CheckReadiness(
	ctx context.Context,
	request *internalrpcauthorityv1.DatabaseCredentialLifecycleServiceCheckReadinessRequest,
) (*internalrpcauthorityv1.DatabaseCredentialLifecycleServiceCheckReadinessResponse, error) {
	if request == nil || grpcserver.HasMalformedProto(request) {
		return nil, status.Error(codes.InvalidArgument, "malformed database credential request")
	}
	generations, err := server.application.Ready(ctx)
	registered := server.application.RegisteredSet()
	if err != nil {
		return &internalrpcauthorityv1.DatabaseCredentialLifecycleServiceCheckReadinessResponse{
			Ready: false,
		}, nil
	}
	return &internalrpcauthorityv1.DatabaseCredentialLifecycleServiceCheckReadinessResponse{
		Ready:                    true,
		ServedSourceRevision:     registered.SourceRevision,
		ServedSourceDigestSha256: registered.SourceDigest,
		Generations:              castDatabaseCredentialGenerations(generations),
	}, nil
}

func castDatabaseCredentialGenerations(
	values []model.DatabaseCredentialGeneration,
) []*internalrpcauthorityv1.DatabaseCredentialGeneration {
	result := make(
		[]*internalrpcauthorityv1.DatabaseCredentialGeneration,
		0,
		len(values),
	)
	for _, value := range values {
		result = append(result, &internalrpcauthorityv1.DatabaseCredentialGeneration{
			Capability:         castDatabaseCredentialCapability(value.Capability),
			Generation:         value.Generation,
			Status:             castDatabaseCredentialStatus(value.Status),
			Principal:          value.Principal,
			SourceRevision:     value.SourceRevision,
			SourceDigestSha256: value.SourceDigest,
		})
	}
	return result
}

func castDatabaseCredentialCapability(
	value model.DatabaseCredentialCapability,
) internalrpcauthorityv1.DatabaseCredentialCapability {
	switch value {
	case model.DatabaseCredentialPublisher:
		return internalrpcauthorityv1.DatabaseCredentialCapability_DATABASE_CREDENTIAL_CAPABILITY_PUBLISHER
	case model.DatabaseCredentialAttestor:
		return internalrpcauthorityv1.DatabaseCredentialCapability_DATABASE_CREDENTIAL_CAPABILITY_READBACK_ATTESTOR
	default:
		return internalrpcauthorityv1.DatabaseCredentialCapability_DATABASE_CREDENTIAL_CAPABILITY_UNSPECIFIED
	}
}

func castDatabaseCredentialStatus(
	value model.DatabaseCredentialStatus,
) internalrpcauthorityv1.DatabaseCredentialLifecycleStatus {
	switch value {
	case model.DatabaseCredentialCurrent:
		return internalrpcauthorityv1.DatabaseCredentialLifecycleStatus_DATABASE_CREDENTIAL_LIFECYCLE_STATUS_CURRENT
	case model.DatabaseCredentialNext:
		return internalrpcauthorityv1.DatabaseCredentialLifecycleStatus_DATABASE_CREDENTIAL_LIFECYCLE_STATUS_NEXT
	default:
		return internalrpcauthorityv1.DatabaseCredentialLifecycleStatus_DATABASE_CREDENTIAL_LIFECYCLE_STATUS_UNSPECIFIED
	}
}
