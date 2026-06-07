---
doc_id: API-CK8S-PROJ-0001
type: api-contract
title: kodex — API-обзор project-catalog
status: active
owner_role: SA
created_at: 2026-05-05
updated_at: 2026-06-03
related_issues: [628, 629, 630, 631, 632, 633, 794, 810, 818, 840, 864, 881, 893, 917]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-05-wave8-project-catalog-kickoff"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-05
---

# API-обзор: project-catalog

## TL;DR

- Тип API: внутренний gRPC `ProjectCatalogService`, доменные события `project.*`.
- Аутентификация: через gateway, сервисный токен или MCP-границу; команды дополнительно проверяются через `access-manager`.
- Версионирование: стабильный транспортный `v1`; источники правды — proto и AsyncAPI.
- Основные операции: проекты, репозитории, политика `services.yaml`, источники документации, правила веток, релизная политика, релизные линии, политика размещения, политика рабочего контура.

## Спецификации

- gRPC proto: `proto/kodex/projects/v1/project_catalog.proto`.
- AsyncAPI: `specs/asyncapi/project-catalog.v1.yaml`.
- Внешний HTTP для операторской и администраторской консоли: через тонкий `staff-gateway` с OpenAPI-контрактом, не напрямую из доменного сервиса.

Этот документ фиксирует обзор операций и событий. Фактическими источниками правды для транспорта являются proto и AsyncAPI; если описание ниже расходится с машинной спецификацией, документ и контракт исправляются одним согласованным изменением.

## Операции

`ImportServicesPolicy` принимает нормализованный `validated_payload_json` как источник построения активной проекции. Транспортное поле `service_descriptors` сохранено в `v1` для совместимости контракта, но не является источником канонической проекции: если `valid` payload не содержит сервисных записей, команда должна вернуть `invalid_argument`.

Нормализованный payload также содержит источники документации. Для `valid` политики сервис проверяет scope, путь рабочего контура, режим доступа и связь с сервисами или зависимостями, затем атомарно синхронизирует источники документации, управляемые политикой, вместе с импортом политики. `project-catalog` не выполняет checkout: `GetWorkspacePolicy` возвращает только разрешённый состав источников для `agent-manager` и `runtime-manager`.

`CreateProviderRepository` — project-side команда для начала bootstrap пустого репозитория. Она создаёт или переиспользует pending `Repository` binding, вызывает `provider-hub CreateRepository` по существующему provider contract, сохраняет только безопасные provider refs и `base_branch`, а provider-native запись, журнал `ProviderOperation`, provider projection и webhook-сверка остаются у `provider-hub`.

`CreateRepositoryBootstrapPullRequest` — project-side команда для сценария пустого репозитория по модели C. Она работает только по уже существующему `Repository` binding, проверяет проектную принадлежность, provider target, `base_branch`, подготовленные файлы, обязательный watermark и проверенную проекцию `services.yaml`, затем делегирует запись в `provider-hub CreateBootstrapPullRequest`. Команда не создаёт provider-native репозиторий, не генерирует шаблон репозитория, не выполняет adoption scan и не импортирует политику после merge; эти шаги остаются отдельными срезами.

`ImportBootstrapServicesPolicy` — низкоуровневая project-side команда завершения bootstrap после merge provider-native PR/MR. Команда не читает GitHub/GitLab и не принимает raw provider payload: вызывающий внутренний контур передаёт уже проверенный сигнал, provider target, `base_branch`, `source_ref`, commit, `content_hash`, watermark и нормализованный `validated_payload_json`. `project-catalog` сверяет сигнал с repository binding, проверяет ожидаемую версию pending binding, импортирует `services.yaml` штатным валидатором, сохраняет checked projection и переводит binding в `active`. Повтор того же commit/source ref идемпотентен; другой commit/ref после активации возвращает конфликт.

