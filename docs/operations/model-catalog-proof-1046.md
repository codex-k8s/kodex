---
id: MODEL-CATALOG-PROOF-1046
title: Авторизация наблюдения каталога моделей
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

Issue #1046, Epic #1018. Контракт adapter принадлежит Secret Broker #1068.
Исходная owner-модель и миграция 633 перенесены из checkpoint
`e3ed46b74334c66ad7c4198139e08e089f269eac`; SQL дополнен точным совпадением
organization, account и credential revision по согласованию с исполнителем.

| Сценарий | Инициатор и authority | RPC и owner | Fence, результат и потребитель |
| --- | --- | --- | --- |
| Наблюдение | Worker control-plane, проверенный system subject | `ObserveProviderModelCatalog`, Secret Broker; proof разрешает control-plane | `UNARY_PROTO_SHA256` совпадает с сохранённым request digest активной CLAIMED task; account version и текущая credential revision неизменны; broker возвращает безопасный каталог owner worker |
| Подмена | Иной workload, actor, tenant, project либо digest | `ResolveAuthorityProof` → специализированный owner resolver | Закрытый отказ до выдачи proof; поля payload не выдают полномочия |
| Истечение или terminal | Lease истёк либо task завершена/отменена | Тот же resolver и authoritative PostgreSQL read | Новое разрешение не выдаётся; нет отдельного события от read-only proof lookup |
| Смена account/credential | Account выключен, версия или credential изменена | Exact task/account/credential tenant join | Старый digest не разрешается; новый snapshot требует новой owner task |
| Повтор | Сохраняется та же активная task | Повтор proof lookup | Чтение не меняет состояние и не создаёт idempotency receipt; эффект и completion принадлежат owner task lifecycle |

Operation и permission: `platform.provider-accounts.model-catalog.observe`.
Caller — control-plane, target — secret-broker; proof producer —
`secret-broker.provider-credential-materializer`, authority source — DOMAIN_STATE.
Resource/version/attempt/idempotency/project metadata запрещены. Claim tuple
находится только в exact protobuf digest. Proof имеет предел возраста 15 секунд;
истечение task дополнительно проверяется владельцем перед выдачей.

Этот checkpoint материализует Proto, owner dispatch и policy61. Автоматическое
создание/claim/completion задач, freshness read model и полный broker consumer
доставляются согласованными contributions; до их интеграции сквозной цикл
каталога остаётся NOT RUN. Регистрация сама по себе не доказывает его готовность.
