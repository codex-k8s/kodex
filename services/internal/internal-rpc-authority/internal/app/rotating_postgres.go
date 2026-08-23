package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	sessionrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type databaseCredentialCandidate struct {
	Role      string
	Principal string
	Directory string
}

type rotatingPostgresConfig struct {
	DSNTemplateFile string
	TLSServerName   string
	Capability      string
	ApplicationName string
	PodUID          string
	Candidates      []databaseCredentialCandidate
}

// openRotatingPostgres удерживает только server-derived CURRENT pool и
// публикует durable readback для CURRENT и NEXT из фактических sessions.
func openRotatingPostgres(
	ctx context.Context,
	config rotatingPostgresConfig,
) (*pgxpool.Pool, error) {
	if !runtimeUUIDPattern.MatchString(config.PodUID) ||
		len(config.Candidates) < 2 ||
		len(config.Candidates) > 3 {
		return nil, errors.New("rotating PostgreSQL credential configuration is invalid")
	}
	templateRaw, err := readPrivateFile(config.DSNTemplateFile, maxDSNFileBytes)
	if err != nil {
		return nil, errors.New("read PostgreSQL connection template")
	}
	var current *pgxpool.Pool
	seen := map[string]bool{"CURRENT": false, "NEXT": false}
	for _, candidate := range config.Candidates {
		pool, status, err := probeDatabaseCredential(
			ctx,
			strings.TrimSpace(string(templateRaw)),
			config,
			candidate,
		)
		if err != nil {
			continue
		}
		if seen[status] {
			pool.Close()
			if current != nil {
				current.Close()
			}
			return nil, errors.New("ambiguous PostgreSQL credential lifecycle status")
		}
		seen[status] = true
		if status == "CURRENT" {
			current = pool
		} else {
			pool.Close()
		}
	}
	if current == nil || !seen["CURRENT"] || !seen["NEXT"] {
		if current != nil {
			current.Close()
		}
		return nil, errors.New("PostgreSQL CURRENT/NEXT credential readback is incomplete")
	}
	return current, nil
}

func probeDatabaseCredential(
	ctx context.Context,
	dsnTemplate string,
	config rotatingPostgresConfig,
	candidate databaseCredentialCandidate,
) (*pgxpool.Pool, string, error) {
	usernameRaw, err := os.ReadFile(filepath.Join(candidate.Directory, "username"))
	if err != nil {
		return nil, "", errors.New("read PostgreSQL credential username")
	}
	passwordRaw, err := os.ReadFile(filepath.Join(candidate.Directory, "password"))
	if err != nil {
		return nil, "", errors.New("read PostgreSQL credential password")
	}
	username := strings.TrimSpace(string(usernameRaw))
	password := strings.TrimSpace(string(passwordRaw))
	if username != candidate.Principal || len(password) < 16 || len(password) > 4096 {
		return nil, "", errors.New("PostgreSQL credential file binding is invalid")
	}
	poolConfig, err := pgxpool.ParseConfig(dsnTemplate)
	if err != nil {
		return nil, "", errors.New("parse PostgreSQL connection template")
	}
	poolConfig.ConnConfig.User = username
	poolConfig.ConnConfig.Password = password
	instrumentPGX(poolConfig, config.ApplicationName)
	if len(poolConfig.ConnConfig.Fallbacks) != 0 ||
		poolConfig.ConnConfig.Host != config.TLSServerName ||
		poolConfig.ConnConfig.TLSConfig == nil ||
		poolConfig.ConnConfig.TLSConfig.RootCAs == nil ||
		poolConfig.ConnConfig.TLSConfig.ServerName != config.TLSServerName ||
		poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		return nil, "", errors.New("PostgreSQL TLS boundary rejected")
	}
	poolConfig.MaxConns = 8
	poolConfig.ConnConfig.RuntimeParams["application_name"] = config.ApplicationName
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		return sessionrepository.Configure(
			ctx,
			connection,
			candidate.Principal,
			config.Capability,
		)
	}
	poolConfig.BeforeAcquire = func(ctx context.Context, connection *pgx.Conn) bool {
		return sessionrepository.Ensure(
			ctx,
			connection,
			candidate.Principal,
			config.Capability,
		) == nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, "", errors.New("open rotating PostgreSQL pool")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Role      string `json:"role"`
		Principal string `json:"principal"`
		Password  string `json:"password"`
	}{
		Role: candidate.Role, Principal: candidate.Principal, Password: password,
	})
	if err != nil {
		pool.Close()
		return nil, "", errors.New("digest PostgreSQL credential")
	}
	var status string
	if err := pool.QueryRow(
		ctx,
		queryRecordDatabaseCredentialSessionReadback,
		digest,
		config.PodUID,
	).Scan(&status); err != nil {
		pool.Close()
		return nil, "", fmt.Errorf("record PostgreSQL credential session readback: %w", err)
	}
	if status != "CURRENT" && status != "NEXT" {
		pool.Close()
		return nil, "", errors.New("PostgreSQL credential lifecycle status rejected")
	}
	return pool, status, nil
}
