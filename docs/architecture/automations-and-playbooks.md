---
id: ARCH-MC-009
title: Автоматизации и управляемые процессы
type: architecture
status: approved
owner: architect
version: 1.0.2
updated: 2026-08-05
---

# Автоматизации и управляемые процессы

## Управляемый процесс

`Playbook` является версионируемым процессом, управляемым промптом и policy revision, а не визуальным графом действий.

Он содержит:

- входную JSON Schema;
- политику агента-координатора;
- шаблон инструкции Markdown;
- разрешенных агентов и интеграции;
- параллельность и тайм-аут;
- контракт обратного вызова;
- критерии завершения;
- шлюзы ручной приемки;
- схему результата.

`ProcessRun` фиксирует версию playbook, policy revision и корневого человеческого инициатора. Изменение `Playbook`, имен ролей или связей не меняет уже запущенный процесс.

Платформа не распознает полномочия по именам ролей. Активная policy MatterCodex
разрешает корневому manager запускать не более двух дочерних manager-сессий,
получать их callbacks, а дочернему manager — запускать рабочие и проверяющие
роли. Другой проект может выбрать иной граф без изменения кода.

Управляемый процесс запускает продуктовое, security и архитектурное review на
одном exact SHA. После исправлений все три направления проверяют новый SHA и
системные аналоги. Human gate недоступен при unresolved thread или отсутствии
явного подтверждения любого направления. Плоский список комментариев не
заменяет GitHub GraphQL `reviewThreads`, а отсутствие `check runs` не является
успешным CI.

## Координация между обсуждениями

Координатор использует MCP согласно своим capabilities:

- `mattermost_start_agent_thread`;
- `mattermost_continue_agent_thread`;
- `mattermost_return_to_requester`;
- `request_human_gate`;
- `report_process_status`;
- `publish_artifact`.
- `mattermost_request_owner_attention`;
- `mattermost_list_active_work`;
- `mattermost_request_sync`.

Дочернее обсуждение хранит идентификатор родительского процесса. Событие завершения ставит обратный вызов в очередь сессии менеджера. Обратный вызов не зависит от упоминания Mattermost и не запускается повторно при повторной доставке события.

Перед межкомнатным запуском координатор проверяет каталог и карточку активного чата. Доменный сервис проверяет relationship rule, сохраняет точное сообщение запуска и наследует root initiator. Первый запуск роли создает долговечное делегирование и привязку `source session -> target role -> target thread/session`. Следующий пакет работы этой роли запускается через `mattermost_continue_agent_thread` по исходному делегированию; доменный сервис повторно проверяет policy и неизменяемую identity и ставит ход в ту же FIFO-очередь. Попытка повторно создать тред для уже привязанной роли отклоняется, кроме запуска новой дочерней сессии координатора той же роли. Дочерняя роль возвращает результат непосредственному разрешенному координатору через callback. Маршрут содержит точные сессию и обсуждение вызывающего и не зависит от его членства в дочернем канале. Один дочерний ход создает не более одного обратного вызова; новый ход той же дочерней сессии может вернуть следующий результат по сохраненному маршруту в ту же сессию вызывающего. Если явный callback пропущен, терминальное завершение один раз использует финальный результат. Обычный callback и завершение не упоминают человека. Ручной шлюз создается через `mattermost_request_owner_attention`, а окончательная runtime-ошибка маршрутизируется платформой корневому инициатору.

Управляющий агент считает дочерний запуск принятым только по успешному MCP-ответу с точными `delegation_id`, runtime run и target session identity. Состояние GitHub, старое сообщение или чужой активный run не подтверждают создание делегирования. После всех запланированных запусков управляющий агент завершает текущий turn. Он не удерживает turn с циклическим polling GitHub, тредов или делегирований: callback становится отдельным следующим turn в FIFO-очереди исходной сессии и не может исполняться параллельно с незавершенным turn координатора. Каждый callback-turn выполняет ограниченный шаг координации и снова завершается, если ожидаются другие результаты.

## AutomationSchedule

Цель расписания:

- конкретный `Agent`;
- версия `Playbook`.

Поля:

- cron или интервал;
- часовой пояс IANA;
- промпт и его версия;
- политика сессии `new|persistent|rolling`;
- параллельность `forbid_overlap|queue_all|coalesce`;
- обработка пропущенного запуска `skip|run_once|catch_up|within_grace_period`;
- уведомления `always|on_action|on_failure|on_action_or_failure|audit_only`;
- целевая комната;
- максимальное время выполнения и политика повторов.

Рекомендуемые значения по умолчанию: `new`, `coalesce`, `run_once`, `on_action_or_failure`.

## Долговечное планирование

