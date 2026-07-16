---
id: PRD-MC-004
title: Пользовательские сценарии MatterCodex
type: product
status: proposed
owner: product
version: 0.1.0
updated: 2026-07-16
---

# Пользовательские сценарии MatterCodex

## USR-ORG-001. Создать Workspace

Owner задает человекочитаемые имя, slug и описание. MatterCodex создает Workspace и Mattermost team либо предлагает выбрать доступную существующую team. Внутренние IDs передаются скрыто.

## USR-AGT-001. Создать ИИ-сотрудника

Owner выбирает RoleDefinition, имя и bot username, AI account policy, integrations, instructions и runtime profile. Система проверяет identity и показывает effective permissions.

## USR-CHAT-001. Поставить задачу с файлами

Пользователь пишет сообщение, прикладывает PDF и изображение. Агент получает manifest и локальные пути, анализирует материалы и возвращает Markdown-отчет и PNG в исходный thread.

## USR-CHAT-002. Продолжить сессию

Пользователь пишет следующее сообщение тому же агенту. Если turn выполняется, сообщение ставится в FIFO queue. После завершения текущего turn та же сессия продолжается со свежим runtime config.

## USR-RES-001. Принять законченный тип результата

Manager передает owner структуру сервиса и контракты после нескольких review. Owner оставляет замечания, worker исправляет, reviewer перепроверяет, owner дает OK, reviewer выполняет merge и запускает improver.

## USR-PAR-001. Запустить несколько независимых волн

Manager создает отдельные threads для нескольких доменов или сервисов, запускает нужные роли и отслеживает callbacks. Каждый thread имеет собственную session graph и human gate.

## USR-INT-001. Подключить безопасную интеграцию

Administrator выбирает integration definition, вводит connection parameters и secret references, выполняет validation и назначает capabilities агентам. Secret value после сохранения не отображается.

## USR-APR-001. Подтвердить опасное действие

Агент запрашивает изменение внешней системы. Пользователь видит инициатора, tool, target, risk, безопасные аргументы и срок действия. Approve/reject не требует typed command.

## USR-SCH-001. Создать почасовую проверку заявок

Пользователь выбирает sales-manager, пресет «каждый час», timezone, почтовую интеграцию и prompt. При отсутствии заявок run остается в audit без сообщения. При наличии manager создает отдельные threads и делегирует обработку.

## USR-SCH-002. Запустить ежедневного improver

Improver раз в сутки собирает review feedback за период, готовит PR с изменениями инструкций и запускает reviewer. При отсутствии полезных замечаний Mattermost не засоряется.

## USR-NOGIT-001. Работать без репозитория

Workspace не имеет Git integration. Пользователь загружает инструкции и рабочие материалы через UI/Mattermost. Агент получает materialized InstructionSet и artifacts, создает результат и возвращает его в thread.

## USR-CFG-001. Изменить окружение следующего turn

Administrator меняет model, reasoning effort, MCP binding или env grant. Выполняющийся turn не прерывается. Перед следующим turn система вычисляет новый RuntimeRevision и при необходимости пересоздает idle pod, сохранив session state.

## USR-ACC-001. Балансировать новые сессии по AI accounts

Role использует account pool. При создании новой сессии scheduler выбирает разрешенный авторизованный account с учетом свежести данных и доступных лимитов. Resume всегда использует account, закрепленный за сессией.

## USR-OPS-001. Восстановить платформу

Operator разворачивает чистую production-инсталляцию, восстанавливает PostgreSQL PITR, S3 artifacts и Kubernetes configuration, затем выполняет проверяемый restore checklist.
