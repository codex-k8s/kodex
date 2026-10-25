---
id: OPS-DOC-INTEGRATION-COMPLETION-1899
title: Идемпотентность completion при восстановлении интеграции
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-25
---

# Идемпотентность completion при восстановлении интеграции

Источник: #1899, эпик #1830, `GUIDE-DOC-006`. Изменяется только ключ отчёта
`integration-gateway` о завершении invocation. Provider adapter, grant, Human
Gate, owner state machine, схема данных и внешний effect key сохраняются.

## Причина и сквозной контракт

Владелец назначает новый lease, fence и монотонное generation при recovery
`UNKNOWN_OUTCOME`. Прежний ключ completion зависел только от invocation.
Первый отчёт UNKNOWN сохранял immutable idempotency receipt, поэтому отчёт
нового поколения с другим payload получал конфликт ещё до обработчика результата.

Путь: `ClaimIntegrationInvocations` у control-plane → точный claim и режим
`EXECUTE` либо `RECOVER_READ_ONLY` → разрешённое действие adapter →
`CompleteIntegrationInvocation` через прежний защищённый gRPC client →
идемпотентность и owner transaction → проверка lease/fence/generation →
результат invocation, effect receipt при успехе, продолжение и событие →
REST/WebSocket и сотрудник. Actor и workload принадлежат проверенному transport
и состоянию владельца; ключ не выдаёт полномочий.

Completion теперь использует invocation и generation. Его ключ отличается от
provider effect key: новый completion не является новым внешним действием.
Содержимое отчёта, его размер, семантический digest и защита exact replay не
изменяются. `completeTest` уже применяет такой же принцип к проверке подключения.

## Матрица жизненного цикла

| Сценарий | Ключ completion и результат | Граница внешнего эффекта |
| --- | --- | --- |
| Первое исполнение, поколение N | Отдельный receipt UNKNOWN, FAILED или SUCCEEDED | Прежние grant и approval; receipt только при подтверждённом успехе |
| Commit принят, ACK потерян | Тот же неизменный отчёт и ключ возвращают прежний receipt | Повтор provider mutation не разрешён |
| Payload изменён в поколении N | Тот же ключ, idempotency conflict | Прежний результат не переписывается |
| Recovery, поколение N+1 | Новый completion key принимает новый exact lease/fence | Прежние invocation/effect/input/scope; только поддержанный read-only recovery |
| Recovery подтвердил результат | SUCCEEDED с exact provider receipt либо штатный отрицательный outcome | Отсутствие доказательств не становится успехом |
| Stale lease/fence, cancel, revoke, terminal root | Прежние authority и state guards | Новый ключ не восстанавливает отозванный grant |
| Gate открыт либо отказан | Прежний owner lifecycle, допуск не расширяется | Completion key не заменяет решение владельца |
| Неподдержанный provider recovery | Прежняя явная неопределённость | Нет автоматического WRITE replay |

Новые события и consumers не добавляются. Существующие owner completion,
cancel и expiry сохраняют свои атомарные события и authoritative read paths.
Не исправляется отдельная гонка раннего результата и окончания модели #1848.

## Проверка и доставка

`go test ./internal/app -run TestInvocationRecoveryCompletionKeepsIndependentReceipts`
в модуле `services/external/integration-gateway` проверяет immutable captured
requests и receipt double: UNKNOWN с потерей ACK, exact replay, конфликт
изменённого старого отчёта, новый terminal report и его replay. Оба terminal
исхода проверяются отдельно. Это локальная проверка client boundary, не
PostgreSQL и не доказательство внешнего эффекта. Прежние тесты fence и unknown
сохраняются; полный набор тестов gateway выполняется отдельно.

В staging доставляются обе реплики immutable gateway штатным scoped release.
Во время перехода не запускать новую QA-работу; перед приёмкой сверить source,
image digest, Pod UID и отсутствие старых workers. Схема, политика доступа и
credential mounts не меняются. Старые выключенные grants и подключения не
включать для оживления неопределённого эффекта.

Ручная проверка: отдельный QA-сотрудник запрашивает одну запись штатной HTTP-
оснастки с `RETRYABLE_ONCE`, владелец проверяет точный intent и разрешает её
через Human Gate. После первого UNKNOWN наблюдать результат recovery без
нового WRITE. Проверять все обслуживающие реплики process-local оснастки:
неизменные UID/restarts, одна попытка, отсутствие повтора и достоверный эффект.
Если процесс оснастки заменён, прежние counters не доказывают cardinality.
Сотрудник должен получить фактический результат и сохранить доступный артефакт.

Возврат выполняется прежним scoped release image; старый gateway снова содержит
дефект generation key, поэтому возврат не является решением recovery. Не удалять
owner receipts и не менять state вручную. Фактические SHA и PASS/FAIL/NOT RUN
публикуются в Issue/PR после выполненных проверок, а не выводятся из этого плана.
