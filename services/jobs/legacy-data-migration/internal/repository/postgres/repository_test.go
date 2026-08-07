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
	"testing"
	"time"
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
