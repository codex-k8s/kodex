---
id: GO-DOC-002
title: PostgreSQL, именованные SQL-запросы и goose
type: guide
status: approved
owner: developer
version: 1.2.1
updated: 2026-08-24
---

# PostgreSQL, именованные SQL-запросы и goose

`GO-DOC-002` задает единый способ доступа Go-сервисов к PostgreSQL и изменения
схемы.

## Библиотеки и границы

- PostgreSQL adapter использует `pgx/v5` и `pgxpool`, если отдельное
  архитектурное решение не требует другой библиотеки.
- Миграции выполняются `pressly/goose/v3`.
- Домен зависит только от repository port и не импортирует PostgreSQL packages.
- Каждый сервис владеет своими таблицами, миграциями и schema history.
- Прямое чтение таблиц другого сервиса запрещено.

Актуальные API библиотек проверяются через Context7 перед реализацией или
обновлением версий.

## Структура PostgreSQL adapter

```text
internal/repository/postgres/<capability>/
├── sql/
│   ├── aggregate__create.sql               # -- name: aggregate__create :exec
│   ├── aggregate__get_by_id.sql            # -- name: aggregate__get_by_id :one
│   └── aggregate__list.sql                 # -- name: aggregate__list :many
├── args.go                                 # domain -> pgx.StrictNamedArgs
├── errors.go                               # SQLSTATE/driver error -> domain error
├── queries.go                              # go:embed и загрузка SQL по имени
├── repository.go                           # реализация domain repository port
└── scan.go                                 # pgx row -> domain types
```

`repository.go` координирует запросы и транзакции, но не содержит SQL literals.
`args.go` и `scan.go` не реализуют бизнес-правила: они преобразуют
представления и вызывают доменные constructors.

## Именованные SQL-файлы

Один production query хранится в одном `.sql` файле. Первая содержательная
строка имеет точный вид:

```sql
-- name: membership__list_for_scope :many
SELECT id, actor_id, tenant_id, role, active
FROM resource_memberships
WHERE actor_id = @actor_id
  AND tenant_id = @tenant_id
ORDER BY id;
```

Допустимые operation suffix:

- `:one` - ожидается ровно одна строка;
- `:many` - ожидается список строк;
- `:exec` - результат строк не читается.

Правила имен:

- `<aggregate>__<operation>.sql`;
- имя после `-- name:` совпадает с именем файла без расширения;
- имя уникально в пределах adapter;
- аргументы именованные: `@argument`;
- порядок результата явный, `SELECT *` запрещен;
- порядок списка задается `ORDER BY`, если API обещает стабильный порядок.

`queries.go` явно встраивает каждый production SQL отдельной директивой
`//go:embed sql/<query>.sql` и связывает его с конкретным полем `querySet`.
Загрузчик закрыто падает при пустом теле, несовпадении имени или cardinality.
Загрузчик каталога отклоняет отсутствующий и неизвестный SQL-файл.
Runtime-обход каталога и map по именам запросов не используются.
SQL не собирается конкатенацией пользовательских значений.

## Аргументы и сканирование

- Для аргументов используется `pgx.StrictNamedArgs`, чтобы отсутствующее или
  лишнее имя закрыто отклонялось. Обычный `pgx.NamedArgs` не используется для
  production query: он допускает отсутствующие и игнорирует лишние аргументы.
- `scan.go` проверяет nullable/required поля и вызывает constructors value
  objects/entities.
- Поврежденное состояние БД не пропускается в домен как частично валидная
  сущность.
- Ошибка `no rows`, conflict, constraint violation и unavailable отображается в
  устойчивую domain error.
- Сырой SQL, аргументы с PII/secrets и полный driver error не возвращаются в
  transport.

## Транзакции

- Транзакционная граница соответствует одному доменному намерению.
- Уровень изоляции выбирается явно для сценариев, где от него зависит
  корректность.
- Все ошибки `Begin`, query/exec, rows iteration, `Commit` и `Rollback`
  обрабатываются.
- Частичный результат при ошибке не возвращается как успешный.
- Retry serialization/deadlock выполняется ограниченно и только для доказанно
  идемпотентной операции.

