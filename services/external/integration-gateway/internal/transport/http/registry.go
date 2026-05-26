package httptransport

import "strings"

type routeRegistry struct {
	providerWebhookEnabled bool
	allowedProviderSlugs   map[string]struct{}
}

func newRouteRegistry(providerWebhookEnabled bool, slugs []string) routeRegistry {
	allowed := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		trimmed := strings.ToLower(strings.TrimSpace(slug))
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return routeRegistry{providerWebhookEnabled: providerWebhookEnabled, allowedProviderSlugs: allowed}
}

func (r routeRegistry) ready() bool {
	return len(r.allowedProviderSlugs) > 0
}

func (r routeRegistry) providerWebhookAllowed(slug string) bool {
	if !r.providerWebhookEnabled {
		return false
	}
	_, ok := r.allowedProviderSlugs[strings.ToLower(strings.TrimSpace(slug))]
	return ok
}
