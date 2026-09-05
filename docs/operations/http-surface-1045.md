---
id: OPS-HTTP-SURFACE-1045
title: Полнота HTTP поверхности MVP и зависимости producer
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница проверки

Источники: #1045/#1021, #1018, утверждённая матрица #1031
`docs/operations/mvp-1031-acceptance.md`. HTTP основан на `5c32fa683`, Proto
сверен с CP `10266a2ef` и `aff6a0a42`: различий нет. CP SQL/handlers не
переносились и не редактировались. PWA handwritten остаётся у Newton.

В таблице указана HTTP поддержка, а не PASS пользовательского сценария.
Проверка generated RPC surface охватывает 71 Query, 135 Command и 11 Assistant
методов. Для всех есть authority profile и HTTP consumer; event cursor
используется только WebSocket resume. Runtime/credential/email worker методы
не публикуются в browser API. Наличие consumer не доказывает CP SQL/runtime.

Все HTTP endpoints ниже имеют префикс `/api/v1`. Session/OIDC и tenant
устанавливает boundary; typed CP client передаёт application identity и signed
context. Ref/filter/OCC не заменяют owner eligibility. Мутации сохраняют
CSRF/idempotency/If-Match правила существующих специализированных команд.

## Все 61 требования

| MVP-UI | HTTP / контрактная поддержка и граница |
| --- | --- |
| 01 | `usertext` catalogs и safe Problem mapping; локализация layout у PWA. |
| 02 | Bootstrap/session/WebSocket status; геометрия badge у PWA. |
| 03 | Данные typed views; адаптивный layout у PWA. |
| 04 | Overview; композиция главной у PWA. |
| 05 | Bounded pages/cursors/total в поддерживающих их CP queries; компактный/modal list у PWA. |
| 06 | CreateAgent/CreateWorkflow и org/project upload; quick actions у PWA. |
| 07 | Публичные статические PWA assets не становятся исключением auth для API; ingress у root. |
| 08 | Environment readiness/agents endpoints, exact CP mapping. |
| 09 | Assistant create/title/turn/context и история с pageSize/pageToken; поиск/archive требуют CP D1. |
| 10 | Project view/overview, серверные aggregates без N+1 у browser. |
| 11 | Session refresh, одноразовый websocket ticket, durable revocation и resume cursor. |
| 12 | Project list/search/cursor; размещение selector у PWA. |
| 13 | Project DTO и server filters; infinite scroll/focus у PWA. |
| 14 | Global speech endpoint + bootstrap availability; исключение sensitive editors у PWA. |
| 15 | Template preview/variables; размеры editor у PWA. |
| 16 | ValidatePromptTemplate/PreviewPromptTemplate, typed errors; renderer принадлежит CP. |
| 17 | Новый model-capabilities, account-specific filter и ProviderDefinition.models; catalog revision требует D2. |
| 18 | Supported/default reasoning effort передаётся без подмены; сохранение через runtime overlay CP. |
| 19 | Provider account/definition pages, lifecycle/readiness; rich selector у PWA. |
| 20 | Editor keyboard behavior не требует отдельного HTTP endpoint. |
| 21 | Config overlay draft/validate/publish/rollback, typed diagnostics и exact digest. |
| 22 | Общие query/page параметры; selector behavior у PWA. |
| 23 | Runtime environment commands; permission/OCC повторно проверяет CP. |
| 24 | Runs list/query/cursor; Kanban columns у PWA. |
| 25 | Artifact lifecycle filters/detail; selection/trash layout у PWA. |
| 26 | Org/project upload, bounded stream; workspace DnD у PWA. |
| 27 | Organization catalogs, optional project filters, typed VFS project roots. |
| 28 | Agent readiness/run refs; итоговый badge у PWA. |
| 29 | Atomic UploadAgentAvatar, size/type checks; scan/admission/cleanup у CP. |
| 30 | TemplateVariable catalog сохранён; отдельные available/reason требуют D3. |
| 31 | Agent capability/integration grants и workflow commands; delegation intersection у CP. |
| 32 | Workflow typed views, aggregate counts и launch commands. |
| 33 | CreateRun/LaunchRun form mapping, ошибки publication/permission/readiness без auto retry. |
| 34 | Workflow step templates и materialized PreviewPromptTemplate; execution renderer у CP. |
| 35 | Preview сохраняет materialization/provenance; semantic slot logic у CP. |
| 36 | AddSessionTurn/LaunchRun и runtime bindings; безопасный previous/current diff требует D4. |
| 37 | VFS list/search, 24 Skill/Memory lifecycle/binding commands, exact source fields; runtime file I/O не browser RPC. |
| 38 | Owner gates list/count; подписи вкладок у PWA. |
| 39 | Integration connection list/query/page/readiness. |
| 40 | Membership/capability/connection selectors и grants, owner intersection у CP. |
| 41 | Общий connection config остаётся CP owned; typed EMAIL configuration/projection требует D5. |
| 42 | Integration definition/grants и safe email receipt/reconciliation; provider effects не выполняет HTTP. |
| 43 | Schedule create/update/revisions/preview/runs; occurrences и timezone logic у CP/scheduler. |
| 44 | Environment list/detail, без browser selection side effect в HTTP. |
| 45 | Environment/secret и managed draft commands; расположение tabs у PWA. |
| 46 | Восемь managed Save/Discard и environment drafts; отдельный staged Secret lifecycle требует D6. |
| 47 | Managed/environment/secret impact и selective rebind; недостающие query/page требуют D7. |
| 48 | Environment readiness/agents, безопасные ошибки hidden/unavailable. |
| 49 | Runtime secret STRING/BASE64/JSON inputs, write-only payload и no-store. |
| 50 | Secret typed list/create/rotate/readback и безопасный Problem; materialization у CP/secret broker. |
| 51 | Provider disable/revoke/delete с blockers/cleanup states из CP, не локальный fake terminal. |
| 52 | Provider device start/observe/refresh/reauthorize, exact idempotency и ошибки. |
| 53 | Search min length/limit/cursor проверяются до RPC; debounce у PWA. |
| 54 | Четыре search kinds, opaque refs/project match, malformed upstream 502. |
| 55 | HTTP потребляет самостоятельный STT client; runtime/deploy принадлежат #1020. |
| 56 | Typed STT parameters/limits draft/readback и raw managed lifecycle; provider probe у STT. |
| 57 | Org speech route, MIME aliases, bounded multipart, permission/session/CSRF. |
| 58 | Speech API и TTL bootstrap; MediaRecorder/editor insertion у PWA. |
| 59 | Protected authenticated STT availability, READY/fresh validity; exact TLS/egress у STT/root. |
| 60 | Live OpenAI fixture smoke: NOT RUN, отдельный credential/staging допуск. |
| 61 | Artifact/VFS user read paths; runtime workspace I/O, immutable mounts и grants у CP/controller/runner. |

