---
id: PRD-MC-001
title: Базовый продуктовый контракт MatterCodex
type: product-index
status: approved
owner: product
version: 1.0.1
updated: 2026-08-22
---

# Базовый продуктовый контракт MatterCodex

MatterCodex — универсальная web-first платформа управления ИИ-сотрудниками и
выполняемыми ими Процессами. Control Center является основной пользовательской
поверхностью. Продажи, поддержка, документы, бухгалтерия, право, контент,
аналитика, разработка и эксплуатация используют одну предметную модель.

## Ценность

Платформа позволяет организации:

- создать Проект, ИИ-сотрудника и версионируемые инструкции;
- дать каждой роли собственный promoted Docker image с нужным окружением,
  инструментами и ПО;
- напрямую запускать Agent или многоагентный Workflow;
- продолжать Session, добавлять FIFO turns, отменять и повторять attempts;
- наблюдать server-owned live graph и решать Human Gates в web;
- загружать input и получать результаты/artifacts без внешнего чата;
- создавать schedules и подключать типизированные integrations по мере
  необходимости;
- конфигурировать платформу через всегда готового системного помощника без
  обхода пользовательских полномочий.

## Пользовательские понятия

- `Organization` — компания или владелец инсталляции.
- `Проект` (`Project`) — единственный контейнер участников, конфигурации и работы.
- `ИИ-сотрудник` (`Agent`) — назначение, инструкции, runtime/model, role image,
  capabilities, grants и знания.
- `Процесс` (`Workflow`) — опубликованный сценарий одного или нескольких Agents.
- `Сессия` (`Session`) — долговечный последовательный контекст Agent.
- `Запуск` (`Run`) — root execution с nodes, edges, attempts и events.
- `Решение` (`Human Gate`) — долговечное one-winner ожидание человека.
- `Artifact` — versioned входной или созданный файл.
- `Integration` — необязательное типизированное подключение внешней системы.
- `Помощник MatterCodex` — встроенный системный Agent с реальным warm runtime.

Workspace, Room, Team, Channel, Thread, repository, cluster и provider account не
являются core alias или обязательным authority. Они могут существовать только
как metadata конкретного integration adapter.

## Принятые направления

- первый профиль содержит одну Organization, но tenant boundary присутствует во
  всех агрегатах;
- web-only работает при нуле внешних IntegrationConnection;
- Mattermost имеет четыре независимые optional capabilities и не участвует в
  core readiness;
- MCP сохраняется как runtime protocol типизированных tools и grants;
- каждый обычный turn запускается в отдельном Pod exact promoted role image;
- system assistant использует отдельный always-hot system role image runtime;
- control-plane является единственным владельцем lifecycle, permissions,
  idempotency, OCC, audit, graph/events и transactional outbox;
- fresh install использует новую baseline, без legacy compatibility и cutover.

## Документы раздела

| Код | Файл | Назначение |
| --- | --- | --- |
| `PRD-MC-001` | `docs/product/README.md` | Индекс и границы продукта |
| `PRD-MC-002` | `docs/product/personas.md` | Персоны MVP |
| `PRD-MC-003` | `docs/product/business-processes.md` | Основные процессы |
| `PRD-MC-004` | `docs/product/user-scenarios.md` | Проверяемые сценарии |
| `PRD-MC-005` | `docs/product/requirements.md` | Функциональные и NFR требования |
| `UX-MC-002` | `docs/design/web-first-reset-prompt-pack.md` | Утверждённая карта экранов и самостоятельный prompt pack |
| `UX-MC-003` | `docs/design/mockups/index.md` | Индекс утверждённых HTML-макетов |

## POST-MVP

- публичный marketplace сторонних integrations;
- визуальный BPMN/drag-and-drop editor;
- полноценный multi-tenant SaaS и billing;
- произвольные непроверенные plugins и universal external API proxy.

Функция готова только когда её основной сценарий доступен через Control Center,
имеет авторитетный backend lifecycle, понятные состояния и безопасный recovery.
