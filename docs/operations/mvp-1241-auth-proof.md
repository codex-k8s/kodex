---
id: OPS-DOC-1241
title: Авторизация, provider accounts и одноразовые WS tickets MVP
type: operation-evidence
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Область и результат

Issue [#1241](https://github.com/codex-k8s/kodex/issues/1241), общая приёмка
[#1031](https://github.com/codex-k8s/kodex/issues/1031). База реализации:
`6061aedc558330d65215e7512df6fd457cfb9e78`. Матрица относится к исходникам
этого PR; точный итоговый SHA и команды локальных проверок закреплены в PR.
Первая волна уже включена в базу. Старые cookie defects #1192/#1218 повторно
не реализуются. Ни deploy, ни обращения к живому provider здесь не выполнены.

Исправлены три исполняемых разрыва: бесполезный idle refresh возле абсолютной
границы; отсутствие одноразового WS ticket; неправильный RPC для pending
device polling и отсутствие recovery первоначального запроса. Временный
отказ device start/re-auth теперь имеет собственный безопасный problem code.

## Сквозная матрица требований

| Требование | Владелец и исполняемый путь | Локальное доказательство | Остаток #1031 |
| --- | --- | --- | --- |
| MVP-UI-11: expiry/две вкладки | Gateway `session.Families` -> `boundary/browser.go` -> GET/PUT `/api/v1/session` -> PWA `session/store.ts`, `renewal-coordinator.ts` | `family_test.go`: 42 no-op вызова не меняют version/CAS, access expiry до absolute по-прежнему требует refresh; `store.test.ts`: две Pinia-вкладки делят один PUT и обе завершаются на исходном deadline; Web Locks test держит lock до завершения promise | Две реальные вкладки с natural refresh, focus/suspend и легитимной OIDC session: NOT RUN |
| MVP-UI-11: WS ticket/cursor/re-auth | Cookie/CSRF boundary -> POST `/api/v1/session/ticket` -> encrypted ledger общего browserstate -> CAS consume перед Upgrade `kodex.session.v2` -> `realtime/ticket.ts`/`store.ts` -> SESSION_RESUME с platform/Run cursors | `websocket_ticket_test.go`: две копии gateway owner на одном CAS, один победитель consume, независимый билет второй вкладки, replay/expiry/browser/CSRF/version/revoke/лимит; realtime integration: новый ticket, сохранённые cursors, terminal stop, поздний ответ после close | Реальные соседние Pods/restart, Keycloak refresh и WS stream: NOT RUN |
| MVP-UI-17/18: account/model/effort | Secret-broker `providercredential/model_catalog_api.go` пересекает credentialed `/v1/models` с проверенным capability source; device catalog принадлежит adapter; CP `model_catalog_*` и `session_catalog_affinity.go` закрепляют account/catalog pins -> generated model endpoint -> PWA `providers/model-catalog.ts`, selectors | Broker providercredential/grpc unit; CP domain unit и два теста canonical observation; PWA model-catalog/usage tests. API-key source не содержит Spark; отсутствие модели не заменяется другой | Реальный account-specific rollout всех ожидаемых моделей и запуска Session/Turn с каждым допустимым effort: NOT RUN |
| MVP-UI-19: readiness | CP authoritative account/model catalog/usage context -> provider account readback -> `ProviderAccountSelector`/`usage.ts`; credential health не является provider-wide SLA | PWA provider model/usage/selector tests; broker diagnostics tests; UNKNOWN/NOT_EVALUATED не подменяются READY | Проверка реальных credential, capacity, grants и каждой причины в dropdown: NOT RUN |
| MVP-UI-21: TOML | `libs/go/runtimecontract/config_overlay*.go` -> CP schema/history owner -> gateway schema/history endpoints -> PWA `overlay-editor.ts`/`overlay-history.ts` | Runtimecontract suite; editor/history unit. Закрытые model_reasoning_effort, personality, allow_login_shell=false, history.persistence; model/provider/credentials/policy не становятся UI-owned | Disposable PG draft/validate/publish/rollback и визуальные completion/hover/диагностика: NOT RUN |
| MVP-UI-51: revoke/delete/cleanup | CP specialized commands -> `provider_account_deletion.go`/blockers/credential cleanup -> generated gateway -> PWA lifecycle panel/store; DELETING закрывает новые назначения, terminal после cleanup сохраняет audit/history | CP domain unit, gateway provider/readback unit, PWA lifecycle/model/store tests; tombstone удаляется из обычного PWA списка | PG blockers active/warm/agent/pool/automation, фактический durable cleanup и pinned history: NOT RUN |
| MVP-UI-52: device lifecycle | PWA start/re-auth сохраняет исходные key/OCC -> CP durable reservation -> credential materializer; PENDING polling идёт через `/authorization-refresh`, AUTHORIZED check через `/device-authorization/verification`; recovered initial challenge возобновляет polling | PWA initial UNKNOWN exact retry, pending -> authorized, modal close stops polling; CP provider credentials tests; gateway typed 503/redaction/correlation/Retry-After tests | Настоящие first/re-auth/check, provider expiry/cancel, потерянный ответ, active-turn blocker и смена credential revision: NOT RUN |

## Lifecycle и authority

| Сценарий | Источник authority, переход и ограничения | Событие / read path |
| --- | --- | --- |
| Session create | Same-origin OIDC Code+PKCE, verified identity; encrypted family ACTIVE | GET `/api/v1/session`; ticket не выдаёт OIDC tokens |
| Idle/access renew | Cookie+CSRF; durable family CAS и неизменяемые issuer/sub/organization/sid/auth_time; absolute не растёт | Metadata version меняется только при реальном renewal |
| Absolute/idle expiry, logout, revoke | Owner state закрывает authority; stale callback/refresh не восстанавливает session | Авторитетное чтение; PWA прекращает timer/reconnect и сохраняет только безопасный return route |
| Ticket issue | Только свежая BFF family и cookie/CSRF; максимум 16 live digest entries, TTL <=30 секунд и owner deadlines | Отдельный encrypted ledger с собственным AAD; domain event отсутствует |
| Ticket consume/claim | Fresh family binding, точная logical version; CAS удаляет digest до Upgrade; после CAS повторно проверяется owner | Отсутствие digest авторитетно означает consumed/expired; replay закрыт |
| Ticket unknown/failure | Никакого слепого retry CAS при неизвестном исходе; upgrade не выполняется | Следующий connect запрашивает независимый ticket, bounded retry |
| Ticket expiry/cleanup | Prune expired/stale entries при выдаче; record retention наследует существующий browserstate | Read/CAS owner, без новой broker subscription, grant или фоновой job |
| Device start/re-auth | Fresh owner permission, original idempotency/OCC, durable reservation, один materializer attempt | При UNKNOWN PWA хранит только безопасный intent, ручной exact retry; старый intent нельзя заменить новой командой |
| Device pending/check | Pending наблюдает прежний materializer attempt; authorized verify ставит отдельное credentialed catalog observation | Авторитетный account/readback; polling прекращается при terminal/expiry/close/account switch |
| Provider disable/revoke/delete | Разные специализированные команды; owner blockers и cleanup-транзакции сохраняют действующие pins и audit | Существующие provider events/readback; terminal tombstone исключён из обычного каталога |

Новый ledger не добавляет поля в JSON `Family`, не двигает её logical version
или storage sequence и не вызывает отказ старого family decoder при смешанных
версиях API. Новый ticket одной вкладки не инвалидирует билет соседней.
Доставка и проверка ledger используют уже материализованные browserstate stream,
ключи, CAS и readiness gateway; новых infra Secrets/subjects/permissions нет.

`PROVIDER_DEVICE_AUTHORIZATION_UNAVAILABLE` возвращается для временного
Unavailable/DeadlineExceeded от start/re-auth path: HTTP 503, correlationId,
retryable=true, `Retry-After: 1`, локализованный безопасный title. Raw upstream
message не выдаётся. Authority failure остаётся 401/403. PWA сохраняет модалку
и исходную account, recovery использует тот же ключ и исходную версию.

## Локальные проверки

Все команды выполняются из данного worktree, без live DSN, SSH, cluster или
платных provider calls. Результат привязывается к итоговому SHA в PR.

| Проверка | Команда | Результат |
| --- | --- | --- |
| Gateway unit и binary build | В `services/external/control-api-gateway`: `GOMAXPROCS=4 TMPDIR=/tmp go test -p 2 ./internal/app ./internal/security/session ./internal/security/boundary ./internal/transport/http ./internal/transport/websocket ./internal/usertext`; `GOMAXPROCS=4 go build -p 2 -o /dev/null ./...` | PASS |
| Broker model/device boundary | В `services/internal/secret-broker`: `GOMAXPROCS=4 TMPDIR=/tmp go test -p 2 ./internal/providercredential ./internal/transport/grpc` | PASS |
| CP provider orchestration | В `services/internal/control-plane`: `GOMAXPROCS=4 TMPDIR=/tmp go test -p 2 ./internal/domain/service/platform` | PASS |
| CP canonical observations | В том же модуле: `GOMAXPROCS=4 TMPDIR=/tmp go test -p 2 ./internal/repository/postgres/platform -run 'TestModelCatalogContentIdentityExcludesFreshnessAndCredentialButBindsCapabilities\|TestModelCatalogObservationFailsClosedWithoutRemoteEvidence'` | PASS, только unit без PG |
| Overlay runtime | В `libs/go/runtimecontract`: `GOMAXPROCS=4 TMPDIR=/tmp go test -p 2 ./...` | PASS |
| PWA unit | В `services/staff/control-center`: `npm run test:unit -- src/features/session src/features/providers src/features/realtime src/features/agents/detail/overlay-editor.test.ts src/features/agents/detail/overlay-history.test.ts` | PASS |
| PWA type/build | `npm run build` | PASS; существующее предупреждение Vite о размере chunk |
| Scoped frontend lint | `eslint` всех изменённых handwritten TS/Vue и synthetic fixture с `--max-warnings 0` | PASS |
| Transition guards | Из корня: `node --test tools/release/websocket-transition.test.mjs` | PASS, 3 теста |
| Contract codegen | `oapi-codegen -config tools/codegen/openapi/control-api-gateway-go.yaml contracts/openapi/control-api-gateway/v1/openapi.yaml`; PWA `npm run generate:openapi`, `npm run generate:asyncapi`; Go generated `gofmt` | PASS; AsyncAPI parser рекомендует 3.1.0, профиль остаётся 3.0.0 |
| Browser/PG/live acceptance | Настоящие OIDC/provider/WS, disposable PG lifecycle и browser suite | NOT RUN, остаются #1031 |

Context7 проверен: MDN `/mdn/content`, Web Locks `request`/`ifAvailable`,
удержание exclusive lock до завершения promise. Новые внешние provider API
не добавлялись; существующий account catalog и capability provenance сохранены.

## Совместимый выпуск и retirement

Следующие команды предназначены для будущего owner-authorized staging release.
В данном PR они не исполнялись. `KODEX_RELEASE_CONTEXT` и cutoff назначает SRE;
script не читает project `.env`, credentials или owner keys.

1. Выбрать абсолютный UTC cutoff не дальше 24 часов. Preview читает только
   точный staging/hot-reload Deployment `kodex-system/control-api-gateway`.

```sh
node tools/release/websocket-transition.mjs \
  --context "$KODEX_RELEASE_CONTEXT" --until "$KODEX_WS_LEGACY_UNTIL"
```

2. После owner OK применить тот же переход. Script использует UID/resourceVersion
   CAS, меняет только env приложения и служебную retirement annotation,
   выполняет exact spec readback и bounded rollout status. Ни images, ни
   sidecars, ни trust/Secrets не меняются.

```sh
node tools/release/websocket-transition.mjs \
  --context "$KODEX_RELEASE_CONTEXT" --until "$KODEX_WS_LEGACY_UNTIL" \
  --confirm WS_TICKET_TRANSITION
```

3. Обновить все API readers текущим scoped application release и проверить
   readiness/точный SHA. До завершения всех API readers новую PWA не публиковать.
   Только затем обновить PWA: она использует `kodex.session.v2` и не имеет
   fallback к v1. Проверить новую сессию, две вкладки, refresh и cursors.

4. Завершить миграцию, исключив старые v1 клиенты, и убрать cutoff:

```sh
node tools/release/websocket-transition.mjs \
  --context "$KODEX_RELEASE_CONTEXT" --until retire --confirm WS_TICKET_TRANSITION
```

Script запрещает extension прежнего cutoff и включение после retirement.
Истёкший cutoff сам закрывает новые v1 handshakes, но не ломает readiness или
перезапуск API. Уже принятые sockets не получают продления identity expiry.
30 секунд жизни ticket не являются freshness bound отзыва из #1221.

Rollback новой PWA допустим до retirement только внутри действующего cutoff:
сначала вернуть совместимую PWA v1, дождаться ухода всех v2 клиентов и sockets,
затем рассматривать старый API. Откат API под живой PWA v2 запрещён: старый API
не умеет ticket endpoint/protocol. После cutoff/retirement нужен совместимый
ticket-capable API/PWA rollback либо отдельное owner-решение о maintenance;
автоматическое снятие ticket boundary или повторное включение v1 запрещены.

Обновлены repo-owned direct WS consumer `realtime/store.ts`, его integration
fixture и synthetic browser API fixture; Go protocol tests используют v2.
Отдельных direct WS clients в `tools/`/`scripts/` не обнаружено. Старый private
qa1466 renewal driver не является файлом PR: release-агент обновляет его до
POST ticket перед каждым новым v2 socket, сохраняет cursors и не пишет ticket,
CSRF, cookies или subprotocols в evidence.

## Ручная приёмка

Открыть две вкладки с новой легитимной OIDC session. Наблюдать естественное
продление без искусственного увеличения expiry, затем абсолютный deadline и
один re-auth flow. После refresh убедиться в новом ticket, непрерывных cursor
и отсутствии дублей. Проверить потерянный initial device response, typed 503,
exact retry, pending -> authorized, explicit verify/re-auth, expiry/cancel,
закрытие модалки и terminal delete. Затем account/model/effort, overlay,
все blockers удаления и cleanup с сохранением immutable history. Результаты
реального окружения записать в #1031 отдельно от локальных PASS этого PR.
