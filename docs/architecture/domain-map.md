---
id: ARCH-MC-003
title: Карта доменов
type: architecture
status: approved
owner: architect
version: 0.1.0
updated: 2026-07-16
---

# Карта доменов

| Домен | Владеет | Не владеет |
| --- | --- | --- |
| Идентификация и доступ | `Organization`, `Membership`, `PlatformRole`, `Policy` | Пользователи Mattermost, учетные данные поставщиков |
| Рабочие области и диалоги | `Workspace`, `Room`, `ThreadBinding`, `ConversationBinding` | Сообщения и файлы Mattermost как двоичные объекты |
| Агенты и инструкции | `RoleDefinition`, `Agent`, `AgentAssignment`, `InstructionSet` | Выполнение сессий, внешние учетные данные |
| Поставщики и учетные записи | `ProviderDefinition`, `AIProviderAccount`, `AccountPool`, наблюдения за лимитами | Промпты агента, сессии другой учетной записи |
| Оркестрация среды выполнения | `AgentSession`, `Turn`, `RuntimeRevision`, `RuntimeLease` | Kubernetes как источник бизнес-состояния |
| Процессы и автоматизации | `Playbook`, `ProcessRun`, `ChildRun`, `AutomationSchedule`, `ScheduledRun` | Выполнение внешних изменений через интеграции |
| Интеграции и согласования | `IntegrationDefinition`, `Connection`, `Capability`, `Grant`, `ApprovalRequest` | Жизненный цикл агента, состояние интерфейса |
| Файлы и знания | `Artifact`, `ArtifactVersion`, `Delivery`, `KnowledgeSpace` | Метаданные файлов Mattermost как источник истины |
| Образы и цепочка поставки | `RoleImageRecipe`, `ImageBuild`, `ImageArtifact` | Состояние сессии среды выполнения |
| Аудит и наблюдаемость | `AuditEvent`, корреляционная модель, операционные проекции | Доменные решения других контекстов |

## Разрешенные направления зависимостей

```text
Identity
  -> Workspaces
  -> Agents

Agents
  -> Providers
  -> Integrations
  -> Images

Conversations / Automations / Processes
  -> Runtime Orchestration

Runtime Orchestration
  -> Providers
  -> Integrations
  -> Artifacts
  -> Kubernetes adapter

Все домены -> Audit / Outbox
```

## Правила границ

- Домен не читает таблицы другого домена напрямую.
- Ссылка на чужую сущность хранит стабильный идентификатор и проверяется через порт прикладного сценария.
- Междоменные изменения выполняются командой или событием, а не общей SQL-транзакцией. Исключение — этап модульного монолита с явно оформленным координатором прикладной транзакции.
- Транспортные DTO не становятся доменными моделями.
- Поля конкретного поставщика хранятся в типизированной конфигурации адаптера и не просачиваются в универсальные сущности.
- `Project`, `Chat` и текущая `AgentRole` являются переходными именами для `Workspace`, `Room`, `Agent` и `RoleDefinition`.

## Миграция текущей модели

| Текущая сущность | Целевая сущность | Правило миграции |
| --- | --- | --- |
| `projects` | `workspaces` | Один к одному; привязка команды Mattermost сохраняется. |
| `chats` | `rooms` | Один к одному; привязка канала сохраняется. |
| `agent_roles` | `role_definitions` + `agents` | Для каждой роли проекта создаются определение и агент без изменения учетной записи бота. |
| `openai_accounts` | `ai_provider_accounts` | Тип поставщика `openai-codex`, ссылка на секрет сохраняется. |
| Учетные записи GitHub, репозитории и env | Соединения и права интеграций | Миграция после появления пакета интеграции GitHub. |
| Шаблоны промптов и наложения конфигурации | `InstructionSet` и `RuntimeProfile` | Сначала адаптер совместимости, затем перенос владения. |
| Сессии и ходы агентов | Домен среды выполнения | Идентификаторы и архив сессии сохраняются. |