## Точные зависимости Bohr Через Root

Все позиции ниже найдены в текущем source Proto, не выведены из отсутствия
локального test. HTTP не добавляет успешные заглушки и не меняет owner contracts.

| ID | Недостающий producer контракт | Последствие |
| --- | --- | --- |
| D1 | ListAssistantConversationsRequest имеет только page/project_ref; нет query, archive command и archived state. | HTTP может передать cursor, но не реализовать server search/archive MVP-UI-09. |
| D2 | ModelCapability/ListModelCapabilitiesResponse не содержат catalog revision/digest. | Account/effort selection доступен; version-bound catalog readback нельзя выдумать. |
| D3 | TemplateVariable содержит описание/type/source, но не available/disabled reason; ListTemplateVariablesRequest не задаёт agent/runtime context. | Невозможно показать authoritative disabled reason по target, не подменяя CP policy. |
| D4 | RuntimeRevisionSnapshot относится к worker execution; публичный previous/current typed diff перед continuation отсутствует. | Нельзя выдавать worker snapshot либо вычислять безопасный diff из неподтверждённых UI данных. |
| D5 | Публичный typed EMAIL safe mailbox configuration отсутствует; worker Resolve/Report/Projection не UI API. | HTTP receipt/reconcile готов, но UI/YAML EMAIL configuration требует согласованного producer. |
| D6 | RuntimeSecret публично имеет PrepareCreate/Rotate/Reveal/Revoke, но не отдельные save/validate/publish/discard staged encrypted draft commands. | Не заявляется staged Secret acceptance по immediate create/rotate. |
| D7 | GetManagedConfigurationImpactRequest содержит только configuration_ref/revision_ref; environment/secret impact query реализованы в CP 98a71da1e и подключены к HTTP/SDK. | Осталась только managed impact query/page; environment/secret filtering до SQL LIMIT потреблено без локальной фильтрации. |

### Повторная Сверка CP 98a71da1e

Потреблён exact `98a71da1e7da9d0ceee2470a6c16e7351eea2e53` cherry-pick
`192c56459`: исходный Proto, generated API и принадлежащая CP реализация
перенесены без ручных правок. Это доступный независимый checkpoint D7, не merge
всей ветки CP. Public Proto теперь совпадает с этим CP HEAD. Новая policy
revision не нужна; существующие exact operations и права сохранены.

