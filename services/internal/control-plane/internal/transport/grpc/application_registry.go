package grpc

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

// ApplicationRegistry выбирает verifier только по exact verified mTLS peer.
type ApplicationRegistry struct {
	authenticators []ApplicationAuthenticator
}

func NewApplicationRegistry(
	authenticators []ApplicationAuthenticator,
) (*ApplicationRegistry, error) {
	if len(authenticators) < 2 {
		return nil, errors.New("application authenticator registry is incomplete")
	}
	return &ApplicationRegistry{authenticators: authenticators}, nil
}

func (registry *ApplicationRegistry) VerifyPeer(ctx context.Context) error {
	_, err := registry.selectAuthenticator(ctx)
	return err
}

func (registry *ApplicationRegistry) Authenticate(
	ctx context.Context,
) (authoritytype.ApplicationIdentity, error) {
	authenticator, err := registry.selectAuthenticator(ctx)
	if err != nil {
		return authoritytype.ApplicationIdentity{}, err
	}
	return authenticator.Authenticate(ctx)
}

func (registry *ApplicationRegistry) selectAuthenticator(
	ctx context.Context,
) (ApplicationAuthenticator, error) {
	var selected ApplicationAuthenticator
	for _, candidate := range registry.authenticators {
		if candidate.VerifyPeer(ctx) == nil {
			if selected != nil {
				return nil, errs.ErrUnauthenticated
			}
			selected = candidate
		}
	}
	if selected == nil {
		return nil, errs.ErrUnauthenticated
	}
	return selected, nil
}