`ReconcileBootstrapMergeSignal` — стабильная project-side команда для сверки safe provider merge signal и checked artifact input после merge bootstrap PR/MR. Команда принимает только safe `BootstrapRepositoryMergeSignal` и `CheckedBootstrapServicesPolicyArtifact`: provider refs, signal key/id, `base_branch`, `source_ref`, merge commit, watermark digest/payload, artifact ref/digest/version и checked `validated_payload_json`. Provider-owned часть сигнала и snapshot читаются из `provider-hub` через safe read surface merge signals/snapshots; checked artifact, checked payload и нормализованный `services.yaml` остаются входом и хранилищем `project-catalog`, а не registry в `provider-hub`. Команда проверяет, что сигнал относится к bootstrap, artifact digest совпадает с `content_hash`, artifact version привязан к merge commit, watermark digest совпадает с переданным watermark payload, затем вызывает `ImportBootstrapServicesPolicy`. В event-driven контуре consumer события `provider.repository.bootstrap_merged` доставляет событие, восстанавливает safe input и вызывает project-side use-case; если событие уже содержит типизированные поля checked artifact/payload и watermark payload, consumer вызывает `ReconcileBootstrapMergeSignal` и доводит импорт до `ServicesPolicy`/активации binding. Бизнес-логика сверки, импорта и активации binding остаётся в команде, а consumer не становится владельцем project policy state. Если событие содержит только provider-owned refs без checked artifact input, consumer фиксирует `OnboardingSignalReconciliation` со статусом `needs_review` через доменный diagnostic use-case и не импортирует `services.yaml` на основе неполных данных. Если caller не передал `command_id` или `idempotency_key`, project-catalog использует provider `signal_key` как идемпотентный ключ импорта. После штатной проверки доступа команда ведёт project-side журнал `OnboardingSignalReconciliation`: сохраняет safe fingerprint, refs, artifact metadata, итоговый статус, короткий summary и safe error code/summary. Повтор того же `signal_key` с тем же fingerprint идемпотентно обновляет статус, а тот же `signal_key` с другим fingerprint конфликтует до импорта. Сырые webhook body, diff, provider response, YAML-текст и файлы в команду и журнал не передаются.

`ReconcileAdoptionMergeSignal` — стабильная публичная gRPC project-side команда для существующего репозитория после merge adoption PR/MR. Команда использует typed `RepositoryAdoptionMergeSignal` и `CheckedAdoptionServicesPolicyArtifact`, принимает только `signal_kind=adoption` и watermark `work_type=repository_adoption`. Lightweight adoption scan snapshot остаётся planning/read signal и не импортируется как `ServicesPolicy`. В event-driven контуре consumer события `provider.repository.adoption_merged` доставляет safe provider signal, добавляет service actor meta, восстанавливает checked artifact/payload input и вызывает тот же доменный use-case; внешний Go integration runner может использовать публичный RPC без прямого доступа к БД, GitHub/GitLab или consumer internals. Если checked input отсутствует или неполный, consumer пишет `OnboardingSignalReconciliation(needs_review)` через diagnostic use-case. Успешная команда сверяет binding, provider refs, base branch, merge commit, artifact digest/version, content hash, watermark digest и fingerprint, затем импортирует checked projection и активирует pending binding или обновляет active binding новой проверенной версией политики. Повтор того же `signal_key` с тем же fingerprint идемпотентен, конфликтующий fingerprint отклоняется до импорта. Сырые scan payload, webhook body, provider response, diff, YAML-текст, stdout/stderr и секреты не проходят через команду и журнал.

`GetSelfDeploySignal` — project-side read RPC для подготовки безопасного входа `agent-manager CreateSelfDeployPlanFromSignal`. Метод принимает `project_id`, опциональный `repository_id` и provider signal identity (`provider_signal_id` или `provider_signal_key`), читает safe `RepositoryChangeSignal` через `provider-hub`, сверяет его с активным repository binding и обогащает checked project-owned данными: `services_yaml_ref`, `services_yaml_digest`, fingerprint/version активной `ServicesPolicy`, affected service keys из `ServiceDescriptor`, safe path categories, expected runtime job type hints и признак обязательного owner/governance gate. `project-catalog` не читает GitHub/GitLab, не подставляет `path_digest` вместо `services_yaml_digest`, не возвращает `validated_payload_json` и не запускает build/deploy. Если provider сообщает изменение root `services.yaml`, а активная checked projection ещё не импортирована с commit сигнала, метод возвращает `needs_services_policy_reconcile`; если path summary недоступен, возвращается `needs_repository_change_summary`. Готовый статус `ready` означает только, что `agent-manager` может создать pending `SelfDeployPlan` из safe refs/fingerprints; runtime jobs всё равно ждут owner/governance approval.

