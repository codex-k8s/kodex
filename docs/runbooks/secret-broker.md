---
id: RUN-MC-026
title: Диагностика secret-broker
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-29
---

# Диагностика secret-broker

`secret-broker` является единственной границей записи и контролируемого чтения
пользовательских Runtime Secrets. Метаданные, полномочия, operation lifecycle и
audit принадлежат control-plane; broker хранит только неизменяемые versioned
Kubernetes Secrets в выделенном installation runtime namespace.

## Probes и метрики

- `/healthz` подтверждает, что process жив;
- `/readyz` проверяет доступность control-plane authority и Kubernetes Secret
  API тем же service account, который обслуживает рабочий путь;
- `kodex_secret_broker_readiness` отражает readiness;
- `kodex_secret_broker_grpc_requests_total` и
  `kodex_secret_broker_grpc_request_duration_seconds` имеют только закрытые
  значения `operation` и gRPC `code`;
- `kodex_secret_broker_recovery_runs_total{outcome}` отражает успешные и
  ошибочные bounded-проходы;
- `kodex_secret_broker_recovery_actions_total{action}` учитывает только
  `keep|delete|not_found|effect_present|claim_failed|claim_conflict`;
- `kodex_secret_broker_recovery_errors_total{stage}` использует закрытые этапы
  `list|readback|decision|delete|protocol|work_list|work_lookup|work_fail|work_conflict|work_protocol`;
- `kodex_secret_broker_recovery_backlog` равен числу materialization, которые
  последний проход не смог разрешить или удалить, и просроченных claim, которые
  не удалось безопасно завершить.

## Обязательная конфигурация workload

- `POD_UID` задаёт устойчивый claimant ID одной реплики и передаётся через
  Kubernetes Downward API;
- `SECRET_BROKER_RUNTIME_NAMESPACE` имеет единственное допустимое значение
  `kodex-runtime`; `POD_NAMESPACE` передаётся через Downward API как фактический
  namespace broker Pod и не используется как target namespace;
- `SECRET_BROKER_RECOVERY_INTERVAL` и `SECRET_BROKER_RECOVERY_TIMEOUT`
  ограничивают период и бюджет одного полного прохода.

Отсутствующий `POD_UID`, другой runtime namespace или некорректный бюджет
закрыто останавливают startup. Deployment должен передать эти env явно.

## Проверка incident

1. Найти correlation ID и безопасный terminal audit в control-plane. Broker не
   логирует refs, names, hashes и значения materialization.
2. Проверить readiness broker, control-plane, authority issuer и Kubernetes API.
3. Проверить состояние операции, lease/fence и terminal audit в control-plane.
4. Для materialized revision сверить namespace, name, UID, resourceVersion,
   content digest и broker annotations. Данные Secret не выводить.
5. Проверить recovery/garbage-collection метрики и последние bounded ошибки.
6. Повторить исходную команду только с тем же idempotency key и тем же intent.
   Новый intent требует новой команды и нового ключа.

## Инварианты восстановления

- каждый проход сначала получает bounded paginated список просроченных
  `CLAIMED` operation из control-plane, а затем сканирует managed Kubernetes
  Secrets;
- `CREATE|ROTATE`, упавшие до materialization, завершаются исходным
  `claimant_id+claim_generation` с `RECONCILIATION_FAILED`; exact найденный
  effect передаётся обычному managed scan для authoritative completion;
- deterministic name с несовпадающими operation metadata или digest завершает
  исходную claim с `MATERIALIZATION_CONFLICT`, остаётся для ручной проверки и
  держит recovery readiness закрытым в текущем проходе;
- просроченные `REVEAL|REVOKE` без pre-terminal durable effect завершаются
  `RECONCILIATION_FAILED`; завершённый revoke в work list не возвращается, а
  его orphan cleanup выполняет managed scan;

- `CREATE` и `ROTATE` активируют только exact materialization с совпадающим
  digest и monotonic revision;
- lost response не создаёт второй logical effect: operation reclaim использует
  новое поколение claim, а immutable Secret содержит `operation_ref`;
- `REVOKE` сначала атомарно закрывает secret в control-plane и повторно
  проверяет активные references; физическое удаление выполняется exact
  UID/resourceVersion и может быть безопасно повторено reconciler;
- stale claim, неизвестный Secret, чужой namespace и несовпадающий digest
  закрыто отклоняются;
- каждая consumed operation получает ровно один terminal audit
  `SUCCEEDED|FAILED` без plaintext;
- broker не имеет доступа к служебным Secrets namespace `kodex-system`.

Запрещено исправлять состояние через SQL, вручную копировать Secret или удалять
его только по имени. Если recovery не сходится, остановить новые secret
operations, сохранить метаданные incident без значений и выполнить исправление
forward-only через control-plane и versioned manifests.
