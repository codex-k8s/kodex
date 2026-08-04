package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maximumSecretBytes = 64 << 10
	maximumCABytes     = 1 << 20
)

func openPostgres(ctx context.Context, config GatewayConfig) (*pgxpool.Pool, error) {
	raw, err := readRuntimeFile(config.PostgresDSNFile, maximumSecretBytes)
	if err != nil {
		return nil, errors.New("read PostgreSQL DSN")
	}
	parsed, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil || len(parsed.ConnConfig.Fallbacks) != 0 || parsed.ConnConfig.Host != config.PostgresTLSServerName ||
		parsed.ConnConfig.TLSConfig == nil || parsed.ConnConfig.TLSConfig.ServerName != config.PostgresTLSServerName ||
		parsed.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("postgresql DSN must use exact verify-full TLS")
	}
	roots, err := loadCertPool(config.PostgresCAFile)
	if err != nil {
		return nil, errors.New("load PostgreSQL CA")
	}
	parsed.ConnConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.PostgresTLSServerName, RootCAs: roots}
	parsed.MaxConns, parsed.MinConns = config.PostgresMaxConnections, 1
	parsed.MaxConnLifetime, parsed.MaxConnIdleTime, parsed.HealthCheckPeriod = 30*time.Minute, 5*time.Minute, 30*time.Second
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		return nil, errors.New("open PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("check PostgreSQL pool")
	}
	var sessionUser string
	var runtimeMember bool
	if err := pool.QueryRow(ctx,
		"SELECT session_user::text, pg_has_role(session_user, 'interaction_gateway_runtime', 'member')",
	).Scan(&sessionUser, &runtimeMember); err != nil || sessionUser != config.PostgresExpectedUser || !runtimeMember {
		pool.Close()
		return nil, errors.New("postgresql runtime identity readback failed")
	}
	return pool, nil
}

func publicTLS(config GatewayConfig) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.TLSCertificateFile, config.TLSPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load interaction gateway server identity")
	}
	roots, err := loadCertPool(config.TLSClientCAFile)
	if err != nil {
		return nil, errors.New("load interaction gateway client CA")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate},
		ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: roots,
	}, nil
}

func loadCertPool(path string) (*x509.CertPool, error) {
	raw, err := readRuntimeFile(path, maximumCABytes)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("certificate bundle is invalid")
	}
	return pool, nil
}

func readRuntimeFile(path string, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("runtime file path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("runtime file is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open runtime file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime file")
	}
	return raw, nil
}

func boundedHTTP(next http.Handler, maximum int, timeout time.Duration) http.Handler {
	semaphore := make(chan struct{}, maximum)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		case <-request.Context().Done():
			return
		default:
			http.Error(response, "request concurrency limit exceeded", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), timeout)
		defer cancel()
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}
