package app

import "testing"

func TestLoadConfigAllowsMissingGRPCAuthTokenWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_PROJECT_CATALOG_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.GRPCAuthRequired {
		t.Fatal("GRPCAuthRequired = true, want false")
	}
}
