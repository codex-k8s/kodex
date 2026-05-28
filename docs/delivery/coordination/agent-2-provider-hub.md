# Агент #2 — provider-native интеграции

## Зона ответственности

Агент #2 ведёт домен provider-native интеграций. Основные сервисы: `provider-hub`, `integration-gateway`.

Подтверждённая ответственность:
- GitHub/GitLab и другие provider-native источники;
- репозитории, Issue, PR/MR, комментарии, ветки, теги и связи как нативные сущности провайдера;
- webhook и сверка внешнего состояния;
- локальные проекции provider-native объектов;
- операционное состояние внешних аккаунтов у провайдера, лимиты и аудит provider-операций;
- взаимодействие с внешними аккаунтами провайдера на границе прав и ссылок на секреты.

`provider-hub` не владеет проектной политикой, пользователями, членством, сырыми секретами, запуском слотов, установками пакетов, публичным HTTP webhook endpoint и UI.

## Что уже сделано

| Срез | Issue/PR | Статус | Результат |
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
| PRV-8a | #748 | готово | Provider-side bootstrap для уже созданного пустого репозитория: подготовленные файлы пишутся в bootstrap branch, создаётся или обновляется bootstrap PR, фиксируются проекция, `project_repository_binding` и событие `provider.repository.bootstrap_completed`. |
| PRV-8b | #761 | готово | Создание GitHub-репозитория на стороне провайдера: `CreateRepository` создаёт репозиторий с `auto_init=true`, фиксирует начальный default branch как `base_branch`, журнал операции и событие `provider.repository.created`; `services.yaml`, шаблоны и adoption scan остаются вне `provider-hub`. |
| PRV-8c | #770 | готово | Provider-side adoption существующего репозитория: подготовленные файлы пишутся в adoption branch, создаётся или обновляется adoption PR, фиксируются проекция, `project_repository_binding` и событие `provider.repository.adoption_pr_created`; scan, отчёт и проектное решение остаются вне `provider-hub`. |
| PRV-8d | #840 | готово | GitHub-first provider-side merge signal для bootstrap/adoption PR: `pull_request closed + merged` фиксируется как безопасные refs/digest/timestamps и публикует `provider.repository.bootstrap_merged` или `provider.repository.adoption_merged`; импорт политики остаётся в `project-catalog`. |
| PRV-8e | #865 | готово | Lightweight provider-side adoption repository scan: `ScanRepositoryForAdoption` читает GitHub metadata/ref/tree, фиксирует safe snapshot refs/markers/counts/warnings/digest и публикует `provider.repository.adoption_scan_completed`; raw file contents и project decision остаются вне `provider-hub`. |
| PRV-8f | #883 | готово | Provider-native read surface для bootstrap/adoption: gRPC отдаёт adoption scan snapshots и safe merge signals по stable ids/context с provider refs, branches, commits, status, timestamps, version и safe `etag`; checked artifact/payload остаются в `project-catalog`. |
| PRV-8g | #895 | готово | Smoke/diagnostic producer path: safe GitHub `pull_request closed + merged` fixture проверяет `integration-gateway` route wiring, `provider-hub IngestWebhookEvent`, read surface merge signal и outbox/event-log readiness без consumer framework. |
| PRV-8h | #909 | готово | Adoption merge signal smoke и producer-side hardening: bootstrap/adoption fixtures проверяют route/ingest/read/outbox, replay не создаёт дубль merge event, conflict даёт безопасную диагностику, а raw/canonical webhook payload не попадает в safe signal/read surface/outbox. |
| PRV-4 privacy retention | #908 | готово частично | Retryable webhook payload получает управляемый TTL: `CleanupExpiredWebhookPayloads` явно очищает истёкшие `pending`/`failed` payload в safe envelope с `payload_expired`, а retry после очистки безопасно отказывает без raw payload. |
| PRV-9 | #754 | готово | Эксплуатационный контур: Dockerfile, Kubernetes manifests, PostgreSQL bootstrap, migration job, build/smoke scripts, runbook и monitoring docs. |
| IGW-0 | #781 | готово | Смежный срез `integration-gateway`: зафиксированы границы внешнего HTTP-входа, первый route provider webhook -> `provider-hub.IngestWebhookEvent`, требования security/backpressure/retry/idempotency и OpenAPI-каркас. Реализация gateway-сервиса не входит. |
| IGW-1 | #792 | готово | Сервисный каркас `integration-gateway`: process/config/graceful shutdown, health/readiness/metrics, HTTP router, OpenAPI validation/generated models, safe middleware и provider-hub client interface. Provider route зарегистрирован как отключённый stub до verifier-среза. |
| IGW-2 | #807 | готово | GitHub provider webhook route в `integration-gateway`: source binding `provider_slug=github`, обязательные GitHub headers, HMAC SHA-256 проверка `X-Hub-Signature-256` через `secret_store_type + secret_store_ref`, payload limit и вызов `provider-hub.IngestWebhookEvent` без хранения state в gateway. |
| IGW-4 | #819 | готово | Security hardening активного GitHub route: per-route/per-source in-memory limits, backpressure с `Retry-After`, safe audit summary, replay/idempotency tests и OpenAPI compatibility tests без gateway inbox, БД или provider business logic. |
| IGW-5 | #829 | готово | Deploy-контур `integration-gateway`: Dockerfile, Kubernetes manifests, secret refs, smoke, runbook, monitoring и rollback без реального remote deploy и без хранения gateway state. |
| IGW-6 | #853 | готово | Generic channel/package callback route в `integration-gateway`: `/v1/external-callbacks/{callback_source}` проверяет source binding, HMAC SHA-256 подпись, payload/backpressure guard и вызывает `interaction-hub.RecordChannelCallback` safe envelope без gateway business state. |

