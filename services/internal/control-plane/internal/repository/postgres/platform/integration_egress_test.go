package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

func TestIntegrationEgressShippedPackagePins(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	r := &Repository{integrationDefinitions: definitions}
	shipped := definitions["context7"]
	for _, fixture := range []struct {
		name, key, version, digest, format, content string
		valid                                       bool
	}{
		{"exact", "context7", shipped.Metadata.Version, shipped.Digest, "", "", true},
		{"stale_version", "context7", "0.0.1", shipped.Digest, "", "", false},
		{"foreign_digest", "context7", shipped.Metadata.Version, strings.Repeat("f", 64), "", "", false},
		{"other_adapter", "openapi-mcp", definitions["openapi-mcp"].Metadata.Version, definitions["openapi-mcp"].Digest, "", "", false},
		{"missing_format", "context7", shipped.Metadata.Version, shipped.Digest, "", "{}", false},
		{"missing_content", "context7", shipped.Metadata.Version, shipped.Digest, "JSON", "", false},
		{"invalid_bound_content", "context7", shipped.Metadata.Version, shipped.Digest, "JSON", "{}", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			definition, err := r.integrationEgressPackage(fixture.key, fixture.version, fixture.digest, fixture.format, fixture.content)
			if (err == nil) != fixture.valid || fixture.valid && definition.Digest != shipped.Digest {
				t.Fatal("shipped egress package pin validation mismatch")
			}
		})
	}
}
