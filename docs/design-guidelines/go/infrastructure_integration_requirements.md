# Инфраструктура: требования к интеграции

Правила работы с PostgreSQL/Redis/Kubernetes/repository providers/секретами/внешними API.
Инфраструктура — деталь и должна быть инкапсулирована (domain/service слои не знают про конкретные SDK).

## PostgreSQL: обязательная модель хранения

- PostgreSQL — единый backend состояния платформы.
- Используем:
  - реляционные таблицы для сущностей и связей;
  - `JSONB` для сессионных/событийных payload;
  - `pgvector` для векторного индекса документов/чанков.
- Схема меняется только миграциями (goose; `-- +goose Up/Down`).
  - В монорепо миграции живут *внутри держателя схемы*:
    `services/<zone>/<service>/cmd/cli/migrations/*.sql`.
  - Если БД/схема общая для нескольких сервисов, всё равно должен быть *один владелец*,
    а остальные сервисы обращаются к БД через его API/контракты (shared DB без владельца запрещён).
- SQL только в `internal/repository/postgres/<model>/sql/*.sql` + `//go:embed`.
- Каждый SQL-запрос в repo слое должен иметь имя-комментарий
  `-- name: <model>__<operation> :one|:many|:exec`.
- Repo слой возвращает доменно-осмысленные ошибки; домен не знает про SQL/pgx.
- Для нового Go-кода подключение к PostgreSQL выполнять через `pgx`-native контур (`pgxpool`).
- `database/sql` допустим только как compatibility-path для внешних библиотек/интеграций, которые не работают с `pgxpool`; такие исключения должны быть явно отражены в коде и в PR.
- Запись коллекций в PostgreSQL не должна маскироваться вспомогательной функцией с циклом `Exec`.
  Для нескольких однотипных строк использовать `pgx.Batch`, `COPY` или один пакетный SQL-запрос.
  Последовательные вспомогательные функции допустимы только для короткого фиксированного набора разных мутаций
  внутри транзакции, например агрегат + outbox + idempotency result.

## PostgreSQL: тестовый контур

- `make test-go` запускает только герметичные Go unit/component tests и не должен требовать PostgreSQL, Docker
  или `KODEX_*_TEST_DATABASE_DSN`.
- PostgreSQL repository, migrations, SQL и storage-level изменения проверяются явным target `make test-go-postgres`.
  Этот target не делает silent skip в required mode: если тестовая БД недоступна, проверка считается незапущенной
  или упавшей в зависимости от требований среза.
- Полный Go-контур с тестовой БД запускается через `make test-go-all`; он объединяет `make test-go`
  и `make test-go-postgres`.
- Предпочтительный remote-agent путь для integration tests — Kubernetes-native runner:
  `KODEX_TEST_POSTGRES_MODE=kubernetes make test-go-postgres`. Runner поднимает временный namespace или использует
  заданный `KODEX_TEST_POSTGRES_K8S_NAMESPACE`, создаёт ephemeral PostgreSQL pod/service, тестовые БД, локальный
  `port-forward`, экспортирует `KODEX_*_TEST_DATABASE_DSN` и удаляет созданные ресурсы после завершения.
- Docker fallback допустим только как локальный convenience path (`KODEX_TEST_POSTGRES_MODE=docker make test-go-postgres`)
  и не является требованием к remote-agent серверу или CI.
- Production PostgreSQL запрещено использовать для repository integration tests. Для required integration-проверки
  нужны внешние тестовые DSN, Kubernetes test namespace/RBAC или локальный Docker fallback в developer-окружении.

## Redis (опционально)

- Только для кэша/эфемерных данных/локов.
- Redis не source of truth.
- TTL обязателен по умолчанию.

## Kubernetes интеграция

- Только Kubernetes как оркестратор.
- Все операции выполняются через Go SDK (`client-go`) и адаптеры.
- Shell-вызовы `kubectl` допустимы только как аварийный fallback и не должны быть основной реализацией.
- Действия, меняющие состояние (pods/deployments/namespaces/secrets), логируются в аудит.
- Для multi-pod корректности используется блокировка/синхронизация через БД.

## Webhook/event processing

- Вход в систему — webhook события от repository providers.
- Каждое событие получает correlation id, сохраняется в БД и обрабатывается идемпотентно.
- Повторная доставка webhook не должна приводить к дублированию действий.
- Долгие операции выполняются через job worker с retry/backoff и фиксированием статуса.

## Repository providers (GitHub/GitLab)

- Интеграция только через provider-интерфейсы.
- GitHub — первая реализация; GitLab добавляется без изменения доменного слоя.
- Token lifecycle (создание/ротация/ревокация) реализуется в сервисе.
- Токены в БД храним в зашифрованном виде.

## Секреты и конфигурация

- Секреты платформы и конфиг деплоя `kodex` читаются из env.
- Пользовательские настройки продукта хранятся в БД и управляются через UI.
- Секреты не коммитим, не логируем и не возвращаем в API-ответах.
- В логах и теле аудита запрещены ключи и токены в открытом виде.

## CI/CD, образы и окружения

- Каждый Go сервис имеет Dockerfile и воспроизводимую сборку по общим правилам
  `docs/design-guidelines/common/project_architecture.md`.
- Стадия `build` компилирует Go-бинарник из исходников сервиса и получает нужные входы:
  `go.mod`, `go.sum`, `proto/**`, `libs/go/**` и сгенерированные контракты, если сервис их использует.
- Стадия `dev` содержит Go hot reload инструмент, например `CompileDaemon`, исходники сервиса,
  `proto/**`, `libs/go/**`, `GOCACHE=/tmp/.cache/go-build` и поддерживает dev-слоты
  без пересборки production-образа.
- Стадия `prod` запускает готовый бинарник в минимальном runtime без исходников и инструментов разработки.
- Корневой `services.yaml` — единый stack inventory deploy-конфигурации в рамках репозитория kodex.
- Go tools читают root stack inventory через `libs/go/stackinventory`; не добавлять новый YAML parser для тех же версий, образов и deploy inventory.
- Проектный `services.yaml` пользовательских репозиториев является project policy и принадлежит `project-catalog`, а не bootstrap/render tooling.
- `services.yaml/spec.versions` задаётся только объектным форматом:
  - `service: { value: "0.1.0", bumpOn: ["services/<zone>/<service>", ...] }`.
- Для Go runtime-сервисов стартовый `resources.limits.cpu` по умолчанию — `2` CPU. Иное значение
  задаётся через конфигурацию окружения и требует явной причины: профиль нагрузки, ограничения кластера
  или результаты замеров.
- Для build image `tagTemplate` должен ссылаться на `spec.versions` через typed helper (`{{ version "<service>" }}`), а не дублировать версию литералом.
- Для `push` в `main/master` допускается авто-bump версии по `bumpOn`:
  - если в merge-коммитах есть изменённый путь, совпадающий с `bumpOn`, платформа поднимает последний numeric token версии;
  - bump коммитится в `main/master`, после чего deploy идёт по новому тегу.
- Платформа должна уметь развернуться в готовый кластер или установить `k3s` через `bootstrap`.
