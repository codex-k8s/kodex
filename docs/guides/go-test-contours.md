---
id: GUIDE-MC-009
title: Герметичный и PostgreSQL-контуры Go
type: guide
status: approved
owner: developer
version: 0.1.0
updated: 2026-07-20
---

# Герметичный и PostgreSQL-контуры Go

## Единые команды

- `make test-go` запускает только герметичные модульные и компонентные тесты. PostgreSQL-тесты отделены тегом сборки `postgres`; команда очищает внешние `GOFLAGS`, отключает пользовательский `GOENV` и `GOWORK` и не использует PostgreSQL, Docker или тестовый DSN.
- `make test-go-postgres` последовательно запускает полный обязательный набор на PostgreSQL 15 и 16. Успех возможен только после двух фактических запусков. Отсутствующая версия имеет исход `NOT RUN` и завершает цель ошибкой; ошибка оснастки или теста имеет исход `FAIL`.
- `make test-go-all` сначала выполняет `make test-go`, затем `make test-go-postgres`. Параллельное или частичное выполнение не подменяет эту цель.

Каждая major-версия получает независимый server-owned sentinel. Он записывается только обязательным PostgreSQL-тестом после подключения к одноразовой базе, проверки фактической major-версии и доступности `vector` и `amcheck`. Поэтому внешние `GOFLAGS=-run=^$` не могут дать ложный `PASS`, а `GOFLAGS=-tags=postgres` не включает БД-тесты в `make test-go`.

По умолчанию для обратной совместимости используется явно ограниченный режим `local-binaries`. Для удалённого агента основной путь — `kubernetes`; `docker` является только локальным резервным вариантом; `scoped-dsn` принимает только отдельные disposable-пары каждой major-версии. Неизвестный режим и неполная конфигурация завершаются отказом.

## Режимы PostgreSQL 15 и 16

### Kubernetes — основной путь удалённого агента

```bash
MATTERCODEX_TEST_POSTGRES_MODE=kubernetes \
MATTERCODEX_TEST_POSTGRES_K8S_NAMESPACE=<disposable-test-namespace> \
make test-go-postgres
```

Оснастка работает только внутри кластера, требует уже созданный явно тестовый namespace с меткой `mattercodex.dev/disposable-postgres-tests=true` и не создаёт namespace, RBAC или ServiceAccount. Через `client-go` она создаёт только Pod и внутренний ClusterIP Service с уникальной run identity, отключённым токеном ServiceAccount, ограниченными ресурсами и без добавленных Linux capabilities. Подключение идёт через loopback `port-forward`. Перед удалением Pod и Service повторно сверяются label и точный UID; Kubernetes UID precondition запрещает удалить replacement. Если политика или права тестового namespace отсутствуют, режим имеет исход `NOT RUN`. Промышленный namespace и production apply/deploy этим контуром запрещены.

### Docker — локальный резервный путь

```bash
MATTERCODEX_TEST_POSTGRES_MODE=docker make test-go-postgres
```

Для каждой major-версии создаётся отдельный контейнер с закреплённым OCI index digest:

- PostgreSQL 15 — `pgvector/pgvector:0.8.5-pg15@sha256:18d16372b8406bb38a9f94cbff15d125c463d71fde2770aa8b5c64bfcc1578ee`;
- PostgreSQL 16 — `pgvector/pgvector:0.8.5-pg16@sha256:1d533553fefe4f12e5d80c7b80622ba0c382abb5758856f52983d8789179f0fb`.

Дайджесты проверены 2026-07-20 непосредственно через Docker Registry HTTP API V2 для `registry-1.docker.io`; один ref нельзя переиспользовать для другой major-версии. Контейнер получает уникальные имя и label, случайный порт только на `127.0.0.1` и не получает общий том. Перед `docker rm --force` сверяются точный container ID и label; replacement не удаляется. Недоступный daemon даёт `NOT RUN`, а не успех. Kubernetes mode использует те же закреплённые ref и отвергает mutable tag или неверное соответствие major-версии до создания Pod.

### Локальные server binaries

Для автоматически созданного контура нужны серверные исполняемые файлы `initdb`, `pg_ctl`, `postgres` и установленное для этой версии расширение `vector`. Стандартные каталоги `/usr/lib/postgresql/15/bin` и `/usr/lib/postgresql/16/bin` обнаруживаются автоматически. Для отдельной сборки задаются только пути:

```bash
MATTERCODEX_TEST_POSTGRES_MODE=local-binaries \
MATTERCODEX_POSTGRES_TEST_BINDIR_15=/path/to/postgresql-15/bin \
MATTERCODEX_POSTGRES_TEST_BINDIR_16=/path/to/postgresql-16/bin \
make test-go-postgres
```

