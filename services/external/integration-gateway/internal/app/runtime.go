package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func loadPublicTLSConfig(config Config) (*tls.Config, error) {
	return loadServerTLSConfig(config, config.TLSAllowedClientSPIFFEIDs)
}

func loadResultTLSConfig(config Config) (*tls.Config, error) {
	return loadServerTLSConfig(config,
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner")
}

func loadServerTLSConfig(config Config, allowedIdentities string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.TLSCertificateFile, config.TLSPrivateKeyFile)
	if err != nil {
		return nil, errors.New("load HTTPS server certificate")
	}
	caRaw, err := readRuntimeFile(config.TLSClientCAFile, 1<<20)
	if err != nil {
		return nil, errors.New("read HTTPS client CA")
	}
	clientRoots := x509.NewCertPool()
	if !clientRoots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse HTTPS client CA")
	}
	allowed := make(map[string]struct{})
	for _, raw := range strings.Split(allowedIdentities, ",") {
		identity := strings.TrimSpace(raw)
		parsed, parseErr := url.Parse(identity)
		if parseErr != nil || parsed.Scheme != "spiffe" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, errors.New("HTTPS client SPIFFE allowlist is invalid")
		}
		allowed[identity] = struct{}{}
	}
	if len(allowed) == 0 || len(allowed) > 8 {
		return nil, errors.New("HTTPS client SPIFFE allowlist is invalid")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientRoots,
		VerifyConnection: func(connection tls.ConnectionState) error {
			if len(connection.VerifiedChains) != 1 || len(connection.PeerCertificates) == 0 ||
				len(connection.PeerCertificates[0].URIs) != 1 {
				return errors.New("HTTPS client workload identity is invalid")
			}
			identity := connection.PeerCertificates[0].URIs[0].String()
			if _, ok := allowed[identity]; !ok {
				return fmt.Errorf("HTTPS client workload identity is not allowed")
			}
			return nil
		},
	}, nil
}

func readRuntimeFile(path string, maximum int64) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("runtime file path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("runtime file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime file")
	}
	return raw, nil
}

func openPostgres(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	rawDSN, err := readRuntimeFile(config.PostgresDSNFile, 64<<10)
	if err != nil {
		return nil, errors.New("read PostgreSQL runtime DSN")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(rawDSN)))
	if err != nil || poolConfig.ConnConfig.Host != config.PostgresTLSServerName || len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.TLSConfig == nil || poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("PostgreSQL runtime DSN must use exact verify-full TLS")
	}
	caRaw, err := readRuntimeFile(config.PostgresCAFile, 1<<20)
	if err != nil {
		return nil, errors.New("read PostgreSQL CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse PostgreSQL CA")
	}
	poolConfig.ConnConfig.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.PostgresTLSServerName, RootCAs: roots}
	poolConfig.MaxConns = config.PostgresMaxConnections
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open PostgreSQL runtime pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("check PostgreSQL runtime connection")
	}
	return pool, nil
}
