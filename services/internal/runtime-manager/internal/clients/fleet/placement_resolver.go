// Package fleet adapts fleet-manager placement calls to the runtime-manager domain.
package fleet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	fleetv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/fleet/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const callerID = "runtime-manager"

// Config contains fleet-manager gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// PlacementResolver calls fleet-manager ResolvePlacement.
type PlacementResolver struct {
	client    fleetv1.FleetManagerServiceClient
	authToken string
	timeout   time.Duration
}

var _ runtimeservice.PlacementResolver = (*PlacementResolver)(nil)

// NewConnection creates a lazy gRPC client connection to fleet-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		return nil, fmt.Errorf("fleet-manager address is required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// NewPlacementResolver wraps a generated fleet-manager client.
func NewPlacementResolver(client fleetv1.FleetManagerServiceClient, cfg Config) (*PlacementResolver, error) {
	if client == nil {
		return nil, fmt.Errorf("fleet-manager client is required")
	}
	authToken := strings.TrimSpace(cfg.AuthToken)
	if authToken == "" {
		return nil, fmt.Errorf("fleet-manager auth token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &PlacementResolver{client: client, authToken: authToken, timeout: timeout}, nil
}

// ResolvePlacement returns only fleet-owned refs required by runtime-manager.
func (r *PlacementResolver) ResolvePlacement(ctx context.Context, request runtimeservice.PlacementResolutionRequest) (runtimeservice.PlacementResolution, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	response, err := r.client.ResolvePlacement(r.outgoingContext(ctx), resolvePlacementRequest(request))
	if err != nil {
		return runtimeservice.PlacementResolution{}, mapFleetError(err)
	}
	decision := response.GetDecision()
	if decision == nil {
		return runtimeservice.PlacementResolution{}, errs.ErrDependencyUnavailable
	}
	if decision.GetStatus() == fleetv1.PlacementDecisionStatus_PLACEMENT_DECISION_STATUS_REJECTED {
		return runtimeservice.PlacementResolution{}, errs.ErrPlacementRejected
	}
	if decision.GetStatus() != fleetv1.PlacementDecisionStatus_PLACEMENT_DECISION_STATUS_RESOLVED {
		return runtimeservice.PlacementResolution{}, errs.ErrDependencyUnavailable
	}
	fleetScopeID, err := uuid.Parse(strings.TrimSpace(decision.GetFleetScopeId()))
	if err != nil || fleetScopeID == uuid.Nil {
		return runtimeservice.PlacementResolution{}, errs.ErrDependencyUnavailable
	}
	clusterID, err := uuid.Parse(strings.TrimSpace(decision.GetClusterId()))
	if err != nil || clusterID == uuid.Nil {
		return runtimeservice.PlacementResolution{}, errs.ErrDependencyUnavailable
	}
	return runtimeservice.PlacementResolution{FleetScopeID: fleetScopeID, ClusterID: clusterID}, nil
}

func (r *PlacementResolver) outgoingContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+r.authToken,
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		callerID,
	)
}

func resolvePlacementRequest(request runtimeservice.PlacementResolutionRequest) *fleetv1.ResolvePlacementRequest {
	return &fleetv1.ResolvePlacementRequest{
		ProjectId:                optionalUUIDString(request.ProjectID),
		RepositoryId:             optionalSingleUUIDString(request.RepositoryIDs),
		ServiceKey:               optionalSingleString(request.ServiceKeys),
		RuntimeMode:              runtimeModeToFleet(request.RuntimeMode),
		RuntimeProfile:           strings.TrimSpace(request.RuntimeProfile),
		PreferredFleetScopeId:    optionalUUIDString(request.PreferredFleetScopeID),
		PlacementConstraintsJson: string(defaultPlacementJSON(request.PlacementConstraintsJSON)),
		RuntimeRequirementsJson:  string(defaultPlacementJSON(request.RuntimeRequirementsJSON)),
		Meta:                     commandMetaToFleet(request.Meta),
	}
}

func defaultPlacementJSON(payload []byte) []byte {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return []byte(`{}`)
	}
	return []byte(trimmed)
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalSingleUUIDString(ids []uuid.UUID) *string {
	if len(ids) != 1 || ids[0] == uuid.Nil {
		return nil
	}
	value := ids[0].String()
	return &value
}

func optionalSingleString(values []string) *string {
	if len(values) != 1 {
		return nil
	}
	value := strings.TrimSpace(values[0])
	if value == "" {
		return nil
	}
	return &value
}

func runtimeModeToFleet(mode enum.RuntimeMode) fleetv1.RuntimeMode {
	switch mode {
	case enum.RuntimeModeCodeOnly:
		return fleetv1.RuntimeMode_RUNTIME_MODE_CODE_ONLY
	case enum.RuntimeModeFullEnv:
		return fleetv1.RuntimeMode_RUNTIME_MODE_FULL_ENV
	case enum.RuntimeModeReadOnlyProduction:
		return fleetv1.RuntimeMode_RUNTIME_MODE_READ_ONLY_PRODUCTION
	case enum.RuntimeModePlatformJob:
		return fleetv1.RuntimeMode_RUNTIME_MODE_PLATFORM_JOB
	default:
		return fleetv1.RuntimeMode_RUNTIME_MODE_UNSPECIFIED
	}
}

func commandMetaToFleet(meta value.CommandMeta) *fleetv1.CommandMeta {
	return &fleetv1.CommandMeta{
		CommandId:       optionalString(meta.CommandID.String(), meta.CommandID != uuid.Nil),
		IdempotencyKey:  optionalString(meta.IdempotencyKey, strings.TrimSpace(meta.IdempotencyKey) != ""),
		ExpectedVersion: meta.ExpectedVersion,
		Actor: &fleetv1.Actor{
			Type: strings.TrimSpace(meta.Actor.Type),
			Id:   strings.TrimSpace(meta.Actor.ID),
		},
		Reason:    strings.TrimSpace(meta.Reason),
		RequestId: strings.TrimSpace(meta.RequestID),
		RequestContext: &fleetv1.RequestContext{
			Source:       strings.TrimSpace(meta.RequestContext.Source),
			TraceId:      optionalString(meta.RequestContext.TraceID, strings.TrimSpace(meta.RequestContext.TraceID) != ""),
			SessionId:    optionalString(meta.RequestContext.SessionID, strings.TrimSpace(meta.RequestContext.SessionID) != ""),
			ClientIpHash: optionalString(meta.RequestContext.ClientIPHash, strings.TrimSpace(meta.RequestContext.ClientIPHash) != ""),
		},
	}
}

func optionalString(raw string, include bool) *string {
	if !include {
		return nil
	}
	value := strings.TrimSpace(raw)
	return &value
}

func mapFleetError(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument
	case codes.Unauthenticated, codes.PermissionDenied:
		return errs.ErrForbidden
	case codes.FailedPrecondition:
		return errs.ErrPlacementRejected
	case codes.NotFound:
		return errs.ErrPreconditionFailed
	case codes.Aborted, codes.AlreadyExists:
		return errs.ErrConflict
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable
	default:
		return errs.ErrDependencyUnavailable
	}
}
