package integrationpackage

import (
	"errors"
	"strings"
)

// ValidSentrySlug accepts one bounded literal organization/project path segment without traversal, encoding, separators or selectors.
// ValidSentrySlug допускает один ограниченный буквальный сегмент организации/проекта без traversal, кодирования, разделителей и селекторов.
func ValidSentrySlug(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

// validateSentryConfiguration preserves the server-owned Cloud origin allowlist and literal tenant/project coordinates even if a managed definition attempts to weaken field constraints.
// validateSentryConfiguration сохраняет серверный Cloud origin allowlist и буквальные tenant/project координаты даже при ослаблении ограничений полей managed definition.
func validateSentryConfiguration(definition Package, configuration map[string]string) error {
	if (definition.Metadata.Key != "sentry" && definition.Metadata.Key != "sentry-self-hosted" && definition.Metadata.Key != "sentry-organization" && definition.Metadata.Key != "sentry-self-hosted-organization") || !ValidSentrySlug(configuration["organization_slug"]) || (!strings.HasSuffix(definition.Metadata.Key, "-organization") && !ValidSentrySlug(configuration["project_slug"])) {
		return errors.New("Sentry configuration is invalid")
	}
	if (definition.Metadata.Key == "sentry" || definition.Metadata.Key == "sentry-organization") && configuration["base_url"] != "https://sentry.io" && configuration["base_url"] != "https://us.sentry.io" && configuration["base_url"] != "https://de.sentry.io" {
		return errors.New("Sentry Cloud origin is invalid")
	}
	return nil
}