## Текущий бэклог

| Срез | Статус | Почему не завершён |
|---|---|---|
| End-to-end repository adoption | ждёт проектного и агентного контура | Adoption существующего repo требует project-catalog policy/import, agent-manager orchestration, deep workspace scan/report и approval; provider-hub уже закрывает lightweight provider snapshot, provider-native PR/relationship/projection запись, safe merge signal и gRPC read surface для этих provider-owned фактов. |
| Webhook inbox privacy-hardening | частично готово | PRV-4 inbox хранит canonical provider webhook payload только для `pending`/`failed` retry/reprocess до `retain_until`; терминальные `processed`/`ignored` и истёкшие retryable записи заменяют `payload_json` safe envelope и используют `payload_sha256` для replay/conflict; наружу read RPC/events/diagnostics отдают только safe envelope/digest. Осталось решить encryption-at-rest/KMS и re-fetch/reprocess для отказа от payload в долгих failed-сценариях. |
| GitLab webhook route | ждёт отдельного решения владельца | IGW-2 активирует только GitHub; GitLab требует отдельный verifier/source policy и OpenAPI-уточнение. |

## Блокировки от `access-manager`

Снято:
- системные действия `provider.work_item.read`, `provider.issue.write`, `provider.pull_request.write`, `provider.repository.write`, `provider.comment.write`, `provider.review_signal.write`, `provider.relationship.write`, `provider.reconciliation.run` заведены в `libs/go/accesscatalog`;
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
- end-to-end bootstrap/adoption требует проектной политики, проверенной версии `services.yaml`, состояния repository binding и выбора владельца по вариантам repository onboarding; PRV-8e возвращает lightweight safe snapshot для planning, PRV-8a/PRV-8b/PRV-8c принимают `project_id`, `repository_id`, provider-native параметры, prepared files и refs как готовый вход, PRV-8d фиксирует safe merge signal, PRV-8f отдаёт эти provider-owned факты через gRPC read surface, а PRV-8g/PRV-8h дают producer-side smoke для route -> ingest -> signal -> outbox по bootstrap/adoption; `provider-hub` не ходит в `project-catalog`;
- фильтры операционных состояний по проекту/организации не должны включаться, пока область аккаунта не связывается с проектной моделью.

## Блокировки от `package-hub`

Сейчас не блокирует ускоряющие сигналы и базовые provider-операции.

Будущие точки синхронизации:
- если источник пакета находится в Git/provider, `package-hub` должен хранить пакетную истину и нормализованный снимок, а provider-доступ остаётся за `provider-hub` или отдельным адаптерным контуром;
- для end-to-end bootstrap/adoption нужно согласовать, как пакетные шаблоны, `source_ref` и store refs отображаются в provider relationships.

## Блокировки от `runtime-manager`

Сейчас не блокирует проекции провайдера, webhook, очередь сверки и ускоряющие сигналы.

Требует решения позже:
- если пакетная сверка будет исполняться как платформенная job, нужен контракт постановки и claim job в `runtime-manager`;
- если сверка будет внутренним worker-процессом `provider-hub`, `runtime-manager` не нужен для текущего контура;
- реальный запуск smoke на кластере выполняется операторским действием с нормализованным bootstrap env, когда владелец даст команду на развёртывание сервиса.

## Блокировки для других агентов

- `project-catalog` зависит от `provider-hub` по provider-native refs, webhook-фактам и связям, но проектная привязка остаётся у `project-catalog`.
- `package-hub` зависит от `provider-hub`, когда пакетный источник является Git/provider-native источником или когда магазин/пакет обновляется через provider-native PR/Issue.
- `agent-manager` и `platform-mcp-server` могут использовать ускоряющие сигналы `provider-hub`; provider-инструменты записи получили типизированный контракт, общий pipeline и GitHub write-адаптер. MCP-0 фиксирует внешнюю поверхность и маршрутизацию, но не переносит provider write pipeline, PRV-8a bootstrap/adoption или agent-manager integration в MCP.

## Рекомендуемый следующий шаг

Следующий provider-срез — интеграция provider tools с `agent-manager`/platform MCP, GitLab write/webhook adapter, encryption/refetch продолжение webhook inbox privacy-hardening или расширение callback routes после новых owner-service contracts. Project-side импорт bootstrap/adoption должен читать lightweight snapshot и safe provider merge signal через provider-hub read surface, а producer-side route/signal можно проверять через staged smoke; проверка `services.yaml`, deep workspace scan/report и активация binding остаются в `project-catalog`/`agent-manager`.
