# PostgreSQL helpers

Минимальная общая библиотека для Go-сервисов, которые работают с PostgreSQL через `pgxpool`.
Библиотека задаёт единый инфраструктурный контракт подключения, выполнения коротких транзакций,
проверки количества затронутых строк, нормализации инфраструктурных ошибок и повторно используемых
аргументов для outbox/event-log сценариев.

## Публичный контракт

- `PoolSettings` — типизированные параметры подключения, размера пула, `Ping` и повторных попыток подключения.
- `ParsePoolConfig(settings PoolSettings)` — разбирает DSN, применяет границы пула и возвращает `pgxpool.Config`.
- `OpenPool(ctx context.Context, settings PoolSettings)` — создаёт `pgxpool`, проверяет `Ping` и при старте использует ограниченные повторы с backoff и jitter.
- `Execer`, `ExecQuerier`, `TxBeginner`, `RowScanner` — минимальные интерфейсы для `*pgxpool.Pool`, `pgx.Tx`, `pgx.Row`, `pgx.Rows` и тестовых дублей.
- `Exec(ctx, db, sql, args...)` — выполняет `Exec` и возвращает исходную ошибку `pgx`.
- `ExecRequireRow(ctx, db, sql, args...)` — выполняет `Exec` и возвращает `pgx.ErrNoRows`, если команда не затронула строки.
- `Mutation`, `RunMutation` — выполнение одного SQL-изменения с обязательной проверкой `RowsAffected`, когда операция должна изменить существующий агрегат.
- `RunDistinctMutations` — выполнение фиксированного набора разных SQL-изменений внутри короткой транзакции. Вспомогательная функция отклоняет повторяющийся SQL-текст и не предназначена для записи коллекций.
- `WithTx(ctx, db, fn)` — короткая транзакция с автоматическим rollback до успешного commit.
- `WrapError(operation, err, sentinels)` — переводит типовые ошибки PostgreSQL в доменные sentinel-ошибки сервиса и сохраняет исходную причину.
- `ScanRows` — thin wrapper над `pgx.CollectRows` для ручных scanner-ов с доменной конвертацией.
- `NullableUUID`, `NullableTime`, `NullableCommandID`, `IdempotencyLookupKey`, `StringValues`, `UUIDValues`, `JSONPayload`, `AddBaseArgs` — безопасные аргументы для SQL без копипасты по сервисам.
- `OutboxClaimArgs`, `OutboxPublishedArgs`, `OutboxDeliveryFailureArgs`, `ExecOutboxPublished`, `ExecOutboxDeliveryFailure`, `ScanOutboxEventRow` — общий каркас для сервисных outbox-таблиц.

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
- `WrapError` нормализует только инфраструктурные классы ошибок: `unique_violation`, `foreign_key_violation`, `check_violation`, `serialization_failure`, `deadlock_detected`, `pgx.ErrNoRows`. Бизнесовые ошибки остаются в сервисе-владельце.
- `RunMutation` и `RunDistinctMutations` не решают бизнес-конфликты сами. Они только превращают `RowsAffected() == 0` у помеченной mutation в переданный sentinel conflict; SQL обязан содержать проверку версии или другой инвариант.
- `RunDistinctMutations` нельзя использовать для построчной записи коллекций. Если нужно вставить или обновить несколько однотипных строк, использовать `pgx.Batch`, `COPY` или один пакетный SQL-запрос с `unnest`/`jsonb_to_recordset`.

## Пример короткой транзакции с optimistic concurrency

```go
err := postgres.WithTx(ctx, pool, func(tx pgx.Tx) error {
    return postgres.RunDistinctMutations(
        ctx,
        tx,
        errs.ErrConflict,
        postgres.Mutation{Query: updateSQL, Args: args, RequireAffected: true},
        postgres.Mutation{Query: outboxSQL, Args: outboxArgs, RequireAffected: true},
    )
})
if err != nil {
    return postgres.WrapError("domain.Repository.UpdateProject", err, postgres.ErrorSentinels{
        AlreadyExists:      errs.ErrAlreadyExists,
        Conflict:           errs.ErrConflict,
        InvalidArgument:    errs.ErrInvalidArgument,
        NotFound:           errs.ErrNotFound,
        PreconditionFailed: errs.ErrPreconditionFailed,
    })
}
```

SQL для `RequireAffected` должен быть устроен так, чтобы устаревшая версия или несуществующий агрегат не меняли строки:

```sql
UPDATE project_catalog_projects
SET display_name = @display_name, version = @version, updated_at = @updated_at
WHERE id = @id AND version = @previous_version;
```

## Совместимость API

- Публичный API меняется обратно совместимо: новые поля `PoolSettings` должны иметь безопасные значения по умолчанию.
- Несовместимое изменение допускается только с планом миграции потребителей и отдельным описанием в PR.
- Библиотека не должна скрыто менять доменное поведение сервисов: изменения повторов, границ пула и ошибок фиксируются в README и тестах.
