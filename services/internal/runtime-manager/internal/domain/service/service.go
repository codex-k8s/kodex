// Package service implements runtime-manager domain use cases.
package service

import (
	"time"

	"github.com/google/uuid"

	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
)

// Config contains runtime domain defaults and integrations.
type Config struct {
	DefaultFleetScopeID uuid.UUID
	DefaultClusterID    uuid.UUID
	NamespacePrefix     string
	DefaultLeaseTTL     time.Duration
	Authorizer          Authorizer
}

// Service coordinates runtime-manager domain commands and reads.
type Service struct {
	repository runtimerepo.Repository
	clock      runtimerepo.Clock
	ids        runtimerepo.IDGenerator
	config     Config
	authorizer Authorizer
}

// NewWithConfig creates a runtime domain service with explicit MVP placement defaults.
func NewWithConfig(repository runtimerepo.Repository, clock runtimerepo.Clock, ids runtimerepo.IDGenerator, cfg Config) *Service {
	cfg = normalizedConfig(cfg)
	authorizer := cfg.Authorizer
	if authorizer == nil {
		authorizer = AllowAllAuthorizer{}
	}
	return &Service{repository: repository, clock: clock, ids: ids, config: cfg, authorizer: authorizer}
}

func normalizedConfig(cfg Config) Config {
	if cfg.NamespacePrefix == "" {
		cfg.NamespacePrefix = "kodex-rt"
	}
	if cfg.DefaultLeaseTTL <= 0 {
		cfg.DefaultLeaseTTL = 30 * time.Minute
	}
	return cfg
}
