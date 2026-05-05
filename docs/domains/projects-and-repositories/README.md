# Проекты и репозитории

## Назначение

Домен описывает проекты, репозитории, конфигурацию проекта, политики `services.yaml`, подключение новых репозиториев, источники проектной документации, правила веток, релизные политики, релизные линии и стратегии выкладки.

## Что входит

- проекты;
- репозитории внутри проекта;
- конфигурация проекта и репозитория;
- подключение новых репозиториев;
- привязка источников проектной документации и базовых правил проекта;
- проверенная проекция `services.yaml` для рабочего контура агента;
- правила веток;
- релизные политики;
- релизные линии и стратегии выкладки.

## Что не входит

- зеркало `Issue`, `PR/MR`, комментариев, webhook, лимиты и операции провайдера — зона `provider-hub`;
- роли агентов, процессы, этапы и шаблоны промптов — зона `agent-manager`;
- checkout рабочего контура, slot, `run`, `job`, build и deploy — зона `runtime-manager`;
- серверы, Kubernetes-кластеры и доступность инфраструктуры — зона `fleet-manager`;
- Human gate, уведомления и внешняя обратная связь — зона `interaction-hub`;
- вычисление прав доступа и внешние аккаунты как субъекты политики — зона `access-manager`.

## Документы

- Требования: `product/requirements.md`.
- Дизайн: `architecture/design.md`.
- Модель данных: `architecture/data_model.md`.
- API-обзор: `architecture/api_contract.md`.
- План поставки реализации: `delivery/wave8_project_catalog.md`.

## Реализация

- Сервис-владелец: `services/internal/project-catalog`.
- gRPC-контракт: `proto/kodex/projects/v1/project_catalog.proto`.
- AsyncAPI: `specs/asyncapi/project-catalog.v1.yaml`.
- Миграции: `services/internal/project-catalog/cmd/cli/migrations`.
- Статус контрактов и бэклог реализации: `delivery/wave8_project_catalog.md`.

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/projects-and-repositories.md`.
- Волновая карта: `docs/delivery/issue-map/waves/wave-008-projects-and-repositories.md`.
