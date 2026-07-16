---
id: PRD-MC-001
title: Продуктовый baseline MatterCodex
type: product-index
status: proposed
owner: product
version: 0.1.0
updated: 2026-07-16
---

# Продуктовый baseline MatterCodex

MatterCodex — платформа управления ИИ-сотрудниками, их рабочими пространствами, инструкциями, интеграциями и долгоживущими процессами через Mattermost.

Разработка программного обеспечения является одним из готовых сценариев, но не определяет границы продукта. На той же платформе могут работать менеджеры по продажам, бухгалтеры, аналитики, сотрудники поддержки, операторы, редакторы и другие роли.

## Ценность

Платформа должна позволять организации:

- создавать ИИ-сотрудников с понятными ролями и визуально различимыми Mattermost identities;
- выдавать каждому агенту только необходимые AI accounts, integrations, инструкции и runtime capabilities;
- работать с агентами в обычных Mattermost channels и threads;
- запускать управляемые процессы с несколькими агентами и human gates;
- запускать агентов по расписанию без обязательного пользовательского сообщения;
- передавать агентам файлы и получать созданные файлы и изображения обратно в Mattermost;
- сохранять сессии, результаты, audit и artifacts после перезапуска pod;
- использовать платформу без обязательного GitHub или другого репозитория;
- управлять конфигурацией через Control Center, Mattermost и GitOps YAML без ручного ввода технических идентификаторов.

## Продуктовая модель

- `Organization` — компания или клиент инсталляции.
- `Workspace` — проект, отдел или направление; соответствует Mattermost team.
- `Room` — рабочий чат; соответствует Mattermost channel.
- `Thread` — конкретная инициатива или обсуждение внутри room.
- `RoleDefinition` — переиспользуемое описание роли.
- `Agent` — конкретный ИИ-сотрудник с ролью, identity, runtime и grants.
- `Integration` — управляемый доступ к внешней системе или инструменту.
- `InstructionSet` — версионируемый набор инструкций, необязательно связанный с Git.
- `Playbook` — prompt-driven процесс координации агентов и human gates.
- `AutomationSchedule` — расписание запуска агента или playbook.
- `Artifact` — входной или созданный агентом файл.

## Принятые направления

- Одна `Organization` на инсталляцию в первом production-профиле; `organization_id` присутствует в данных с начала миграции.
- Mattermost остается рабочей средой, а отдельный Vue Control Center становится основным интерфейсом сложной настройки и диагностики.
- UI и GitOps YAML поддерживаются одновременно; у каждой сущности есть единственный владелец конфигурации `managed_by: ui|git`.
- OpenAI/Codex является первым runtime provider, но доменная модель остается provider-neutral.
- S3-compatible storage является источником истины для artifacts, attachments, instruction bundles и session archives.
- Опасные интеграционные действия выполняются через MCP и human approval; credential не передается агенту.
- Новая работа реализуется через последовательно проверяемые волны без big-bang rewrite действующего инстанса.

## Документы раздела

| Код | Файл | Назначение |
| --- | --- | --- |
| `PRD-MC-001` | `docs/product/README.md` | Индекс и границы продукта. |
| `PRD-MC-002` | `docs/product/personas.md` | Персоны и критерии успеха. |
| `PRD-MC-003` | `docs/product/business-processes.md` | Основные процессы платформы. |
| `PRD-MC-004` | `docs/product/user-scenarios.md` | Стабильные пользовательские сценарии. |
| `PRD-MC-005` | `docs/product/requirements.md` | Функциональные и нефункциональные требования. |

## Границы первого production baseline

В baseline входят универсальные агенты, Mattermost UX, Control Center, GitOps configuration, AI provider accounts, integrations, approvals, sessions, artifacts, schedules, playbooks, Kubernetes runtime, observability, backup и безопасный deployment.

Не входят до отдельного решения:

- visual BPMN/flow designer;
- биллинг и автоматическое выставление счетов клиентам MatterCodex;
- публичный marketplace сторонних интеграций без review;
- автоматическое выполнение опасных действий в обход approval policy;
- одновременное размещение нескольких недоверяющих организаций в одной инсталляции;
- поддержка произвольных AI runtime providers до стабилизации provider contract.

## Критерий product-ready результата

Функция считается готовой не тогда, когда существует API или typed command, а когда пользователь может пройти основной сценарий без знания внутренних ID, получить понятный результат, диагностировать ошибку и безопасно повторить либо отменить действие.