`GetSelfDeployBuildPlan` — project-side read RPC для подготовки безопасного входа runtime `JOB_TYPE_BUILD` после approved `SelfDeployPlan`. Метод принимает project/repository/source refs, provider/self-deploy signal refs, affected service keys и ожидаемые digest/fingerprint/version проверенной `ServicesPolicy`. Источник данных — только checked project-owned projection `services.yaml`: сервис ищет affected service descriptors, берёт typed build spec из `ServicesPolicy`, сверяет ожидаемую версию политики и возвращает per-service build items, совместимые с `runtime-manager BuildExecutionSpec`: image ref/tag, build context ref/digest, Dockerfile ref/target, builder image ref, allowed secret refs без значений, output refs и стабильный plan fingerprint. Если политика устарела, сервис отсутствует или build spec не задан, метод возвращает safe статус `policy_stale`, `service_not_found` или `build_plan_unavailable`; `agent-manager` не должен создавать `JOB_TYPE_BUILD` при non-ready статусе. Метод не читает raw webhook body, provider response, diff, GitHub/GitLab или полный YAML и не подбирает Dockerfile/image refs по файловой системе.

`cmd/onboarding-runner` использует эти публичные команды как проверочный product API контур для bootstrap/adoption. Dry-run проверяет `ListProjects`, `GetRepository`/`ListRepositories`, provider-owned merge signals, repository change signals, adoption scan snapshots, готовность `bootstrap_setup`, self-repo `repository_binding` и safe `repository_change` plan summary без мутаций. Если `project_id` не передан, runner ищет active project по `organization_id` и slug/display name через `ListProjects`, а apply при отсутствии project вызывает `CreateProject` и печатает safe `project_id` для `KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID`. Apply включается только явно и вызывает `CreateProviderRepository`/`CreateRepositoryBootstrapPullRequest` для пустого тестового репозитория, `AttachRepository` для существующего self-repo binding `codex-k8s/kodex`, а после слияния владельцем и появления safe signal — `ReconcileBootstrapMergeSignal`/`ReconcileAdoptionMergeSignal` при наличии проверенного входа сценария. Для self-deploy binding runner также вызывает публичный `access-manager PutAccessRule` и создаёт или переиспользует узкие service rules: `service/agent-manager` и `service/staff-gateway` получают только `project.policy.read` на `services_policy` в scope `project/<project_id>`, после чего runner проверяет итоговый `CheckAccess`. Эти правила покрывают `GetSelfDeploySignal` и `GetSelfDeployBuildPlan` для orchestration и pre-plan summary, но не дают admin-доступ, доступ к секретам или право изменять project policy. Для apply обязательна safe target policy: разрешённый provider owner, префикс тестового repository name для `bootstrap_setup.create_repository` и точное разрешённое имя репозитория через `KODEX_ONBOARDING_RUNNER_ALLOWED_REPOSITORY` для `repository_binding`. Перед `AttachRepository` runner читает `ListRepositories` и повторно использует existing active binding, поэтому повторный запуск не создаёт дубли. Runner может подготовить checked input из уже нормализованного JSON payload: проверяет JSON-объект, вычисляет `content_hash` как обычный SHA-256 от байтов payload, передаёт совместимый `artifact_digest = content_hash`, формирует artifact ref/version и добавляет watermark payload из сценария или безопасного JSON-файла. `repository_change` строит локальные safe categories/counts/fingerprints для root `services.yaml` и deploy-relevant путей, затем читает `provider-hub GetRepositoryChangeSignal`/`ListRepositoryChangeSignals` по provider refs, branch, commit и kind. Для `push` ожидается path summary с категориями; для `pull_request_merged` допускается отсутствие path summary, потому что детальный список путей приходит через `push` или будущий safe refetch. Этот provider-owned signal является входом self-adoption/update planning, но `project-catalog` не запускает build/deploy и не читает GitHub/GitLab напрямую. Runner не читает БД, не вызывает GitHub/GitLab напрямую, не валидирует сырой YAML и не печатает подготовленные файлы, checked payload, webhook body, provider response, token, DSN или private URL. Содержимое подготовленных файлов используется только как typed input `CreateRepositoryBootstrapPullRequest`; checked artifact и import input остаются безопасными refs/digests/payload projection.

