package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var _ packageservice.SecretRefReader = (*PackageSecretRefReader)(nil)

// PackageSecretRefReader reads value-free package secret refs from access-manager.
type PackageSecretRefReader struct {
	client    accessaccountsv1.AccessManagerServiceClient
	authToken string
	timeout   time.Duration
}

// NewPackageSecretRefReader creates access-manager adapter for package installation secret refs.
func NewPackageSecretRefReader(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*PackageSecretRefReader, error) {
	if client == nil {
		return nil, fmt.Errorf("access-manager client is required")
	}
	authToken := strings.TrimSpace(cfg.AuthToken)
	if authToken == "" {
		return nil, fmt.Errorf("access-manager auth token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = accesscheck.DefaultTimeout
	}
	return &PackageSecretRefReader{
		client:    client,
		authToken: authToken,
		timeout:   timeout,
	}, nil
}

// ListPackageInstallationSecretRefs returns value-free refs for one package installation.
func (r *PackageSecretRefReader) ListPackageInstallationSecretRefs(ctx context.Context, input packageservice.ListPackageInstallationSecretRefsInput) (packageservice.ListPackageInstallationSecretRefsResult, error) {
	if r == nil || r.client == nil {
		return packageservice.ListPackageInstallationSecretRefsResult{}, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(r.outgoingContext(ctx), r.timeout)
	defer cancel()
	response, err := r.client.ListPackageInstallationSecretRefs(callCtx, listPackageInstallationSecretRefsRequest(input))
	if err != nil {
		return packageservice.ListPackageInstallationSecretRefsResult{}, mapListSecretRefsError(err)
	}
	return listPackageInstallationSecretRefsResult(response)
}

func (r *PackageSecretRefReader) outgoingContext(ctx context.Context) context.Context {
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

func listPackageInstallationSecretRefsRequest(input packageservice.ListPackageInstallationSecretRefsInput) *accessaccountsv1.ListPackageInstallationSecretRefsRequest {
	return &accessaccountsv1.ListPackageInstallationSecretRefsRequest{
		PackageInstallationId: input.PackageInstallationID.String(),
		InstallationScope: &accessaccountsv1.ScopeRef{
			Type: string(input.InstallationScope.Type),
			Id:   input.InstallationScope.Ref,
		},
		LogicalKeys: append([]string(nil), input.LogicalKeys...),
		Meta:        commandMeta(input.Meta),
	}
}

func commandMeta(meta value.CommandMeta) *accessaccountsv1.CommandMeta {
	return &accessaccountsv1.CommandMeta{
		CommandId:      meta.CommandID.String(),
		IdempotencyKey: meta.IdempotencyKey,
		ExpectedVersion: func() *int64 {
			if meta.ExpectedVersion == nil {
				return nil
			}
			value := *meta.ExpectedVersion
			return &value
		}(),
		Actor: &accessaccountsv1.Actor{
			Type: meta.Actor.Type,
			Id:   meta.Actor.ID,
		},
		Reason:    meta.Reason,
		RequestId: meta.RequestID,
		RequestContext: &accessaccountsv1.RequestContext{
			Source:       meta.RequestContext.Source,
			TraceId:      meta.RequestContext.TraceID,
			SessionId:    meta.RequestContext.SessionID,
			ClientIpHash: meta.RequestContext.ClientIPHash,
		},
	}
}

func listPackageInstallationSecretRefsResult(response *accessaccountsv1.ListPackageInstallationSecretRefsResponse) (packageservice.ListPackageInstallationSecretRefsResult, error) {
	refs := response.GetSecretRefs()
	result := packageservice.ListPackageInstallationSecretRefsResult{
		SecretRefs: make([]value.PackageInstallationSecretRef, 0, len(refs)),
	}
	for _, ref := range refs {
		status, err := packageSecretRefStatus(ref.GetStatus())
		if err != nil {
			return packageservice.ListPackageInstallationSecretRefsResult{}, err
		}
		result.SecretRefs = append(result.SecretRefs, value.PackageInstallationSecretRef{
			LogicalKey: strings.TrimSpace(ref.GetLogicalKey()),
			Status:     status,
			StoreType:  strings.TrimSpace(ref.GetSecretStoreType()),
			StoreRef:   strings.TrimSpace(ref.GetSecretStoreRef()),
		})
	}
	return result, nil
}

func packageSecretRefStatus(status accessaccountsv1.PackageInstallationSecretRefStatus) (enum.PackageInstallationSecretRefStatus, error) {
	switch status {
	case accessaccountsv1.PackageInstallationSecretRefStatus_PACKAGE_INSTALLATION_SECRET_REF_STATUS_CONFIGURED:
		return enum.PackageInstallationSecretRefStatusConfigured, nil
	case accessaccountsv1.PackageInstallationSecretRefStatus_PACKAGE_INSTALLATION_SECRET_REF_STATUS_MISSING:
		return enum.PackageInstallationSecretRefStatusMissing, nil
	case accessaccountsv1.PackageInstallationSecretRefStatus_PACKAGE_INSTALLATION_SECRET_REF_STATUS_INVALID:
		return enum.PackageInstallationSecretRefStatusInvalid, nil
	case accessaccountsv1.PackageInstallationSecretRefStatus_PACKAGE_INSTALLATION_SECRET_REF_STATUS_DISABLED:
		return enum.PackageInstallationSecretRefStatusDisabled, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func mapListSecretRefsError(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errs.ErrDependencyUnavailable
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument
	case codes.PermissionDenied, codes.Unauthenticated:
		return errs.ErrForbidden
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable
	default:
		return fmt.Errorf("%w: access-manager secret refs failed", errs.ErrDependencyUnavailable)
	}
}
