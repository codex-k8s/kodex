---
doc_id: DSG-CK8S-PACKAGE-0001
type: design-doc
title: kodex — дизайн домена пакетной платформы
status: active
owner_role: SA
created_at: 2026-05-06
updated_at: 2026-05-07
related_issues: [642, 655, 678]
related_prs: []
related_adrs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-06-package-platform-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-06
---

# Детальный дизайн: пакетная платформа

## TL;DR

- Что меняем: вводим `package-hub` как сервис-владелец пакетов, источников магазинов, доступного каталога, установок, версий, manifest и верификации.
- Почему: пакеты должны быть управляемым расширением платформы, а не набором неявных submodule, ручных инструкций и встроенных интеграций.
- Основные компоненты: БД `package-hub`, gRPC API, outbox событий, валидатор manifest, синхронизатор доступного каталога и чтения установленных пакетов.
- Риски: превратить `package-hub` в магазин пакетов, runtime-оркестратор, биллинг или Git-провайдер. Эти контуры должны оставаться в своих доменах.

## Цели

- Зафиксировать границу `package-hub`.
- Подготовить кодовые срезы без старой реализации из `deprecated/**`.
- Дать другим сервисам авторитетные чтения о доступных и установленных пакетах.
- Разделить пакетную истину, runtime-исполнение, provider-native операции и коммерческий контур.

## Не-цели

- Не реализовывать магазин пакетов как часть ядра `package-hub`.
- Не запускать runtime-нагрузки плагинов.
- Не делать checkout источников пакета внутри `package-hub`.
- Не выставлять счета и не принимать платежи.
- Не делать UI в этом домене.

## Граница сервиса

| Владеет `package-hub` | Не владеет |
|---|---|
| Источники магазинов, локальный доступный каталог, локальный установленный каталог, версии пакетов, снимки manifest, статусы проверки, схемы секретов, статус заполненности секретов установки, требования API платформы, ценовые метаданные, события `package.*`. | Бизнес-логика магазина, исходники пакетов как Git-истина, webhook провайдера, runtime-нагрузки, Kubernetes, счета биллинга, сырые секреты, канонические ссылки на заполненные секреты, проектная документация, пользовательский интерфейс. |

Магазин пакетов является устанавливаемым пакетом. `package-hub` знает подключение к магазину, синхронизирует каталог и управляет локальными установками, но не становится владельцем бизнес-логики магазина или его публичного сайта.

Source submodule пакетов остаются одним из способов получить пакет в рабочий контур. `package-hub` хранит проверенную пакетную метаинформацию, версию, источник и статус, а не заменяет Git, объектное хранилище или runtime.

## Компоненты

| Компонент | Назначение |
|---|---|
| `package-hub` | Сервис-владелец пакетного домена. |
| БД `package-hub` | Каноническое состояние источников магазинов, пакетов, версий, установок и проверок. |
| Валидатор manifest | Проверяет структуру пакета, права, secret schema, runtime-требования и локализованные метаданные. |
| Синхронизатор каталога | Получает доступные пакеты из подключённых магазинов и пользовательских источников. |
| Outbox-доставщик | Публикует `package.*` события после фиксации транзакции. |
| Чтения пакетов | Возвращают доступные и установленные пакеты для сервисов, gateway и MCP-инструментов. |

## Основные потоки

### Подключение источника магазина

```mermaid
sequenceDiagram
  participant UI as web-console
  participant G as staff-gateway
  participant A as access-manager
  participant P as package-hub
  participant DB as package DB
  UI->>G: POST /package-sources
  G->>P: ConnectPackageSource(command) over gRPC
  P->>A: CheckAccess(package.source.connect)
  A-->>P: allow
  P->>DB: insert source + command result + outbox
  P-->>G: source
  G-->>UI: PackageSourceResponse
```

Внешняя HTTP-поверхность появляется через `staff-gateway`. Сам `package-hub` остаётся внутренним gRPC-сервисом и не содержит UI-логики.

### Синхронизация доступного каталога

```mermaid
sequenceDiagram
  participant W as worker
  participant P as package-hub
  participant S as store package
  participant DB as package DB
  W->>P: SyncAvailablePackages(source id)
  P->>S: Fetch catalog snapshot
  S-->>P: packages + versions + manifests
  P->>DB: upsert catalog entries + outbox
  P-->>W: sync result
```

Синхронизация не делает пользовательский запрос зависимым от внешнего магазина. UI и внутренние сервисы читают локальный доступный каталог.

### Установка пакета

```mermaid
sequenceDiagram
  participant UI as web-console
  participant G as staff-gateway
  participant A as access-manager
  participant P as package-hub
  participant R as runtime-manager
  UI->>G: POST /package-installations
  G->>P: InstallPackage(command)
  P->>A: CheckAccess(package.install)
  A-->>P: allow
  P->>P: validate manifest, scope, secrets
  P->>P: persist installation + outbox
  P-->>G: installation pending/active
  P-->>R: package.installation.requested event
```

