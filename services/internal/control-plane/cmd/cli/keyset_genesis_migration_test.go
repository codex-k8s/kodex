package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/schema"
)

func TestKeysetGenesisDigestSchemaMigrationIsInsideExpandFence(t *testing.T) {
	const migrationVersion int64 = 20260812000100
	if schema.CurrentVersion < migrationVersion {
		t.Fatalf("schema fence %d excludes keyset genesis digest migration %d", schema.CurrentVersion, migrationVersion)
	}

	raw, err := migrations.ReadFile("migrations/20260812000100_control_plane_keyset_genesis_digest_schema.sql")
	if err != nil {
		t.Fatalf("read keyset genesis digest migration: %v", err)
	}
	statement := string(raw)
	if !strings.Contains(statement, "control_plane_extensions.digest(") {
		t.Fatal("keyset genesis digest must use the private pgcrypto schema")
	}
	if strings.Contains(statement, "public.digest(") {
		t.Fatal("keyset genesis digest must not depend on the public schema")
	}
	if !strings.Contains(statement, "SET version = 20260812000100") {
		t.Fatal("latest control-plane migration must advance the runtime schema fence")
	}
}
