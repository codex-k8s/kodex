package app

import (
	"context"
	"errors"
	"strings"

	sessionrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/repository/postgres/session"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresConnectionConfig binds one capability pool to its DSN file, exact TLS server and session user, capability role, application name and maximum connections. It carries locators and expected identities, not parsed passwords or proof that a connection has been authenticated.
// postgresConnectionConfig связывает пул одной возможности с файлом DSN, точными TLS-сервером и пользователем сессии, ролью возможности, именем приложения и пределом соединений. Хранятся адреса и ожидаемые идентичности, а не разобранные пароли или доказательство аутентифицированного подключения.
type postgresConnectionConfig struct {
	DSNFile         string
	TLSServerName   string
	ExpectedUser    string
	Capability      string
	ApplicationName string
	MaxConnections  int32
}

// openCapabilityPostgres requires nonempty expected identity/capability and one through sixty-four connections, reads a bounded DSN and rejects fallbacks, a foreign host/user, absent password, missing CA/SNI or insecure TLS. It installs tracing and exact session setup on connect plus identity/capability checking before each acquire, then pings the pool and closes it on ping failure. Rejected configuration is sanitized; the returned pool belongs to the caller, and this function does not explicitly erase the parsed DSN buffer.
// openCapabilityPostgres требует непустые ожидаемые идентичность и возможность и от одного до шестидесяти четырёх соединений, читает ограниченный DSN и отклоняет запасные подключения, чужие host или user, отсутствие пароля, CA или SNI и небезопасный TLS. Устанавливает tracing, точную настройку сессии при подключении и проверку идентичности и возможности перед каждым acquire, затем проверяет пул ping и закрывает его при ошибке ping. Отказы конфигурации безопасны; возвращённый пул принадлежит вызывающему, явной очистки разобранного буфера DSN нет.
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
