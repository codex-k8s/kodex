---
id: OPS-DOC-1198
title: Привязка materialization к runtime lease
type: operation
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Привязка materialization к runtime lease

Issue: https://github.com/codex-k8s/kodex/issues/1198.
Источники: GUIDE-DOC-003, GUIDE-DOC-006 и контракт secretbroker/v1.
Документ определяет договор исправления, но не свидетельствует о готовности
реализации или успешной проверке staging.

## Сквозные сценарии

Пользователь создаёт запуск через control-api-gateway. Control-plane назначает
root actor и project, создаёт run/session/turn и immutable RuntimeRevision.
Runtime-controller получает lease через ClaimExecution, затем вызывает
MaterializeRuntimeCredentials у secret-broker. Producer proof разрешает actor
и project из активного исполнения control-plane, а не из worker credential.
Secret-broker проверяет подписанный context и повторно разрешает исполнение у
control-plane перед выдачей ограниченной credential projection. Потребитель
runtime-controller связывает projection с точными lease, revision и attempt.

System assistant проходит отдельный MaterializeSystemAssistantCredentials
с wrapper execution. Его project отсутствует; root actor принадлежит серверной
цепочке assistant session/turn. Обычный project request не может быть использован
как assistant request и наоборот.

Дайджест детерминированного protobuf request и точная operation фиксируются
в runtime_leases в той же транзакции, что lease и TURN_STARTED. Fence входит
в дайджест, но не сохраняется открытым текстом. Lookup по дайджесту не заменяет
проверки trusted workload, организации, срока lease, generation и всего графа.
ProjectRef является только проверяемым указателем на серверный project.

Proof использует RUNTIME_EXECUTION и серверный root actor. Его срок не превышает
сроки worker grant и lease. Неизвестный или старый lease без зарегистрированного
дайджеста отклоняется. Общий запрет выбора project произвольным worker сохраняется.

## Жизненный цикл пользовательского исполнения

| Переход | Состояние полномочий и событие |
| --- | --- |
| create | До claim права materialization отсутствуют; авторитетный read path control-plane |
| claim | Новый fence/generation и request digest атомарны с lease и TURN_STARTED; consumer runtime-controller |
| renew | Продлевается существующий живой lease; proof снова читает его срок, старый proof сам не продлевается; отдельного события регистрации нет |
| complete | Закрытый lease запрещает новые proof/projection; отдельного события регистрации нет, read path runtime_leases |
| cancel | Owner-транзакция закрывает исполнение и lease; сохранённый digest не разрешает повторную выдачу; отдельного события регистрации нет |
| delete | Удалённое или недоступное исполнение не разрешается; отдельного события регистрации нет, read path control-plane |
| retry | Новая attempt/revision/fence и новый digest; прежний lease недействителен; новый TURN_STARTED при claim |
| lease expiry | Истёкший lease не разрешается даже до фоновой уборки; отдельного события регистрации нет |
| dead-letter | Terminal graph не разрешает materialization; отдельного события регистрации нет |
| WAITING_OWNER | Нет действующего execution lease для выдачи; отдельного события регистрации нет |
| CHANGES_REQUESTED | Прежнее исполнение закрыто; продолжение требует нового claim; отдельного события регистрации нет |

## Жизненный цикл system assistant

| Переход | Состояние полномочий и событие |
| --- | --- |
| create | Project отсутствует; root actor/session назначает control-plane; права materialization ещё нет |
| claim | Проверяется assistant lineage; сохраняется digest wrapper request в транзакции lease и TURN_STARTED |
| renew | Проверяются активные assistant session/turn и lease; срок нового proof ограничен lease и worker grant |
| complete | Закрытые turn/lease запрещают выдачу; отдельного события регистрации нет, read path control-plane |
| cancel | Отзыв следует owner-транзакции закрытия assistant execution; отдельного события регистрации нет |
| delete | Отсутствующие или недоступные assistant session/turn запрещают выдачу; отдельного события регистрации нет |
| retry | Новые attempt/revision/fence и wrapper digest; старый digest не переносится; TURN_STARTED при claim |
| lease expiry | Проверка времени закрывает выдачу без ожидания уборки; отдельного события регистрации нет |
| dead-letter | Terminal assistant execution запрещает выдачу; отдельного события регистрации нет |
| WAITING_OWNER | Нет активного execution lease; отдельного события регистрации нет |
| CHANGES_REQUESTED | Нужен новый owner-approved execution и claim; отдельного события регистрации нет |

## Readiness и доставка

CheckRuntimeCredentialProjectionReadiness не материализует секрет и не выбирает
произвольный project. Его projectless worker profile отделён от execution proof.
Новая самостоятельная workload не добавляется: владелец таблицы и миграции
control-plane, producer runtime-controller, consumer secret-broker. Изменение
policy требует увеличения revision и обычной доставки authority publisher.

## Необходимые доказательства

Требуются положительные user/assistant проверки и отказы при чужом project,
actor, workload, изменённом поле protobuf, неверном wrapper, expired/cancelled
lease, старой attempt и отсутствующем digest. После общей выкатки проверяются
настоящий CODEX_SHELL, workspace/artifacts и quota. До этих результатов Issue
не считается закрытым.
