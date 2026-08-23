---
id: ARCH-MC-003
title: Карта доменов
type: architecture
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-23
---

# Карта доменов

| Домен | Владеет | Не владеет |
| --- | --- | --- |
| Идентификация и доступ | `Organization`, `Membership`, `PlatformRole`, `Policy` | Пользователи Mattermost, учетные данные поставщиков |
| Проекты и внешние диалоги | `Project`, `ProjectMembership`, optional `ExternalConversationBinding` | Сообщения и каналы внешнего provider как core state |
| Агенты и инструкции | `RoleDefinition`, `Agent`, `AgentAssignment`, `InstructionSet` | Выполнение сессий, внешние учетные данные |
| Поставщики и учетные записи | `ProviderDefinition`, `AIProviderAccount`, `AccountPool`, наблюдения за лимитами | Промпты агента, сессии другой учетной записи |
| Оркестрация среды выполнения | `Session`, `Turn`, `Run`, `RunNode`, `RunEdge`, `RunEvent`, `RuntimeRevision`, `RuntimeLease` | Kubernetes как источник бизнес-состояния |
| Процессы и автоматизации | `Workflow`, `WorkflowVersion`, `Schedule`, `ScheduleOccurrence` | Выполнение внешних изменений через интеграции |
| Интеграции и согласования | `IntegrationDefinition`, `Connection`, `Capability`, `Grant`, `ApprovalRequest` | Жизненный цикл агента, состояние интерфейса |
| Файлы и знания | `Artifact`, `ArtifactVersion`, `Delivery`, `KnowledgeSpace` | Метаданные файлов Mattermost как источник истины |
| Образы и цепочка поставки | `RoleImageRecipe`, `ImageBuild`, `ImageArtifact` | Состояние сессии среды выполнения |
| Аудит и наблюдаемость | `AuditEvent`, корреляционная модель, операционные проекции | Доменные решения других контекстов |

## Разрешенные направления зависимостей

```text
Identity
  -> Projects
  -> Agents

Agents
  -> Providers
  -> Integrations
  -> Images

Owner sessions / Automations / Processes
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
- `Project` является единственным пользовательским контейнером. `Workspace`,
  `Room`, `Team` и `Chat` не являются core aliases; их locator живут только в
  integration adapters.

## Fresh install

Reset не переносит старые данные и не создаёт compatibility aliases. Bootstrap
идемпотентно материализует Organization, owner claim, системного помощника,
platform capabilities, built-in integration definitions, role runtime ABI и
safe default policies.