HTTP `getRuntimeEnvironmentImpact`/`getRuntimeSecretImpact` принимают query,
сохраняют pageSize/pageToken/ETag/total, отклоняют более 200 Unicode-символов,
NUL и malformed UTF-8 до RPC. CP выполняет trim, literal case-insensitive
search до LIMIT и связывает cursor с нормализованным запросом. Другая строка
поиска со старым cursor даёт 400, не пустую успешную страницу.

Фактически просмотрены public Proto и следующие реализации в CP WT:
`email_authorization.go`, `email_receipts.go` транспорта, mailbox projection
handoff и diff `fed22b1f6`/`b20884535`. Итог по прежним зависимостям:

- D1: public assistant request всё ещё только page/project_ref; нет query,
  archive RPC и archived state. HTTP pagination уже сохранена.
- D2: ModelCapability/ListModelCapabilitiesResponse по-прежнему без catalog
  revision/digest; текущие model/effort/account поля HTTP передаёт полностью.
- D3: TemplateVariable и ListTemplateVariablesRequest не получили target
  context/available/reason. Обычный project/query/page каталог сохранён.
- D4: `b20884535` корректно очищает codexSessionID при смене context digest
  в worker snapshot. Это не публичный previous/current diff для UI; D4 открыт.
- D5: CP handlers ResolveEmailAuthorization/Report/ResolveReconciliation и
  Get/Reconcile receipts реализованы. Их отсутствие больше не blocker.
  `fed22b1f6` задаёт authoritative mailbox policy и EMAIL minimum NONE;
  HTTP schema уже допускает NONE и HUMAN_EACH_EFFECT, не назначает gate сама.
  Startup import `CONTROL_PLANE_EMAIL_CONFIGURATION_FILE` не является UI RPC.
  Остались typed mailbox commands, write-only credential lifecycle и dynamic
  projection delivery/readback. HTTP worker endpoints не создавались.
- D6: отдельных staged Secret draft/save/validate/publish/discard RPC всё ещё
  нет; existing PrepareCreate/Rotate/Reveal/Revoke не подменяют этот lifecycle.
- D7: environment/secret query закрыты в HTTP. Managed impact query/page остаются.

`fed22b1f6` и `b20884535` отдельно не переносились: это CP/worker/shared catalog
реализация, не новая HTTP dependency. Root сохраняет их при общей интеграции.
Старые абзацы producer handoff о незавершённых EMAIL handlers не используются
как актуальное доказательство. Никакой CP или handwritten PWA файл вручную
не изменялся; full неизменившиеся gateway/PG/Docker suites не повторялись.

Локальные проверки этого изменения: targeted HTTP race
`TestImpactSearch|TestSecretImpact|TestSecretRebind|TestIdentityEnvironment|TestPublicRPCSurface`
с outer timeout 180s и Go timeout 120s — PASS; focused HTTP vet, строгая
OpenAPI validation kin-openapi 0.135.0, Go/TS generation и strict generated SDK
typecheck — PASS. Новая fixture проверяет exact RPC, query/cursor/ETag,
200 Unicode-символов, literal `%_`, NUL/malformed UTF-8/oversize отказ до RPC,
400 для stale-filter cursor и безопасные 403/404/503. Это HTTP fake boundary,
не повтор CP PostgreSQL либо live проверки.

## Проверки И Передача

`055d8e050`: новый model route/SDK и search/VFS hardening; focused race
HTTP/security/app, vet и gateway build PASS. Strict generated SDK typecheck
PASS, повтор Go/TS generation воспроизводим. Общий PWA test/build не повторялся:
handwritten ownership Newton. CP producer SQL, реальные provider и browser
acceptance: NOT RUN. Нет push/PR/merge/deploy.

Для Newton: `listModelCapabilities` принимает providerDefinitionKey,
providerAccountRef, query, pageSize, pageToken; возвращает models с supported/
default effort, exact eligible accounts, available/blockers. Не выбирать другую
модель при исчезновении selection. `listAssistantConversations` принимает
pageSize/pageToken и возвращает nextPageToken; query/archive пока не обещаны.

Повторная проверка набора инструментов не обнаружила `send_input` или другого
thread-message API. Прямая отправка в тред Newton
`01a06dcf-baa3-73b1-8ead-f6425d3454f6` технически недоступна; передача не
объявляется выполненной. Этот tracked документ содержит готовый контракт для
чтения из HTTP WT без изменения handwritten PWA. Gateway/SDK не зависят от
нового cherry-pick CP: исходные Proto уже совпадают с указанным checkpoint.

После добавления assistant pagination повторены focused race HTTP/security/app,
focused vet, `go build ./...` gateway и strict generated SDK typecheck: PASS.
Матрица всех 61 строк проверена по QA источнику; это карта ownership/evidence,
не 61 успешный E2E. D1..D7 блокируют полное продуктовое acceptance.

