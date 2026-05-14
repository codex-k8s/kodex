# Агент #2 — provider-native интеграции

## Зона ответственности

Агент #2 ведёт домен provider-native интеграций. Основной сервис: `provider-hub`.

Подтверждённая ответственность:
- GitHub/GitLab и другие provider-native источники;
- репозитории, Issue, PR/MR, комментарии, ветки, теги и связи как нативные сущности провайдера;
- webhook и сверка внешнего состояния;
- локальные проекции provider-native объектов;
- операционное состояние внешних аккаунтов у провайдера, лимиты и аудит provider-операций;
- взаимодействие с внешними аккаунтами провайдера на границе прав и ссылок на секреты.

`provider-hub` не владеет проектной политикой, пользователями, членством, сырыми секретами, запуском слотов, установками пакетов, публичным HTTP webhook endpoint и UI.

## Что уже сделано

| Срез | PR | Статус | Результат |
|---|---:|---|---|
| PRV-0 | #645 | готово | Доменная документация, границы, требования, модель данных, API-карта и delivery-план `provider-hub`. |
| PRV-1 | #648 | готово | gRPC/AsyncAPI контракты, сгенерированный Go-код и таблица реализации операций. |
| PRV-2 | #653 | готово | Сервисный процесс, PostgreSQL-схема, миграции, репозиторий, конфигурация, health/readiness и базовые тесты. |
| PRV-3 | #666 | готово | Операционное состояние внешних аккаунтов у провайдера, снимки лимитов, журнал операций и базовый GitHub-адаптер `/rate_limit`. |
| PRV-4 | #674 | готово | Webhook inbox, дедупликация, нормализация базовых GitHub-событий и outbox-события `provider.webhook.*`. |
| PRV-5 | #677 | готово | Проекции `Issue`, `PR/MR`, комментариев, review-сигналов, watermark и provider relationships. |
| PRV-6.1 | #682 | готово | Идемпотентная очередь сверки, `sync_cursor`, чтение, список и короткая аренда курсора через `RunReconciliationBatch`. |
| Access bridge | #686 | готово | В `accesscatalog` добавлены provider-действия, `ResolveExternalAccountUsage` возвращает `provider_slug` и ссылку на секрет без значения секрета. |
| PRV-6.2a | #688 | готово | Курсоры сверки и запрос постановки явно фиксируют выбранный `external_account_id`; повтор с другим аккаунтом конфликтует. |
| PRV-6.3 | #703 | готово | `RegisterProviderArtifactSignal` принимает внутренний сигнал от `agent-manager`/MCP/slot-агента, сохраняет signal-level идемпотентность и ставит `hot` cursor без чтения секрета и обращения к provider API. |
| PRV-6.2b | #719 | готово | Пакетная сверка GitHub подтверждает выбранный внешний аккаунт через `access-manager`, получает токен через `libs/go/secretresolver` только в памяти процесса, читает GitHub API, обновляет проекции провайдера, курсор, лимитный бюджет и операционное состояние без хранения токена. |
| PRV-7a | #725 | готово | Контрактный каталог инструментов записи провайдера для `agent-manager`/MCP: типизированные инструменты, общий конвейер команд, контекст политики, ссылка на approval/gate и безопасный результат команды без реализации операций записи. |
| PRV-7b | #731 | готово | Общий конвейер команд операций записи реализован в `provider-hub`: типизированные gRPC handlers, casters, доменный конвейер, идемпотентная запись `ProviderOperation`, проверка `expected_version`, контекст политики и `approval_gate_ref`, но без реальных GitHub/GitLab write-вызовов. |
| PRV-7c | #737 | готово | GitHub write-адаптер подключён к общему конвейеру: создаёт и обновляет задачи, комментарии, `PR`, review-сигналы и provider-native связи, получает секрет только через resolver, обновляет локальные проекции после успешной записи и не повторяет внешний write при replay команды. |

## Текущий бэклог

| Срез | Статус | Почему не завершён |
|---|---|---|
| PRV-8 | требует выбора владельца | Bootstrap/adoption зависят от проектной политики, repository binding и решения по модели `services.yaml`, внешней документации и пакетов. Варианты зафиксированы в `docs/platform/architecture/repository_onboarding.md`; provider-запись должна использовать общий resolver-контракт и существующий конвейер операций записи. |
| PRV-9 | запланировано позже | Kubernetes-манифесты, migration job, metrics, alerts, runbook и smoke можно делать по паттерну `runtime-manager`, когда будет принято решение разворачивать `provider-hub` на сервере. |

