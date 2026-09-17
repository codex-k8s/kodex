package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestBootstrapProviderCredentialPresence(t *testing.T) {
	t.Parallel()
	for mask := range 16 {
		t.Run(fmt.Sprintf("presence-%04b", mask), func(t *testing.T) {
			config := Config{}
			if mask&1 != 0 {
				config.DefaultProviderSecretName = "bootstrap-provider"
			}
			if mask&2 != 0 {
				config.DefaultProviderSecretUID = "00000000-0000-4000-8000-000000000001"
			}
			if mask&4 != 0 {
				config.DefaultProviderSecretVersion = "1"
			}
			if mask&8 != 0 {
				config.DefaultProviderCredentialSHA256 = strings.Repeat("a", 64)
			}
			if got, want := validBootstrapProviderCredentialConfig(config), mask == 0 || mask == 15; got != want {
				t.Fatalf("credential configuration accepted = %v, want %v", got, want)
			}
		})
	}
}
