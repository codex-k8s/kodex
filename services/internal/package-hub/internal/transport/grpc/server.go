package grpc

import (
	"context"

	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
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
	SyncAvailablePackages(context.Context, packageservice.SyncAvailablePackagesInput) (packageservice.SyncAvailablePackagesResult, error)
	GetPackage(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageEntry, error)
	ListPackages(context.Context, packageservice.ListPackagesInput) (packageservice.ListPackagesResult, error)
	GetPackageVersion(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageVersion, error)
	ListPackageVersions(context.Context, packageservice.ListPackageVersionsInput) (packageservice.ListPackageVersionsResult, error)
	GetPackageManifest(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageManifestSnapshot, error)
	RequestPackageInstallation(context.Context, packageservice.RequestPackageInstallationInput) (entity.PackageInstallation, error)
	UpdatePackageInstallation(context.Context, packageservice.UpdatePackageInstallationInput) (entity.PackageInstallation, error)
	DisablePackageInstallation(context.Context, packageservice.DisablePackageInstallationInput) (entity.PackageInstallation, error)
	UninstallPackage(context.Context, packageservice.UninstallPackageInput) (entity.PackageInstallation, error)
	GetPackageInstallation(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageInstallation, error)
	ListPackageInstallations(context.Context, packageservice.ListPackageInstallationsInput) (packageservice.ListPackageInstallationsResult, error)
	SetPackageVerification(context.Context, packageservice.SetPackageVerificationInput) (packageservice.SetPackageVerificationResult, error)
}

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
	return grpcserver.HandleUnary(ctx, request, grpccasters.ConnectPackageSourceInput, server.service.ConnectPackageSource, grpccasters.PackageSourceResponse)
}

// UpdatePackageSource changes safe source metadata and lifecycle status.
func (server *Server) UpdatePackageSource(ctx context.Context, request *packagesv1.UpdatePackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdatePackageSourceInput, server.service.UpdatePackageSource, grpccasters.PackageSourceResponse)
}

// DisablePackageSource disables a source without deleting catalog history.
func (server *Server) DisablePackageSource(ctx context.Context, request *packagesv1.DisablePackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.DisablePackageSourceInput, server.service.DisablePackageSource, grpccasters.PackageSourceResponse)
}

// GetPackageSource returns authoritative source state.
func (server *Server) GetPackageSource(ctx context.Context, request *packagesv1.GetPackageSourceRequest) (*packagesv1.PackageSourceResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPackageSourceInput, server.getPackageSource, grpccasters.PackageSourceResponse)
}

// ListPackageSources returns sources visible in the requested scope.
func (server *Server) ListPackageSources(ctx context.Context, request *packagesv1.ListPackageSourcesRequest) (*packagesv1.ListPackageSourcesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListPackageSourcesInput, server.service.ListPackageSources, grpccasters.ListPackageSourcesResponse)
}

// SyncAvailablePackages stores a normalized catalog snapshot for a source.
func (server *Server) SyncAvailablePackages(ctx context.Context, request *packagesv1.SyncAvailablePackagesRequest) (*packagesv1.SyncAvailablePackagesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.SyncAvailablePackagesInput, server.service.SyncAvailablePackages, grpccasters.SyncAvailablePackagesResponse)
}

// GetPackage returns an authoritative local package catalog entry.
func (server *Server) GetPackage(ctx context.Context, request *packagesv1.GetPackageRequest) (*packagesv1.PackageResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPackageInput, server.getPackage, grpccasters.PackageResponse)
}

// ListPackages returns local available package catalog entries.
func (server *Server) ListPackages(ctx context.Context, request *packagesv1.ListPackagesRequest) (*packagesv1.ListPackagesResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListPackagesInput, server.service.ListPackages, grpccasters.ListPackagesResponse)
}

// GetPackageVersion returns one package version.
func (server *Server) GetPackageVersion(ctx context.Context, request *packagesv1.GetPackageVersionRequest) (*packagesv1.PackageVersionResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPackageVersionInput, server.getPackageVersion, grpccasters.PackageVersionResponse)
}

// ListPackageVersions returns versions for one package.
func (server *Server) ListPackageVersions(ctx context.Context, request *packagesv1.ListPackageVersionsRequest) (*packagesv1.ListPackageVersionsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListPackageVersionsInput, server.service.ListPackageVersions, grpccasters.ListPackageVersionsResponse)
}

// GetPackageManifest returns a normalized manifest snapshot for a package version.
func (server *Server) GetPackageManifest(ctx context.Context, request *packagesv1.GetPackageManifestRequest) (*packagesv1.PackageManifestResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPackageManifestInput, server.getPackageManifest, grpccasters.PackageManifestResponse)
}

// RequestPackageInstallation records an installation intent for a package version and scope.
func (server *Server) RequestPackageInstallation(ctx context.Context, request *packagesv1.RequestPackageInstallationRequest) (*packagesv1.PackageInstallationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.RequestPackageInstallationInput, server.service.RequestPackageInstallation, grpccasters.PackageInstallationResponse)
}

// UpdatePackageInstallation changes installation version, desired state or safe lifecycle status.
func (server *Server) UpdatePackageInstallation(ctx context.Context, request *packagesv1.UpdatePackageInstallationRequest) (*packagesv1.PackageInstallationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UpdatePackageInstallationInput, server.service.UpdatePackageInstallation, grpccasters.PackageInstallationResponse)
}

// DisablePackageInstallation disables an installation without deleting history.
func (server *Server) DisablePackageInstallation(ctx context.Context, request *packagesv1.DisablePackageInstallationRequest) (*packagesv1.PackageInstallationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.DisablePackageInstallationInput, server.service.DisablePackageInstallation, grpccasters.PackageInstallationResponse)
}

// UninstallPackage marks an installation as uninstalled.
func (server *Server) UninstallPackage(ctx context.Context, request *packagesv1.UninstallPackageRequest) (*packagesv1.PackageInstallationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.UninstallPackageInput, server.service.UninstallPackage, grpccasters.PackageInstallationResponse)
}

// GetPackageInstallation returns one authoritative installation state.
func (server *Server) GetPackageInstallation(ctx context.Context, request *packagesv1.GetPackageInstallationRequest) (*packagesv1.PackageInstallationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.GetPackageInstallationInput, server.getPackageInstallation, grpccasters.PackageInstallationResponse)
}

// ListPackageInstallations returns installations visible in the requested scope.
func (server *Server) ListPackageInstallations(ctx context.Context, request *packagesv1.ListPackageInstallationsRequest) (*packagesv1.ListPackageInstallationsResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.ListPackageInstallationsInput, server.service.ListPackageInstallations, grpccasters.ListPackageInstallationsResponse)
}

// SetPackageVerification records a verification decision for a version.
func (server *Server) SetPackageVerification(ctx context.Context, request *packagesv1.SetPackageVerificationRequest) (*packagesv1.PackageVerificationResponse, error) {
	return grpcserver.HandleUnary(ctx, request, grpccasters.SetPackageVerificationInput, server.service.SetPackageVerification, grpccasters.PackageVerificationResponse)
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

func (server *Server) getPackageInstallation(ctx context.Context, input grpccasters.IDQueryInput) (entity.PackageInstallation, error) {
	return server.service.GetPackageInstallation(ctx, input.ID, input.Meta)
}