| Операция | Вид | Доступ | Идемпотентность | Примечание |
|---|---|---|---|---|
| `CreateProject` | gRPC command | `project.create` | `CommandMeta.command_id` | Создаёт проект, включая опциональную ссылку на иконку. |
| `UpdateProject` | gRPC command | `project.update` | ожидаемая версия | Обновляет название, описание, статус и ссылку на иконку. |
| `GetProject` | gRPC query | `project.read` | нет | Авторитетное чтение проекта. |
| `ListProjects` | gRPC query | `project.list` | нет | Пакетное чтение для внутренних сервисов и `staff-gateway`. |
| `AttachRepository` | gRPC command | `repository.attach` | `CommandMeta.command_id` | Привязывает репозиторий к проекту. |
| `CreateProviderRepository` | gRPC command | `repository.attach` + provider-side `provider.repository.write` | `CommandMeta.command_id` через `provider-hub` | Резервирует pending binding, создаёт provider repo/base ref через `provider-hub CreateRepository` и сохраняет безопасные refs в binding. |
| `CreateRepositoryBootstrapPullRequest` | gRPC command | `repository.bootstrap` | `CommandMeta.command_id` через `provider-hub` | Готовит project-side bootstrap-контекст для существующего binding и вызывает provider-native bootstrap PR. |
| `UpdateRepository` | gRPC command | `repository.update` | ожидаемая версия | Обновляет статус, ссылку на иконку и поля политики привязки. |
| `DetachRepository` | gRPC command | `repository.detach` | ожидаемая версия | Архивирует привязку репозитория и убирает её из активной политики проекта. |
| `GetRepository` | gRPC query | `repository.read` | нет | Авторитетное чтение привязки репозитория. |
| `ListRepositories` | gRPC query | `repository.list` | нет | Список репозиториев проекта. |
| `ImportServicesPolicy` | gRPC command | `project.policy.import` | `CommandMeta.command_id` | Импортирует `services.yaml`, управляемый через Git, после первичной загрузки, слияния PR или сверки и сохраняет проверенную проекцию. |
| `ImportBootstrapServicesPolicy` | gRPC command | `project.policy.import` | `CommandMeta.command_id` + source commit replay | Принимает проверенный merge-сигнал bootstrap PR, импортирует `services.yaml` и активирует pending repository binding. |
| `ReconcileBootstrapMergeSignal` | gRPC command | `project.policy.import` | `CommandMeta` или provider `signal_key` + source commit replay | Принимает safe provider merge signal и checked artifact metadata, валидирует связь signal/artifact/binding, ведёт safe status journal и запускает import bootstrap policy. |
| `ReconcileAdoptionMergeSignal` | gRPC command | `project.policy.import` | `CommandMeta` или provider `signal_key` + source commit replay | Принимает safe adoption merge signal и checked artifact metadata, валидирует signal/artifact/binding, ведёт safe status journal и импортирует checked policy для существующего репозитория. |
| `GetSelfDeploySignal` | gRPC query | `project.policy.read` | нет | Читает provider-owned `RepositoryChangeSignal` через `provider-hub`, связывает его с repository binding и checked `ServicesPolicy`, возвращает safe enriched signal для pending `SelfDeployPlan` без raw payload, full YAML и build/deploy side effects. |
| `GetSelfDeployBuildPlan` | gRPC query | `project.policy.read` | нет | Читает checked `ServicesPolicy`, сверяет expected digest/fingerprint/version и возвращает per-service `BuildExecutionSpec`-совместимые refs для approved self-deploy build jobs без raw YAML, provider payload и значений секретов. |
| `GetServicesPolicy` | gRPC query | `project.policy.read` | нет | Читает активную проверенную проекцию `services.yaml`. |
| `ListServiceDescriptors` | gRPC query | `project.policy.read` | нет | Читает типизированный список сервисов из последней политики `valid + synced/overridden`. |
| `CreatePolicyEditProposal` | gRPC command | `project.policy.propose` | `CommandMeta.command_id` | Создаёт запрос на PR-изменение `services.yaml` вместо прямой записи в БД. |
| `CreatePolicyOverride` | gRPC command | `project.policy.override` | `CommandMeta.command_id` | Создаёт временное операторское переопределение с причиной, сроком действия и аудитом. |
| `CancelPolicyOverride` | gRPC command | `project.policy.override.cancel` | ожидаемая версия | Досрочно отменяет активное операторское переопределение. Причина берётся из command meta и аудита запроса. |
| `ListPolicyOverrides` | gRPC query | `project.policy.override.read` | нет | Читает активные или исторические операторские переопределения политики. |
| `PutDocumentationSource` | gRPC command | `project.docs.update` | ожидаемая версия | Обновляет источник документации. |
| `GetDocumentationSource` | gRPC query | `project.docs.read` | нет | Читает конкретный источник документации. |
| `ListDocumentationSources` | gRPC query | `project.docs.read` | нет | Читает источники документации проекта, репозитория или сервиса. |
| `GetWorkspacePolicy` | gRPC query | `project.workspace.read` | нет | Возвращает разрешённый состав рабочего контура из активной проверенной политики и активные операторские переопределения. |
| `PutBranchRules` | gRPC command | `project.branch_rules.update` | ожидаемая версия | Обновляет правила веток. |
| `GetBranchRules` | gRPC query | `project.branch_rules.read` | нет | Читает конкретный набор правил веток. |
| `ListBranchRules` | gRPC query | `project.branch_rules.read` | нет | Читает активные правила веток проекта или репозитория. |
| `PutReleasePolicy` | gRPC command | `project.release_policy.update` | ожидаемая версия | Обновляет релизную политику. |
| `GetReleasePolicy` | gRPC query | `project.release_policy.read` | нет | Читает конкретную релизную политику. |
| `ListReleasePolicies` | gRPC query | `project.release_policy.read` | нет | Читает релизные политики проекта. |
| `PutReleaseLine` | gRPC command | `project.release_line.update` | ожидаемая версия | Обновляет конкретную релизную линию. |
| `GetReleaseLine` | gRPC query | `project.release_line.read` | нет | Читает конкретную релизную линию. |
| `ListReleaseLines` | gRPC query | `project.release_line.read` | нет | Читает релизные линии проекта или релизной политики. |
| `PutPlacementPolicy` | gRPC command | `project.placement_policy.update` | ожидаемая версия | Обновляет допустимые контуры размещения. |
| `GetPlacementPolicy` | gRPC query | `project.placement_policy.read` | нет | Читает конкретную политику размещения. |
| `ListPlacementPolicies` | gRPC query | `project.placement_policy.read` | нет | Читает политики размещения проекта, репозитория или сервиса. |

