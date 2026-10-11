---
id: GO-DOC-007
title: Наблюдения лимитов и credits Codex
type: guide
status: draft
owner: developer
version: 0.1.0
updated: 2026-10-11
---

# Наблюдения лимитов и credits Codex

Реализация Issue1796 использует supported `account/rateLimits/read` установленного Codex 0.160.0. Контракт подтверждён schema установленного бинаря и architecture-review.md@23, semantic PASS_CODE_ONLY. Это не измерение реального баланса владельца и не live acceptance.

## Путь данных

Обычный авторизованный managed process после `account/read` для ChatGPT делает пассивное чтение с `supportsLunaReserve=false`, `excludeResetCreditDetails=true`. Финальное чтение выполняется до остановки активного процесса. `account/rateLimits/updated` только будит повторное чтение; из notification нельзя получить account authority. Чтения ограничены 64 наблюдениями на execution. Дополнительный idle process, копия credentials и internal-only `chatgptAuthTokens` не используются.

Broker передаёт закрытый `ProviderFinancialObservation`, отдельно от model messages и ToolCall. Sequence проверяется в sealed stream. Controller проверяет execution binding и RuntimeRevision digest; CP выводит account/current credential из immutable revision. Lease/fence/generation/expiry, root/node/session/turn/attempt, текущая credential и порядок collector проверяются перед записью. Root lock сериализует запись с terminal transition. Account lock защищает collector ordering; старое валидное наблюдение отбрасывается, sequence с изменённым содержимым отклоняется как idempotency reuse. Проекция, account version, command receipt/audit и outbox записываются одной существующей command transaction.

Существующий `PROVIDER_ACCOUNT_CHANGED` приводит к защищённому `PROVIDER_ACCOUNT` snapshot общего WebSocket. Cursor CP закрепляет источник всех страниц; gateway заканчивает bounded discovery до ranking, не смешивает drift. При изменении agent/membership eligibility используется штатный resync. REST polling финансовых данных отсутствует; REST сохраняет bootstrap/detail-проекции того же account.

## Защищённый subset

`financialEligible` назначает CP после `provider.account.view` для resource instance. В organization scope требуется `organization.manage`; в project scope учитываются только current candidates/selected account agents, для которых доступен `agent.manage` или `agent.launch`. При смене scope/session, resync и stream problem финансовое состояние очищается.

Идентификатор upstream account, email, diagnostics, upsell и arbitrary provider JSON в финансовую проекцию не входят. Сохраняются только codex primary/secondary windows, точная строка credits balance, hasCredits/unlimited, nullable ordinaryUsageAllowed/spendControlReached и закрытый restriction. Неподдерживаемый bucket, malformed/duplicate/oversized JSON и неподдерживаемое значение дают UNAVAILABLE. Sparse/null остаётся unknown; значение не подменяется нулём.

UI показывает до трёх eligible accounts по минимальному известному included remaining среди окон; ties — по opaque ref. Unknown не участвует в числовом сравнении и явно помечает неполноту порядка. Exhausted группируется счётчиком, credits остаются отдельно. Unlimited credits не означает unlimited included window. Balance не преобразуется в число, USD или tokens; единицы неизвестны. Reset показывается только по upstream seconds, наступление даты не объявляется восстановлением лимита.

Порог 5 минут в UI — индикатор устаревания отображения, не policy запуска или expiration upstream. Локальный clock обновляет только подпись, не делает network requests. При потере live WS значения считаются устаревшими.

Weekly exhaustion и ordinaryUsageAllowed=false не добавляют локальный запрет запуска. Проекция не меняет существующий admission и не обходят upstream spending/account restrictions. Автоматического повторения billable turn нет. Optional callback transport/5xx не прерывает execution и не повторяет запись при неизвестном исходе; authority/integrity rejection остаётся ошибкой. Для upstream RPC мягко обрабатывается только supported-method absence (-32601); другие protocol/auth failures сохраняют закрытый gate.

## Проверки и ограничения

Адресные pure Go проверки actual финансовых source files и Node проверки actual UI model проверяют почти60K fixture, precision, null/unknown/unlimited, malformed/duplicate/budgets, 0/1/2/3/>3, набор за первой страницей, ties, eligibility-before-counts, reset и stale. Fixture не является upstream measurement. Дополнительно добавлены broker/notification и callback denial/replay regression tests; полный запуск этих Go packages требует отсутствующих в runtime зависимостей.

Полные runner/controller/CP/gateway suites, disposable PostgreSQL lifecycle/isolation/receipt/outbox checks и Web-first baseline остаются NOT RUN. Frontend typecheck попытка FAIL из-за отсутствующих dependencies/types; полного typecheck PASS нет. Chromium launcher получил Permission denied на /proc/cpuinfo: desktop/mobile и Console/Network проверки NOT RUN/BLOCKED, без обхода запрета.

Gateway bounded set — максимум 4096 accounts; общий frame budget сохраняется. При превышении frame budget snapshot целиком unavailable, без ложного top-three по усечённому набору. Это остаточное ограничение для больших account catalog; нужен независимый review.

Реальный кейс владельца «weekly exhausted + почти60K paid credits», реальный balance/units/spending, trusted auth deployment и paid continuation остаются UNKNOWN / live NOT RUN. Изменения не deploy, не merge и не owner approval. Независимый comprehensive review actual SHA ещё не выполнен; после него automatic fix/re-review не разрешены. Финальный owner gate OPEN/WAITING_HUMAN.
