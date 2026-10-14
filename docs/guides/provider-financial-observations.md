---
id: GO-DOC-007
title: Наблюдения лимитов и credits Codex
type: guide
status: approved
owner: developer
version: 0.2.0
updated: 2026-10-14
---

# Наблюдения лимитов и credits Codex

Реализация Issue1796 использует supported `account/rateLimits/read` Codex 0.160.0. Точный upstream source — `rust-v0.160.0`, commit `a956835d020762cb2b570053af06f643a11c0ecc`; owner packet.md, SHA256 `a382233ba184727cce10e7b10ff0a7030b4e65722f4c5a586d61b7d0ba60270b`, прочитан native до EOF467840. Текущий architecture-review.md@33 имеет verdict `PASS_CODE_ONLY_BOUNDED_DESIGN`. Контракт и fixtures не измеряют баланс владельца и не доказывают live acceptance.

## Путь данных

Обычный авторизованный managed process после `account/read` для ChatGPT делает пассивное чтение с `supportsLunaReserve=false`, `excludeResetCreditDetails=true`. Новый collector требует private identity из проверенного immutable managed snapshot с `auth_mode=chatgpt`; external-token snapshot не активирует finance reader. Отсутствующий binding не назначается из email, routing или notification. Ответ сверяется с private expected accountId и process/auth epoch; missing/null остаётся unavailable, mismatch отклоняется без выдачи ID.

Финальное чтение выполняется до остановки активного процесса. `account/rateLimits/updated` только будит повторное чтение; из notification нельзя получить account authority. Чтения ограничены 64 наблюдениями на execution: обычные чтения используют максимум63, последний слот резервируется для финального read. Достигнутый предел очищает wake без дополнительного RPC. Дополнительный idle process, копия credentials и internal-only `chatgptAuthTokens` для finance не используются. Новый external-token auth/deployment gate этим изменением не разрешается.

Broker передаёт закрытый `ProviderFinancialObservation`, отдельно от model messages и ToolCall. Sequence проверяется в sealed stream. Controller проверяет execution binding и RuntimeRevision digest; CP выводит account/current credential из immutable revision. Lease/fence/generation/expiry, root/node/session/turn/attempt, текущая credential и порядок collector проверяются перед записью. Root lock сериализует запись с terminal transition. Account lock защищает collector ordering; старое валидное наблюдение отбрасывается, sequence с изменённым содержимым отклоняется как idempotency reuse. Проекция, account version, command receipt/audit и outbox записываются одной существующей command transaction.

Существующий `PROVIDER_ACCOUNT_CHANGED` приводит к защищённому `PROVIDER_ACCOUNT` snapshot общего WebSocket. Cursor CP закрепляет источник всех страниц; gateway заканчивает bounded discovery до ranking, не смешивает drift. При изменении agent/membership eligibility используется штатный resync. REST polling финансовых данных отсутствует; REST сохраняет bootstrap/detail-проекции того же account.

## Защищённый subset

`financialEligible` назначает CP после `provider.account.view` для resource instance. В organization scope требуется `organization.manage`; в project scope учитываются только current candidates/selected account agents, для которых доступен `agent.manage` или `agent.launch`. При смене scope/session, resync и stream problem финансовое состояние очищается.

Идентификатор upstream account, email, diagnostics, upsell и arbitrary provider JSON в финансовую проекцию не входят. Сохраняются только codex primary/secondary windows, точная строка credits balance, hasCredits/unlimited, nullable ordinaryUsageAllowed/spendControlReached и закрытый restriction. Неподдерживаемый bucket, malformed/duplicate/oversized JSON и неизвестные wire fields дают UNAVAILABLE. Имена полей response, codex bucket, windows и credits закреплены allowlist точного upstream source. Известные metadata не становятся публичной authority. Поля supported subset проверяются независимо: недоступный balance не удаляет достоверное окно или flags. Sparse/null остаётся unknown; значение не подменяется нулём.

UI показывает до трёх eligible accounts по минимальному известному included remaining среди окон; ties — по opaque ref. Unknown не участвует в числовом сравнении и явно помечает неполноту порядка. Exhausted группируется счётчиком, credits остаются отдельно. Unlimited credits не означает unlimited included window. Balance не преобразуется в число, USD или tokens; единицы неизвестны. Reset показывается только по upstream seconds, наступление даты не объявляется восстановлением лимита.

Порог 5 минут в UI — индикатор устаревания отображения, не policy запуска или expiration upstream. Локальный clock обновляет только подпись, не делает network requests. При потере live WS значения считаются устаревшими.

Weekly exhaustion и ordinaryUsageAllowed=false не добавляют локальный запрет запуска. Проекция не меняет существующий admission и не обходят upstream spending/account restrictions. Автоматического повторения billable turn нет. Optional callback transport/5xx не прерывает execution и не повторяет запись при неизвестном исходе; authority/integrity rejection остаётся ошибкой. Для upstream RPC мягко обрабатываются только supported-method absence (-32601) и точная статическая no-snapshots ошибка pinned account_processor.rs, после завершённого коррелированного RPC. Другие protocol/auth/transport/policy failures сохраняют закрытый gate.

## Проверки и ограничения

Поддерживаемые точки входа: Go tests runtimecontract/provider_finance\*, runner internal/codex finance scenarios, CP financial collector/receipt и entity ranking, callback/HTTP/WS wire tests; Node financial-summary.node.test.mjs; Web-first baseline по GOV-DOC-003. Fixtures включают почти60K decimal string, presence/null/unknown/unlimited, malformed/duplicate/unknown fields, budgets, identity/epoch, method absence, последний зарезервированный read, eligibility-before-counts, сортировку, reset и stale. Fixture не является upstream measurement.

Protected CP overview сканирует полный eligible набор до counts/ranking и выдаёт только три account rows с полными total/exhausted/unknown. Отсутствие финансового snapshot отличается от авторитетного пустого набора. Browser validator проверяет closed fields, канонический OpaqueRef, тип timestamp и Unicode границы name. Общий frame budget сохраняется; oversize не превращается в успешный усечённый top-three.

В текущем implementation выполнены адресные parser/collector и Node tests, CP collector/receipt unit, Proto lint/build, AsyncAPI validation и воспроизводимая generation, frontend typecheck. Эти локальные результаты не доказывают полный baseline. Полная runner codex suite завершилась FAIL: UDS/trusted filesystem fixtures и явный запрет чтения /proc/self/exe. Запрещённый READ не повторялся и не обходился. Штатный Proto codegen check завершился FAIL из-за недоступности remote plugin server; доступность других инструментов из этого отказа не выводится. Точные команды, SHA и оставшиеся проверки фиксируются в implementation evidence.

PostgreSQL atomic rollback/race/tenant/lifecycle/replay, browser desktop/mobile и Console/Network/backend, live owner acceptance требуют собственных фактических результатов. Отсутствующая disposable среда сохраняется NOT RUN; сборка и unit tests не заменяют эти доказательства.

Реальный кейс владельца «weekly exhausted + почти60K paid credits» сохраняет отдельные UNKNOWN для actual balance, единиц, charging и supported paid continuation. Deployment/auth applicability нового пути остаётся отдельным gate. Изменения не разрешают deploy, merge или owner approval. После завершения Developer предусмотрен один независимый comprehensive review actual полного business diff на exact SHA, затем Manager acceptance. Финальный owner gate OPEN/WAITING_HUMAN.
