---
id: OPS-INSTRUCTIONS-1046
title: Привязка Instructions и предварительный план публикации
type: operations
status: approved
owner: control-plane
version: 1.0.0
updated: 2026-09-05
---

# Источник исполнения

Agent хранит server-owned instruction binding с ref/version и точной
опубликованной revision. Backfill фиксирует фактически выбранную последнюю
публикацию; исторические версии и старые RuntimeRevision не меняются.
Managed PROMPT_TEMPLATE binding сохраняет прежний приоритет. Поле effective
показывает, выбран ли собственный Instructions fallback сейчас. Его публикация
не снимает чужой managed binding и не выдаёт неиспользуемый fallback за
изменение текущего prompt.

Защищённый GetAgent возвращает безопасный binding snapshot; mutation receipt
содержит Agent ref/version и итоговый план, затем клиент читает GetAgent.
PublishedInstructions сохраняет последнюю публикацию для истории; выбранную
revision определяет binding. Runtime claim, preview, attachment eligibility,
warm assistant и bootstrap используют точную привязку. Неизвестная либо
повреждённая связь закрыто отклоняется, fallback к MAX(version) отсутствует.

# Матрица владельца

| Переход | Полномочия и OCC | Изменение | Факт или чтение |
| --- | --- | --- | --- |
| Create Agent | текущая project/Agent authority | первая publication и binding в одной TX | существующий AGENT_CHANGED |
| Create/save draft | текущая agent.manage до OCC/replay | mutable draft, Agent version; binding сохранён | receipt/audit и GetAgent, без INSTRUCTIONS_PUBLISHED |
| Validate | текущая agent.manage и actual prompt context | VALID/INVALID; binding сохранён | receipt/audit и GetAgent, без INSTRUCTIONS_PUBLISHED |
| Publish | текущая agent.manage, exact draft/Agent OCC | immutable publication; выбранный binding effect по плану | INSTRUCTIONS_PUBLISHED, receipt/audit и GetAgent/GetRevisionImpactPlan |
| Rollback | текущая agent.manage, exact опубликованный source и Agent OCC | новая immutable publication и явная активация | INSTRUCTIONS_PUBLISHED, receipt/audit и GetAgent |
| Bootstrap/core advance | server-owned installation identity | точный core revision и binding в одной TX | прежний core lifecycle/audit |
| Revoke/archive | актуальная owner eligibility | новые mutation/claim закрыты | существующий Agent read/action path |

Новая binding identity не удаляется и не переиспользуется; version монотонна.
FK и trigger проверяют exact organization/Agent/published revision. Публикация
не создаёт выдуманную Workflow/Automation связь: эти consumers получают
Instructions через фактически выбранного Agent и новую RuntimeRevision.

Binding foundation прошла полный Bootstrap26.819s, включая сохранение binding
при Validate, actual Publish/Rollback, запрет скачка version/чужой Agent revision
и точный attachment dependency после нескольких публикаций. Совмещённый
owner70 прошёл полный Bootstrap27.449s. Последующий отдельный PostgreSQL0.484s
подтвердил publication с пустым выбором, NOT_SELECTED, сохранение прежнего
active binding и exact replay. Три package race, полный vet/build,
SQL/Proto replay/policy70/ABI/web-only release PASS. Live/browser нового
Instructions impact не выполнялись; сквозная приёмка MVP47 остаётся открытой.