### Уже подготовленный scoped disposable DSN

Режим `scoped-dsn` является безопасным ограниченным вариантом для контроллера, который уже владеет одноразовой конечной точкой:

- `MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_15` и `MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER_15`;
- `MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN_16` и `MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER_16`.

Запуск выполняется с `MATTERCODEX_TEST_POSTGRES_MODE=scoped-dsn`. Общая неверсионированная пара не выбирается для матрицы.

Внешняя инициализация вместо готовой базы данных использует отдельные пары `MATTERCODEX_BOT_SERVICE_TEST_BOOTSTRAP_DSN_15|16` и `MATTERCODEX_POSTGRES_TEST_BOOTSTRAP_PROOF_15|16`. Неполная пара отклоняется. Одна общая пара не принимается за доказательство матрицы двух версий. Значения этих переменных нельзя помещать в команды, документацию, журналы или доказательства.

Оснастка отклоняет промышленную конечную точку и базу данных, известные DSN выполнения и миграций, канонические базы данных, внешнюю конечную точку без разрешения и одноразового доказательства, а также несовпадающую основную версию. После admission дочерняя команда получает минимальный allowlist переменных среды: `MATTERCODEX_DATABASE_DSN`, `MATTERCODEX_MIGRATIONS_DATABASE_DSN`, `MATTERCODEX_POSTGRES_*`, `PG*`, `DATABASE_URL`, `POSTGRES_*` и управляющие matrix/proof-переменные родителя не наследуются. В дочерний процесс передаются только scoped disposable DSN/marker, обязательная major-версия и sentinel.

## Обязательный состав PostgreSQL-проверки

Полная цель включает:

- миграции с нуля и обновление непосредственно предыдущей принятой схемы с данными проектов, сессий, ходов и делегирования;
- повторный `Up` только вперёд, отказ `Down`, точный исполняемый файл N-1 и проверки репозитория и среды выполнения на текущей схеме;
- роли, разрешения, владение, `search_path`, запрет `CREATE`/`TEMP`, проверку `SET ROLE` и `amcheck(heapallindexed => true)`;
- конкурентные проверки минимум через два независимых соединения или транзакции;
- внесение отказов до и после `CREATE`, установки маркера, финальной валидации и очистки, включая коллизию, подмену, отмену, срок, повтор доказательства и конкурентную очистку;
- маршрутизацию, сессии, обратные вызовы и доставку с отказами до каждой локальной границы и без внешних побочных эффектов Mattermost или поставщика.

Тесты с доступом к БД не используют `t.Skip`. Запуск отдельного PostgreSQL-теста выполняется только с `-tags=postgres` через безопасную одноразовую цель; отсутствие такой базы данных является ошибкой.

## Характеристика долга #51

`TestMatterCodexMCPBootstrapFailureBaselineRemainsFailedAfterDependencyRecovery` проходит реальный `claim -> runCodexSessionTurn -> subprocess -> complete -> state` путь. Fault injection находится ниже subprocess/MCP bootstrap boundary: тестовый процесс обращается к реально недоступной обязательной MCP-конечной точке и завершается до первого Codex JSONL-события. После terminal completion зависимость восстанавливается, а обычная одноэлементная очередь подтверждает один execution и отсутствие автоматического продолжения. Fake executor или API, кодирующего ожидаемый terminal result, в этом сценарии нет.

