---
id: OPS-INTEGRATION-RESULT-2081
title: Полный результат обычной интеграции после Human Gate
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-10
---

# Полный результат интеграционного продолжения

Issue: [#2081](https://github.com/codex-k8s/kodex/issues/2081), приёмка
[#1830](https://github.com/codex-k8s/kodex/issues/1830). Отдельная история
SERVER_DAG остаётся в [#1972](workflow-evidence-history-1972.md).

## Путь и совместимость

Обычный сотрудник проекта вызывает интеграцию, получает Human Gate, завершает
физический turn. Решение и terminal invocation создают ровно один server-owned
`CONTINUES`. Control-plane сохраняет полный `result_summary` в новой задаче.
Небольшой ответ остаётся inline; лимит turn не повышается. Если ответ не
помещается, owner materializes `kodex.integration-continuation-manifest.v1`:
точные invocation, SHA-256, размер и один outcome. Решение, комментарий и receipt
остаются обозримыми inline; полный источник содержит прежний типизированный
`integrationContinuationSnapshot`, включая весь результат.

Продолжение использует существующие MCP `get_integration_catalog.outcomes_read`
и RPC `GetExecutionIntegrationOutcomes`. Wire fields, operation profiles, роли,
политики подтверждений, БД и runner ABI не меняются. Страница — до 4096 байт,
границы UTF-8 и полный/page digest проверяются сервером. Потерянный ответ можно
повторно прочитать с теми же координатами. Новый вызов интеграции для получения
утраченного результата запрещён инструкцией продолжения.

Это дополнение для обычного project Agent, без workflow version и assistant
scope. Системные/проектные помощники и прежние workflow continuations сохраняют
собственный путь и прежний бюджет; их reader authority этим изменением не
расширяется. SERVER_DAG reader проверяется первым по прежнему контракту;
только отсутствие подходящего source допускает отдельный обычный selector.
Manifest и ref сами по себе не являются полномочиями.

## Матрица жизненного цикла

| Событие | Авторитетное состояние и эффект |
| --- | --- |
| APPROVE → success/failure | Точный terminal outcome, комментарий и receipt при успехе; один continuation |
| REJECT/CANCEL | Точный отрицательный исход; reader не разрешает новый effect |
| Чтение страницы | Живые lease/fence/generation и RR; тот же organization/project/run/root/agent/session/turn; непосредственный source через CONTINUES и parent |
| Источник | Завершённый исходный execution с COMPLETED lease; exact invocation и решение Gate; успешный результат совпадает с receipt |
| Видимость | Активный исходный root actor/membership, действующие project/agent/run visibility; identity выводится сервером |
| Целостность | Полный snapshot заново хешируется; неизменная materialized task должна совпасть; другой ref/hash отклоняется |
| Cancel/terminal/expiry | Новые страницы не выдаются; уже сохранённый receipt не удаляется |
| Read error | Только отказ чтения, без retry внешней операции, fallback credential или нового Gate |
| Повтор страницы | Те же bytes/hash/offset, без изменения графа и invocation |

## Поставка и проверка

Самостоятельный unit — control-plane. Нужен новый immutable image; работающие
совместимые runtime-controller/callback уже имеют этот MCP/RPC reader. Миграции,
новые secrets, API endpoints и инфраструктурные объекты не требуются. Перед
адресным обновлением завершить активные turns. Перед возвратом старого CP
завершить или штатно отменить continuations с новыми manifests: старый owner
не умеет их читать. Историю и receipt не переписывать SQL.

Синтетическая PostgreSQL-проверка `TestIntegrationGateFeedbackComponent`
проводит owner/worker/gateway lifecycle с длинным inline ответом и manifest,
реальным claim после пересоздания repository, сборкой страниц до EOF и полным
SHA-256. Проверяет повтор первого чтения, чужой source, fence/generation/hash,
середину UTF-8 и отказ после cancel. Исходные отрицательные решения, stale Gate,
граница другого проекта и redaction продолжают проверяться той же оснасткой.
Внешний API не вызывается; успешный локальный тест не равен installed acceptance.

Проверена официальная документация PostgreSQL 18 через Context7:
[Repeatable Read](https://www.postgresql.org/docs/18/transaction-iso.html) и
[row locks](https://www.postgresql.org/docs/18/explicit-locking.html). Reader
использует прежнюю ограниченную owner transaction/retry и transaction-local
переиспользование плана, без кэширования решений авторизации.
