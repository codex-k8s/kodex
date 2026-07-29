---
id: GOV-DOC-002
title: Открытые решения владельца
type: governance
status: approved
owner: manager
version: 1.1.0
updated: 2026-07-29
---

# Открытые решения владельца

Здесь хранятся решения, без которых нельзя однозначно реализовать требования.
Документ не подменяет Issue и не расширяет scope.

## Формат

### GOV-OD-001. Название

- адресат: owner / заказчик / architect;
- затронутые документы: `...`;
- статус: `открыт` / `закрыто`;
- рекомендуемый вариант: `...`;
- альтернативы: `...`;
- временная предпосылка до решения: `...`;
- что синхронизировать после решения: docs, contracts, backlog, code,
  configuration, acceptance.

## Реестр

### GOV-OD-001. Модель internal-rpc-authority

- адресат: owner;
- затронутые документы: `ARCH-MC-004`, `GUIDE-DOC-003`, #186;
- статус: `закрыто`;
- выбранный вариант: workload-local issuer/verifier sidecar с UDS, а не
  централизованный сетевой authority service;
- что синхронизировано: architecture, roadmap, unit Issue.

### GOV-OD-002. Координация разработки

- адресат: owner;
- затронутые документы: `ROAD-MC-002`, `ROAD-MC-003`, #179;
- статус: `закрыто`;
- выбранный вариант: корневой manager и не более двух дочерних manager unit;
  director не участвует в активной policy MatterCodex;
- что синхронизировано: roadmap, prompts, policy migration.

Открытых блокирующих решений на начало Epic 1 нет.
