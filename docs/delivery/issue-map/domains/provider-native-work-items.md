---
doc_id: MAP-CK8S-DOMAIN-PROVIDER-NATIVE-WORK-ITEMS
type: issue-map
title: kodex — карта Issue домена рабочих сущностей провайдера
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-12
---

# Карта Issue — рабочие сущности провайдера

## TL;DR

Долгоживущая карта домена `provider-native-work-items` и сервиса-владельца `provider-hub`.

## Матрица

| Issue/PR | Документы | Волна | Статус | Примечание |
|---|---|---|---|---|
| не назначено | `docs/domains/provider-native-work-items/product/requirements.md`, `docs/domains/provider-native-work-items/architecture/design.md`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/architecture/api_contract.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-0 | done | Доменный каркас и границы `provider-hub`. |
| не назначено | `proto/kodex/providers/**`, `specs/asyncapi/provider-hub.v1.yaml`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-1 | done | gRPC/AsyncAPI контракты и сгенерированный код. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-2 | done | Сервисный каркас, схема БД, миграции и слой репозитория. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/design.md`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-3 | done | Внешние аккаунты у провайдера, лимиты, GitHub-адаптер и журнал операций. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/design.md`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-4 | done | Журнал webhook, дедупликация, нормализация GitHub-событий и базовые outbox-события. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-5 | done | Проекции `Issue`, `PR/MR`, комментариев, review-сигналов, watermark и связей. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/architecture/api_contract.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-6.1 | done | Идемпотентная и атомарная очередь сверки, `sync_cursor`, чтение, список и короткая аренда курсора. |
| #719 | `services/internal/provider-hub/**`, `libs/go/accesscheck/**`, `libs/go/secretresolver/**`, `docs/domains/provider-native-work-items/**`, `docs/platform/architecture/secret_resolution.md` | PRV-6.2b | done | Пакетная GitHub-сверка подключает `ResolveExternalAccountUsage` и `libs/go/secretresolver`, читает GitHub API по арендованному курсору, продвигает курсор, обновляет проекции провайдера, лимитный бюджет и операционное состояние без хранения токена. |
| #703 | `services/internal/provider-hub/**`, `proto/kodex/providers/**`, `docs/domains/provider-native-work-items/architecture/api_contract.md`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-6.3 | done | Ускоряющие сигналы от agent-manager/MCP и slot-агентов сохраняют signal-level идемпотентность и ставят hot cursor по provider target и выбранному внешнему аккаунту. |
| #711 | `libs/go/secretresolver/**`, `docs/platform/architecture/secret_resolution.md`, `docs/domains/provider-native-work-items/**`, `docs/domains/access-and-accounts/**`, `docs/domains/package-platform/**` | PRV-6.4 | done | Общий контракт безопасного разрешения секретов по ссылке после `ResolveExternalAccountUsage`; реализации хранилища `kubernetes_mounted_secret`, `env` и `vault`; значения не попадают в gRPC-ответы, БД, события, аудит, трассировку, логи или ошибки. |
| #725 | `proto/kodex/providers/**`, `docs/domains/provider-native-work-items/architecture/api_contract.md`, `docs/domains/provider-native-work-items/architecture/design.md`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md`, `docs/platform/architecture/provider_integration_model.md`, `docs/platform/architecture/service_boundaries.md` | PRV-7a | done | Контрактный каталог инструментов записи провайдера для `agent-manager`/MCP: типизированные инструменты, общий конвейер команд, контекст политики по риску, ссылка на approval/gate и безопасный результат без реализации операций записи. |
| #729 | `services/internal/provider-hub/**`, `docs/delivery/coordination/**`, `docs/domains/provider-native-work-items/README.md`, `docs/domains/provider-native-work-items/architecture/design.md`, `docs/domains/provider-native-work-items/architecture/data_model.md`, `docs/domains/provider-native-work-items/architecture/api_contract.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md`, `docs/platform/architecture/provider_integration_model.md` | PRV-7b | done | Общий конвейер команд операций записи реализован без реальных GitHub/GitLab write-вызовов: типизированные gRPC handlers, casters, единый domain pipeline, `ProviderOperation` с policy/gate trace, optimistic concurrency и outbox-события `provider.operation.completed/failed`. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-7c | planned | GitHub-адаптер записи для операций из каталога с журналом операций, лимитами, проекциями и событиями. |
| #281, #282 | `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-8 | planned | Provider-часть empty repository bootstrap и existing repository adoption; сканирование и отчёт по существующему репозиторию выполняет агентная роль через workspace. |
| не назначено | `services/internal/provider-hub/**`, deploy-манифесты, runbook/monitoring docs | PRV-9 | planned | Эксплуатационный контур `provider-hub`. |
