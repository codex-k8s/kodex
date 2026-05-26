package app

import "testing"

func TestConfigValidateAcceptsDefaultHookSet(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	events, err := cfg.SupportedEvents()
	if err != nil {
		t.Fatalf("SupportedEvents(): %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("events len = %d, want 6", len(events))
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
}

func TestConfigValidateRejectsCompactHooks(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.SupportedHookEvents = defaultSupportedEvents + ",PreCompact"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error is nil, want compact hook rejection")
	}
}

func TestConfigValidateKeepsSchemaLimitsPinned(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.MaxEnvelopeBytes = 65537
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error is nil, want schema limit error")
	}
}

func TestConfigValidateAcceptsDisabledRoutes(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.DisabledRoutes = "operations-feed,governance-manager"
	owners, err := cfg.DisabledRouteOwners()
	if err != nil {
		t.Fatalf("DisabledRouteOwners(): %v", err)
	}
	if len(owners) != 2 {
		t.Fatalf("disabled routes len = %d, want 2", len(owners))
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
}

func TestConfigValidateRejectsUnknownDisabledRoute(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.DisabledRoutes = "raw-store"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error is nil, want disabled route error")
	}
}

func TestConfigValidateRejectsUnknownRouteFailurePolicy(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.RouteFailurePolicy = "continue_anyway"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error is nil, want route failure policy error")
	}
}

func TestConfigValidateRejectsInvalidOpsFeedAndRateLimitPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "ops feed capacity",
			mutate: func(cfg *Config) {
				cfg.OpsFeedCapacity = 0
			},
		},
		{
			name: "ops feed retention",
			mutate: func(cfg *Config) {
				cfg.OpsFeedRetention = 0
			},
		},
		{
			name: "rate limit window",
			mutate: func(cfg *Config) {
				cfg.RateLimitWindow = 0
			},
		},
		{
			name: "rate limit burst",
			mutate: func(cfg *Config) {
				cfg.RateLimitBurst = 0
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error is nil, want policy validation error")
			}
		})
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr:                 ":8080",
		ReadinessTimeout:         1,
		ShutdownTimeout:          1,
		SchemaVersion:            defaultSchemaVersion,
		SanitizerContractID:      defaultSanitizerContractID,
		SupportedHookEvents:      defaultSupportedEvents,
		MaxEnvelopeBytes:         65536,
		MaxTextPreviewBytes:      4096,
		MaxBoundedErrorBytes:     8192,
		RouteFailurePolicy:       defaultRouteFailurePolicy,
		OpsFeedCapacity:          1024,
		OpsFeedRetention:         1,
		RateLimitWindow:          1,
		RateLimitBurst:           300,
		LogicalTransportReadOnly: true,
	}
}
