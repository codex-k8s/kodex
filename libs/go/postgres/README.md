# PostgreSQL helpers

Минимальная общая библиотека для Go-сервисов, которые работают с PostgreSQL через `pgxpool`.
Библиотека задаёт единый инфраструктурный контракт подключения, базового выполнения `Exec`
и проверки количества затронутых строк.

## Публичный контракт

- `PoolSettings` — типизированные параметры подключения, размера пула, `Ping` и повторных попыток подключения.
- `ParsePoolConfig(settings PoolSettings)` — разбирает DSN, применяет границы пула и возвращает `pgxpool.Config`.
- `OpenPool(ctx context.Context, settings PoolSettings)` — создаёт `pgxpool`, проверяет `Ping` и при старте использует ограниченные повторы с backoff и jitter.
- `Execer` — минимальный интерфейс для `*pgxpool.Pool`, `pgx.Tx` и тестовых дублей.
- `Exec(ctx, db, sql, args...)` — выполняет `Exec` и возвращает исходную ошибку `pgx`.
- `ExecRequireRow(ctx, db, sql, args...)` — выполняет `Exec` и возвращает `pgx.ErrNoRows`, если команда не затронула строки.

В библиотеку можно выносить только инфраструктурные примитивы без доменной логики.
Доменные ошибки, SQL-запросы и модели конкретного сервиса остаются внутри сервиса-владельца.

## Минимальный пример

```go
settings := postgres.PoolSettings{
    DSN:                      cfg.DatabaseDSN,
    MaxConns:                 cfg.DatabaseMaxConns,
    MinConns:                 cfg.DatabaseMinConns,
    MaxConnLifetime:          cfg.DatabaseMaxConnLifetime,
    MaxConnIdleTime:          cfg.DatabaseMaxConnIdleTime,
    HealthCheckPeriod:        cfg.DatabaseHealthCheckPeriod,
    PingTimeout:              cfg.DatabasePingTimeout,
    ConnectRetryMaxAttempts:  cfg.DatabaseRetryMaxAttempts,
    ConnectRetryInitialDelay: cfg.DatabaseRetryInitialDelay,
    ConnectRetryMaxDelay:     cfg.DatabaseRetryMaxDelay,
    ConnectRetryJitterRatio:  cfg.DatabaseRetryJitterRatio,
}

pool, err := postgres.OpenPool(ctx, settings)
if err != nil {
    return err
}
defer pool.Close()
```

Пример env-параметров сервиса:

- `KODEX_<SERVICE>_DATABASE_DSN`;
- `KODEX_<SERVICE>_DATABASE_MAX_CONNS`;
- `KODEX_<SERVICE>_DATABASE_MIN_CONNS`;
- `KODEX_<SERVICE>_DATABASE_MAX_CONN_LIFETIME`;
- `KODEX_<SERVICE>_DATABASE_MAX_CONN_IDLE_TIME`;
- `KODEX_<SERVICE>_DATABASE_HEALTH_CHECK_PERIOD`;
- `KODEX_<SERVICE>_DATABASE_PING_TIMEOUT`;
- `KODEX_<SERVICE>_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS`;
- `KODEX_<SERVICE>_DATABASE_CONNECT_RETRY_INITIAL_DELAY`;
- `KODEX_<SERVICE>_DATABASE_CONNECT_RETRY_MAX_DELAY`;
- `KODEX_<SERVICE>_DATABASE_CONNECT_RETRY_JITTER_RATIO`.

## Ошибки

- `ParsePoolConfig` возвращает ошибки валидации DSN и границ пула.
- `OpenPool` возвращает ошибку создания пула, `Ping` или отмены `context.Context`.
- `Exec` и `ExecRequireRow` не нормализуют ошибки `pgx`/`pgconn`: слой репозитория сервиса сам переводит их в доменные ошибки.
- `ExecRequireRow` дополнительно возвращает `pgx.ErrNoRows`, когда команда изменения не затронула ни одной строки.

## Совместимость API

- Публичный API меняется обратно совместимо: новые поля `PoolSettings` должны иметь безопасные значения по умолчанию.
- Несовместимое изменение допускается только с планом миграции потребителей и отдельным описанием в PR.
- Библиотека не должна скрыто менять доменное поведение сервисов: изменения повторов, границ пула и ошибок фиксируются в README и тестах.
