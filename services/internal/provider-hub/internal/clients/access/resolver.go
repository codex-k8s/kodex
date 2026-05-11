// Package access adapts access-manager external account usage checks to provider-hub.
package access

import (
	"context"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/codex-k8s/kodex/libs/go/accesscheck"
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

const callerID = "provider-hub"

// Config contains access-manager client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// ExternalAccountUsageResolver calls access-manager before provider API usage.
type ExternalAccountUsageResolver struct {
	client *accesscheck.Client
}

var _ providerservice.AccountUsageResolver = (*ExternalAccountUsageResolver)(nil)

// NewConnection creates a gRPC connection to access-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return accesscheck.NewConnection(cfg.Addr)
}

// NewExternalAccountUsageResolver wraps a generated access-manager client.
func NewExternalAccountUsageResolver(client accessaccountsv1.AccessManagerServiceClient, cfg Config) (*ExternalAccountUsageResolver, error) {
	checker, err := accesscheck.New(client, accesscheck.Config{AuthToken: cfg.AuthToken, CallerID: callerID, Timeout: cfg.Timeout})
	if err != nil {
		return nil, err
	}
	return &ExternalAccountUsageResolver{client: checker}, nil
}

// ResolveExternalAccountUsage confirms account use and returns only secret metadata.
func (r *ExternalAccountUsageResolver) ResolveExternalAccountUsage(ctx context.Context, input providerservice.ExternalAccountUsageInput) (providerservice.ExternalAccountUsageResult, error) {
	result, err := r.client.ResolveExternalAccountUsage(ctx, accesscheck.ExternalAccountUsageRequest{
		ExternalAccountID: input.ExternalAccountID.String(),
		ActionKey:         input.ActionKey,
		Scope:             accesscheck.Scope{Type: input.ScopeType, ID: input.ScopeID},
	})
	if err != nil {
		return providerservice.ExternalAccountUsageResult{}, mapAccessError(err)
	}
	if _, err := uuid.Parse(result.ExternalAccountID); err != nil {
		return providerservice.ExternalAccountUsageResult{}, errs.ErrDependencyUnavailable
	}
	return providerservice.ExternalAccountUsageResult{
		ExternalAccountID: result.ExternalAccountID,
		ProviderSlug:      enum.ProviderSlug(result.ProviderSlug),
		SecretStoreType:   result.SecretStoreType,
		SecretStoreRef:    result.SecretStoreRef,
		AllowedActionKeys: append([]string(nil), result.AllowedActionKeys...),
	}, nil
}

func mapAccessError(err error) error {
	return accesscheck.MapError(err, accesscheck.DomainErrors{
		InvalidRequest:        errs.ErrInvalidArgument,
		Forbidden:             errs.ErrForbidden,
		DependencyUnavailable: errs.ErrDependencyUnavailable,
	})
}
