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
