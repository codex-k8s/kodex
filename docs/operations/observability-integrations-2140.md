---
id: OPS-DOC-OBSERVABILITY-INTEGRATIONS-2140
title: Управляемые подключения к системам наблюдаемости
type: operations
status: approved
owner: backend
version: 1.1.0
updated: 2026-11-14
---

# Наблюдаемость через integration-gateway

Источники: [#2140](https://github.com/codex-k8s/kodex/issues/2140),
[#1830](https://github.com/codex-k8s/kodex/issues/1830), `ARCH-MC-005`,
`GUIDE-DOC-003`, `GUIDE-DOC-006`, `GOV-DOC-003`.

Кандидат содержит исполняемые consumers Prometheus, Thanos, Grafana и Loki,
включая отдельные ограниченные профили записи. Declared compatibility:
Grafana13.2.0, Prometheus3.14.0, Thanos0.42.5 и Loki3.6.21. Точная версия
Grafana/Prometheus выбрана по операторскому readback установленной среды;
это не live-проверка данного кандидата. Endpoint и credentials для живой
квалификации кандидата не заданы: live — `NOT RUN`.

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

Все16 Prometheus/Thanos capabilities имеют READ/NONE/READ_ONLY и inputless health prerequisite.
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
имеет17 операций: health/datasource/query, annotation CRUD, rules list/detail/create/update
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

Все Grafana mutations имеют EACH, delete — DESTRUCTIVE. Consumer не повторяет
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
Отдельный `grafana-dashboard-annotations` содержит7 capabilities, включая
узкие annotation CRUD. Он требует operator-qualified backend `legacy-sql` и
migration phase `off`; create получает dashboard UID из Connection, а
update/delete повторно проверяют текущую принадлежность. Обычный checkbox
и один pre-read эту границу не открывают.

Rules READ выдаёт только identity/state/provenance без executable query models,
annotations и notification config. File/Git rule reads не означают WRITE.
Single-rule legacy DELETE читает provenance до транзакции и затем удаляет
по org+UID без version precondition; GET provenance не является atomic guard.
Create/update реализованы отдельными typed scalar commands в whole-org
Connection. Сервер строит фиксированные Prometheus instant query A и threshold
condition B, назначает org и effect-bound create UID; произвольный query-model
JSON, notification config и plugin commands не принимаются. Перед записью
проверяются текущие folder permissions, datasource и API provenance. Exact
native UPDATE применяет optimistic version guard и manager update в одной
транзакции; file provisioners также изменяют эту version. Native409 закрывает
операцию без автоматического retry. Полная замена настройки прямо указана в
capability; это не обещание compare-and-set относительно давнего UI snapshot.

DELETE доступен только отдельной `grafana-organization-rule-delete`
Connection с4 capabilities и whole-org authority. Нужен действующий operator
profile `API_ONLY_ORGANIZATION_ALERT_RULES`, исключающий file/Git writers во
всей организации, а не одной папке. Без доказательства эта capability получает
`INTEGRATION_OPERATOR_QUALIFICATION_REQUIRED`; обычные read/create/update
не отключаются. `X-Disable-Provenance` не отправляется.

Проверены exact official source13.2.0: `pkg/api/{api,user,annotations,datasources}.go`,
`pkg/services/accesscontrol/{api/api,accesscontrol,models}.go`,
`pkg/services/query/query.go`, `pkg/services/ngalert/{api,accesscontrol}`,
`pkg/services/annotations/{annotationsapi,annotationsimpl}` и
`pkg/registry/apps/annotation/{k8s_adapter,config}.go`.
Независимый provider subset: `contracts/integrations/providers/grafana/v1/openapi.yaml`.
Context7 обслужил ближайший13.2.4 вместо запрошенного13.2.0; это не выдаётся
за доказательство установленной версии, использован exact tagged source.


## Loki: логи, метрики и правила

`loki` предоставляет7 READ/NONE capabilities: health, instant/range metric
LogQL, range log streams, labels/values/series. Owner явно выбирает
`WHOLE_ENDPOINT` и фиксированный `tenant_id`; `X-Scope-OrgID` является routing,
а не tenant authority. Health проверяет exact3.6.21 build, текущий ключ и
отказ нового неверного ключа на bounded labels GET. `tenant_isolation`
остаётся `UNKNOWN`, в том числе при наличии operator attestation.

Официальный LogQL AST сохраняет исходный query. Пределы:4096bytes,128nodes,
глубина16,8selectors,16matchers/selector,256bytes для regex/matcher parameters,
суммарный range+absolute offset вдоль AST ≤24h. Metric и stream expressions
разделены; metadata API допускает только stream selector. `line_format`,
`label_format` и experimental variants закрываются с отдельной причиной
`INTEGRATION_QUERY_PROFILE_UNSUPPORTED`: node count не ограничивает template
allocations либо альтернативный result contract. Это не собственный renderer
и не урезанная грамматика ради module collision.

HTTP window≤24h, range≤1000steps, общий query deadline5s с cancellation;
неподдерживаемые Loki параметры `timeout`/`lookback_delta` не отправляются.
Exact3.6.21 `querier.WrapQuerySpanAndTimeout` и backend querier сохраняют request
context и собственный per-tenant query timeout. Его фактическое значение,
CPU/memory/cardinality/concurrency limits остаются `UNKNOWN` до deployment
qualification. Response limits не доказывают ограничение upstream cost.
Metric projection использует прежние budgets. Logs ограничены100строками,
50streams и4096bytes/line; timestamps лежат в запрошенном окне, structured
metadata/stats не выдаются. Labels с известными credential-bearing именами
редактируются; произвольная строка лога остаётся `UNTRUSTED`, это не общий DLP.

`loki-ruler-namespace-read` содержит3 READ capabilities для точного namespace,
включая Git/file-managed metadata; write permission из него не выводится.
`loki-ruler-namespace` содержит5 capabilities и отдельный явный
`API_MANAGED_NAMESPACE` scope. Ruler health читает тот же namespace через
рабочий transport; пустой/отсутствующий namespace не считается готовым без
операторской подготовки. Запись требует operator-pinned remote S3 ruler store,
API-only writer inventory и двух действительных ключей/tenant в proof matrix.

Native `POST /loki/api/v1/rules/{namespace}` создаёт или целиком заменяет группу,
поэтому capability называется `loki.ruler.group.replace`, а не create-only/CAS.
Owner EACH согласует полную замену одной группы одной typed alert rule; YAML
строит сервер из scalar inputs. DELETE также EACH/DESTRUCTIVE и затрагивает
только точный namespace/group. HTTP202 означает stored/queued receipt,
`evaluation_state=UNKNOWN`; факт активной оценки не выдумывается. Replace
дополнительно требует authoritative group readback. Потеря ответа/readback
сохраняет UNKNOWN, replay через Recover отсутствует.

YAML boundary допускается только private server-built Loki POST с exact
operation/method/path/EACH tuple; generic transport/input её не получает.
До semantic decode запрещены aliases/anchors/custom tags/complex/duplicate keys,
лишний document и excessive depth/nodes. Точный credential, в том числе YAML-
или JSON-escaped echo, закрывается внутри credential boundary до projection.

## Совместимый официальный parser stack

PromQL0.314.0 сохранён. Loki parser library3.6.17 выбран вместо широкого MVS
обновления от runtime-module3.6.21/3.7.10. Все15 production syntax source files
library3.6.17 byte-identical3.6.21; runtime HTTP/ruler qualification закреплена
отдельно на3.6.21. Официальный Grafana memberlist replacement повторяет
upstream go.mod; своего fork/vendor/parser нет. Обязательны исходный source
hash verifier и whole-affected Go baseline; версия библиотеки сама по себе не
объявляет PASS либо полный API compatibility.

Проверка без сети и effects: `python3 tools/release/verify-observability-parser-profile.py`.
Она сверяет полный15-file набор и SHA обеих immutable module source версий,
а также PromQL pin; отсутствие уже загруженного module cache является отказом.
При первоначальном сравнении workspace3.6.17 менял13 existing indirect
versions,3.6.21 —160; grpc/otel/client-go/crypto/net/protobuf существующего
baseline при выбранном3.6.17 не повышались. Imported closure при этом
расширяется: официальный syntax зависит от upstream Loki utilities/dskit.
Это фиксируется как реальная зависимость и квалифицируется целиком.

## Operator artifact: установка, отзыв и readiness

Optional process inputs `INTEGRATION_GATEWAY_OBSERVABILITY_QUALIFICATION_FILE`
и `INTEGRATION_GATEWAY_OBSERVABILITY_QUALIFICATION_SHA256` принимаются только
парой, с exact path `/var/run/kodex/observability-qualifications/profiles.json`
и SHA immutable bytes. Это deployment input оператора, не поле Connection,
request, prompt или обычный пользовательский JSON. Runtime не выдаёт по нему
grants. Типизированный schema `kodex-observability-qualifications/v1`
содержит1..32 profiles с exact target/origin, version, resource coordinates,
credential/image/config/writer-inventory/runtime-readback SHA и временем
квалификации/expiry≤24h. Неизвестные fields/kinds/duplicates закрываются.

Каждый профиль требует deployment-readback, target-config и writer-inventory
request/response hashes. Loki дополнительно требует positiveA/B с разными
действительными ключами и tenant, crossA→B/B→A403/404, а также create/update/delete202
в точном namespace с тем же selected key. Пара200/401 и один ключ не проходят.
Это доверенная **operator attestation**, а не самостоятельное runtime
наблюдение изоляции. Gateway сообщает `OPERATOR_ATTESTED` отдельно от
`tenant_isolation=UNKNOWN`; фактический источник evidence сохраняет оператор.

Pure installer `tools/install/observability_qualification.py` строит immutable
ConfigMap с content-addressed именем,0444 file, read-only mount только в
integration-gateway и exact env SHA. Он требует уже настроенный native
transport. Публичный renderer принимает `--observability-qualification` и
`--observability-qualification-sha256`; `--check` встроен в renderer, deploy
и portable preflight и сравнивает весь результирующий профиль. Он не вызывает
kubectl apply и не создаёт qualification по checkbox.

| Переход | Авторитетная граница и действие |
|---|---|
| Выбор режима/первый grant | Пользователь создаёт отдельную Connection/Grant; сервер выводит immutable resource scope; operator artifact не создаёт и не расширяет grant |
| Квалификация | Оператор фиксирует exact deployment/image/backend/config/key/writer inventory и реальные source evidence; истечение не более24h |
| Доставка | Renderer монтирует immutable bytes и SHA; штатный release lifecycle проверяет полный новый workload/container inventory, затем Connection test проходит рабочий native path |
| Каждый protected health/effect | Gateway перечитывает bounded regular read-only file, SHA и текущие target/resource/key/expiry перед HTTP; request deadline не переживает expiry |
| Отзыв/смена writer/backend/config/key | Сначала отключить затронутые Connection/Grant и выполнить owner cancel/drain/join; затем отозвать artifact и выполнять изменение/новую квалификацию/rollout/retest |
| Пропажа/tamper/expiry | Новый effect закрывается `INTEGRATION_OPERATOR_QUALIFICATION_REQUIRED`; живой ключ не объявляется неверным, другой Connection/shared key не подставляется |
| Effect уже начат/ответ потерян | Прежний owner сохраняет UNKNOWN/read-only reconciliation; file withdrawal не доказывает remote rollback и не разрешает replay |
| Ротация/повторное включение | Новый artifact/SHA и owner credential/configuration versions; старые immutable pins не переписываются, старый grant автоматически не расширяется |

Provider event для каждой перечисленной read/create/update/replace/delete
операции отсутствует. Receipt и terminal/cancel/retry/grant переходы принадлежат
прежней owner-транзакции; текущий typed native GET является authoritative
read path. Ответ202/204 не становится новым придуманным domain event.

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
Live и итоговая browser/component квалификация отражаются отдельно; локальный consumer PASS не заменяет эти результаты.
Rollback отключает connections/capabilities; owner migration forward-only,
старые pins и существующие connections автоматически не расширяются.