## Полная Независимая Проверка Gateway

На чистом `11401f0acadefd139e0b61616405e4d353d6b7ca` выполнены локально:

- `go test -race ./... -count=1 -timeout=180s`: PASS по всему gateway, включая
  session, revocation boundary, ratelimit, websocket, observability и usertext.
  Пакеты без tests отмечены Go отдельно, не считаются проверенными сценариями.
- `timeout 120s go vet ./...` и `timeout 120s go build ./...`: PASS.
- `timeout 600s docker build --target runtime --build-arg VERSION=11401f0acadefd139e0b61616405e4d353d6b7ca -f services/external/control-api-gateway/Dockerfile -t kodex-control-api-gateway:1045-11401f0ac .`:
  PASS. Existing Dockerfile, UID/GID `10001:10001`, canonical binary entrypoint;
  локальный image digest `sha256:18ee5b52db51d39e362f907540780c1f42fd0db50c26df92043b6f4835865078`.
  Это local build, не registry push либо deploy.
- `timeout 120s make gen-control-api-gateway-openapi-go gen-openapi-ts`, затем
  `git diff --exit-code`: PASS, повторная генерация не меняет tracked files.
- Из PWA каталога только generated SDK:
  `timeout 120s ./node_modules/.bin/tsc --noEmit --strict --skipLibCheck --target ES2022 --module ESNext --moduleResolution bundler --lib ES2022,DOM src/shared/api/generated/openapi/index.ts`:
  PASS; handwritten PWA не проверялся и не редактировался.

Отдельная строгая validation:

```sh
timeout 120s go run github.com/getkin/kin-openapi/cmd/validate@v0.135.0 \
  contracts/openapi/control-api-gateway/v1/openapi.yaml
```

Первый запуск: FAIL, `SystemSTTConfigurationDraftInput` содержал лишнее YAML
поле `не произвольным LLM catalog.`. Причина: запятая внутри некавыченного
description в flow mapping. Исправлены кавычки в исходном OpenAPI, Go/TS
описания регенерированы. Повтор строгой validation: PASS; flags, отключающие
defaults/examples/patterns, не использовались. Runtime schema bounds и STT
policy не менялись. Проверены Context7 `/getkin/kin-openapi` (CLI и
`Validate`) и фактический source `cmd/validate` закреплённой версии 0.135.0,
совпадающей с dependency установленного oapi-codegen 2.7.1.

Итоговый проверенный code SHA:
`83609eeb0e85e73cb70f93679ea4da0e06b9428f`. После исправления на нём повторены
все перечисленные проверки: полный gateway race с внешним `timeout 300s` и
внутренним `-timeout=180s`, full vet/build, строгая OpenAPI validation,
Go/TS codegen с чистым readback и strict SDK typecheck — PASS.
Docker runtime повторно собран из того же Dockerfile с `VERSION`, равным
этому exact SHA: PASS; local image
`kodex-control-api-gateway:1045-83609eeb0`, digest
`sha256:ac22fd678349281cf8db9f0c8dcacac58ef1365b7d0e0c66b5f6ad827771212c`.
Последующая запись этого evidence меняет только документацию, не проверенный код.

NOT RUN: real CP protected integration по незавершённым D1..D7, browser E2E,
live provider, staging/cluster. Никаких новых acceptance-требований эта
проверка не вводит; код CP, handwritten PWA и Dockerfile не изменялись.

## Текст PR Для Root

Связь: #1045, #1021, #1018. Полная HTTP интеграция доступных CP Query/Command/
Assistant methods, managed lifecycle, global catalogs, Skills/Memory/bindings,
STT bootstrap/configuration, EMAIL safe receipt/fresh one-use reconciliation.
Дополнительно устранены потерянные model capabilities и assistant pagination,
закреплены schema/SDK и negative response validation.

Ручная проверка: открыть org catalogs без project; получить account model page
и следующую страницу; сохранить managed draft и использовать новый revision/ETag;
пройти Skill/Memory binding с agentVersion; прочитать EMAIL receipt и подтвердить
его exact digest через fresh receipt-bound session; проверить повторный отказ.
Недоступный CP возвращает ошибку, не пустой успешный результат.

Риски: новые обязательные typed поля уже существующих CP responses требуют
согласованного PWA consumption; cookie elevation одноразовая, после неизвестного
исхода сначала authoritative read. Rollback только согласованного gateway/SDK
release, без rollback CP revisions или выдачи более широкого grant. Новых
миграций/Secrets/портов этот HTTP пакет не вводит. Секреты не раскрывались.

PR не переводится в full acceptance до закрытия D1..D7 либо явного решения
владельца по каждой границе. Это не предложение исключить требования #1018.
