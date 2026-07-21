package testsupport

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const controllerPostgresServerPort = 5432

// PrepareControllerOwnedPostgres создаёт one-shot proof только на свежем
// loopback-туннеле к ресурсу, точную identity и удаление которого контролирует
// вызывающая оснастка Docker или Kubernetes.
func PrepareControllerOwnedPostgres(ctx context.Context, bootstrapDSN string, expectedMajor string) (string, error) {
	if expectedMajor != "15" && expectedMajor != "16" {
		return "", fmt.Errorf("неподдерживаемая ожидаемая PostgreSQL major-версия")
	}
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return "", err
	}
	if !localEndpoint(config.ConnConfig.Host) || strings.TrimSpace(config.ConnConfig.Database) != "postgres" {
		return "", fmt.Errorf("controller-owned PostgreSQL доступен только через loopback к maintenance database")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return "", fmt.Errorf("controller-owned PostgreSQL endpoint недоступен")
	}
	defer pool.Close()
	connection, identity, err := acquireControllerPostgres(ctx, pool, config)
	if err != nil {
		return "", fmt.Errorf("controller-owned PostgreSQL endpoint недоступен")
	}
	defer connection.Release()
	var versionNumber int
	var extensionCount int
	if err := connection.QueryRow(ctx, `select current_setting('server_version_num')::integer`).Scan(&versionNumber); err != nil || strconv.Itoa(versionNumber/10000) != expectedMajor {
		return "", fmt.Errorf("controller-owned PostgreSQL имеет несовпадающую major-версию")
	}
	if err := connection.QueryRow(ctx, `select count(*) from pg_available_extensions where name in ('vector', 'amcheck')`).Scan(&extensionCount); err != nil || extensionCount != 2 {
		return "", fmt.Errorf("controller-owned PostgreSQL не предоставляет обязательные extensions")
	}
	if _, err := connection.Exec(ctx, bootstrapProofRegistryStatement()); err != nil {
		return "", fmt.Errorf("controller-owned PostgreSQL не подготовил fresh proof registry")
	}
	if err := validateBootstrapProofRegistry(ctx, connection); err != nil {
		return "", fmt.Errorf("controller-owned PostgreSQL не подтвердил exact proof registry")
	}
	return issueBootstrapProof(ctx, connection, identity)
}

func acquireControllerPostgres(
	ctx context.Context,
	pool *pgxpool.Pool,
	config *pgxpool.Config,
) (*pgxpool.Conn, postgresServerIdentity, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		connection, err := pool.Acquire(ctx)
		if err == nil {
			identity, identityErr := readControllerPostgresServerIdentity(ctx, connection, config)
			if identityErr == nil && identity.currentDatabase == "postgres" {
				return connection, identity, nil
			}
			connection.Release()
			err = identityErr
			if err == nil {
				err = fmt.Errorf("controller-owned PostgreSQL maintenance database identity mismatch")
			}
		}
		if time.Now().After(deadline) {
			return nil, postgresServerIdentity{}, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, postgresServerIdentity{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func readControllerPostgresServerIdentity(
	ctx context.Context,
	querier rowQuerier,
	config *pgxpool.Config,
) (postgresServerIdentity, error) {
	var identity postgresServerIdentity
	if err := querier.QueryRow(ctx, `
select current_database(), coalesce(host(inet_server_addr()), ''), coalesce(inet_server_port(), 0),
	current_setting('data_directory'), system_identifier::text, current_user,
	(select oid::bigint from pg_roles where rolname = current_user)
from pg_control_system()
`).Scan(
		&identity.currentDatabase, &identity.serverAddress, &identity.serverPort,
		&identity.dataDirectory, &identity.systemIdentifier, &identity.currentUserName, &identity.currentUserOID,
	); err != nil {
		return postgresServerIdentity{}, fmt.Errorf("controller-owned PostgreSQL identity query failed")
	}
	serverAddress := net.ParseIP(strings.TrimSpace(identity.serverAddress))
	if identity.currentDatabase != strings.TrimSpace(config.ConnConfig.Database) ||
		serverAddress == nil || (!serverAddress.IsLoopback() && !serverAddress.IsPrivate()) ||
		identity.serverPort != controllerPostgresServerPort || strings.TrimSpace(identity.systemIdentifier) == "" ||
		strings.TrimSpace(identity.dataDirectory) == "" || identity.currentUserOID <= 0 {
		return postgresServerIdentity{}, fmt.Errorf("controller-owned PostgreSQL identity mismatch")
	}
	identity.endpointFingerprint = sha256Text(strings.Join([]string{
		strings.TrimSpace(config.ConnConfig.Host), strconv.Itoa(int(config.ConnConfig.Port)),
		strings.TrimSpace(identity.serverAddress), strconv.Itoa(identity.serverPort),
	}, "\x00"))
	identity.serverFingerprint = sha256Text(strings.Join([]string{
		strings.TrimSpace(identity.systemIdentifier), strings.TrimSpace(identity.dataDirectory),
	}, "\x00"))
	return identity, nil
}
