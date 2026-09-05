---
id: OPS-DOC-1046-RUNTIME-CATALOG
title: Exact model catalog и schema ConfigOverlay
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

# Exact model catalog и schema ConfigOverlay

Источники: Issue #1046, Epic #1018, MVP-UI-17/18/21. Этот документ описывает
контракт вклада в единый producer unit; декларация Proto сама по себе не
подтверждает исполняемый owner path.

## Сквозной путь

| Сценарий | Authority и owner | RPC / HTTP consumer | OCC, эффект и readback |
| --- | --- | --- | --- |
| Выбор модели | Проверенный actor; существующая owner eligibility account | ListModelCapabilities / typed model catalog | Snapshot account-scoped: providerDefinitionKey + accountRef; revision `mcat_<lower64>`, digest lower64; pagination не заменяет exact pin |
| Публикация runtime configuration | Существующая команда agent runtime configuration, server-resolved agent/project | PublishAgentRuntimeConfiguration / typed gateway mutation | Agent If-Match и idempotency; каждый candidate имеет exact catalog pin, доступную выбранную модель; default effort вычисляет owner и сохраняет immutable policy snapshot |
| Редактор TOML | Existing agent.view, owner read транзакция | GetAgentRuntimeConfiguration / runtime configuration view | Versioned schema содержит только разрешённые поля, значения, completion/hover; schema digest покрывает весь опубликованный descriptor |
| Draft / validate | Existing agent configuration authority | CreateConfigOverlayDraft / ValidateConfigOverlayDraft | Agent OCC, idempotency, bounded payload; syntax/type/unknown/protected errors закрыты до использования частичной модели; typed diagnostics не содержат исходных значений |
| Publish / rollback overlay | Existing agent configuration authority | PublishConfigOverlayDraft / RollbackConfigOverlay | Повторная проверка canonical TOML и совместимости effort всех policy candidates; fresh immutable revision, digest и authoritative view |
| Новый turn / materialization | Server-owned run/session lineage и существующий claim | Existing worker claim/materialization | Exact pin выбранного candidate и текущая eligibility проверяются повторно; drift блокирует новый turn до republish, не меняет старый RuntimeRevision |

Все команды сохраняют существующую атомарную owner-транзакцию состояния,
idempotency receipt, audit и обязательного domain event. Нового event kind
этот вклад не вводит: каталог/schema читаются через существующий защищённый
read path, конфигурационный consumer получает существующую runtime revision.

## Наблюдение каталога

Worker control-plane выбирает только enabled/AUTHORIZED account с текущей
credential той же organization/account. Claim транзакционно сохраняет immutable
task, account version, credential revision, claimant/fence и SHA-256 точного
protobuf до выдачи proof. Lease ограничена 15 секундами; consumer оставляет
одну секунду на owner completion. Poll выполняется каждые 5 секунд по одной
задаче; успешное или безопасно неуспешное наблюдение обновляется через 5 минут,
freshness ограничена 15 минутами. Startup barrier и cancel/join принадлежат
общему lifecycle процесса; transport denial не превращается в provider result.

Broker проверяет mTLS и exact owner proof до чтения credential и возвращает
только verified remote capabilities либо закрытый failure без models.
Владелец в completion повторно проверяет всю task/account/credential/lease
связь и сохраняет неизменяемый receipt. Terminal replay требует тот же request
digest и receipt; иной результат отклоняется. Expired/revoked claim закрывается
в CANCELLED, новая попытка получает новый task/ref/fence. Отдельного domain
event для claim, observation, failure и expiry нет: authoritative read path —
`ListModelCapabilities`, а immutable task/observation rows сохраняют audit
источника, времени, request и результата.

`catalog_status` доступен для account-scoped запроса: PENDING, READY, FAILED
или EXPIRED; `observed_at/expires_at/source/failure` не включаются в content pin.
Пустой verified READY catalog допустим. После failure старый успешный catalog
может показываться только как unavailable; builtin/cache fallback не становится
доказательством availability. Definition и account queries не содержат второго
локального списка моделей.

Capabilities content pin включает tenant/account/provider identity, источник
и canonical models/defaults/efforts. Operational credential refresh не меняет
этот pin сам по себе: credential revision остаётся отдельной обязательной
связью task, observation receipt, freshness и RuntimeRevision. Старое наблюдение
не подходит новой current credential даже при том же content pin. После нового
verified наблюдения неизменных capabilities продолжение возможно автоматически;
изменённые модели/default/efforts/source требуют новой публикации.

## Session affinity

По ADR-MC-004 Session account не выбирается повторно при смене policy.
Migration 636 добавляет server-owned immutable Session binding исходной
account policy и полного безопасного каталога. Обычный Run, Assistant
Conversation, delegated child и новая warm Session сохраняют binding в своей
create транзакции. Bootstrap до первого remote observation остаётся без usable
pin; warm reconciler заменяет такую подготовительную Session новой pinned
Session. Legacy Session без binding не получает новый executable snapshot.

На следующем turn модель и TOML берутся из свежей runtime configuration.
Если её policy содержит уже выбранный Session account с новым опубликованным
pin, используется этот approved pin без повторного выбора account. Иначе
проверяется retained Session catalog, а RuntimeRevision сохраняет original
provider policy provenance вместе со свежим runtime configuration. Любой путь
проверяет current account/credential и свежесть наблюдения; отсутствие account
в новой policy само по себе не является revoke. Старые RuntimeRevision и
Session binding не переписываются.

Warm materialization системного ассистента проверяет ту же Session binding,
выводит effort/mode из exact catalog и включает оба поля в revision digest.
При замене недоступной warm Session сервер публикует новую policy только из
свежих eligible candidates. Чтение и изменение runtime configuration/overlay
системного ассистента требуют `organization.manage`; обычное право
`agent.view|agent.manage`, включая wildcard Agent scope, его не заменяет.

## Публичные поля

`ProviderAccountCandidate.catalog_revision/catalog_digest/provider_definition_key`
обязательны для новой пользовательской публикации. `default_reasoning_effort`
является output полем: nonempty input отвергается; owner берёт default из
exact модели. Пользовательское значение задаётся только
`model_reasoning_effort` в ConfigOverlay. При отсутствии override используется
сохранённый default конкретного выбранного candidate.

`ConfigOverlaySchema` имеет content-addressed revision/digest, максимум 65536
байтов и ровно четыре разрешённых leaf field: `model_reasoning_effort`,
`personality`, `allow_login_shell`, `history.persistence`. Boolean completion
содержит только false. Effort values соответствуют допустимому пересечению
capabilities policy candidates, а не глобальному enum UI. Description/hover
не являются исполняемым authority input.

Диагностика содержит закрытый code, безопасный key и координаты от 1;
неприменимые координаты равны 0. Raw parser message, TOML value и credentials
не возвращаются. Отсутствие возможности определить безопасный key не
разрешает подставлять произвольный исходный текст.

## Проверка

Обязательны negative сценарии wrong actor/account, stale digest, модель вне
account catalog, несовместимый effort, подмена default, draft parser failure,
protected/unknown field и повторная проверка fresh materialization. Runtime
unit/PG результаты фиксируются отдельно на exact executable SHA. Live и
deployment не входят в этот локальный вклад.
