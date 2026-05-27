# Оркестрация агентов

## Назначение

Домен описывает `agent-manager`: flow, этапы, роли, шаблоны промптов, агентные сессии, `Run`, безопасную историю действий, машину приёмки, follow-up задачи и запуск ролевых агентов через runtime-контур.

## Что входит

- `agent-manager` как сервис-владелец оркестрации агентов;
- flow, stage, role, stage-role binding и prompt template;
- версии flow, ролей и prompt, используемые в конкретном `Run`;
- агентные сессии и агентные запуски;
- safe activity timeline по session/run без raw tool payload, stdout/stderr, prompt, transcript и workspace paths;
- машина приёмки;
- правила создания follow-up provider-native задач;
- запуск ролевых агентов через `runtime-manager`;
- чтение руководящих пакетов через `package-hub`;
- provider-native операции через `provider-hub`;
- ожидание Human gate и normalized owner outcome как orchestration state, со ссылками на `interaction-hub` и `governance-manager`;
- MCP-инструменты через `platform-mcp-server`.

## Что не входит

- Слоты, workspace filesystem и platform jobs принадлежат `runtime-manager`.
- Provider-native `Issue`, `PR/MR`, комментарии, связи и GitHub/GitLab операции принадлежат `provider-hub`.
- Пакеты, версии, manifest и установки принадлежат `package-hub`.
- Диалоги, уведомления, внешние каналы и доставка решений принадлежат `interaction-hub`; governance/risk/release gate request и decision record принадлежат `governance-manager`.
- Проектная политика, `services.yaml` и workspace policy принадлежат `project-catalog`.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| Контекст руководящих пакетов в workspace | `architecture/guidance_workspace_context.md` |
| Модель данных | `architecture/data_model.md` |
| API-обзор | `architecture/api_contract.md` |
| План поставки | `delivery/agent_manager_delivery.md` |

## Связанные каталоги

- `docs/catalogs/prompt-roles/`.
- `docs/catalogs/guidance-packages/`.

## Контракты

| Контракт | Путь |
|---|---|
| gRPC `agent-manager` | `proto/kodex/agents/v1/agent_manager.proto` |
| Go-контракты gRPC | `proto/gen/go/kodex/agents/v1/**` |
| AsyncAPI событий `agent.*` | `specs/asyncapi/agent-manager.v1.yaml` |
| Go-контракты событий | `libs/go/platformevents/agent/events.gen.go` |

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/agent-orchestration.md`.
