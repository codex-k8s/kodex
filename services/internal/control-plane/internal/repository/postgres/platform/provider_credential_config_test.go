package platform

import (
	"fmt"
	"strings"
	"testing"
)

func TestConfigureOptionalBootstrapProviderCredential(t *testing.T) {
	t.Parallel()
	complete := ProviderCredentialConfig{
		SecretName: "bootstrap-provider", SecretUID: "00000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64),
	}
	for mask := range 16 {
		t.Run(fmt.Sprintf("presence-%04b", mask), func(t *testing.T) {
			repository := &Repository{providerCredential: complete}
			config := complete
			if mask&1 == 0 {
				config.SecretName = ""
			}
			if mask&2 == 0 {
				config.SecretUID = ""
			}
			if mask&4 == 0 {
				config.SecretResourceVersion = ""
			}
			if mask&8 == 0 {
				config.ContentSHA256 = ""
			}
			err := repository.ConfigureProviderCredential(config)
			if want := mask == 0 || mask == 15; (err == nil) != want {
				t.Fatalf("credential configuration error = %v, expected acceptance %v", err, want)
			}
			if err != nil && repository.providerCredential != complete {
				t.Fatal("invalid configuration changed existing credential metadata")
			}
			if err == nil && repository.providerCredential != config {
				t.Fatal("accepted configuration was not stored exactly")
			}
		})
	}
}
