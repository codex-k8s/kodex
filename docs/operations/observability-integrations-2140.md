---
id: OPS-DOC-OBSERVABILITY-INTEGRATIONS-2140
title: Управляемые подключения к системам наблюдаемости
type: operations
status: approved
owner: backend
version: 1.0.0
updated: 2026-11-14
---

# Наблюдаемость через integration-gateway

Источники: [#2140](https://github.com/codex-k8s/kodex/issues/2140),
[#1830](https://github.com/codex-k8s/kodex/issues/1830), `ARCH-MC-005`,
`GUIDE-DOC-003`, `GUIDE-DOC-006`, `GOV-DOC-003`.

Текущий срез содержит настоящие consumers Prometheus, Thanos и два явных
профиля Grafana. Qualified narrow annotation/rule writes и Loki остаются
следующими срезами того же unit; наличие реестра не означает их готовность.
Declared compatibility: Grafana13.2.0, Prometheus3.14.0 и Thanos0.42.5. Точная версия Prometheus
выбрана по сообщённому оператором установленному профилю; это не live-проверка
данного кандидата. Живые endpoint и credentials не заданы, live — `NOT RUN`.

## Полномочия и доставка

Пользователь явно выбирает `WHOLE_ENDPOINT`: grant распространяется на всю
область данного endpoint. Подключение не обещает project/tenant isolation.
У Thanos обязательный `tenant_id` становится только серверным `THANOS-TENANT`;
сам заголовок не является authority. Для ограничения области оператор выделяет
отдельный endpoint и соответствующий provider credential.

Личный ключ выбирается прежним owner по root initiator; его отсутствие, чужой
владелец, отзыв, истечение, rotation и изменение configuration не допускают
fallback к общему ключу. `PROJECT_SHARED` остаётся отдельным явным выбором
service identity. Сотрудник не получает credential, transport либо произвольный
HTTP command. Generic UI получает русские labels/help и типизированные scalar
fields, включая явное описание полной области endpoint.

Обслуживает существующий deployable `integration-gateway`. Новых процессов,
RPC, Pods и secret mounts нет. Обязателен прежний operator-pinned native
CONNECT transport с exact destination, TLS hostname/CA и запретом redirects;
core/direct/public fallback отсутствует. Пока native target не настроен,
подключение не проходит health. Адреса в этом документе synthetic.

## Сквозной контракт и lifecycle

| Сценарий | Внешний фиксированный path | Consumer и результат |
|---|---|---|
| Проверка подключения | `GET /api/v1/status/buildinfo` | Exact declared version string и отказ отдельного случайного неверного ключа на том же path; безопасная сводка |
| Instant PromQL | `GET /api/v1/query` | Явный UTC time, AST profile, timeout5s, lookback_delta5m, typed vector/scalar/string/histogram |
| Range PromQL | `GET /api/v1/query_range` | Явные UTC start/end/step, AST profile, typed matrix/histogram |
| Labels | `GET /api/v1/labels` | Один parsed metric selector и явный bounded time window, ≤200 имён |
| Label values | `GET /api/v1/label/{name}/values` | Literal legacy label name, parsed selector/window, ≤200 значений |
| Series | `GET /api/v1/series` | Parsed selector/window, ≤50 label maps |
| Rules | `GET /api/v1/rules` | ≤50 groups/100 rules; name/type/state/health/labels без file/expression/lastError |
| Alerts | `GET /api/v1/alerts` | ≤100 alerts; labels/state/value без annotations/generatorURL |

Actor приходит из проверенного transport и owner root state; публичный endpoint
управления — существующий control-api-gateway, затем прежние CP owner commands,
claim/Begin/Complete и gateway. Owner назначает connection/grant/credential и
immutable definition/configuration/input pins; request payload не доказывает
владение. Новый enum18 выбирает обязательный typed consumer, а не выдаёт права.

Все16 capabilities имеют READ/NONE/READ_ONLY и inputless health prerequisite.
Нет внешних изменений, recovery POST, Human Gate либо configuration write.
Provider read создаёт прежний receipt с exact effect/input/response digests;
успех/отказ/timeout завершает прежний owner lifecycle, cancellation закрывает
HTTP request context. Событий провайдера нет; authoritative read path — текущий
bounded GET. Lease expiry, stale completion, revoke, retry и root lineage
остаются прежними owner-переходами. Повтор READ не утверждает прежний snapshot.

## Что именно ограничено

HTTP window ≤24h, ≤1000 range steps; consumer принимает ≤50 query series,
≤5000 samples и безопасную проекцию ≤64KiB. Provider body дополнительно
ограничен общей boundary. Полная страница query series помечается
`limit_reached`; число50 не доказывает полноту. Prometheus получает limit50;
Thanos0.42.5 не имеет такого query parameter, поэтому ограничение series у него
только consumer-side, без обещания ограничения upstream работы.

Официальный Prometheus parser v0.314.0 отклоняет любую parser error до
использования восстановленной модели. Профиль разрешает ≤128 AST nodes,
глубину16, ≤8 selectors, ≤16 matchers на selector и matcher values ≤256bytes.
Сумма ranges и абсолютных offsets вдоль пути AST ≤24h; отрицательный offset
считается по абсолютному значению. Absolute `@`, `@ start/end`, experimental
features и dynamic durations закрыты. Subquery одна на путь, без nesting,
range≤1h, явный step≥1m и ≤120 внутренних steps. Выражение не переписывается.
Metadata match[] разбирается штатным parser как metric selector, а не expression.

Это **не** доказательство лимита CPU, памяти или cardinality. Даже короткое
выражение может затронуть много рядов. Forced timeout5s и lookback_delta5m
проверены по exact source Prometheus3.14.0 и Thanos0.42.5: query/request context
достигает engine и отменяется при timeout/cancel. HTTP window и AST history —
разные пределы: суммарная временная область может включать оба и default
lookback5m. Серверные max-samples, concurrency, resource limits и фактическая
реакция deployment на cancel должны отдельно фиксироваться оператором как
declared/observed; сейчас они `UNKNOWN`, live cost qualification `NOT RUN`.
Consumer response cutoff не выдаётся за upstream computation limit.

Thanos получает partial_response=false для query/labels/series и dedup=true для queries.
В0.42.5 rules/alerts не читают HTTP partial_response: ими управляет отдельный
deployment flag, который здесь остаётся UNKNOWN. Для этих endpoints ложный
параметр не передаётся; все warning/info envelopes также закрываются.
Любые warning/info/error envelopes закрываются без частичной «успешной»
квитанции. Дубликаты JSON keys, case aliases typed fields, недостоверные
histograms и неизвестные result types отклоняются. Регистрозависимые label
maps сохраняют `a` и `A`. Из известных credential-bearing label names скрываются
значения; это не универсальный поиск секретов. Metadata и сырые diagnostics
не выдаются, общий HTTP guard запрещает отражение выбранного credential.
Любые metric/log payloads остаются недоверенным содержимым, не instructions
и не источником authority.

Неверный-key probe доказывает только наблюдённый authentication rejection.
Health всегда возвращает `tenant_isolation=UNKNOWN`: успешный ключ и отказ
другого ключа не доказывают межtenant изоляцию. Для её наблюдения необходима
отдельная матрица двух действительных ключей и двух tenant с positive/negative
requests. Данный worker её не имитирует.

## Текущий контракт Grafana13.2.0

`grafana-dashboard` имеет6 READ/NONE операций: authenticated health, metadata
точного dashboard, metadata одного datasource Prometheus, bounded PromQL через
него и annotation list/detail выбранного dashboard. `grafana-organization`
имеет15 операций: health/datasource/query, annotation CRUD, rules list/detail
и silence list/detail/create/update/expire. Для организации пользователь
явно выбирает `WHOLE_ORGANIZATION`; прежний dashboard scope не расширяется.

Health использует exact13.2.0 `GET /api/health`, текущую signed identity/org из
`GET /api/user` и invalid-key rejection на том же authenticated path. User
email/login/identity не попадают в результат. Query использует только fixed
`GET /api/datasources/proxy/uid/{selectedUID}/api/v1/query_range`: ни URL,
ни HTTP path, datasource config либо plugin method из input не принимаются.
Generic `POST /api/ds/query` не используется: exact source выбирает plugin
по текущему datasource, а pre-read type не является atomic type lock.
Применяются общий PromQL AST profile, timeout5s/lookback5m, partial_response=false,
dedup=true и typed sample/histogram budgets. Чужие datasource URL, credentials,
JSON settings и secure fields из metadata не выдаются.

Dashboard GET выдаёт только UID/title/version без templates, links и targets.
Annotation GET/list выдаёт только id/dashboardUID/time/timeEnd/text без автора,
email и embedded data. Перед mutation owner whole-org grant остаётся
обязательным; текущие provider action/scope metadata загружаются с
`reloadcache=true`, annotation разрешается в current organization. POST/PATCH
принимают только server-built text/time/timeEnd/dashboardUID, затем требуют
точный readback. Время не округляется скрыто: принимается точность до
миллисекунд и период≤24h. DELETE возвращает `deletion_requested`.

Silence API всегда выбран встроенный Alertmanager текущей организации, без
caller datasource URL. Для effect разрешён один literal equality matcher;
regex и arbitrary JSON отсутствуют. Current general-silence permissions —
`alert.instances:create|write` вместе с read, scoped rule-only permission не
выдаётся за authority подавлять произвольный matcher. List использует один
server-built exact filter. Create/update202 подтверждаются typed readback;
DELETE в этом API означает истечение и даёт `expiration_requested`, не
физическое удаление. Creator identity не выдаётся.

Все6 Grafana mutations имеют EACH, delete — DESTRUCTIVE. Consumer не повторяет
POST/PATCH/DELETE, UNKNOWN сохраняется после lost transport/5xx/malformed success
или неудачного readback, включая403. Recover не переотправляет effect. Личный
ключ требует profile19 `GRAFANA_TYPED_WRITE_V1` и current metadata observation3
`GRAFANA_CURRENT_PERMISSIONS_V1`; observation не подтверждает глобальный WRITE.
Known read-only блокирует launch, unknown metadata оставляет WRITE unconfirmed.
READ использует profile18 с inputless health prerequisite и прежними owner pins.

Узкая mutation scope требует доказательства immutable ownership, которого
нельзя получить одним pre-read. В exact13.2.0 legacy SQL store update не меняет
`dashboard_uid`; новый annotation API также запрещает изменение dashboardUID
одного resource. Однако новый store сохраняет caller-selected legacyID,
`GetByLegacyID` выбирает newest live resource, а migration time edit создаёт
новую запись с тем же legacyID. Поэтому numeric legacyID между pre-read и
последующим legacy mutation способен разрешиться в другой dashboard. Это
не утверждение о поле DashboardUID в PATCH DTO: такого поля DTO не принимает.
Отдельный operator-qualified `LEGACY_SQL_ONLY` вариант даст узкие mutations;
он остаётся следующим срезом, обычный checkbox эту границу не откроет.

Rules READ выдаёт только identity/state/provenance без executable query models,
annotations и notification config. File/Git rule reads не означают WRITE.
Single-rule legacy DELETE читает provenance до транзакции и затем удаляет
по org+UID без version precondition; GET provenance не является atomic guard.
Поддержка writes потребует отдельного operator-qualified API-managed group,
исключающего file/Git writers. `X-Disable-Provenance` не отправляется.

Проверены exact official source13.2.0: `pkg/api/{api,user,annotations,datasources}.go`,
`pkg/services/accesscontrol/{api/api,accesscontrol,models}.go`,
`pkg/services/query/query.go`, `pkg/services/ngalert/{api,accesscontrol}`,
`pkg/services/annotations/{annotationsapi,annotationsimpl}` и
`pkg/registry/apps/annotation/{k8s_adapter,config}.go`.
Независимый provider subset: `contracts/integrations/providers/grafana/v1/openapi.yaml`.
Context7 обслужил ближайший13.2.4 вместо запрошенного13.2.0; это не выдаётся
за доказательство установленной версии, использован exact tagged source.

## Документы и воспроизводимая проверка

Проверены Context7 `/prometheus/prometheus`, `/thanos-io/thanos`,
`/grafana/grafana`, `/grafana/loki`; exact source имеет приоритет при отличии
индексированной версии. Для этого среза использованы официальные
[Prometheus3.14.0 API source](https://github.com/prometheus/prometheus/blob/v3.14.0/web/api/v1/api.go),
[Prometheus parser](https://github.com/prometheus/prometheus/tree/v0.314.0/promql/parser),
[Thanos0.42.5 query source](https://github.com/thanos-io/thanos/blob/v0.42.5/pkg/api/query/v1.go)
и [Thanos base API](https://github.com/thanos-io/thanos/blob/v0.42.5/pkg/api/api.go).
Независимый provider subset: `contracts/integrations/providers/prometheus-compatible/v1/openapi.yaml`.

Локальные synthetic tests покрывают все16 capabilities через package, exact
owner claim pins, personal profile, credential loader, native transport,
независимые wire assertions/OpenAPI, typed response и receipt. Negative matrix
проверяет missing native transport/profile, unknown version, открытый auth,
неопределённый probe, foreign/revoked/rotated key, malformed/partial JSON,
нативные histograms, AST time modifiers и отсутствие arbitrary HTTP query.

Ручная проверка после разрешённой настройки: выбрать whole-endpoint область,
сохранить личный ключ, проверить подключение, выполнить ограниченный read;
под другим пользователем проверить отсутствие общего fallback, затем
отозвать ключ/изменить origin и проверить обязательную повторную проверку.
Live, browser/render и schema045 component квалификация пока не выполнены.
Rollback отключает connections/capabilities; owner migration forward-only,
старые pins и существующие connections автоматически не расширяются.
