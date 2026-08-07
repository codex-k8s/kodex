package postgres

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

func TestSecurePoolConfig(t *testing.T) {
	t.Parallel()
	caFile := writeTestCA(t)
	valid := ConnectionConfig{
		DSN:           "postgres://migration:secret@source.database.svc:5432/mattercodex?sslmode=verify-full",
		TLSServerName: "source.database.svc", CAFile: caFile,
	}
	config, err := securePoolConfig(valid)
	if err != nil {
		t.Fatalf("securePoolConfig() error = %v", err)
	}
	if config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.ServerName != valid.TLSServerName ||
		config.ConnConfig.TLSConfig.MinVersion != tls.VersionTLS13 ||
		config.ConnConfig.TLSConfig.MaxVersion != tls.VersionTLS13 ||
		len(config.ConnConfig.Fallbacks) != 0 {
		t.Fatal("securePoolConfig() did not pin the exact TLS contract")
	}

	tests := []struct {
		name string
		edit func(*ConnectionConfig)
	}{
		{name: "plaintext", edit: func(value *ConnectionConfig) {
			value.DSN = "postgres://migration:secret@source.database.svc:5432/mattercodex?sslmode=disable"
		}},
		{name: "non verifying TLS", edit: func(value *ConnectionConfig) {
			value.DSN = "postgres://migration:secret@source.database.svc:5432/mattercodex?sslmode=require"
		}},
		{name: "wrong SNI", edit: func(value *ConnectionConfig) { value.TLSServerName = "other.database.svc" }},
		{name: "IP endpoint", edit: func(value *ConnectionConfig) {
			value.DSN = "postgres://migration:secret@127.0.0.1:5432/mattercodex?sslmode=verify-full"
			value.TLSServerName = "127.0.0.1"
		}},
		{name: "different DSN CA", edit: func(value *ConnectionConfig) {
			value.DSN += "&sslrootcert=/different/ca.crt"
		}},
		{name: "duplicate sslmode", edit: func(value *ConnectionConfig) { value.DSN += "&sslmode=disable" }},
		{name: "routing override", edit: func(value *ConnectionConfig) { value.DSN += "&host=other.database.svc" }},
		{name: "relative CA", edit: func(value *ConnectionConfig) { value.CAFile = "ca.crt" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			if _, err := securePoolConfig(candidate); err == nil {
				t.Fatal("securePoolConfig() accepted an unsafe connection contract")
			}
		})
	}
}

func TestNamedSQLContracts(t *testing.T) {
	t.Parallel()
	if err := validateQueries(); err != nil {
		t.Fatalf("validateQueries() error = %v", err)
	}
}

func TestPrincipalReadbackContainsExactSourceInventory(t *testing.T) {
	t.Parallel()
	query := mustQuery("principal__readback.sql")
	for _, table := range inventory.Tables {
		if !strings.Contains(query, "('public', '"+table+"')") {
			t.Fatalf("principal readback misses exact source table %q", table)
		}
	}
}

func TestTargetCutoverUsesClosedOwnerCapabilities(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"target_cutover__prepare.sql", "target_cutover__verify_restore.sql", "target_cutover__abort.sql"} {
		query := mustQuery(name)
		if strings.Contains(query, "UPDATE control_plane.legacy_data_cutovers") ||
			strings.Contains(query, "INSERT INTO control_plane.legacy_data_cutovers") {
			t.Fatalf("%s содержит прямой receipt DML", name)
		}
	}
	path := filepath.Join("..", "..", "..", "..", "..", "internal", "control-plane", "cmd", "cli",
		"migrations", "20260807019601_legacy_data_cutover_hardening.sql")
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("прочитать hardening migration: %v", err)
	}
	text := string(migration)
	for _, operation := range []string{"UPSERT_PROJECT", "UPSERT_TEAM", "UPSERT_CHAT",
		"UPSERT_PROTECTED_CONFIGURATION", "UPSERT_SESSION", "UPSERT_TURN", "UPSERT_TURN_ATTEMPT",
		"UPSERT_PROCESS_RUN", "UPSERT_SCHEDULE"} {
		if !strings.Contains(text, "'"+operation+"'") {
			t.Fatalf("hardening migration не содержит operation %s", operation)
		}
	}
	if !strings.Contains(text, "REVOKE INSERT, UPDATE, DELETE, TRUNCATE") ||
		!strings.Contains(text, "legacy_data_cutovers_immutable_transition") ||
		!strings.Contains(text, "legacy_data_cutover_provenance") {
		t.Fatal("hardening migration не закрывает receipt/provenance boundary")
	}
}

func TestSafeSourceProjectionDropsPrivatePayload(t *testing.T) {
	t.Parallel()
	projected, retained, err := safeSourceProjection("matter_codex_agent_session_turns", []byte(
		`{"id":5,"session_id":4,"run_id":"run","status":"succeeded","binding_version":3,`+
			`"message":"private prompt","final_message":"private result","artifacts":{"a":{},"b":{}}}`,
	))
	if err != nil || !retained {
		t.Fatalf("safeSourceProjection() retained=%t error=%v", retained, err)
	}
	if bytes.Contains(projected, []byte("private")) || !bytes.Contains(projected, []byte(`"artifacts":2`)) {
		t.Fatalf("unsafe source projection: %s", projected)
	}
	if projected, retained, err = safeSourceProjection("matter_codex_memory_record_versions",
		[]byte(`{"id":1,"record_id":2,"version":1,"content_hash":"hash","content":"private memory"}`)); err != nil || !retained || bytes.Contains(projected, []byte("private")) ||
		!bytes.Contains(projected, []byte(`"record_id":2`)) {
		t.Fatalf("unsafe memory lineage projection: value=%s retained=%t error=%v", projected, retained, err)
	}
	if projected, retained, err = safeSourceProjection("matter_codex_agent_roles",
		[]byte(`{"id":1,"project_id":2,"name":"worker","prompt_template":"private instruction","enabled":true}`)); err != nil || !retained || bytes.Contains(projected, []byte("private")) ||
		!bytes.Contains(projected, []byte(`"prompt_sha256"`)) {
		t.Fatalf("unsafe role instruction projection: value=%s retained=%t error=%v", projected, retained, err)
	}
}

func TestArtifactProjectionSHARequiresExactAdmittedArtifact(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	digest := string(bytes.Repeat([]byte{'a'}, 64))
	resource := model.TargetResource{ID: "artifact-id", OrganizationID: "organization-id",
		ProjectID: "project-id", ParentID: "turn-id", OwnerActorID: "owner-id", Kind: "ARTIFACT",
		Name: "artifact", State: "ACTIVE", Version: 1, CreatedAt: now, UpdatedAt: now,
		Spec: map[string]any{"kind": "turn-input", "direction": "INPUT",
			"storageRef": "s3://bucket/key?versionId=immutable-v1", "sizeBytes": 1,
			"mediaType": "text/markdown", "sha256": digest, "scanStatus": "CLEAN",
			"retentionPolicyRef": "control-plane://retention/default", "scanPolicyRevision": 1,
			"scanEvidenceSha256": digest, "scannerWorkloadId": "artifact-scanner", "scannedAt": now}}
	if projection, err := artifactProjectionSHA(resource); err != nil || !validSHA256(projection) {
		t.Fatalf("artifactProjectionSHA() projection=%q error=%v", projection, err)
	}
	resource.Spec["unexpected"] = true
	if _, err := artifactProjectionSHA(resource); err == nil {
		t.Fatal("artifactProjectionSHA() accepted an unknown spec field")
	}
}

func writeTestCA(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	content := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
