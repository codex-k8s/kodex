// Package owners contains typed route configuration for service owners.
package owners

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	TransportGRPC = "grpc"
)

const (
	ServiceAccessManager  = "access-manager"
	ServiceAgentManager   = "agent-manager"
	ServiceProjectCatalog = "project-catalog"
	ServiceProviderHub    = "provider-hub"
	ServiceRuntimeManager = "runtime-manager"
	ServiceFleetManager   = "fleet-manager"
	ServicePackageHub     = "package-hub"
	ServiceInteractionHub = "interaction-hub"
)

// RouteConfig describes a future client route to an authoritative service owner.
type RouteConfig struct {
	Service   string
	GRPCAddr  string
	AuthToken string
	Timeout   time.Duration
	Enabled   bool
}

// Route is a value-safe dependency route exposed to diagnostics.
type Route struct {
	Service        string
	Transport      string
	Target         string
	Enabled        bool
	AuthConfigured bool
	Timeout        time.Duration
}

// Catalog keeps dependency routes without opening business clients in MCP-2.
type Catalog struct {
	routes []Route
}

// NewCatalog validates owner routes and keeps them for future business tools.
func NewCatalog(configs []RouteConfig) (Catalog, error) {
	routes := make([]Route, 0, len(configs))
	seen := make(map[string]struct{}, len(configs))
	for _, cfg := range configs {
		service := strings.TrimSpace(cfg.Service)
		if service == "" {
			return Catalog{}, fmt.Errorf("owner route service is required")
		}
		if _, ok := seen[service]; ok {
			return Catalog{}, fmt.Errorf("owner route %s is duplicated", service)
		}
		seen[service] = struct{}{}
		target := strings.TrimSpace(cfg.GRPCAddr)
		if cfg.Enabled && target == "" {
			return Catalog{}, fmt.Errorf("owner route %s gRPC address is required", service)
		}
		if cfg.Enabled && cfg.Timeout <= 0 {
			return Catalog{}, fmt.Errorf("owner route %s timeout is invalid", service)
		}
		routes = append(routes, Route{
			Service:        service,
			Transport:      TransportGRPC,
			Target:         target,
			Enabled:        cfg.Enabled,
			AuthConfigured: strings.TrimSpace(cfg.AuthToken) != "",
			Timeout:        cfg.Timeout,
		})
	}
	return Catalog{routes: routes}, nil
}

// Routes returns a copy of configured owner routes.
func (catalog Catalog) Routes() []Route {
	return slices.Clone(catalog.routes)
}

// Ready reports whether route catalog was composed.
func (catalog Catalog) Ready() bool {
	return catalog.routes != nil
}
