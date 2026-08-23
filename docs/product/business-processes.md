---
id: PRD-MC-003
title: Бизнес-процессы web-first MatterCodex
type: product
status: approved
owner: product
version: 1.0.1
updated: 2026-08-23
---

# Бизнес-процессы web-first MatterCodex

## BP-SETUP-001. Первый запуск

1. Владелец входит через OIDC; bootstrap разрешает его Membership по
   инсталляционному owner contract.
2. Control Center показывает готовность Помощника MatterCodex и короткий
   onboarding, не требующий внешней интеграции.
3. Владелец создаёт первый Проект через форму или просит помощника подготовить
   типизированный план.
4. Создаёт ИИ-сотрудника, выбирает назначение, инструкции, runtime/model и
   готовый образ роли либо создаёт рецепт нового образа.
5. Публикует инструкции и запускает ИИ-сотрудника.
6. Наблюдает queued/running/succeeded в live Run workspace, открывает результат
   и скачивает artifact.

## BP-AGENT-001. Создание и запуск ИИ-сотрудника

1. Пользователь открывает Проект и выбирает «Новый ИИ-сотрудник».
2. Указывает только пользовательские данные; capability, integration grant,
   knowledge source и role image выбираются из авторитетных каталогов.
3. Инструкции сохраняются как draft, проходят validation и публикуются
   неизменяемой версией.
4. При запуске control-plane повторно разрешает Project/Agent, создаёт Session,
   Turn, root Run, RuntimeRevision и execution task одной транзакцией.
5. Runtime-controller получает claim и создаёт отдельный Pod exact promoted role
   image. Внутри него защищённый `agent-runner` исполняет только выданную attempt.
6. Результат, usage и artifacts фиксируются до terminal transition.

## BP-WORKFLOW-001. Процесс нескольких ИИ-сотрудников

1. Пользователь создаёт draft Процесса, выбирает coordinator и доступных агентов,
   задаёт bounded input, concurrency, timeout, completion criteria и Human Gates.
2. После validation публикуется immutable Workflow version.
3. Ручной либо плановый запуск создаёт root process node.
4. Координатор вызывает типизированный MCP-инструмент делегирования. Control-plane
   проверяет relationship/capability и создаёт child RunNode/RunEdge без доверия к
   переданному parent/root locator.
5. Каждый child agent выполняется в собственном role image Pod.
6. Terminal child result доставляется coordinator session одним callback turn;
   явный и компенсирующий callback используют одну idempotency запись.
7. Live graph показывает active, completed, waiting и future nodes; low-level
   tool calls остаются в detail/timeline выбранного node.

## BP-ASSISTANT-001. Конфигурация через системного помощника

1. Пользователь формулирует цель обычным языком.
2. Warm assistant runtime анализирует доступные каталоги и предлагает безопасный
   план с точными изменениями, но ничего не меняет напрямую.
3. После явного действия пользователя помощник вызывает закрытый типизированный
   MCP-инструмент той же специализированной команды, что использует web-форма.
4. Команда повторно проверяет полномочия пользователя и сохраняет двойную
   атрибуцию user + system assistant.
5. План может создать Project, Agent, Workflow, metadata подключения, поставить
   проверку подключения, изменить capability/grant, создать Schedule или
   запустить работу. Секрет подключения помощнику не передаётся: он открывает
   обычную защищённую форму Control Center.
6. Подтверждённое изменение сразу появляется в обычном интерфейсе через domain
   event.

## BP-GATE-001. Решение человека

1. Активная attempt открывает server-owned Human Gate с safe context и recipient
   policy и переходит в `WAITING_HUMAN`.
2. Gate появляется в Control Center; optional adapters могут только зеркалировать
   его и принять подписанное решение.
3. Первая допустимая resolution с совпавшей version становится winner, атомарно
   закрывает Gate и создаёт один continuation.
4. Exact retry возвращает сохранённый receipt; иная поверхность получает
   `409 Conflict` с безопасным winner readback.

## BP-INTEGRATION-001. Подключение интеграции

1. Администратор выбирает definition в общем каталоге.
2. Создаёт connection по schema конкретного adapter; browser не получает
   сохранённое значение секрета обратно.
3. Test выполняется отдельной typed operation и меняет только connection
   readiness, не Pod readiness платформы.
4. Capability выдаётся конкретному Agent или Workflow через grant.
5. RuntimeRevision материализует session-scoped MCP binding; dangerous effect
   выполняется gateway с policy, audit, idempotency и при необходимости Gate.
6. Disable/revoke закрывает новые invocation и отображается отдельно от core Run.

## BP-SCHEDULE-001. Автоматический запуск

1. Пользователь выбирает Agent или Workflow, preset/cron, timezone, input,
   session policy и уведомления.
2. Scheduler claim-ит due occurrence через специализированный RPC.
3. Control-plane материализует Run с source `SCHEDULE`; Mattermost room не нужен.
4. Недоступный optional notification adapter создаёт retryable DeliveryAttempt,
   но не меняет успешный core Run на failed.

## BP-RECOVERY-001. Ошибка, cancel и retry

1. Run workspace получает типизированный incident/error code и server-owned
   next actions через realtime event.
2. Cancel одной owner-транзакцией закрывает claims, leases, grants, open gates и
   все активные nodes графа.
3. Retry terminal attempt создаёт новую attempt, RuntimeRevision и `RETRY_OF`
   edge; прежняя попытка остаётся доступной для сравнения и аудита.
