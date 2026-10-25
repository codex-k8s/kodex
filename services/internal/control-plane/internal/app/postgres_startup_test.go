package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// startupPoolFixture exposes only counted Ping/Close, with no network or SQL execution capability.
// startupPoolFixture предоставляет только учитываемые Ping/Close без возможности сети или выполнения SQL.
type startupPoolFixture struct {
	ping          func(context.Context) error
	pings, closes int
}

// Ping counts the single requested probe before returning the synthetic typed cause.
// Ping учитывает единственную запрошенную проверку до возврата синтетической типизированной причины.
func (pool *startupPoolFixture) Ping(ctx context.Context) error { pool.pings++; return pool.ping(ctx) }

// Close records ownership cleanup without waiting for or contacting real connections.
// Close учитывает очистку владения без ожидания либо обращения к реальным соединениям.
func (pool *startupPoolFixture) Close() { pool.closes++ }

// TestPostgresStartupProbeLifecycle pins the ten-second cap, earlier deadline/cancellation, one probe
// and exactly one rejected-pool close; success passes ownership to Run without closing or canceling its context.
// TestPostgresStartupProbeLifecycle закрепляет предел десять секунд, меньший deadline/отмену, один ping
// и одно закрытие отвергнутого pool; успех передаёт владение Run, не закрывая pool и caller context.
func TestPostgresStartupProbeLifecycle(t *testing.T) {
	if postgresStartupPingBudget != 10*time.Second {
		t.Fatal("unexpected startup ping budget")
	}
	for _, phase := range []string{"success", "before", "during", "after", "failure"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if phase == "before" {
				cancel()
			}
			if phase == "during" {
				var expire context.CancelFunc
				ctx, expire = context.WithTimeout(ctx, 30*time.Millisecond)
				defer expire()
			}
			pool := &startupPoolFixture{ping: func(probe context.Context) error {
				deadline, ok := probe.Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("unbounded startup ping")
				}
				if caller, ok := ctx.Deadline(); ok && caller.Before(time.Now().Add(10*time.Second)) && deadline != caller {
					t.Fatal("caller budget extended")
				}
				switch phase {
				case "during":
					<-probe.Done()
					return probe.Err()
				case "after":
					cancel()
					return nil
				case "failure":
					return syscall.ECONNREFUSED
				}
				return nil
			}}
			err := verifyPostgresStartup(ctx, pool)
			if phase == "success" {
				if err != nil || pool.pings != 1 || pool.closes != 0 || ctx.Err() != nil {
					t.Fatal("successful pool ownership changed")
				}
				return
			}
			want, pings := error(context.Canceled), 1
			if phase == "during" {
				want = context.DeadlineExceeded
			}
			if phase == "failure" {
				want = syscall.ECONNREFUSED
			}
			if phase == "before" {
				pings = 0
			}
			if !errors.Is(err, want) || pool.pings != pings || pool.closes != 1 {
				t.Fatal("failed startup retried or lost cleanup/cause")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if pool, err := openPostgres(ctx, Config{}); pool != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled startup opened configuration or pool")
	}
}

// startupPostgresConfigFixture writes only synthetic DSN/public CA files and returns the expected trust pool.
// The generated signing key stays in memory and no socket, live credential or environment secret is used.
// startupPostgresConfigFixture записывает только синтетические DSN/public CA и возвращает ожидаемый trust pool.
// Созданный signing key остаётся в памяти; socket, живые реквизиты и секреты окружения не используются.
func startupPostgresConfigFixture(t *testing.T) (Config, *x509.CertPool) {
	t.Helper()
	t.Setenv("PGSERVICE", "")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic CA key")
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal("generate synthetic CA")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	directory := t.TempDir()
	config := Config{PostgresDSNFile: filepath.Join(directory, "dsn"), PostgresCAFile: filepath.Join(directory, "ca"), PostgresTLSServerName: "postgres.fixture.invalid", PostgresMaxConnections: 7}
	if os.WriteFile(config.PostgresDSNFile, []byte("postgres://fixture:synthetic@127.0.0.1:65432/fixture?sslmode=disable&connect_timeout=60"), 0600) != nil || os.WriteFile(config.PostgresCAFile, ca, 0600) != nil {
		t.Fatal("write synthetic PostgreSQL config")
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	return config, roots
}

// TestPostgresStartupConfigurationPreservesTrustAndWipe verifies the unchanged TLS13/SNI/root/pool pins,
// typed parse rejection and DSN-buffer clearing on success/failure without constructing a network pool.
// TestPostgresStartupConfigurationPreservesTrustAndWipe проверяет прежние TLS13/SNI/root/pool привязки,
// типизированный отказ разбора и очистку DSN-buffer при успехе/отказе без создания сетевого pool.
func TestPostgresStartupConfigurationPreservesTrustAndWipe(t *testing.T) {
	config, roots := startupPostgresConfigFixture(t)
	parsed, err := loadPostgresPoolConfig(config)
	if err != nil {
		t.Fatal("parse synthetic PostgreSQL config")
	}
	tlsConfig := parsed.ConnConfig.TLSConfig
	if tlsConfig == nil || tlsConfig.MinVersion != tls.VersionTLS13 || tlsConfig.InsecureSkipVerify || tlsConfig.ServerName != config.PostgresTLSServerName || !tlsConfig.RootCAs.Equal(roots) || parsed.MaxConns != 7 || parsed.ConnConfig.ConnectTimeout != 60*time.Second {
		t.Fatal("startup TLS or pool configuration changed")
	}
	for _, input := range []string{"postgres://fixture:synthetic@127.0.0.1:65432/fixture?sslmode=disable", "host='SENTINEL_PRIVATE_DSN"} {
		raw := []byte(input)
		_, err := parsePostgresStartupConfig(raw)
		if !bytes.Equal(raw, make([]byte, len(raw))) || (input[0] == 'h' && err == nil) {
			t.Fatal("raw DSN not wiped or malformed config accepted")
		}
	}
	for _, phase := range []string{"dsn-read", "dsn-parse", "ca-read", "ca-parse"} {
		t.Run(phase, func(t *testing.T) {
			config, _ := startupPostgresConfigFixture(t)
			switch phase {
			case "dsn-read":
				config.PostgresDSNFile += ".missing"
			case "dsn-parse":
				if os.WriteFile(config.PostgresDSNFile, []byte("host='SENTINEL_PRIVATE_DSN"), 0600) != nil {
					t.Fatal("write fixture")
				}
			case "ca-read":
				config.PostgresCAFile += ".missing"
			case "ca-parse":
				if os.WriteFile(config.PostgresCAFile, []byte("SENTINEL_PRIVATE_CA"), 0600) != nil {
					t.Fatal("write fixture")
				}
			}
			pool, err := loadPostgresPoolConfig(config)
			var failed *startupError
			if pool != nil || !errors.As(err, &failed) || normalizedStartupStage(failed.stage) == unknownStartupStage {
				t.Fatal("configuration failure escaped closed startup stage")
			}
		})
	}
}
