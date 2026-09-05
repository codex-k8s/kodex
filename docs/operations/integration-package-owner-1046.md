---
id: INTEGRATION-PACKAGE-OWNER-1046
title: Управляемый package в исполняемом connection
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-05
---

Issue #1046 / #1028, CFG. Общий контракт библиотеки описан в
`libs/go/integrationpackage/README.md`; consumer получает package только в
private Test/Invocation claim. Public DTO не получает дополнительные bytes.

| Переход | Actor и owner | Fence и effect | Readback / событие |
| --- | --- | --- | --- |
| UI CREATE/SAVE | actor из signed context, managed set tenant ACL | OCC/idempotency; валидный package получает UI origin и canonical JSON; ошибочный draft остаётся для диагностики | immutable revision; существующее событие managed configuration |
| VALIDATE/PUBLISH | тот же owner | closed compiled adapter contract, без сравнения с единственным shipped digest | published immutable revision |
| REBIND | managed set authority и exact connection integration.manage | одна owner transaction меняет binding и connection version/digest, выключает grants; активные и UNKNOWN_OUTCOME effects закрыто блокируют rebind | INTEGRATION_CONNECTION_CHANGED и managed событие; connection требует нового Test |
| Test | owner connection authority; health READ/NONE | проверка до task create; усиленный health gate закрыто запрещён и TEST action не выдаётся | worker получает exact package, configuration, credential и lease; completion возвращает connection |
| Invocation | current root actor, active grant и RuntimeRevision | exact bound package и input validation; HUMAN_EACH_EFFECT требует owner gate также при risk READ | immutable intent, claim/receipt и существующий gate/event lifecycle |
| COPY/DETACH | existing managed owner ACL | server UI origin назначается новому draft, история исходного GIT package сохраняется | существующие draft/readback и managed event |

Повторный worker read не меняет глобальный registry adapter. Истечение READ lease
может вернуть invocation в READY, но SQL снова требует APPROVED gate для
HUMAN_EACH_EFFECT. Unresolved UNKNOWN_OUTCOME не допускает смену connection.

Проверки на рабочем дереве: library race/vet/codegen PASS; CP targeted
revision/repository/transport race PASS; disposable PostgreSQL с prerequisite
RoleImage→Environment и managed lifecycle PASS (1.610s), включая UI package
publish→connection pin→private Test claim→completion и gated health denial.
Полный TestBootstrapComponent отдельно FAIL (16.685s): email mailbox bind
`conflict` и system assistant warm configuration `not found`; эти результаты
остаются открытыми, полный baseline не объявлен успешным.

Git source sync/write-back owner, RoleImage mapping, prepublish impact и полный
HTTP/browser сценарий остаются в полном unit. Этот checkpoint не завершает CFG.