`package-hub` фиксирует установку и публикует событие. Если пакет требует runtime-нагрузку, `runtime-manager` выполняет техническую работу по своему контракту.

### Использование руководящего пакета агентом

```mermaid
sequenceDiagram
  participant AM as agent-manager
  participant P as package-hub
  participant PC as project-catalog
  participant R as runtime-manager
  AM->>P: ListGuidancePackages(scope)
  P-->>AM: package refs + versions + local paths
  AM->>PC: GetWorkspacePolicy(project context)
  PC-->>AM: workspace policy + guidance refs + placement constraints
  AM->>R: PrepareRuntime(agent_run_id, workspace policy, runtime profile, placement constraints)
```

Руководящий пакет не смешивается с проектной документацией. `project-catalog` отвечает за проектные источники, `package-hub` отвечает за пакет и версию руководства.

## Manifest пакета

Manifest является обязательным файлом репозитория-источника пакета. Точный формат будет закреплён в контрактном срезе, но домен уже исходит из следующих блоков:

| Блок | Назначение |
|---|---|
| `identity` | Slug, вид пакета, издатель, лицензия, локализованные название и описание. |
| `source` | Репозиторий, допустимые ref, версия, digest и способ получения. |
| `capabilities` | Возможности пакета: внешний канал, MCP-инструмент, руководство, сайт, магазин. |
| `required_platform_apis` | Какие API платформы нужны пакету. |
| `required_access_actions` | Какие действия доступа нужно выдать пакету или его runtime-нагрузке. |
| `secrets` | Локализованные поля секретов, типы, обязательность и подсказки. |
| `runtime` | Требования runtime-нагрузки, Kubernetes-манифесты, ресурсы, health и ограничения. |
| `pricing` | Бесплатный, платный или коммерчески ограниченный пакет. |
| `verification` | Проверенная версия, статус доверия и ограничения установки. |

## Междоменные связи

| Домен | Связь |
|---|---|
| `access-manager` | Проверяет права на источники, установки, верификацию и управление scope пакета; владеет каноническими ссылками на заполненные секреты. |
| `provider-hub` | Отражает Git-истину репозиториев пакетов, webhook, PR и доступность источника. |
| `project-catalog` | Хранит проектную политику, где пакетные источники и руководящие пакеты могут участвовать в рабочем контуре. |
| `runtime-manager` | Исполняет runtime-нагрузку, checkout, подготовку локального источника и технические задания. |
| `fleet-manager` | Предоставляет допустимые кластеры и контуры размещения для runtime-нагрузки пакета. |
| `agent-manager` | Использует установленные руководящие пакеты, роли и возможности пакетов при подготовке агентной работы. |
| `interaction-hub` | Использует пакеты внешних каналов как подключаемый способ доставки сообщений и согласований. |
| `billing-hub` | Использует ценовые метаданные, установки и факты использования для будущего расчёта. |

## События

Минимальные события:
- `package.source.connected`;
- `package.source.updated`;
- `package.source.disabled`;
- `package.catalog.synced`;
- `package.package.discovered`;
- `package.package.updated`;
- `package.version.discovered`;
- `package.version.updated`;
- `package.version.revoked`;
- `package.verification.updated`;
- `package.installation.requested`;
- `package.installation.activated`;
- `package.installation.updated`;
- `package.installation.disabled`;
- `package.installation.uninstalled`;
- `package.secret_schema.updated`.

События публикуются через сервисный outbox и общий `platform-event-log`. Потребители строят свои проекции или запускают собственную бизнес-логику, но не меняют каноническое состояние `package-hub` напрямую.

Физическое удаление не является штатным бизнес-сценарием первой версии. Завершение жизненного цикла выражается через `disabled`, `revoked` или `uninstalled`.

## Конкурентные изменения

- Изменяемые агрегаты имеют версию.
- Команда, основанная на ранее прочитанном состоянии, передаёт ожидаемую версию.
- Сервис выполняет проверку manifest, scope, секретов и прав в одной короткой транзакции там, где меняет своё состояние.
- При конфликте вызывающая сторона перечитывает актуальное состояние.
- Долгие операции, например запуск runtime-нагрузки или checkout, не держат SQL-блокировку и выполняются в runtime-контуре как отдельное задание.

## Наблюдаемость

- Логи: команда, пакет, версия, источник, scope, actor, correlation id, результат.
- Метрики: количество источников, синхронизаций, установок, ошибок manifest, ошибок доступа к источнику и конфликтов версий.
- Трейсы: входящий gRPC, проверка доступа, валидация manifest, слой репозитория, публикация outbox.
- Алерты: сбой синхронизации магазина, отзыв установленной версии, систематическая ошибка manifest, потеря доступа к приватному источнику.

## Апрув

- request_id: `owner-2026-05-06-package-platform-kickoff`
- Решение: approved
- Комментарий: дизайн домена пакетной платформы согласован как целевое состояние стартового среза.
