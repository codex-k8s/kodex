---
id: GOV-DOC-004
title: Адаптация технического профиля project-template
type: governance
status: approved
owner: architect
version: 2.0.0
updated: 2026-08-23
---

# Адаптация технического профиля project-template

## Источник

Технические и управленческие правила MatterCodex адаптированы из
`codex-k8s/project-template` на commit
`c354b23412ebc2b19e71d5b154ee1bcb905d9364`.

Копия правил в MatterCodex является нормативной для этого репозитория. Агенты
не читают `project-template` во время обычной реализации и не подменяют
локальные документы более новой внешней версией без отдельного Issue и PR.

## Адаптированные области

- структура монорепозитория и полного unit;
- Go service/gateway/job, PostgreSQL/goose и named SQL;
- OpenAPI, Proto/gRPC, AsyncAPI и единый error contract;
- синхронная межсервисная связь и `internal-rpc-authority`;
- PostgreSQL transactional outbox, broker-neutral relay, NATS JetStream и
  durable inbox;
- Redis read-through cache и правила общих Go libraries;
- lifecycle, observability, Sentry, metrics, traces и alerts;
- Vue/TypeScript PWA и generated clients;
- Kubernetes, images, secrets, supply chain и runbooks;
- эпики, параллельные unit, три направления review и human gate.

## Локальные отклонения

- продуктовые, доменные и архитектурные требования MatterCodex сохраняют свои
  идентификаторы `*-MC-*` и имеют приоритет в вопросах поведения платформы;
- целевая PWA одна: `services/staff/control-center`;
- одна активная программа разработки содержит не более двух unit одновременно;
- верхнеуровневую координацию выполняет manager без обязательной роли director;
- `services/jobs/agent-runner` является защищённым runtime ABI, который
  встраивается в каждый promoted role image; это активный target unit;
- `role-image-builder` и image admission обеспечивают отдельное окружение и
  набор инструментов для каждой роли;
- Mattermost реализуется только optional interaction adapter-ом и не участвует
  в core authority/readiness;
- fresh install не содержит compatibility facade, legacy migration, dual-write
  или cutover path; Git history служит архивом удалённого контура.

## Синхронизация

Обновление upstream выполняется только отдельным документационным Issue:

1. зафиксировать новый upstream commit;
2. сравнить каждый адаптированный документ;
3. классифицировать изменение как применимое, неприменимое или требующее ADR;
4. обновить локальные документы и этот manifest одним PR;
5. пройти архитектурное и security review;
6. получить human gate.

Автоматическое копирование `main` внешнего репозитория запрещено.
