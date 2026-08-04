package grpc

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
)

// ApplicationRegistry выбирает проверяющий компонент только по точному
// подтверждённому mTLS-узлу.
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
	for _, candidate := range registry.authenticators {
		if candidate.VerifyPeer(ctx) == nil {
			return nil
		}
	}
	return errs.ErrUnauthenticated
}

func (registry *ApplicationRegistry) Authenticate(
	ctx context.Context,
) (authoritytype.ApplicationIdentity, error) {
	var selected authoritytype.ApplicationIdentity
	matches := 0
	for _, candidate := range registry.authenticators {
		if candidate.VerifyPeer(ctx) == nil {
			identity, err := candidate.Authenticate(ctx)
			if err == nil {
				selected = identity
				matches++
			}
		}
	}
	if matches != 1 {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return selected, nil
}
