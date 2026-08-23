---
id: DOM-MC-004
title: ИИ-сотрудники и инструкции
type: domain
status: approved
owner: architect
version: 1.0.1
updated: 2026-08-23
---

# ИИ-сотрудники и инструкции

## Agent

`Agent` принадлежит Project и содержит name, avatar, purpose, role description,
model/runtime selection, current published instructions, role image binding,
capabilities, integration grants, knowledge bindings, external identities,
lifecycle, enabled state и version. Mattermost bot и Git account не обязательны.

## Инструкции

`InstructionDraft` редактируется, проходит structural/security validation и
публикуется как immutable `InstructionVersion`. Runtime получает только exact
published version и provenance. Rollback активирует прежнюю опубликованную
версию новой auditable командой; history не переписывается.

Инструкции не могут:

- назначить actor, permission, root lineage или orchestration authority;
- добавить capability/grant либо изменить policy;
- получить secret value;
- потребовать запуск по display name вместо server-owned catalog ref.

## Capabilities и MCP

Capability — закрытая типизированная возможность платформы или integration.
Разрешённые MCP servers и tools materialize-ятся в свежую RuntimeRevision перед
каждым turn. MCP сохраняется как стандартный runtime protocol, а типизация
означает bounded input/output schema и специализированную серверную команду, а
не замену MCP произвольным внутренним вызовом.

Делегирование, callback, request sync и owner attention выполняются отдельными
платформенными MCP tools. Агент выбирает target из авторитетного каталога и
получает opaque `delegation_ref`; control-plane самостоятельно назначает root,
parent, child, recipient policy и route.

## System Assistant

Помощник MatterCodex — системный Agent со stable key `system-assistant`.
Bootstrap создаёт один экземпляр, protected versioned core prompt и owner
supplement. Domain constraints и команды запрещают delete, archive, disable,
смену purpose и замену core prompt.

Помощник использует только allowlisted MCP tools специализированных owner
commands. Каждая invocation повторно проверяет текущего User и записывает
двойную атрибуцию. Прямые PostgreSQL, Kubernetes и secret-store credentials ему
не выдаются.

## Role image

Agent ссылается только на admitted promoted role image digest. Образ определяет
доступные ОС-пакеты, CLI, языки и прикладное ПО роли. Защищённый
`matter-codex-agent-runner` и runtime contract добавляются после недоверенного
installation step и проверяются supply chain admission.

## События и критерии приёмки

События: `agent.created`, `agent.updated`, `agent.enabled_changed`,
`agent.instructions_published`, `agent.instructions_rolled_back`,
`agent.capability_changed`.

- системный помощник не удаляется и не отключается ни одной transport surface;
- новый turn использует exact published instructions, grants и role image;
- имя роли, prompt и provider response не дают полномочий;
- обычный Agent работает без external identity и integration.
