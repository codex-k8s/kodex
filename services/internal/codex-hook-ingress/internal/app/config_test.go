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
		LogicalTransportReadOnly: true,
	}
}
