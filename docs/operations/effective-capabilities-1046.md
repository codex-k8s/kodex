---
id: OPS-EFFECTIVE-CAPABILITIES-1046
title: Авторитетная проекция возможностей сотрудника и этапа
type: operational-contract
status: approved
owner: platform
version: 1.1.0
updated: 2026-09-05
---

# Граница MVP-UI-31

Issue #1046, epic #1018. `GetAgentEffectiveCapabilities` выводит actor/tenant
из проверенного context и разрешает Agent, выбранный Workflow/step и runtime
в одной owner transaction. Payload не назначает capabilities или actor.
Публичный ответ содержит requested/effective/unavailable, стабильные причины,
точные версии Agent/runtime/Workflow и digest проекции. Connection grants
показываются отдельно по exact connection/ref/version; совпадение capability
key двух подключений не объединяет их authority.

| Путь | Authority и состояние | Переход/consumer/read |
| --- | --- | --- |
| Просмотр | agent.view, для system assistant organization.manage; Workflow того же Project с workflow.view | Защищённый query/profile64 → HTTP/SDK/PWA, без события |
| Выдача capability | agent.manage и текущие permissions выдающего actor на target scope, до OCC/receipt | Existing ChangeAgentCapability; атомарный state/audit/event |
| Workflow stage | Опубликованный server-owned step и назначенный Agent | Required keys сужают effective; несовпадение имеет отдельную причину |
| Материализация | Сохранённый root initiator, current access bindings и active exact grants | Новый RuntimeRevision; тот же предикат eligibility до prompt/MCP projection |
| Отзыв/expiry | Текущие access conditions/grant/connection/package/credential | Новый query показывает unavailable; новый turn не получает отозванный grant |
| Pagination/reload | Actor/tenant/Agent/Workflow/query + exact projection digest | Чужой либо устаревший cursor отклоняется; клиент перечитывает первую страницу |

Public query не возвращает credential descriptors, private package bytes,
prompt/file contents или worker snapshot. Runtime query использует actual
bound package для input schema; shipped metadata не заменяет UI/Git revision.
Сужение authority одного connection не скрывает разрешённый соседний connection
и не разрешает чужой через общий capability key.

## Проверки вклада

Локальные PASS: scoped race (current direct/group permissions, expiry/revoke,
exact connection и READ/WRITE, actor/tenant/filter/snapshot cursor), полный
race трёх затронутых пакетов, полный CP vet/build, Proto lint/build/gen и clean
replay, policy64/codegen, SQL boundary. PostgreSQL scoped PASS 0,669 s включает
owner assignment, запрет escalation до OCC, hidden Agent/system assistant,
published Workflow/assigned step и повторное owner-чтение runtime после revoke.
Полный `TestBootstrapComponent` PASS 19,982 s.

Предыдущие FAIL сохранены в локальных логах: неверное имя SQL project lifecycle;
две ошибки новой тестовой оснастки (нулевая stale version и неразрешённый raw
principal); регрессия кода ошибки неизвестной capability. Исправления проверены
последующим полным PostgreSQL запуском. Начальный transport compile FAIL по
имени repeated Proto поля также исправлен до race/build.

Дополнительный PostgreSQL сценарий подтверждает два active connection с одним
`synthetic.journal.write`, exact actor binding только к одному из них и
назначенную через UI immutable схему с `value.maximumLength=16` вместо shipped
4096. Публичная проекция сохраняет отдельные причины для обеих связей, runtime
получает ровно разрешённый grant и actual schema. Полный Bootstrap с этой
проверкой PASS 20,802 s; неудачный первоначальный focused запуск требовал
существующий Runtime lifecycle fixture и не считался PASS.

`authorizeCommand` повторно проверяет право выдачи до idempotency receipt.
Новый negative сценарий отзывает launch binding после успешной выдачи и
подтверждает запрет exact receipt replay; runtime query также исключает
отозванное право. Полный Bootstrap после этой проверки PASS 21,044 s, race
трёх пакетов и полный CP vet/build PASS. Предыдущая ошибка теста с повторной
выдачей уже назначенной capability исправлена; conflict не скрывался.

HTTP/SDK/PWA consumer, новый browser сценарий, общий baseline, live runtime и
staging — NOT RUN в этом CP checkpoint. Временная вклад-ветка относится к
#1046 и не получает отдельный PR.

## Включение в основной unit

Источник вклада — `0912f568d390465b747dad99f8575c3a31611cf0`.
Изменения включены поверх RoleImage checkpoint
`b9402939a3ccdcef384d44cc2c04dfa5554f73b5` с повторной канонической генерацией
Proto и policy64. Повторный локальный race repository/domain/transport прошёл;
совместная PostgreSQL проверка этого дерева пока NOT RUN. Предыдущая проверка
вклада не подменяет проверку объединённого runtime.