## Модель ошибок

| Ошибка | Когда возвращается |
|---|---|
| `invalid_argument` | Невалидный slug, идентичность провайдера, шаблон ветки, путь рабочего контура или содержимое политики. |
| `permission_denied` | `access-manager` запретил действие. |
| `not_found` | Проект, репозиторий или политика не найдены. |
| `already_exists` | Дубликат slug проекта или идентичности провайдера у активного репозитория. |
| `failed_precondition` | Нельзя применить политику к архивному проекту, отключённому репозиторию или repository binding не принадлежит указанному проекту. |
| `aborted` | Конфликт ожидаемой версии. |
| `unavailable` | Временная ошибка зависимости, БД или provider-side bootstrap команды. |

## События

События фиксируют бизнес-факты жизненного цикла, а не полный CRUD. Физическое удаление не входит в штатный `v1`: вместо `deleted` используются архивирование, отключение, отвязка, истечение срока или отмена.

| Event | Aggregate | Payload минимум |
|---|---|---|
| `project.project.created` | project | `project_id`, `organization_id`, `slug`, `version`; `icon_object_uri`, если задано |
| `project.project.updated` | project | `project_id`, `status`, `version`; `icon_object_uri`, если задано |
| `project.project.archived` | project | `project_id`, `status`, `version` |
| `project.project.disabled` | project | `project_id`, `status`, `version` |
| `project.repository.attached` | repository | `project_id`, `repository_id`, `provider`, `provider_owner`, `provider_name`, `version`; `icon_object_uri`, если задано |
| `project.repository.updated` | repository | `project_id`, `repository_id`, `status`, `version`; `icon_object_uri`, если задано |
| `project.repository.detached` | repository | `project_id`, `repository_id`, `status`, `version` |
| `project.services_policy.imported` | services_policy | `project_id`, `policy_id`, `policy_version`, `source_commit_sha`, `content_hash`, `source_path`, `summary`; `repository_id`, `source_ref`, `source_blob_sha`, `provider_work_item_projection_id`, `provider_web_url` передаются, когда доступны как безопасные ссылки |
| `project.policy_override.created` | policy_override | `project_id`, `override_id`, `target_type`, `expires_at` |
| `project.policy_override.expired` | policy_override | `project_id`, `override_id`, `target_type` |
| `project.policy_override.cancelled` | policy_override | `project_id`, `override_id`, `target_type` |
| `project.documentation_source.created` | documentation_source | `project_id`, `source_id`, `scope_type`, `access_mode` |
| `project.documentation_source.updated` | documentation_source | `project_id`, `source_id`, `scope_type`, `access_mode` |
| `project.documentation_source.disabled` | documentation_source | `project_id`, `source_id`, `status` |
| `project.branch_rules.created` | branch_rules | `project_id`, `branch_rules_id`, `version` |
| `project.branch_rules.updated` | branch_rules | `project_id`, `branch_rules_id`, `version` |
| `project.branch_rules.disabled` | branch_rules | `project_id`, `branch_rules_id`, `status`, `version` |
| `project.release_policy.created` | release_policy | `project_id`, `release_policy_id`, `version` |
| `project.release_policy.updated` | release_policy | `project_id`, `release_policy_id`, `version` |
| `project.release_policy.archived` | release_policy | `project_id`, `release_policy_id`, `status`, `version` |
| `project.release_policy.disabled` | release_policy | `project_id`, `release_policy_id`, `status`, `version` |
| `project.release_line.created` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `version` |
| `project.release_line.updated` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `version` |
| `project.release_line.archived` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `status`, `version` |
| `project.release_line.disabled` | release_line | `project_id`, `release_policy_id`, `release_line_id`, `status`, `version` |
| `project.placement_policy.created` | placement_policy | `project_id`, `placement_policy_id`, `version` |
| `project.placement_policy.updated` | placement_policy | `project_id`, `placement_policy_id`, `version` |
| `project.placement_policy.disabled` | placement_policy | `project_id`, `placement_policy_id`, `status`, `version` |

