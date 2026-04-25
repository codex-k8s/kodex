# Архитектура проекта

Цель: предсказуемое развитие `kodex` как платформы управления агентами, задачами, runtime и delivery-процессами в Kubernetes.

База: DDD (bounded contexts) + Clean Architecture (зависимости “снаружи внутрь”) + явный инвентарь деплоя + единый каркас директорий.

Устаревшие и архивные каталоги не считаются активной структурой проекта. Новая реализация создаётся в целевой структуре, а не восстанавливается переносом старого кода.

## Архитектурные ограничения kodex

- Оркестратор: только Kubernetes.
- Интеграция с Kubernetes: Go SDK (`client-go`) через интерфейсы и адаптеры.
- Интеграция с репозиториями: интерфейсы провайдеров (`github` сейчас, `gitlab` позже).
- Процессы: webhook-driven (GitHub webhooks/внутренние события), без workflow-first модели.
- Хранилище и синхронизация multi-pod: PostgreSQL (`JSONB` + `pgvector`).
- MCP служебные ручки: реализуются в Go внутри `kodex`.

## Структура репозитория

Целевой верхний уровень:
- `services.yaml` или его новая согласованная замена — инвентарь деплоя и окружений.
- `services/` — сервисы по зонам (`internal|external|staff|jobs|dev`), создаются заново.
- `libs/` — переиспользуемый код (`go|ts|vue`), создаётся только при наличии реального переиспользования.
- `proto/` — gRPC контракты, создаётся заново при появлении внутренних gRPC API.
- `deploy/` — Kubernetes манифесты и overlays, создаются заново.
  - Манифесты и шаблоны YAML (`*.yaml.tpl`) живут в `deploy/base/**`.
  - Рендер и применение выполняются Go-компонентами новой архитектуры; shell-скрипты
    не являются основным механизмом deploy/sync.
  - Для production порядок выкладки задаётся по слоям:
    `stateful dependencies -> migrations -> internal domain services -> edge services -> frontend`.
    Ожидание зависимостей оформляется через `initContainers` в манифестах сервисов.
  - Для monorepo multi-service deploy используются раздельные образы/репозитории для каждого
    deployable-сервиса (шаблон нейминга: `KODEX_<SERVICE>_IMAGE`,
    `KODEX_<SERVICE>_INTERNAL_IMAGE_REPOSITORY`).
- `bootstrap/` — скрипты bootstrap (готовый кластер или установка k3s).
- `docs/` — документация и решения.
- `tools/` — утилиты и генерация, создаются заново при необходимости.

### Изменение состава deployable-сервисов (обязательно синхронно)

Если в монорепо добавляется/удаляется deployable-сервис, либо меняются его docker context / Dockerfile / image repository:
- обновлять сборку/зеркалирование образов в runtime/deploy контуре новой архитектуры;
- обновлять production deploy env/vars, манифесты и инвентарь сервисов;
- обновлять bootstrap-синхронизацию GitHub vars/secrets и секретов Kubernetes:
  `bootstrap/host/config.env.example`,
  `bootstrap/host/bootstrap_remote_production.sh` или новые согласованные bootstrap-компоненты;
- обновлять deploy manifests сервиса в `deploy/base/<service>/*.yaml.tpl`
  и Dockerfile сервиса в `services/<zone>/<service>/Dockerfile`.
- если меняется набор инструментов для agent-runner (dogfooding), обновлять
  целевой runtime/toolchain bootstrap и документировать изменения в новой delivery/ops документации.

Целевое ядро сервисов определяется актуальной архитектурной документацией проекта.
Устаревшие сервисы не являются базой новой реализации.

## Зоны сервисов: internal / external / staff / jobs / dev

### `services/internal/`
- Доменные правила платформы.
- Работа с БД, Kubernetes и repository providers через интерфейсы.
- Нет публичного ingress для бизнес-эндпоинтов.

### `services/external/`
- Публичные webhook/API точки входа.
- Валидация подписи, authn/authz, rate limiting, аудит.
- Без доменной логики orchestration внутри transport слоя.

### `services/staff/`
- Внутренняя консоль (Vue3).
- Доступ через GitHub OAuth и внутреннюю матрицу прав проекта.
- Для каждого frontend-сервиса обязателен собственный Dockerfile с target `dev` и `prod`,
  а также отдельный deploy manifest в `deploy/base/<service>/*.yaml.tpl`.

### `services/jobs/`
- Async/фоновые процессы: reconciliation, ретраи, cleanups, ротации, индексация.
- Идемпотентность и устойчивость обязательны.

### `services/dev/`
- Только dev-инструменты.
- Не деплоятся в production.

## Границы ответственности

Правила:
- Один сервис = один bounded context и одна причина для изменения.
- Домен не зависит от transport/DB SDK напрямую.
- Kubernetes/GitHub/GitLab детали изолированы в адаптерах.
- Shared DB без владельца контекста запрещён; таблицы и данные имеют явного владельца.

## Схема взаимодействия (высокоуровнево)

Конкретная схема взаимодействия определяется актуальной архитектурной документацией проекта.
Этот гайд фиксирует только общую слойность:
- `external/*` принимает внешние события, валидирует вход и маршрутизирует запросы;
- `internal/*` владеет доменной логикой и каноническим состоянием;
- `jobs/*` выполняет фоновые и долгие процессы идемпотентно;
- `staff/*` предоставляет операторский интерфейс и работает через API, а не напрямую с БД;
- интеграции с Kubernetes и repository providers идут через интерфейсы и адаптеры.
