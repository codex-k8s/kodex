---
id: RUNBOOK-MC-016
title: Automation scheduler
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-05
---

# Automation scheduler

Runbook относится к `services/jobs/automation-scheduler` и
`deploy/k8s/base/automation-scheduler`. Любое применение manifest, изменение
staging/production либо откат требует отдельного подтверждения владельца.

## Безопасная диагностика

Проверить Deployment, Pod readiness, ServiceMonitor и bounded business
метрики. Application grant должен быть ограничен одной организацией и не
содержать project scope: точный проект выбирает только owner-side cursor;
project-bound grant закрыто отклоняется. Не выводить application grant,
сертификаты, ключи, DSN, Sentry DSN, authority proof или содержимое Vault CSI.

Рабочая readiness — это не TCP probe. Она последовательно подтверждает
`AuthorityProofResolver`, локальный issuer и protected control-plane
`CheckReadiness`. `/readyz=503` при отказе любого звена является правильным
fail-closed состоянием.

Ключевые метрики:

- `mattercodex_automation_scheduler_cycles_total{outcome}`;
- `mattercodex_automation_scheduler_occurrences_total{operation,outcome}`;
- `mattercodex_automation_scheduler_tracked_claims`;
- `mattercodex_automation_scheduler_readiness{ready}`;
- `mattercodex_control_plane_schedule_maintenance_total{effect}` для отдельно
  committed `requeue/dead_letter/overlap_skip/quarantine/repair` даже когда
  итог следующего poll — `NotFound`.

Labels закрыты и не содержат organization, project, schedule, occurrence,
actor или credential identifiers.

## Ручная happy-path проверка владельца

1. В тестовом проекте через утверждённый control API создать Schedule по
   mailbox fixture, заменив placeholder IDs на существующие pinned resources.
2. Убедиться authoritative read, что `next_run_at` рассчитан control-plane и
   timezone сохранён как IANA name.
3. Вызвать `POST /schedules/{id}/run-now` с owner session, CSRF,
   `Idempotency-Key` и `If-Match`; проверить отдельную немедленную occurrence и
   неизменившийся плановый `next_run_at`. Повтор exact key не создаёт вторую.
4. Дождаться due и проверить ровно один `ScheduleOccurrence` с неизменяемыми
   `schedule_id + scheduled_for`, один `ScheduledRun`, один root Turn и, для
   PLAYBOOK, один ProcessRun.
5. Проверить, что job не создаёт Pod: Pod создаёт только runtime-controller
   после отдельного owner/runtime claim.
6. Завершить Turn штатным runner path с закрытым outcome и убедиться, что
   occurrence/run закрыты либо создана ровно одна новая attempt по retry policy;
   retry сохраняет root ProcessRun ID и получает fresh Turn/RuntimeRevision.
7. Для `no_action` и `AUDIT_ONLY` проверить audit без Mattermost сообщения. Для
   `action_taken/failed` проверить policy eligibility и exact канал из schedule
   RoomID, а не actor-default route.
8. Для `requires_human` проверить один OwnerGate и durable карточку в exact
   schedule room; до owner decision/expiry RuntimeExecution, Turn, ProcessRun,
   occurrence и run не terminal. После решения сверить один согласованный graph.

## Негативные сценарии

- Подменить только в изолированном тестовом проекте pinned prompt/target
  version до materialization. Нематериализованная occurrence должна получить
  owner-side `DEAD_LETTER/materialization_invalid` и audit; следующая строка
  другого расписания должна быть обработана в том же RPC.
- Для повреждённой expired execution binding проверить cardinality-one
  `repair_schedule_occurrence_binding` из coherent ScheduledRun либо
  `dead_letter_absent_schedule_graph` после доказанного отсутствия graph и
  продвижение следующей строки того же schedule. Потенциально живой graph не
  должен получить частичный terminal envelope.
- Отключить доступ к local issuer. `/readyz` должен стать 503, рабочие RPC не
  должны перейти на plaintext либо mTLS-only fallback.
- Вернуть `requires_human`. Occurrence, ScheduledRun и ProcessRun должны
  остаться `WAITING_OWNER`; scheduler completion не должен закрыть их без
  exact owner decision.
- Создать overlap race при `FORBID`/`SKIP`/`QUEUE` и проверить соответственно
  сохранённый due watermark, terminal skip audit либо FIFO без второго
  параллельного execution graph.
- Использовать тот же completion key с другим occurrence/attempt. Control-plane
  должен закрыто отклонить idempotency conflict.
- Повторить committed-but-unknown due/claim/completion после ротации
  short-lived application grant с тем же semantic key. Новый bearer должен
  пройти transport replay protection и получить прежний owner receipt без
  второго эффекта.
- Создать due work в двух проектах одной организации. Organization-scoped
  scheduler grant не должен принимать caller-provided project; durable
  owner-side cursor должен последовательно сделать оба проекта достижимыми.

## Restart и race

При рестарте удалить только Pod через контролируемый orchestrator path, не
Schedule/Occurrence/receipt. После запуска job снова вызывает due/claim;
unique occurrence и PostgreSQL locks не допускают второй process run. Потеря
локальной карты lease не теряет работу: после server-issued deadline
control-plane watchdog закрывает старый graph и создаёт retry/dead-letter.
При retry сравнить ProcessRun ID до и после restart: меняются только
attempt/Turn/RuntimeRevision/grant, root lineage остаётся прежним.

Для проверки двух реплик одновременно разбудить одно расписание и убедиться,
что один claim возвращает lease, а другой выбирает следующую строку либо
`NotFound`. Нельзя вручную копировать lease token между Pod.

Гонка terminal runner и watchdog допускает одного owner-side победителя.
Итоговый `ScheduledRun`, occurrence, Turn/ProcessRun, grants и audit должны
быть согласованы одной disposition; частичный terminal envelope является
инцидентом control-plane.

## Инциденты

`AutomationSchedulerCycleFailures` обычно означает недоступный protected RPC
либо owner-side invariant. Сначала проверить readiness dependency chain и
control-plane logs по correlation ID без credentials. Не исправлять отказ
прямым SQL, generic transition, новым permission либо wildcard NetworkPolicy.

`AutomationSchedulerClaimBacklog` означает, что terminal outcomes ещё не
согласованы либо control-plane не отвечает. Сверить состояние Turn/ProcessRun,
owner gate и server-issued lease deadline authoritative read path. Локальное
число tracked claims не является источником состояния.

`AutomationSchedulerProtectedPathUnavailable` требует проверить точный SNI,
CA, workload certificate generation, application grant metadata, issuer
snapshot/readback и control-plane readiness. `skipTLSVerify`, plaintext и
выключение authorization запрещены.

## Rollback

После отдельного owner OK вернуть Deployment на предыдущий подписанный digest
тем же canonical render и дождаться protected readiness. Additive migration
добавляет organization scheduler cursor, exact scheduled runtime lineage и
route/policy поля durable delivery; при application rollback она остаётся на
месте, а прежний binary игнорирует новые nullable поля. Down migration без
отдельного owner решения не выполнять.
Перед rollback можно штатно поставить затронутые Schedule на `PAUSED` через
`ManageSchedule`; прямой SQL запрещён. Уже созданные occurrence, runs,
receipts, owner gates и audit не удалять. После восстановления активировать
Schedule через owner API и проверить next run/retry state.
