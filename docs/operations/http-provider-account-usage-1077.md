---
id: OPS-HTTP-PROVIDER-USAGE-1077
title: HTTP доступность provider account
type: implementation
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Контекстная доступность учётной записи

Refs #1045, #1046, #1077, #1022. Producer — CP contribution
`399cf1f0321156fd4fb645a6825f286d23cd271b`, включающий migration 648 и реальные
owner observations. HTTP не оценивает доступность самостоятельно.

Existing GET `/api/v1/provider-accounts` и GET одной записи принимают
`usagePurpose`, `usageAgentRef`, `usageRuntimeProfileRef`,
`usageProviderDefinitionKey`, `usageModel`, `usageReasoningEffort`. Любое поле
этой группы требует purpose и Agent; CONFIGURE требует candidate profile и
provider, модель может ещё не быть выбрана. LAUNCH запрещает overrides и
использует сохранённую конфигурацию владельца. Административное чтение без
контекста оставляет model/actor-use NOT_EVALUATED.

Путь: signed actor/org → existing HTTP GET → List/GetProviderAccounts с typed
usageContext → owner transaction с exact visibility и manage/launch permission
→ usage projection → canonical Go/TS SDK → selector PWA. Контекст не назначает
полномочия, не создаёт grants и не резервирует capacity. Existing mutation,
OCC, idempotency и event lifecycle не изменены. Повторное авторитетное чтение
заменяет истёкшую проекцию; cursor связан с текущим actor/context/source.

Mapper сохраняет шесть dimensions, typed reason/remediation, точные timestamps,
account/config/catalog/context pins и явные false/zero. Подтверждённый health
имеет только область CREDENTIALED_CATALOG_REACHABILITY. UNKNOWN не заменяется
READY, а CAPACITY_EXHAUSTED не отменяет разрешённый queued submission. Несовпадение
request context, account version, health observation или неизвестный enum
закрываются INVALID_UPSTREAM_RESPONSE. Mutation responses могут не иметь usage;
List/Get требуют authoritative usage каждой возвращённой записи.

Ручная проверка после подключения PWA: выбрать account до модели и увидеть
eligibleForSelection при model NOT_EVALUATED; сменить профиль формы, затем
модель; убедиться, что предыдущий context больше не используется. Сравнить
CONFIGURE и LAUNCH у actor с одним agent.manage. При заполненной capacity
проверить отдельные allowedToSubmit и operationalState. Не показывать raw
credential coordinates, значения секретов либо чужие lease descriptors.

Локальные HTTP/SDK проверки: полный gateway race PASS (HTTP 6.260 s), полный
vet/build PASS; canonical Go/TS replay побайтно PASS; строгий SDK TypeScript
PASS; Proto lint/build/replay и policy72 replay PASS. Первый targeted запуск
был FAIL из-за прежней read fixture без usage; fixture приведена к owner admin
NOT_EVALUATED, повтор targeted PASS 1.160 s, затем полный набор выше.
Exact checkpoint фиксируется в PR #1066. CP полный
PostgreSQL PASS 33.432 s и Avatar 0.410 s относятся к указанному producer.
PWA/browser/live/provider/staging в этой HTTP проверке: NOT RUN. Секреты не
раскрыты; rollback — возврат HTTP consumer вместе с совместимым source/SDK,
без обратной миграции данных владельца.
