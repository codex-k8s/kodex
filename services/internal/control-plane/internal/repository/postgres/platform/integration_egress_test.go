package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestIntegrationEgressShippedPackagePins accepts only the exact supported shipped egress package
// pins and rejects stale/foreign adapter identity or incomplete/invalid bound content without
// network access.
// TestIntegrationEgressShippedPackagePins принимает только точные привязки поддерживаемого
// поставляемого egress-пакета и отклоняет устаревшую либо чужую идентичность адаптера и неполное
// либо неверное связанное содержимое без сетевого доступа.
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

// TestSentryEgressShippedPins keeps Cloud projection on exact shipped revisions while self-hosted credentials remain on the separate operator-pinned native transport.
// TestSentryEgressShippedPins сохраняет Cloud projection на точных shipped revisions; self-hosted credentials остаются в отдельном operator-pinned native transport.
func TestSentryEgressShippedPins(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{integrationDefinitions: definitions}
	for _, key := range []string{"sentry", "sentry-self-hosted", "sentry-organization", "sentry-self-hosted-organization"} {
		definition := definitions[key]
		_, err := repository.integrationEgressPackage(key, definition.Metadata.Version, definition.Digest, "", "")
		if (err == nil) != (key == "sentry" || key == "sentry-organization") {
			t.Fatal("Sentry projection crossed transport boundary")
		}
	}
	definition := definitions["sentry"]
	if _, err := repository.integrationEgressPackage("sentry", definition.Metadata.Version, strings.Repeat("f", 64), "", ""); err == nil {
		t.Fatal("Sentry foreign digest reached projection")
	}
	if _, err := repository.integrationEgressPackage("sentry", definition.Metadata.Version, definition.Digest, "JSON", "{}"); err == nil {
		t.Fatal("Sentry invalid managed binding fell back to shipped")
	}
}
