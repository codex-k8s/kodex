package owners

import (
	"testing"
	"time"
)

func TestNewCatalogRejectsDuplicateServices(t *testing.T) {
	t.Parallel()

	_, err := NewCatalog([]RouteConfig{
		{Service: ServiceAgentManager, GRPCAddr: "agent-manager:9090", Timeout: time.Second, Enabled: true},
		{Service: ServiceAgentManager, GRPCAddr: "agent-manager:9090", Timeout: time.Second, Enabled: true},
	})
	if err == nil {
		t.Fatal("NewCatalog() error is nil, want duplicate error")
	}
}

func TestNewCatalogKeepsSafeRouteMetadata(t *testing.T) {
	t.Parallel()

	catalog, err := NewCatalog([]RouteConfig{{
		Service:   ServiceProjectCatalog,
		GRPCAddr:  "project-catalog:9090",
		AuthToken: "secret-token",
		Timeout:   3 * time.Second,
		Enabled:   true,
	}})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	routes := catalog.Routes()
	if len(routes) != 1 {
		t.Fatalf("routes len = %d, want 1", len(routes))
	}
	if routes[0].AuthConfigured != true || routes[0].Target != "project-catalog:9090" {
		t.Fatalf("route = %+v", routes[0])
	}
}
