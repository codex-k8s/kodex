package grpc

import (
	"context"

	"github.com/google/uuid"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
	grpccasters "github.com/codex-k8s/kodex/services/internal/package-hub/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ packagesv1.PackageHubServiceServer = (*Server)(nil)

// Server exposes PackageHubService over gRPC.
type Server struct {
	packagesv1.UnimplementedPackageHubServiceServer

	service packageService
}

type packageService interface {
	ConnectPackageSource(context.Context, packageservice.ConnectPackageSourceInput) (entity.PackageSource, error)
	UpdatePackageSource(context.Context, packageservice.UpdatePackageSourceInput) (entity.PackageSource, error)
	DisablePackageSource(context.Context, packageservice.DisablePackageSourceInput) (entity.PackageSource, error)
	GetPackageSource(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageSource, error)
	ListPackageSources(context.Context, packageservice.ListPackageSourcesInput) (packageservice.ListPackageSourcesResult, error)
	GetPackage(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageEntry, error)
	ListPackages(context.Context, packageservice.ListPackagesInput) (packageservice.ListPackagesResult, error)
	GetPackageVersion(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageVersion, error)
	ListPackageVersions(context.Context, packageservice.ListPackageVersionsInput) (packageservice.ListPackageVersionsResult, error)
	GetPackageManifest(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageManifestSnapshot, error)
	SetPackageVerification(context.Context, packageservice.SetPackageVerificationInput) (packageservice.SetPackageVerificationResult, error)
}

type unaryCaster[Request any, Input any] func(*Request) (Input, error)
type unaryCaller[Input any, Output any] func(context.Context, Input) (Output, error)
type unaryResponder[Output any, Response any] func(Output) *Response

// NewServer creates a package-hub gRPC server boundary.
func NewServer(service packageService) *Server {
	if service == nil {
		panic("package-hub grpc service is required")
	}
	return &Server{service: service}
}

// RegisterPackageHubService registers package-hub handlers in a gRPC runtime.
func RegisterPackageHubService(registrar grpcruntime.ServiceRegistrar, service packageService) {
	packagesv1.RegisterPackageHubServiceServer(registrar, NewServer(service))
}

// ConnectPackageSource connects a source and records an idempotent command result.
func (server *Server) ConnectPackageSource(ctx context.Context, request *packagesv1.ConnectPackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return handleUnary(ctx, request, grpccasters.ConnectPackageSourceInput, server.service.ConnectPackageSource, grpccasters.PackageSourceResponse)
}

// UpdatePackageSource changes safe source metadata and lifecycle status.
func (server *Server) UpdatePackageSource(ctx context.Context, request *packagesv1.UpdatePackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return handleUnary(ctx, request, grpccasters.UpdatePackageSourceInput, server.service.UpdatePackageSource, grpccasters.PackageSourceResponse)
}

// DisablePackageSource disables a source without deleting catalog history.
func (server *Server) DisablePackageSource(ctx context.Context, request *packagesv1.DisablePackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return handleUnary(ctx, request, grpccasters.DisablePackageSourceInput, server.service.DisablePackageSource, grpccasters.PackageSourceResponse)
}

// GetPackageSource returns authoritative source state.
func (server *Server) GetPackageSource(ctx context.Context, request *packagesv1.GetPackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetPackageSourceInput, server.getPackageSource, grpccasters.PackageSourceResponse)
}

// ListPackageSources returns sources visible in the requested scope.
func (server *Server) ListPackageSources(ctx context.Context, request *packagesv1.ListPackageSourcesRequest) (*packagesv1.ListPackageSourcesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListPackageSourcesInput, server.service.ListPackageSources, grpccasters.ListPackageSourcesResponse)
}

// GetPackage returns an authoritative local package catalog entry.
func (server *Server) GetPackage(ctx context.Context, request *packagesv1.GetPackageRequest) (*packagesv1.PackageResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetPackageInput, server.getPackage, grpccasters.PackageResponse)
}

// ListPackages returns local available package catalog entries.
func (server *Server) ListPackages(ctx context.Context, request *packagesv1.ListPackagesRequest) (*packagesv1.ListPackagesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListPackagesInput, server.service.ListPackages, grpccasters.ListPackagesResponse)
}

// GetPackageVersion returns one package version.
func (server *Server) GetPackageVersion(ctx context.Context, request *packagesv1.GetPackageVersionRequest) (*packagesv1.PackageVersionResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetPackageVersionInput, server.getPackageVersion, grpccasters.PackageVersionResponse)
}

// ListPackageVersions returns versions for one package.
func (server *Server) ListPackageVersions(ctx context.Context, request *packagesv1.ListPackageVersionsRequest) (*packagesv1.ListPackageVersionsResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListPackageVersionsInput, server.service.ListPackageVersions, grpccasters.ListPackageVersionsResponse)
}

// GetPackageManifest returns a normalized manifest snapshot for a package version.
func (server *Server) GetPackageManifest(ctx context.Context, request *packagesv1.GetPackageManifestRequest) (*packagesv1.PackageManifestResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetPackageManifestInput, server.getPackageManifest, grpccasters.PackageManifestResponse)
}

// SetPackageVerification records a verification decision for a version.
func (server *Server) SetPackageVerification(ctx context.Context, request *packagesv1.SetPackageVerificationRequest) (*packagesv1.PackageVerificationResponse, error) {
	return handleUnary(ctx, request, grpccasters.SetPackageVerificationInput, server.service.SetPackageVerification, grpccasters.PackageVerificationResponse)
}

func handleUnary[Request any, Input any, Output any, Response any](ctx context.Context, request *Request, cast unaryCaster[Request, Input], call unaryCaller[Input, Output], respond unaryResponder[Output, Response]) (*Response, error) {
	route := unaryRoute[Request, Input, Output, Response]{cast: cast, call: call, respond: respond}
	return route.invoke(ctx, request)
}

type unaryRoute[Request any, Input any, Output any, Response any] struct {
	cast    unaryCaster[Request, Input]
	call    unaryCaller[Input, Output]
	respond unaryResponder[Output, Response]
}

func (route unaryRoute[Request, Input, Output, Response]) invoke(ctx context.Context, request *Request) (*Response, error) {
	input, err := route.cast(request)
	if err != nil {
		return nil, err
	}
	output, err := route.call(ctx, input)
	if err != nil {
		return nil, err
	}
	return route.respond(output), nil
}

func (server *Server) getPackageSource(ctx context.Context, input grpccasters.IDQueryInput) (entity.PackageSource, error) {
	return server.service.GetPackageSource(ctx, input.ID, input.Meta)
}

func (server *Server) getPackage(ctx context.Context, input grpccasters.IDQueryInput) (entity.PackageEntry, error) {
	return server.service.GetPackage(ctx, input.ID, input.Meta)
}

func (server *Server) getPackageVersion(ctx context.Context, input grpccasters.IDQueryInput) (entity.PackageVersion, error) {
	return server.service.GetPackageVersion(ctx, input.ID, input.Meta)
}

func (server *Server) getPackageManifest(ctx context.Context, input grpccasters.IDQueryInput) (entity.PackageManifestSnapshot, error) {
	return server.service.GetPackageManifest(ctx, input.ID, input.Meta)
}
