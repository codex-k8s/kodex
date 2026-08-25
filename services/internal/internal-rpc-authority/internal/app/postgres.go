package app

import (
	"context"
	"errors"
	"strings"

	sessionrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresConnectionConfig struct {
	DSNFile         string
	TLSServerName   string
	ExpectedUser    string
	Capability      string
	ApplicationName string
	MaxConnections  int32
}

// openCapabilityPostgres открывает статическую installation-scoped identity и
// после каждого acquire подтверждает неизменяемый session_user.
func openCapabilityPostgres(
	ctx context.Context,
	config postgresConnectionConfig,
) (*pgxpool.Pool, error) {
	if config.ExpectedUser == "" || config.Capability == "" ||
		config.MaxConnections < 1 || config.MaxConnections > 64 {
		return nil, errors.New("PostgreSQL capability configuration is invalid")
	}
	raw, err := readPrivateFile(config.DSNFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read PostgreSQL DSN file")
	}
	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("parse PostgreSQL DSN")
	}
	instrumentPGX(poolConfig, config.ApplicationName)
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.TLSServerName ||
		poolConfig.ConnConfig.User != config.ExpectedUser ||
		poolConfig.ConnConfig.Password == "" ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.TLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("PostgreSQL TLS or identity boundary rejected")
	}
	poolConfig.MaxConns = config.MaxConnections
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.ApplicationName
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(ctx, connection, config.ExpectedUser, config.Capability)
	}
	poolConfig.BeforeAcquire = func(ctx context.Context, connection *pgx.Conn) bool {
		return sessionrepository.Ensure(ctx, connection, config.ExpectedUser, config.Capability) == nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open PostgreSQL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("verify PostgreSQL connectivity")
	}
	return pool, nil
}
