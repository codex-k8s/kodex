package githubratelimit

import (
	"fmt"
	"time"
)

// NewService constructs canonical GitHub rate-limit domain service.
func NewService(cfg Config, deps Dependencies) (*Service, error) {
	if deps.Runs == nil {
		return nil, fmt.Errorf("agent run repository is required")
	}
	if deps.Waits == nil {
		return nil, fmt.Errorf("github rate-limit wait repository is required")
	}

	capabilities, err := ResolveRolloutCapabilities(cfg.RolloutState)
	if err != nil {
		return nil, err
	}

	return &Service{
		cfg:          cfg,
		runs:         deps.Runs,
		waits:        deps.Waits,
		flowEvents:   deps.FlowEvents,
		runStatus:    deps.RunStatusRetry,
		platform:     deps.PlatformReplay,
		capabilities: capabilities,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}, nil
}
