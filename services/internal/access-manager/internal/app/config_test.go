package app

import "testing"

func TestLoadConfigRequiresDatabaseDSN(t *testing.T) {
	t.Setenv("KODEX_ACCESS_MANAGER_DATABASE_DSN", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() err = nil, want required database DSN error")
	}
}

func TestLoadConfigAcceptsDatabaseDSNFromEnvironment(t *testing.T) {
	t.Setenv("KODEX_ACCESS_MANAGER_DATABASE_DSN", "postgres://postgres:5432/kodex_access_manager?sslmode=disable")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.DatabaseDSN == "" {
		t.Fatal("DatabaseDSN is empty")
	}
}
