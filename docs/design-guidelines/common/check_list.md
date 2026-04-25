# Общий чек-лист перед PR

Используется как self-check перед созданием PR. В PR достаточно написать: «чек-лист выполнен, релевантно N пунктов, все выполнены».

## Общее
- Не размыты доменные границы: один сервис/приложение = один bounded context; нет “service-олигарха”.
- Зона выбрана корректно: `internal|external|staff|jobs|dev` (см. `docs/design-guidelines/common/project_architecture.md`).
- Для `external|staff` edge остаётся thin-edge (валидация/auth/маршрутизация), без доменной логики backend.
- Перед написанием кода перечитаны профильные гайды по размещению:
  - backend: `docs/design-guidelines/go/services_design_requirements.md`;
  - frontend: `docs/design-guidelines/vue/frontend_architecture.md`, `docs/design-guidelines/vue/frontend_code_rules.md`, `docs/design-guidelines/vue/frontend_data_and_state.md`;
  - общие принципы: `docs/design-guidelines/common/design_principles.md`.
- Контракты транспорта не “вшиты в код строками” и имеют источник правды:
  - gRPC: целевой `proto/` после его повторного создания (версионирование/совместимость)
  - HTTP: OpenAPI YAML
  - async/webhook payloads: AsyncAPI YAML (если используются)
- Модели/типы/DTO размещены по слоям, а не ad-hoc в service/handler/component файлах.
- Повторяющиеся литералы и ключи вынесены в централизованные константы/enum/type-alias.
- Helper-код размещён по уровню переиспользования:
  - локальный файл (только одно место использования);
  - пакет/модуль (`*_helpers.*`, `lib/*`);
  - целевой `libs/*` (если используется в нескольких сервисах/приложениях и каталог уже заново создан).
- В production-коде нет анонимных структур/объектов для контрактов/персистентных payload-моделей.
- Секреты не хардкодятся и не коммитятся; в логах нет секретов/PII.
- Имена platform env/secrets/CI variables унифицированы с префиксом `KODEX_`;
  имена без префикса `KODEX_` не добавляются.
- Kubernetes манифесты не “вшиты” heredoc’ами в bash: шаблоны лежат в целевом deploy-каталоге,
  а рендер/применение выполняются Go-рантаймом новой архитектуры.
- Для runtime-токенов дефолтные TTL не меньше времени жизни соответствующих контейнеров;
  для MCP baseline TTL не ниже `24h`.
- Для multi-service deploy у каждого deployable-сервиса есть собственные image vars/repositories
  (шаблон нейминга: `KODEX_<SERVICE>_IMAGE` и `KODEX_<SERVICE>_INTERNAL_IMAGE_REPOSITORY`).
- При изменении состава deployable-сервисов синхронно обновлены runtime deploy, build orchestration, bootstrap sync и документация новой архитектуры.
- Порядок выкладки production задан явно и соблюдён:
  stateful dependencies -> migrations -> internal domain services -> edge services -> frontend;
  ожидание зависимостей выполнено через `initContainers` в Kubernetes-манифестах.
- Вынос общего кода в целевой `libs/*` оправдан (>= 2 потребителя); нет “god-lib”.
- Новый production-код не размещён внутри устаревших или архивных каталогов.
- Если добавлена/обновлена внешняя зависимость, обновлён
  `docs/design-guidelines/common/external_dependencies_catalog.md`.
- Для русскоязычных документов, PR и комментариев проверено отсутствие лишней смеси русского и английского; английские термины оставлены только там, где они действительно нужны как точные технические идентификаторы.

## Специфика kodex
- Поддержка оркестрации ограничена Kubernetes; нет кода под другие оркестраторы.
- Kubernetes-операции выполняются через SDK/интерфейсы (не shell-first как основной путь).
- Интеграция с репозиториями идет через интерфейс провайдера; GitHub-специфика не просачивается в домен.
- Для agent runtime соблюдён split access model:
  - git transport (commit/push в рабочую ветку) допускает отдельный bot-token в pod;
  - governance-операции GitHub/Kubernetes (issue/pr/comments/labels/branch context/runtime actions) выполняются через MCP policy/audit контур.
- Процессы запускаются webhook-событиями; workflow-first сценарии не добавлены в обход архитектуры.
- Состояние long-running процессов, слотов, агентных запусков и блокировок хранится в PostgreSQL.
- Данные, требующие гибкой структуры, хранятся в `JSONB`; векторный поиск использует `pgvector`.
- Секреты платформы читаются из env; repo-токены хранятся шифрованно.
- Новые product/runtime switches, которые должны управляться на лету, не введены как env-only flags:
  они оформлены через typed platform settings catalog с явными reload semantics и audit trail.

## Профильные чек-листы
- Если PR затрагивает Go: выполнен `docs/design-guidelines/go/check_list.md`.
- Если PR затрагивает Vue: выполнен `docs/design-guidelines/vue/check_list.md`.
- Если PR затрагивает Go: выполнен `go mod tidy` в изменённых модулях и прогнаны `make lint-go` и `make dupl-go` (или `make lint`) с устранением нарушений.
- Перед пушем выполнена повторная сверка с релевантными чек-листами и устранены все нарушения по размещению моделей/типов/констант/helper-кода.
