package service

import (
	"github.com/google/uuid"

	fleetrepo "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/repository/fleet"
)

// Config contains fleet domain defaults and integrations.
type Config struct {
	Authorizer          Authorizer
	PlatformDefaultSeed PlatformDefaultSeed
}

// Service coordinates fleet-manager domain commands and reads.
type Service struct {
	repository fleetrepo.Repository
	clock      fleetrepo.Clock
	ids        fleetrepo.IDGenerator
	authorizer Authorizer
	seed       PlatformDefaultSeed
}

// NewWithConfig creates a fleet domain service with explicit dependencies.
func NewWithConfig(repository fleetrepo.Repository, clock fleetrepo.Clock, ids fleetrepo.IDGenerator, cfg Config) *Service {
	authorizer := cfg.Authorizer
	if authorizer == nil {
		authorizer = AllowAllAuthorizer{}
	}
	return &Service{repository: repository, clock: clock, ids: ids, authorizer: authorizer, seed: normalizedSeed(cfg.PlatformDefaultSeed)}
}

func normalizedSeed(seed PlatformDefaultSeed) PlatformDefaultSeed {
	if seed.FleetScopeID == uuid.Nil {
		seed.FleetScopeID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	}
	if seed.ClusterID == uuid.Nil {
		seed.ClusterID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	}
	if seed.ScopeKey == "" {
		seed.ScopeKey = platformDefaultKey
	}
	if seed.ScopeDisplayName == "" {
		seed.ScopeDisplayName = "Platform default"
	}
	if seed.ClusterKey == "" {
		seed.ClusterKey = platformDefaultKey
	}
	return seed
}