Составной авторитетный snapshot не читается несколькими независимыми
statement через pool. Если aggregate собирается из основной строки и дочерних
коллекций, все части читаются в одной read-only `REPEATABLE READ` transaction
либо одним SQL path с эквивалентной snapshot-семантикой. Иначе конкурентный
commit может смешать version `N` основной записи с version `N+1` дочерней и
превратить корректное состояние в ложную ошибку целостности.

Для `Get`, `BatchGet` и `List` действует одно правило snapshot consistency.
Открытые `Rows` закрываются и проверяются до следующего statement. Commit,
rollback и закрытие получают контекст вызывающего lifecycle; обязательный
rollback после отмены request выполняется через отдельный bounded shutdown
контекст, переданный composition root, без скрытого `context.Background()`.

## Роли подключения и достижимость

- Runtime, migrator, bootstrap/reconciler и read-only diagnostics используют
  разные PostgreSQL roles с минимальными permissions.
- Если bootstrap отзывает `CONNECT` у `PUBLIC`, migration до первого runtime
  запуска явно выдаёт `CONNECT` каждому утверждённому точному `LOGIN`
  principal, включая `CURRENT`, `NEXT`, reconciler и restore controller.
  Членство `NOINHERIT` в capability-role не доказывает возможность открыть
  соединение и не заменяет этот grant.
- Runtime и migration manifests указывают на один фактически существующий
  environment-owned `Service` или exact egress gateway, а не на
  предполагаемый pod selector.
- DSN не определяет сетевую authority самостоятельно: destination дополнительно
  ограничивается `NetworkPolicy`, TLS server name и доверенной CA.
- Ротация credentials сохраняет ограниченный overlap, проверяет новое
  подключение до отзыва прежнего и не выводит DSN или password.
- Ошибки parse, connect и `Ping` отображаются в bounded diagnostics без host,
  user, database и query parameters.

## RLS и идентичность сессии

Область tenant/actor в RLS связывается с неизменяемым `session_user`, точным
LOGIN principal, устойчивыми поколением/состоянием credential и текущей
транзакцией. GUC, установленная вызывающей стороной, actor/organization из
аргумента SQL или членство в общей роли не являются идентичностью. Если
приложение активирует контекст транзакции, его подписывает принадлежащая серверу
граница, а каждая policy/привилегированная функция повторно проверяет principal
и поколение до доступа.

- Runtime получает только минимальные привилегии table/function и `FORCE RLS`.
- Прямой grant к таблице не обходит ограждение owner/tenant либо
  `SECURITY DEFINER` API.
- Открытая session со статусом `RETIRED` перестаёт проходить ограждение каждого
  statement даже до завершения backend.
- Readiness подключается точным runtime principal и проверяет фактические
  привилегии, RLS и отрицательный путь cross-tenant/direct-DML.
- Поиск owner выполняется внутри доверенной tenant boundary до OCC и
  idempotency; неизвестный либо чужой ресурс скрыто отклоняется.

Жизненный цикл credential имеет устойчивую монотонную верхнюю отметку и
закрытые `CURRENT|NEXT|PREVIOUS|RETIRED`. Любое впервые встреченное generation
должно
быть выше watermark; меньшее поколение допустимо только как уже сохранённый
predecessor. `RETIRED` не воскресает после отката ConfigMap/Vault intent.
Promotion разрешён только для сохранённого `NEXT` после независимого readback
точного LOGIN. Retirement согласованно выполняет `NOLOGIN`, отзыв членства,
ограниченные termination/readback и устойчивый статус.

Управление LOGIN принадлежит отдельной минимальной controller role с
`NOLOGIN`. Миграционный/bootstrap path до записи intent создаёт или принимает
точный LOGIN, выдаёт controller только необходимые `ADMIN OPTION`, управление
ролью и фактические возможности `pg_signal_backend`, затем проверяет их
readback. `NOINHERIT`, `SET ROLE`, владение функцией и поддерживаемые версии
PostgreSQL проверяются как фактическая семантика; runtime не получает
`CREATEROLE`.

## Extensions и `SECURITY DEFINER`

Каждое используемое extension устанавливается code-first до первого вызова в
точную защищённую schema. Для `pgcrypto` вызовы `digest`/`hmac` всегда
квалифицируются схемой; добавлять доступную для записи `public` в `search_path`
запрещено. Миграция отзывает лишний `PUBLIC`, выдаёт только требуемые
`USAGE/EXECUTE` и проверяет фактические schema/owner/version.

