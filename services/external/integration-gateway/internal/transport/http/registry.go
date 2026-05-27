package httptransport

import "strings"

type routeRegistry struct {
	providerWebhookEnabled  bool
	allowedProviderSlugs    map[string]struct{}
	externalCallbackEnabled bool
	allowedCallbackSources  map[string]struct{}
}

func newRouteRegistry(providerWebhookEnabled bool, slugs []string, externalCallbackEnabled bool, sources []string) routeRegistry {
	allowedProviders := normalizeRegistryValues(slugs)
	allowedSources := normalizeRegistryValues(sources)
	return routeRegistry{
		providerWebhookEnabled:  providerWebhookEnabled,
		allowedProviderSlugs:    allowedProviders,
		externalCallbackEnabled: externalCallbackEnabled,
		allowedCallbackSources:  allowedSources,
	}
}

func normalizeRegistryValues(values []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return allowed
}

func (r routeRegistry) ready() bool {
	return len(r.allowedProviderSlugs) > 0 || len(r.allowedCallbackSources) > 0
}

func (r routeRegistry) providerWebhookAllowed(slug string) bool {
	if !r.providerWebhookEnabled {
		return false
	}
	_, ok := r.allowedProviderSlugs[strings.ToLower(strings.TrimSpace(slug))]
	return ok
}

func (r routeRegistry) externalCallbackAllowed(source string) bool {
	if !r.externalCallbackEnabled {
		return false
	}
	_, ok := r.allowedCallbackSources[strings.ToLower(strings.TrimSpace(source))]
	return ok
}
