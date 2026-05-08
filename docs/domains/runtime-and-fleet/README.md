# Runtime и контур серверов и кластеров

## Назначение

Домен описывает два связанных, но разных owner-контура:

- `runtime-manager` владеет слотами, workspace materialization, platform jobs, prewarm, reuse, cleanup, short log tail и техническим статусом среды исполнения;
- `fleet-manager` владеет серверами, Kubernetes-кластерами, связностью, health и placement scope.

`Run` принадлежит `agent-manager`. Runtime хранит только внешние ссылки на agent run и сессии, если они нужны для диагностики, связи с job или операторских проекций.

## Что входит

- режимы выполнения `code-only`, `full-env` и production-контур только для чтения;
- namespace-per-task slot как первая физическая форма слота;
- задел на nested cluster и multi-cluster без изменения доменной модели слота;
- подготовка workspace по политике, которой владеет `project-catalog`;
- prewarmed slots и безопасное повторное использование по deterministic fingerprint;
- platform jobs для mirror/build/deploy/cleanup/health-check/housekeeping;
- короткий хвост лога и ссылки на полный источник логов;
- cleanup и retention policy для runtime-объектов;
- явная граница с `fleet-manager`, который выбирает и проверяет инфраструктурный контур.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| Модель данных | `architecture/data_model.md` |
| API-карта | `architecture/api_contract.md` |
| План поставки | `delivery/runtime_manager_delivery.md` |
| Runbook runtime-manager | `ops/runtime_manager_runbook.md` |
| Наблюдаемость runtime-manager | `ops/runtime_manager_monitoring.md` |

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/runtime-and-fleet.md`.
