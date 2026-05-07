// Package service implements runtime-manager domain use cases.
package service

import (
	"time"

	"github.com/google/uuid"

	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
)

// Config contains runtime domain defaults for the MVP single-cluster mode.
type Config struct {
	DefaultFleetScopeID uuid.UUID
	DefaultClusterID    uuid.UUID
	NamespacePrefix     string
	DefaultLeaseTTL     time.Duration
}

// Service coordinates runtime-manager domain commands and reads.
type Service struct {
	repository runtimerepo.Repository
	clock      runtimerepo.Clock
	ids        runtimerepo.IDGenerator
	config     Config
}

// NewWithConfig creates a runtime domain service with explicit MVP placement defaults.
func NewWithConfig(repository runtimerepo.Repository, clock runtimerepo.Clock, ids runtimerepo.IDGenerator, cfg Config) *Service {
	return &Service{repository: repository, clock: clock, ids: ids, config: normalizedConfig(cfg)}
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
