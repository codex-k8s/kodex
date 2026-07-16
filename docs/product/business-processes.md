---
id: PRD-MC-003
title: Бизнес-процессы MatterCodex
type: product
status: proposed
owner: product
version: 0.1.0
updated: 2026-07-16
---

# Бизнес-процессы MatterCodex

## BP-SETUP-001. Подготовка рабочей области

1. Owner создает или импортирует Organization.
2. Создает Workspace; система создает или привязывает Mattermost team.
3. Подключает AI provider accounts и integrations.
4. Создает RoleDefinitions и конкретных Agents.
5. Назначает agents integrations, accounts, instructions и runtime profiles.
6. Создает rooms и добавляет участников.
7. Проверяет readiness без просмотра raw secret values.

## BP-CHAT-001. Интерактивная работа с агентом

1. Пользователь пишет top-level сообщение или reply.
2. При отсутствии явного адресата выбирается default agent room/thread.
3. Вложения сообщения проходят artifact ingestion и materialization.
4. Система подтверждает прием сообщения и ставит turn в durable queue.
5. Runtime разрешает свежую конфигурацию, запускает либо продолжает сессию.
6. Агент обновляет progress отдельными notrigger-сообщениями.
7. Финальный ответ и artifacts публикуются от bot identity агента.

## BP-RESULT-001. Доработка результата через human gates

Human gate применяется к завершенному типу результата, например:

- структуре сервиса и границам модулей;
- OpenAPI/gRPC/AsyncAPI контрактам;
- доменным моделям, repositories, services и handlers;
- UX-макетам и описанию экранов;
- runbook, deployment или отчету.

Порядок:

1. Manager формулирует один проверяемый результат и критерии приемки.
2. Worker создает результат.
3. Reviewer выполняет 2-3 прохода с разными либо уточненными целями проверки.
4. Worker устраняет замечания после каждого прохода.
5. Manager передает владельцу только прошедший review результат.
6. Owner оставляет замечания или дает предварительный OK.
7. Worker отрабатывает owner feedback.
8. Reviewer выполняет повторный полный проход.
9. Owner выполняет финальную приемку.
10. После owner OK reviewer выполняет merge или другое утвержденное действие публикации.
11. Reviewer через MCP запускает improver.
12. Improver собирает замечания цикла и обновляет инструкции, guides и playbooks, чтобы ошибки не повторялись.
13. Manager может запускать следующие независимые домены или сервисы параллельными волнами.

Ни manager, ни reviewer не должны трактовать отсутствие ответа owner как автоматическое одобрение.

## BP-DELEGATE-001. Делегирование агенту

1. Агент вызывает другого агента только через MCP delegation tool.
2. Платформа создает audit-событие и служебное notrigger-сообщение.
3. Если target agent занят в этой session scope, запрос сохраняется в очереди.
4. Совместимые queued requests могут быть объединены с указанием каждого инициатора и prompt.
5. По завершении target agent отправляет callback инициатору через durable queue.
6. Обычные Mattermost mentions от bot identities не запускают агентов.

## BP-PAR-001. Параллельные волны

1. Manager создает отдельный thread на каждую независимую инициативу.
2. Для каждого thread задаются цель, роли, integrations, repositories, criteria и human gate.
3. Child runs выполняются независимо и не смешивают session history.
4. Manager получает callbacks и агрегирует статус.
5. Владелец принимает каждый тип результата отдельно либо согласованным batch gate.

## BP-SCHED-001. Периодический запуск агента

1. Пользователь создает AutomationSchedule через UI, выбирая agent/playbook, расписание, timezone и prompt.
2. Scheduler создает уникальное occurrence в PostgreSQL.
3. Run получает актуальные account, integrations, env, config и instructions.
4. Агент выполняется headless либо в связанном Mattermost thread.
5. При `no_action` notification policy может не создавать сообщение.
6. При обнаружении работы агент через MCP создает thread или запускает другого агента.
7. Результат, artifacts, child runs и ошибки сохраняются в audit/history.

## BP-ART-001. Обработка вложений и результатов

1. Входной файл скачивается из Mattermost или integration source.
2. Выполняются normalization, hashing, policy checks и malware scan.
3. Canonical object сохраняется в S3-compatible storage.
4. Файл материализуется read-only в inbox session workspace.
5. Prompt получает manifest с именем, типом, размером, checksum и локальным путем.
6. Агент создает результат только в разрешенном output directory.
7. `publish_artifact` проверяет файл, сохраняет artifact и публикует его в thread.

## BP-APR-001. Опасное интеграционное действие

1. Агент вызывает MCP tool.
2. Integration Gateway проверяет AgentGrant и risk policy.
3. Для gated action создается ApprovalRequest.
4. Человек получает Mattermost/Control Center карточку с безопасным описанием действия.
5. После approve операция выполняется идемпотентно; после reject агент получает структурированный отказ.
6. Request, решение и execution result сохраняются в audit.

## BP-IMPR-001. Непрерывное улучшение инструкций

1. Improver запускается после принятого результата или по расписанию.
2. Собирает review comments, owner feedback, failed runs и повторяющиеся дефекты.
3. Группирует причины, не копируя чувствительные данные.
4. Готовит изменения InstructionSet, guides, role templates или playbooks.
5. Изменения проходят review и human gate как отдельный результат.
