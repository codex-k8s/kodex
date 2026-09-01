package app

import (
	"testing"
	"time"
)

func TestValidProviderCredentialCleanupConfig(t *testing.T) {
	t.Parallel()
	valid := Config{
		ProviderCleanupBatchSize:    16,
		ProviderCleanupPollInterval: 250 * time.Millisecond,
		ProviderCleanupTimeout:      10 * time.Second,
	}
	if !validProviderCredentialCleanupConfig(valid) {
		t.Fatal("default provider credential cleanup configuration was rejected")
	}
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{name: "zero batch", change: func(value *Config) { value.ProviderCleanupBatchSize = 0 }},
		{name: "oversized batch", change: func(value *Config) { value.ProviderCleanupBatchSize = 17 }},
		{name: "short poll", change: func(value *Config) { value.ProviderCleanupPollInterval = 49 * time.Millisecond }},
		{name: "long poll", change: func(value *Config) { value.ProviderCleanupPollInterval = time.Minute + time.Millisecond }},
		{name: "short timeout", change: func(value *Config) { value.ProviderCleanupTimeout = 99 * time.Millisecond }},
		{name: "long timeout", change: func(value *Config) {
			value.ProviderCleanupTimeout = 30*time.Second + time.Millisecond
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := valid
			test.change(&value)
			if validProviderCredentialCleanupConfig(value) {
				t.Fatal("invalid provider credential cleanup configuration was accepted")
			}
		})
	}
}