Пользовательские расписания хранятся в PostgreSQL. `automation-scheduler`
ограниченно вызывает специализированный RPC, а авторитетный `control-plane`
выбирает наступившие записи через `FOR UPDATE SKIP LOCKED`, создаёт экземпляр
расписания и корневой execution graph в owner-транзакциях. Уникальный
`(schedule_id, scheduled_for)` предотвращает дубли.

Первый и последующие `next_run_at` вычисляет `control-plane` из
cron/interval/timezone по PostgreSQL time; caller timestamp не является
authority. Специализированный `RunScheduleNow` под owner/version/idempotency
lock создаёт отдельную немедленную occurrence и не двигает плановый watermark.
Scheduler application grant ограничен организацией и разрешает только due/
reservation: exact project выбирает durable server-owned cursor. Исполняемый
graph создаёт отдельная one-time capability exact project/occurrence/attempt/
immutable input/generation/full method/workload/SPIFFE; completion получает
другую capability с durable issue/consume/revoke/readback. Eligibility paused/FORBID/open graph применяется до
bounded `LIMIT`, поэтому штатная строка не создаёт global head-of-line blocker.
Защищённая readiness использует другой короткоживущий grant и отдельный operation
set без прав на due/reservation/materialization/completion. Оба bearer
перевыпускаются до истечения, но не подменяют друг друга.

В target rebuild очередь occurrence и retry принадлежит `control-plane`;
`automation-scheduler` не использует River и его in-memory scheduler. Следующий
запуск и правила пропущенных запусков реализуются доменной моделью. Cron
разбирается готовой библиотекой внутри owner path, а не собственным
синтаксическим анализатором job.

Повтор execution одной occurrence сохраняет один root `ProcessRun`; новая
attempt получает fresh Turn, RuntimeRevision и grant, а immutable
`ScheduledRun` хранит историю попыток. Short-lived bearer JTI/revision/digest
остаются transport replay metadata и не меняют semantic receipt уже
проверенного workload intent.

Kubernetes CronJob не используется для пользовательских расписаний: он не знает о сессиях, привязке поставщика, правах, согласованиях и доставке Mattermost.

## Выполнение без чата

`ScheduledRun` может не иметь `ConversationBinding`. Результат принимает только
закрытые значения `no_action`, `action_taken`, `requires_human`, `failed`.
`no_action` и `audit_only` остаются в аудите без Mattermost delivery. Для
разрешённого policy исхода `control-plane` сохраняет durable delivery вместе с
точным schedule RoomID; `interaction-gateway` не заменяет его actor-default
маршрутом. Это правило одинаково для primary Markdown и всех FILE/IMAGE.
Runner до старта получает из server-owned `RuntimeRevision` и `Turn` immutable
`mattercodex.scheduled-result.v1` manifest через generated control-plane read path;
отсутствующий, неизвестный, дублированный, oversized или неверный JSON fail
closed и не превращается в `action_taken`. Агент может через MCP создать отдельные обсуждения и делегированные
запуски.

## Ручной шлюз запуска по расписанию

Первый принятый runtime result с итогом `requires_human` атомарно переводит
`RuntimeExecution`, `Turn`, `ScheduledRun`, occurrence и связанный `ProcessRun`
в `WAITING_OWNER/SUSPENDED`. В той же транзакции создаётся единственный
server-owned `OwnerGate`, связанный с exact attempt/input/result и schedule
RoomID. Корневой инициатор и policy revision читаются только из сохранённого
process context; payload, имя роли и текущая активная policy не предоставляют
полномочий.

Публикацию карточки после owner transaction выполняет bounded worker
`interaction-gateway` по сохранённым delivery ID/payload/room. До внешнего POST
он получает долговечный claim с lease и fence. Потерянный HTTP-ответ и restart
повторяют ту же logical delivery без второго OwnerGate; scheduler Mattermost не
вызывает.

Только решение сохранённого корневого инициатора после exact delivery binding
разрешает открытый gate. Решение либо expiry одной owner transaction закрывает
Gate, Turn, ProcessRun, occurrence и ScheduledRun либо создаёт server-owned
continuation с fresh Turn/RuntimeRevision. Повтор решения идемпотентен; другой
actor, route, post либо stale attempt/input не закрывает шлюз.

## Процесс ручной приемки результата

`Playbook` поставки unit реализует процесс `ROAD-MC-003`: полный результат,
три параллельных review, пакеты исправлений, повторные трехсторонние проверки,
ограничение пятью автоматическими циклами и обязательный human gate. Manager
не выполняет merge без отдельного решения владельца.

Ежедневный `improver` является отдельным портфельным процессом: он выбирает
merged PR без `improved`, обновляет долговечные инструкции и помечает
обработанные PR. Его выполнение не блокирует независимые волны.

Следующая независимая волна может стартовать параллельно, если не зависит от непринятого результата.
