# Runtime и контур серверов и кластеров

## Назначение

Домен описывает два связанных, но разных owner-контура:

- `runtime-manager` владеет слотами, workspace materialization, platform jobs, prewarm, reuse, cleanup, short log tail и техническим статусом среды исполнения;
- `fleet-manager` владеет серверами, Kubernetes-кластерами, связностью, health и placement scope.

`Run` принадлежит `agent-manager`. Runtime хранит только внешние ссылки на agent run и сессии, если они нужны для диагностики, связи с job или операторских проекций.

## Что входит

- режимы выполнения `code-only`, `full-env` и production-контур только для чтения;
- namespace-per-task slot как первая физическая форма слота;
- задел на вложенные и несколько кластеров без изменения доменной модели слота;
- подготовка workspace по политике, которой владеет `project-catalog`;
- materialization руководящих пакетов как read-only workspace sources по refs из `agent-manager` и `package-hub`;
- prewarmed slots и безопасное повторное использование по deterministic fingerprint;
- platform jobs для mirror/build/deploy/cleanup/health-check/housekeeping/workspace materialization/agent Run;
- короткий хвост лога и ссылки на полный источник логов;
- cleanup и retention policy для runtime-объектов;
- явная граница с `fleet-manager`, который выбирает и проверяет инфраструктурный контур;
- реестр нескольких серверов, scope и кластеров как MVP fleet-контура;
- базовый placement resolver и журнал решений размещения;
- bootstrap seed `platform-default` для одиночной установки без ограничения модели нескольких кластеров.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Требования fleet-manager | `product/fleet_manager_requirements.md` |
| Дизайн | `architecture/design.md` |
| Дизайн fleet-manager | `architecture/fleet_manager_design.md` |
| Модель данных | `architecture/data_model.md` |
| Модель данных fleet-manager | `architecture/fleet_manager_data_model.md` |
| API-карта | `architecture/api_contract.md` |
| API-карта fleet-manager | `architecture/fleet_manager_api_contract.md` |
| План поставки | `delivery/runtime_manager_delivery.md` |
| План поставки fleet-manager | `delivery/fleet_manager_delivery.md` |
| Runbook runtime-manager | `ops/runtime_manager_runbook.md` |
| Наблюдаемость runtime-manager | `ops/runtime_manager_monitoring.md` |
| Runbook fleet-manager | `ops/fleet_manager_runbook.md` |
| Наблюдаемость fleet-manager | `ops/fleet_manager_monitoring.md` |

## Контракты

| Контракт | Путь |
|---|---|
| gRPC `runtime-manager` | `proto/kodex/runtime/v1/runtime_manager.proto` |
| AsyncAPI `runtime-manager` | `specs/asyncapi/runtime-manager.v1.yaml` |
| Go-события `runtime.*` | `libs/go/platformevents/runtime/events.gen.go` |
| gRPC `fleet-manager` | `proto/kodex/fleet/v1/fleet_manager.proto` |
| AsyncAPI `fleet-manager` | `specs/asyncapi/fleet-manager.v1.yaml` |
| Go-события `fleet.*` | `libs/go/platformevents/fleet/events.gen.go` |

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/runtime-and-fleet.md`.