## Блокировки от `access-manager`

Снято:
- системные действия `provider.work_item.read`, `provider.issue.write`, `provider.pull_request.write`, `provider.comment.write`, `provider.review_signal.write`, `provider.relationship.write`, `provider.reconciliation.run` заведены в `libs/go/accesscatalog`;
- `ResolveExternalAccountUsage` подтверждает выбранный внешний аккаунт, действие и область использования;
- `access-manager` возвращает `provider_slug`, `secret_store_type` и `secret_store_ref`, но не значение секрета.

Снято общим срезом:
- добавлен `libs/go/secretresolver` с контрактами `Resolver` и `Checker`, безопасным `SecretValue`, mux по `store_type`, поддержкой смонтированных Kubernetes Secret, env и Vault KV v2;
- пакетная сверка и операции провайдера могут получать значение по `secret_store_type + secret_store_ref` после положительного ответа `ResolveExternalAccountUsage`, не сохраняя токен в `provider-hub`.

Снято в `provider-hub`:
- resolver-клиент подключён к обработчику пакетной сверки и GitHub-адаптеру;
- значение секрета очищается после внешнего вызова и не попадает в журнал операций, события, тело аудита, трассировку, логи и ошибки.
- операции записи используют тот же resolver-контур; PRV-7b закрепил общий command pipeline, а PRV-7c подключил GitHub write-вызовы поверх него.
- GitLab write-адаптер остаётся отдельным расширением того же контура.

Требует отдельного решения:
- где физически живёт подтверждение владельца/gate service для политики по риску; `provider-hub` принимает только `approval_gate_ref` и не владеет решением;
- где фиксируется политика выбора внешнего аккаунта для автоматических фоновых сверок, если область содержит несколько подходящих аккаунтов.

## Блокировки от `project-catalog`

Снято для текущего provider-контура:
- `provider-hub` может принимать webhook и строить проекции без `project_id` и `repository_id`; эти поля в проекциях допускают пустое значение.

Реальные блокировки:
- привязка проекций к проекту и repository binding требует контракта сопоставления `provider_slug + repository_full_name/provider repository id -> project_id + repository_id`;
- PRV-8 bootstrap/adoption требует проектной политики, проверенной версии `services.yaml`, состояния repository binding и выбора владельца по вариантам repository onboarding;
- фильтры операционных состояний по проекту/организации не должны включаться, пока область аккаунта не связывается с проектной моделью.

## Блокировки от `package-hub`

Сейчас не блокирует ускоряющие сигналы и базовые provider-операции.

Будущие точки синхронизации:
- если источник пакета находится в Git/provider, `package-hub` должен хранить пакетную истину и нормализованный снимок, а provider-доступ остаётся за `provider-hub` или отдельным адаптерным контуром;
- для PRV-7/PRV-8 нужно согласовать, как пакетные PR, `source_ref` и store refs отображаются в provider relationships.

## Блокировки от `runtime-manager`

Сейчас не блокирует проекции провайдера, webhook, очередь сверки и ускоряющие сигналы.

Требует решения позже:
- если пакетная сверка будет исполняться как платформенная job, нужен контракт постановки и claim job в `runtime-manager`;
- если сверка будет внутренним worker-процессом `provider-hub`, `runtime-manager` не нужен для PRV-6.2;
- deploy-контур `provider-hub` можно делать по уже появившемуся паттерну `runtime-manager`, но только когда будет команда на развёртывание сервиса.

## Блокировки для других агентов

- `project-catalog` зависит от `provider-hub` по provider-native refs, webhook-фактам и связям, но проектная привязка остаётся у `project-catalog`.
- `package-hub` зависит от `provider-hub`, когда пакетный источник является Git/provider-native источником или когда магазин/пакет обновляется через provider-native PR/Issue.
- `agent-manager` и `platform-mcp-server` могут использовать ускоряющие сигналы `provider-hub`; provider-инструменты записи получили типизированный контракт, общий pipeline и GitHub write-адаптер, но MCP/server surface и agent-manager integration остаются отдельными срезами.

## Рекомендуемый следующий шаг

Следующий provider-срез — PRV-8 или отдельный интеграционный срез для MCP/server surface, если владелец решит сначала подключать provider tools к `agent-manager`. Не смешивать bootstrap/adoption с UI или deploy.