## Состояние реализации

| Область | Статус |
|---|---|
| gRPC proto `ProjectCatalogService` | Стабильный `v1`, покрывает весь согласованный объём операций. |
| AsyncAPI `project.*` | Стабильный `v1`, покрывает события из этого документа. |
| Сервисный процесс `project-catalog` | Подключены entrypoint, конфигурация, health/readyz/metrics, gRPC-сервер, проверка доступа через `access-manager` и outbox-dispatcher. |
| Бизнес-обработчики gRPC и event-driven use-case | Подключены к доменному сервису для проектов, репозиториев, создания provider repo/base ref через `provider-hub`, bootstrap PR по существующему binding, reconciliation safe bootstrap/adoption merge signal, импорта bootstrap/adoption checked policy после merge, проверенной проекции `services.yaml`, операторских переопределений, источников документации, правил веток, релизных политик, релизных линий и политики размещения. |
| PostgreSQL и outbox | Модель БД, миграции, слой репозитория, журнал onboarding signal, сервисный outbox и публикация событий в `platform-event-log` подключены. |

## Совместимость

- Стабильный `v1` контракт не удаляет поля без цикла `deprecate -> migrate -> remove`.
- Если этот обзор опережает реализацию, документ поставки содержит таблицу реализованных операций и бэклог.
- gRPC-контракт не импортирует transport DTO в домен; преобразование живёт в transport caster слое.

## Апрув

- request_id: `owner-2026-05-05-wave8-project-catalog-kickoff`
- Решение: approved
- Комментарий: API-обзор `project-catalog` согласован как целевое состояние стартового среза; стабильные transport-спецификации создаются отдельным срезом.