Функция `SECURITY DEFINER`:

- имеет точного owner, который реально способен выполнить все statements;
- использует фиксированный безопасный `search_path` только из `pg_catalog` и
  schema, а внешние функции всё равно schema-qualified;
- принимает ограниченные типизированные входы и повторно проверяет
  tenant/principal;
- недоступна `PUBLIC` и вызывается только явно разрешёнными roles;
- не строит динамический SQL из данных вызывающей стороны;
- закрыто откатывает business state, receipt/audit и lifecycle при
  невозможности revoke/terminate/readback;
- имеет статическую проверку и readback фактических привилегий, а не только
  декларативных атрибутов роли.

## Миграции goose

Миграции находятся в `cmd/cli/migrations` и встраиваются в CLI через
`embed.FS`.

Имя файла:

```text
YYYYMMDDHHMMSS_<service>_<change>.sql
```

Минимальный файл:

```sql
-- +goose Up
CREATE TABLE domain_entities (
    id text PRIMARY KEY,
    created_at timestamptz NOT NULL
);
```

Для PostgreSQL function/trigger или другого блока с внутренними `;`
используются:

```sql
-- +goose StatementBegin
...
-- +goose StatementEnd
```

Политика проекта forward-only:

- примененная миграция не редактируется, не переименовывается и не меняет
  порядок;
- production CLI не предоставляет штатную команду `down`;
- rollback схемы выполняется новой компенсирующей forward migration;
- миграции не запускаются неявно при старте application server;
- deploy явно выполняет CLI-команду `up` отдельным Kubernetes Job до rollout.

Для owner-approved web-first reset установка создаётся с нуля. Активный
control-plane содержит одну baseline migration, которая создаёт только новую
схему. В этом reset запрещены legacy backfill, aliases, compatibility tables,
dual-read и dual-write. Повторный `up` на уже применённой baseline является
идемпотентным readback, а не новой миграцией данных.

После первого развертывания baseline любое изменение заполненной схемы требует
отдельного owner-approved lifecycle. Совместимые добавления, bounded backfill,
online indexes и destructive contract описываются точными последовательными
forward migrations и проверяются на disposable копии поддерживаемой схемы.
Наличие такой будущей последовательности не разрешает возвращать в текущий
reset compatibility facade или параллельный старый/новый путь.

`-- +goose NO TRANSACTION` ставится до `-- +goose Up` и применяется только к
файлу, где PostgreSQL запрещает transaction wrapper или где backfill сам
фиксирует ограниченные порции. Смешивать `CREATE INDEX CONCURRENTLY` с
транзакционным `ALTER TABLE` в одном файле запрещено. Политика разрешения
дубликатов задается явно; молчаливый выбор строки без детерминированного
порядка запрещен.

Пустой или неполный `Down` не считается rollback: удаление версии из goose
history без обратного изменения схемы создает ложное состояние. Поэтому
операционные инструкции не используют `goose down`.

## CLI миграций

`cmd/cli` предоставляет как минимум:

- `up` — применить все миграции deployable в forward-only порядке;
- `status` — показать безопасный статус без DSN.

Дополнительные команды, например bootstrap broker state, не маскируются под
миграции PostgreSQL и документируются контрактом соответствующего deployable.

CLI:

- устанавливает dialect `postgres`;
- использует migrations только своего сервиса;
- завершает процесс ненулевым кодом при partial/failed migration;
- не печатает DSN, password или значения секретов;
- пригоден для повторяемого Kubernetes Job.

## Проверки

Профиль определяется `GOV-DOC-003`. Для fresh baseline обязательны как минимум
синтаксическая проверка, запуск на пустой disposable PostgreSQL, повторный `up`,
bootstrap readback и отсутствие legacy объектов. Upgrade-path становится
обязательным только после появления второй поддерживаемой версии схемы.

## Проверенная документация

При подготовке документа через Context7 проверены:

- `/pressly/goose`: SQL annotations `Up`, `Down`,
  `StatementBegin`/`StatementEnd`, embedded migrations и запуск через Go API;
- `/jackc/pgx`: `pgxpool`, transactions, `CollectRows`, `NamedArgs` и
  `StrictNamedArgs`.

Связанные документы: `REPO-DOC-001`, `GO-DOC-001`, `GUIDE-DOC-003`,
`INFRA-DOC-001`.
