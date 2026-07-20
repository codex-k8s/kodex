package testsupport

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("controller-owned PostgreSQL endpoint недоступен")
	}
	defer connection.Release()
	identity, err := readPostgresServerIdentity(ctx, connection, config)
	if err != nil || identity.currentDatabase != "postgres" {
		return "", fmt.Errorf("controller-owned PostgreSQL server identity не подтверждена")
	}
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