Зелёный исход PR-1 означает только воспроизведение текущего поведения: ход завершён как `failed` и автоматически не продолжился. Это известный долг [#51](https://github.com/codex-k8s/matter-codex/issues/51), а не целевой контракт. Повтор, возврат в очередь, `queued`, `retry_wait` и восстановление в PR-1 не реализуются. Реализация #51 обязана изменить ожидание той же оснастки на восстановительный исход без добавления пропуска или ожидаемого падения.

## Матрица синтетических секретов

Проверки создают уникальные синтетические контрольные значения для OpenAI, GitHub, Mattermost, Kubernetes, PostgreSQL DSN, токена сессии и токена MCP. Действующие значения переменных среды, Secret и `.env` в качестве фикстур не читаются.

| Канал | Обязательное доказательство |
| --- | --- |
| Промпт и Codex `config.toml` | `TestSyntheticSecretMatrixIsRedactedFromRunnerChannels` проверяет сформированный ввод и ссылку `bearer_token_env_var` без значения. |
| Структурированные журналы, stderr, ошибка, итог, статус и метаданные артефактов | Та же проверка пропускает каждый класс через централизованное скрытие и проверяет исходное, экранированное JSON и base64-представления. |
| Архив сессии | Перед упаковкой исходные session-файлы атомарно санитизируются. Лимиты до чтения и `WriteHeader`: 8 MiB на файл, 32 MiB суммарно, 512 файлов и 1024 записи tar, включая корень `sessions`, каталоги, regular files и каждый допустимый header. Create и restore используют один entry limit; принятый create архив проходит restore при неизменённых данных. Превышение даёт типизированный fail-closed исход и удаление небезопасного source tree. |
| Полезная нагрузка Mattermost | `TestSessionTransportProtectsRawJSONAndBase64ValuesBeforeBotService` доказывает, что raw, JSON-escaped и base64-значения без префикса `KEY=` не пересекают HTTP-границу runner → bot-service. `TestAgentSessionFailedCompletionPublishesOnlyRunnerSanitizedPayload` проводит уже безопасный payload через реальные completion persistence, status card, result publisher и `notifyRootInitiatorFailure`. Bot-service не получает доступ к значениям секретов. |
| Рабочая нагрузка Kubernetes и отрендерованный YAML | `TestSyntheticSecretMatrixDoesNotReachRenderedWorkloadObjects` проверяет Pod: присутствуют только имена Secret и ключей, а не значения. Сами синтетические Secret используются только внутри поддельного клиента. |
| Аудит PostgreSQL | Миграционные проверки и проверки допуска сохраняют только безопасные события, ссылки и хеши и проверяют отсутствие синтетического секрета в `matter_codex_audit_events`. |

Единый inventory запуска строится из фактически выданных `env`/`extraEnv` и credential-файлов GitHub, OpenAI/Codex, Kubernetes ServiceAccount, MCP-сессии, `KUBECONFIG` и других scoped `_FILE` источников. Любое непустое sensitive-значение сохраняется в inventory. Значение короче 16 байт считается неподдерживаемым для безопасного поиска фрагментов и закрыто отклоняет запуск до child, публикации и сети; короткая общая строка не передаётся глобальному replacer.

Перед каждым child значения файлов один раз копируются в уникальный каталог `0700` как файлы `0600`; `KUBECONFIG`, `MATTERCODEX_GITHUB_TOKEN_FILE` и scoped `_FILE` в child env переписываются на этот снимок. Исходные projected и иные пути, которые child всё ещё технически способен открыть, контролируются весь период `cmd.Run`. Изменение, удаление или появление значения отменяет subprocess, навсегда помечает текущий запуск небезопасным, удаляет raw staging и session source и запрещает events, final, archive, status, completion и любой последующий HTTP-вызов этого запуска.

Поддерживаемый набор защиты ограничен raw и JSON-escaped значениями, standard/raw base64 и base64url, URL/path percent-encoding с обоими регистрами hex digits, lower/uppercase hex и последовательными фрагментами исходного значения с кусками длиной 1–7 байт и ограниченным промежутком. Exact-представления скрываются; совпадение после консервативной percent/case-нормализации или streaming-fragment detection даёт типизированный fail-closed отказ до публикации и сети. Corpus тестов строит эти формы независимо от sanitizer. Защита не заявляется для произвольного шифрования, хеширования, перестановки или неограниченного преобразования. Новый secret-bearing output path разрешён только через этот общий fail-closed boundary; если нужное преобразование им не поддерживается, path не получает credential либо публикация запрещается.

Raw stdout/stderr/events/final и рабочие session-файлы находятся только в уникальном каталоге `/tmp/mattercodex-run-*` с режимом `0700`, а не на workspace/PVC. На persistent workspace атомарно публикуются только ограниченные по размеру санитизированные файлы. После обычного завершения source-файлы сессии атомарно санитизируются перед архивированием; при небезопасном содержимом или превышении лимита source tree удаляется. При kill/OOM container-ephemeral staging исчезает вместе с контейнером. Восстановление между запусками сохраняет только последний принятый bot-service санитизированный bounded archive; сырой rollout не является частью recovery-контракта.

## Ручная проверка и доказательства

```bash
make test-go
make test-go-postgres
make test-go-all
```

В PR для точного SHA раздельно фиксируются:

- `PASS` герметичного контура;
- `PASS` PostgreSQL 15;
- `PASS` PostgreSQL 16;
- `PASS`, `FAIL` или `NOT RUN` общей цели;
- фактически выполненные проверки гонок, статического анализа и безопасности;
- отсутствие или состояние GitHub `check runs`.

Отсутствие GitHub `check runs` означает отсутствие свидетельств CI, а не успешный CI. Ручной запуск не использует промышленный DSN, Kubernetes apply/deploy, прямые побочные эффекты действующих Mattermost или поставщика и действующие значения секретов.
