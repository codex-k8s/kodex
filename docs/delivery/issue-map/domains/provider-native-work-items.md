---
doc_id: MAP-CK8S-DOMAIN-PROVIDER-NATIVE-WORK-ITEMS
type: issue-map
title: kodex — карта Issue домена рабочих сущностей провайдера
status: active
owner_role: KM
created_at: 2026-04-25
updated_at: 2026-05-08
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
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/design.md` | PRV-6.2 | partial | Курсоры сверки теперь фиксируют выбранный внешний аккаунт; чтение GitHub API, продвижение курсора, лимитный бюджет и drift status остаются следующими срезами PRV-6.2. |
| #703 | `services/internal/provider-hub/**`, `proto/kodex/providers/**`, `docs/domains/provider-native-work-items/architecture/api_contract.md`, `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-6.3 | done | Ускоряющие сигналы от agent-manager/MCP и slot-агентов ставят hot cursor по provider target и выбранному внешнему аккаунту. |
| не назначено | `services/internal/provider-hub/**`, `docs/domains/provider-native-work-items/architecture/api_contract.md` | PRV-7 | planned | Платформенные provider-операции для agent-manager/MCP. |
| #281, #282 | `docs/domains/provider-native-work-items/delivery/provider_hub_delivery.md` | PRV-8 | planned | Provider-часть empty repository bootstrap и existing repository adoption; сканирование и отчёт по существующему репозиторию выполняет агентная роль через workspace. |
| не назначено | `services/internal/provider-hub/**`, deploy-манифесты, runbook/monitoring docs | PRV-9 | planned | Эксплуатационный контур `provider-hub`. |
