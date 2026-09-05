---
id: OPS-ISSUE-1046
title: Доказательства corrective producer unit
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Промежуточная интеграция #1046

Это промежуточная передача контрактов, не заявление о завершении #1046.
База: `1cf399a5f04145395fa746a8f0e7527e5cad01ff`; scheduler и integration
сохранены. Policy revision: 47, включая новые scheduler operations revision 44.

## Стабильные контракты

- `BootstrapState.speech_transcription`: `eligible`, `available`, `reason`.
  CP возвращает `available=false`: runtime проверяет gateway через STT.
- `PlatformQueryService.ListManagedConfigurations`: `project_ref`, `kind`,
  `query`, `page`; ответ `configurations`, `total`, `page`. Content не выдаётся.
- `ListProjectMembershipsRequest.query=3`, прежние поля сохранены.
- `Membership.project_ref=9`: project membership сохраняет свой проект;
  platform membership возвращает пустое значение.
- `GetRuntimeEnvironmentDraft` и специализированные команды
  `CreateRuntimeEnvironmentDraft`, `SaveRuntimeEnvironmentDraft`,
  `ValidateRuntimeEnvironmentDraft`, `PublishRuntimeEnvironmentDraft`,
  `DiscardRuntimeEnvironmentDraft`. Все команды используют `MutationContext`.
  Draft содержит `ref`, `version`, `project_ref`, `environment_ref`,
  `expected_environment_version`, `state`, `specification`, `validation_digest`,
  `diagnostics`, `published_environment_ref`; publish дополнительно возвращает
  `environment`. Create допускает неполную спецификацию без публикации.
- `TranscriptionPolicyProjectionService.ResolveTranscriptionPolicy`: существующий
  контракт получил CP producer и регистрацию; locator сверяется с verified context.
- `RuntimeCredentialProjectionService.MaterializeSystemAssistantCredentials`:
  `execution: MaterializeRuntimeCredentialsRequest`; ответ `projection`.
- `BindAgentRuntimeEnvironmentRequest.version_ref=4` и
  `AgentRuntimeEnvironmentBinding.version_ref=6`: обычный agent binding закрепляет
  точную immutable environment revision; publish не меняет привязки.
- `GetRuntimeEnvironmentImpact(environment_ref,version_ref,page)` возвращает
  `environment_ref,environment_version,target_version_ref,target_digest,consumers,total,page`.
  Consumer: `agent_ref,agent_version,binding_ref,binding_version,version_ref,project_ref`.
  Выдача содержит только доступные actor привязки, которым требуется rebind.
- `RebindRuntimeEnvironment(mutation,environment_ref,version_ref,consumers)`
  возвращает `bindings`. Все выбранные consumers проверяются и обновляются
  атомарно; максимум 100. `mutation.expected_version` относится к environment set.
- `CompleteInteractionDeliveryRequest.unknown_outcome=10`,
  `confirmed_no_effect=11`. Старый отрицательный ответ без доказательства
  отсутствия эффекта тоже считается неизвестным результатом, не retry.

## Границы

| Сценарий | Инициатор и authority | Owner и проверка | Результат |
|---|---|---|---|
| Каталоги Agents/Workflows/Runs/Schedules/Environments/Secrets/Members | Gateway, verified actor/tenant и optional project | CP, повторная application eligibility каждого результата в одном снимке | Ограниченная страница; cursor связан с actor/tenant/filter; событий нет |
| Managed catalog | Gateway, verified actor/tenant | CP, project.view или organization.view | Метаданные и безопасная current revision; точный eligible total; событий нет |
| STT policy | STT continuation от gateway | CP, exact locator и platform.stt.use root actor; enabled config, исполняемая model/language, account и API-key generation | Immutable config identity; не runtime readiness; событий нет |
| STT credential | STT continuation, secret-broker exact peer | CP повторно проверяет end-user eligibility и config/account generation | Exact credential descriptor, без выдачи credential клиенту |
| Project runtime credential | Runtime-controller, execution proof | CP exact lease/fence/generation/session/turn/revision | Project обязателен |
| Global assistant credential | Runtime-controller, отдельная execution operation | CP проверяет system_key, server-owned session/root/turn lineage и отсутствие project/secrets | Org projection; обычная operation не ослаблена |
| Environment draft create/save/validate/discard | Gateway, verified actor/tenant, project.manage | CP owner transaction, OCC и idempotency; scope существующего draft выводится из БД | DRAFT/VALID/INVALID/DISCARDED; audit и receipt, без события; GetRuntimeEnvironmentDraft является read path |
| Environment draft publish | Те же полномочия; VALID и exact dependency digest | CP атомарно публикует environment, проверяет target version и digest; закрывает draft | PUBLISHED; прежний environment AGENT_CHANGED event и idempotent readback |
| VFS и managed catalog | Gateway, verified actor/tenant и optional project | SQL eligibility до LIMIT; exact authorized total; cursor связан с областью | Только ограниченная страница; VFS path не источник полномочий |
| Environment selected rebind | Gateway, project.manage и agent.manage каждого consumer | Owner блокирует target и отсортированные agent rows; сверяет environment/agent/binding versions | По одному AGENT_CHANGED на выбранный consumer в одной транзакции; running RuntimeRevision неизменна |
| Interaction delivery expiry/completion/cancel | Interaction workload, exact lease/fence/generation | Неопределённый исход не доказывает отсутствие внешнего write | UNKNOWN_OUTCOME без reclaim; отрицательный confirmed-no-effect допускает ограниченный retry; incident read path |

## Проверки промежуточной передачи

- `make test-control-plane-postgres`: PASS локально. Повторный прогон включает
  organization-scoped STT credential и stale workspace assertion из #1028.
- `GOWORK=off go test ./...` в control-plane и controlplaneclient: PASS локально.
- Targeted Go secret-broker transport/kubernetes и runtime-controller
  credentialprojection: PASS локально.
- `make test-authority-policy-codegen check-proto-codegen`: PASS локально.
- Дополнительная матрица `TestRuntimeCredentialProjectionScopeMatrix` и
  `TestOrganizationProjectionScopeIsMethodSpecific` закрепляет различие scope.
- Workspace assertion сравнивает весь `runtimeWorkspacePolicy()`, включая
  read-only credential file из #1047; production policy не менялась.
- Повторный PostgreSQL suite с environment draft lifecycle и SQL/evaluator
  parity: PASS локально. Проверены incomplete/invalid, save OCC, validate,
  publish replay, terminal rejection, target environment version conflict,
  discard. Файл: `environment_draft_component_test.go`.
- `TestEnvironmentDraftPolicyRoundTrip`: PASS локально; DNS TCP/UDP сохраняет
  одну destination в редактируемой спецификации.
- `make lint-proto build-proto check-proto-codegen test-authority-policy-codegen`:
  PASS локально. Authority continuation получил отдельный response wrapper;
  номера и типы wire-полей сохранены, lint не отключён.
- Targeted authorityclient и authority transport: PASS локально.
- `make test-web-only-release`: PASS локально с policy revision 46.
- Повторный `make test-web-only-release` с policy 47: PASS локально.
- PostgreSQL suite после SQL eligibility всех восьми каталогов и после
  interaction UNKNOWN_OUTCOME: PASS локально. Проверены expiry без reclaim,
  default failure/confirmed-no-effect/success и честная incident projection.
- Impact PostgreSQL: PASS локально для pagination, target-bound cursor,
  rollback всего stale batch, selected consumer и idempotent replay.
- `TestPublishedEnvironmentDraftRejectsIncompleteOwnerResult`: PASS локально;
  неполный owner result возвращает Internal вместо nil dereference.

## Четвёртая промежуточная передача

- `ListInteractionIdentities`, `BindInteractionIdentity`,
  `RevokeInteractionIdentity`: owner назначает связь exact connection version,
  external team/channel/user digest и активного platform subject. Требуются
  `access.manage` организации и `integration.manage` connection, включая replay.
  Bind использует expected version connection, revoke — version identity.
  Событий нет: авторитетный read path — ListInteractionIdentities; audit и
  idempotency receipt входят в owner transaction.
- `AcceptInteractionMessageRequest`: `external_team_ref=10`, `gate_ref=11`,
  `expected_gate_version=12`, `run_ref=13`. Gate decision проверяет mapped
  human permission, а не полномочия workload. Receipt сохраняет identity и subject.
- `InteractionDeliveryClaim`: `gate_ref=12`, `gate_version=13`, `run_ref=14`.
- Policy revision 48: `platform.interactions.invocations.claim/complete`
  используют существующие RuntimeWorkService RPC. CP выводит route из workload,
  фильтрует claim и expiry, сохраняет workload/lease/fence попытки для проверки
  completion до idempotency readback. Generic gateway не получает INTERACTION.
  Исторический completion без сохранённого fence закрыто отклоняется.
- Draft caster сохраняет отсутствие policy; publish caster проверяет состояние,
  digest, положительную version и соответствие опубликованной ссылки.
- PostgreSQL suite после этих изменений: PASS локально. Отдельный запуск
  identity вместе с human-gate fixture: identity PASS; integration subtest
  отдельно от остальных общих fixtures FAIL на отсутствии runtime claim.
  Полный штатный suite этот сценарий проходит; runtime invariant не ослаблен.
- Targeted Go domain/revision/transport/repository: PASS локально.
- `lint-proto`, `test-authority-policy-codegen`, `check-proto-codegen` и Go
  controlplaneclient: PASS локально. Render policy 48 и race: NOT RUN на этой
  промежуточной передаче.
- ACK delivery, точная привязка delivery к external channel, положительный
  INTERACTION adapter path и connection-test consumer ещё не завершены.
  Эта передача не объявляет #1030 или полный #1046 готовыми.

## Пятая промежуточная передача: ACK

- `InteractionDeliveryClaim.external_team_ref=15`, `external_channel_ref=16`,
  `external_root_post_ref=17`, `acceptance_receipt_ref=18`. Внутренняя capability
  `mattermost.acknowledgements` не является самостоятельно выдаваемым grant:
  delivery создаётся только из owner acceptance receipt с project/run/grant.
- Receipt и ACK вставляются одной owner transaction. Уникальность receipt
  исключает второй ACK при повторном accept. Target team/channel берутся из
  проверенного identity mapping, root/thread — из принятого сообщения.
  IGNORED без маршрута и project/run не создаёт ACK.
- `CompleteInteractionDeliveryRequest.external_team_ref=12`,
  `external_channel_ref=13` обязательны для успешного completion. ACK completion
  сверяет exact channel/team/thread. Gate reply теперь дополнительно сверяет
  сохранённые team/channel delivery; legacy delivery без этих полей закрыто
  отклоняется. UNKNOWN_OUTCOME/retry semantics общие с остальными deliveries.
- `InteractionSource.connection_version=8`, `credential_revision_ref=9`,
  `credential_revision=10`: listener может обнаружить смену immutable projection,
  даже если legacy materialization ref остаётся тем же. Активация credential
  повышает connection.version и переводит connection в NOT_CONNECTED.
- Исправлен недостижимый inbound Agent route: PUBLISHED отсутствует в enum
  Agent. Теперь используется READY с опубликованной instruction, как у launch.
- `testInteractionACK`: PASS в disposable PostgreSQL через реальный accept,
  replay, один ACK, exact claim, wrong-channel/root rejection, complete/replay.
  Identity source test подтверждает смену version при стабильном credential ref.
- Targeted Go transport/repository, `lint-proto`, `check-proto-codegen`: PASS.
  Полный PostgreSQL suite, render и race для этого checkpoint: NOT RUN.
  Внешнего Mattermost вызова тест не выполняет; consumer реализует root #1030.

## Шестая промежуточная передача: credentials и tests

- `InteractionSource.credential_descriptor=11` и
  `InteractionDeliveryClaim.credential_descriptor=19` имеют существующий тип
  `IntegrationCredentialRevision`. SQL проверяет exact connection/organization
  revision; source/delivery без descriptor не выдаётся. Legacy ref сохранён
  только для wire compatibility, не как право чтения произвольного Secret key.
- Policy revision 49 добавляет
  `platform.interactions.connection-tests.claim/complete` для существующих
  `RuntimeWorkService.ClaimIntegrationConnectionTests/CompleteIntegrationConnectionTest`.
  Claim/expiry фильтруют server-derived workload/route; durable workload/lease/fence
  проверяются перед idempotency replay. Generic и interaction worker разделены.
- SQL RuntimeRevision, resolve и claim invocation исключают exact операции
  `mattermost.inbound` и `mattermost.gate_decisions` из agent-callable набора,
  сохраняя системные grants. Подключение общего Capability.CallableByAgent
  ожидает shared registry checkpoint root #1030; этот файл здесь не дублируется.
- Полный PostgreSQL suite с typed descriptor: PASS локально. Дополнительный
  targeted `integration connection tests bind exact workload before replay`:
  PASS; проверены claim изоляция, чужой workload с известным fence, success,
  replay и отказ чужому workload на terminal receipt.
- `lint-proto`, `check-proto-codegen`, `test-authority-policy-codegen`: PASS.
  Targeted Go transport/domain/repository: PASS.
  Render policy 49, race и actual HTTPS Mattermost: NOT RUN на этой передаче.

## Седьмая промежуточная передача: secret impact/rebind

- `GetRuntimeSecretImpact(secret_ref, revision, page)` возвращает secret version,
  target revision, consumers, total и page. Consumer содержит environment ref,
  version/ref, исходные secret revisions и optional agent binding. Eligibility
  `secret.rotate`, `project.manage`, `agent.manage` применяется до SQL LIMIT;
  cursor связан с actor/tenant/secret/точной target revision.
- `RebindRuntimeSecret(mutation, secret_ref, revision, selections)` принимает
  expected secret version и до 32 environment selections / 100 agent consumers.
  Selection содержит environment ref/expected version, source version ref и
  exact consumer tuple. Ответ: environments и bindings. Policy revision 50,
  operations `platform.query.runtime-secrets.impact` и
  `platform.command.runtime-secrets.rebind`.
- Owner transaction создаёт новые immutable environment revisions, сохраняет
  остальные зависимости и меняет только выбранные bindings. Публикация и
  AGENT_CHANGED фиксируются вместе с audit/idempotency receipt. Stale consumer
  откатывает весь batch. Уже созданные RuntimeRevision не переписываются.
- `RuntimeSecretBinding.revision=3`: 0 выбирает текущую revision; положительное
  значение требует точную ACTIVE revision. Recovery сохраняет materialization,
  пока её использует current environment, pinned binding либо активный run.
  Перед выдачей DELETE owner необратимо отмечает revision RETIRED; дальнейшая
  привязка отклоняется. Runtime projection проверяет immutable descriptor, а не
  требует совпадения с текущей revision секрета.
- Migration 00609 закрыто помечает старые non-current revisions RETIRED:
  прежний recovery мог уже удалить materialization без устойчивого DB receipt.
  Миграция не утверждает наличие таких объектов. Возобновление использования
  требует новой rotation/rebind, а не реактивации старой записи.
- Критерий selected/replay/OCC/cursor/retention закреплён в
  `secret_impact_component_test.go`, test `testSecretImpact`. Первый полный
  PostgreSQL suite и targeted Go: PASS. Proto lint/codegen и policy codegen:
  PASS. Повторный полный PostgreSQL suite с forward-only DB trigger: PASS.
  Runtime projection полного credential flow, race и render policy 50:
  NOT RUN на этой передаче. Live Kubernetes не использовался.

## Подключение registry #1030

- Из `6649449a4f143e180298d54a95e6429b8e2e38d1` приняты только Mattermost
  manifest 2.2.0 и общий integrationpackage registry/tests. Generated каталог
  пересоздан штатным `make gen-integration-packages`. Gateway не изменялся.
- RuntimeRevision вызывает `Capability.CallableByAgent()` до prompt capability
  intersection и перед сохранением effective grants; SQL resolve/claim сохраняет
  закрытое исключение двух system operations. Tests
  `TestRuntimeIntegrationGrantsExcludeSystemSubscriptions` и
  `TestFilterIntegrationGrantsCannotBypassEffectiveCapabilities`: PASS.
- `testInteractionHealthRouting` проверяет READY/18 capabilities, создание
  connection, typed credential fixture, только INTERACTION claim/completion и
  отказ generic worker с известным fence. Полный PostgreSQL suite: PASS.
  Прежнее NOT_READY assertion было stale после принятия executable registry.
  Отдельный subtest без предшествующей общей fixture: FAIL (`no rows`),
  не выдаётся за независимую поддержанную проверку.
- integrationpackage Go и `check-integration-package-codegen`: PASS.
  Actual HTTPS Mattermost, race и render на этой передаче: NOT RUN.

## Независимая interaction fixture и Git ownership

- `interaction_identity_component_test.go` создаёт gate на собственном ACK run;
  сначала проверяет положительное owner permission, затем отказ mapped MEMBER.
  UNKNOWN_OUTCOME fixture использует exact ACK run/grant вместо произвольного
  чужого run. Health, identity и connection-test cases больше не зависят от
  предшествующих subtests. Узкий PostgreSQL filter
  `^TestBootstrapComponent$/(interaction_health|interaction_identity|integration_connection_tests)`:
  PASS. Предыдущий fixture FAIL устранён без удаления boundary assertions.
- UI create/validate/publish Git-owned managed configuration закрыто запрещены.
  Copy сохраняет exact parent revision. Detach создаёт новую UI draft с parent,
  сохраняя published current и существующие consumers. Command snapshot сохраняет
  current revision и при работе с новой draft. Проверки находятся в
  `managed_git_component_test.go` и общем managed lifecycle component test.
- Targeted Go repository/transport/domain: PASS. Полный PostgreSQL suite после
  последних fixture corrections: PASS. Git import/writeback и
  immutable Git source provenance ещё не объявлены реализованными.

## STT immutable parameters

- Из #1020 `c91cc7476959687d9e882109977655aec467f9fd` приняты shared STT Proto,
  availability/catalog helpers и modelprofile. Generated пересоздан codegen;
  service/deploy worker #1020 здесь не изменялись.
- `SystemSTTConfiguration`: enabled=12, parameters=13, maximum_audio_bytes=14,
  maximum_audio_duration_milliseconds=15, provider_timeout_milliseconds=16.
  `SystemSTTParameters` содержит languages, keywords, prompt, temperature,
  chunking_strategy, stream. ResolveTranscriptionPolicy отдаёт исходный shared
  `TranscriptionParameters` field 12, без upload overrides.
- Managed JSON содержит `stt.parameters` с camelCase `chunkingStrategy`.
  Модель и параметры проверяются shared modelprofile; неподдерживаемые сочетания,
  stream=true, неизвестные поля и credential values закрыто отклоняются.
  Настраиваемые limits ограничены 25 MiB / 600 s / 120 s provider timeout;
  отсутствующие значения получают 10 MiB / 120 s / 15 s. Digest сверяется с
  immutable content; publish не перепривязывает consumer самовольно.
- Runtime STT admission больше не использует LLM catalog и устаревший SQL Scan.
  Owner разрешает исходного actor по server-owned root run и проверяет его
  `platform.stt.use`, enabled configuration и credential eligibility.
  Это не подтверждение live provider доступности; actual protected stream #1020
  остаётся обязательной второй частью user availability.
- `TestSystemSTTImmutableParametersAndLimits`, `TestSystemSTTModelMatchesExecutableProfile`,
  managed STT PostgreSQL lifecycle: PASS. Полный CP PostgreSQL, targeted Go,
  shared sttapi Go, Proto lint/codegen: PASS. Render/race этой передачи: NOT RUN.
- Дополнительно исправлено stale revision-validator ожидание NOT_READY Mattermost:
  negative case теперь явно портит READY registry descriptor, а не отклоняет
  принятый executable package.

## STT permission существующих установок

Migration 00610 создаёт следующую immutable version только для активных
SYSTEM OWNER/ADMINISTRATOR, которым не хватает `platform.stt.use`.
Переводятся только активные bindings прежней current revision; custom roles,
архивные роли и отключённые bindings не расширяются. Повторное применение
не создаёт новых версий. `STT_system_roles_advance_immutably`: PASS в targeted
PostgreSQL; проверены новая version, неизменность старой и перевод bindings.
Полный suite после 00610: NOT RUN; последний полный PASS относится к STT params.

## Managed draft Save и Discard

Policy 53 и migration 00614 добавляют восемь специализированных RPC:
`Save|Discard{PromptTemplate,RoleImageRevision,IntegrationDefinition,SystemSTTConfiguration}Draft`.
Оба request содержат mutation/configuration_ref/revision_ref; Save дополнительно
content_format/content. Response содержит configuration/revision.

Owner разрешает существующий set по tenant до OCC/receipt, требует
project.manage либо organization.manage и запрещает запись в Git-owned set.
Save допускает неполное содержимое до 256 KiB в закрытом формате, переводит
старый DRAFT/INVALID/VALID в DISCARDED и создаёт новый DRAFT с immutable parent.
Новый revision_ref обязателен; set version увеличивается один раз. Discard
терминален, не меняет current published revision и не переписывает content.
Published/superseded revisions нельзя сохранить или discard. SQL trigger
защищает immutable identity/content и закрытый граф переходов.

В ответах history добавляется строковое state `DISCARDED`; source content
остаётся в immutable истории. Идентификаторы operations:
`platform.command.{prompt-templates,role-image-revisions,integration-definitions,system-stt}.{save-draft,discard-draft}`.
Mutation, audit и receipt сохраняются одной owner-транзакцией. Новый domain
event не вводится; авторитетный read path — ListManagedConfigurationHistory.

Локально PASS: Proto lint/codegen, authority policy codegen, Go transport/domain;
PostgreSQL `^TestBootstrapComponent$/(role_image_promotion|runtime_environment_lifecycle|managed)`.
Проверены четыре kind, неполный save, lineage/history, OCC, terminal discard,
новый draft после discard, published guard и Git write guard. Первые более
узкие запуски FAIL из-за зависимостей старого managed scenario от environment
и promoted-image fixtures; assertions не ослаблялись, зависимости включены явно.
Один `managed_draft` subtest самодостаточен. Git import/writeback этим не реализованы.

## Поиск Impact D7

`GetRuntimeEnvironmentImpactRequest.query = 4` и
`GetRuntimeSecretImpactRequest.query = 4`: строка до 200 Unicode символов,
без NUL; пробелы по краям удаляются. Поиск без учёта регистра по именам и refs
consumer/environment/project выполняется литеральной подстрокой в SQL до LIMIT.
Eligibility остаётся до LIMIT; total относится к доступным найденным строкам.
Cursor связан с organization/actor/authority project, ресурсом, target revision
и поиском. Изменённый поиск с прежним cursor возвращает InvalidArgument.

Локально PASS: Proto lint/codegen, `TestImpactSearchRejectsInvalidInput`/race,
targeted PG `role_image_promotion|runtime_environment_draft|secret_revision_impact`.
Первый запуск без image fixture: FAIL (no rows), исправление assertions не
требовалось.

`GetManagedConfigurationImpactRequest`: `query = 3`, `page = 4`;
`ManagedConfigurationImpact`: `total = 5`, `page = 6`. Поиск по consumer kind/ref,
лимит и правила cursor аналогичны. SQL вычисляет неизменённый full-binding digest
для OCC независимо от доступной страницы; consumer eligibility выполняется до
LIMIT. Rebind отдельно проверяет права на каждого выбранного consumer.
Targeted PG `role_image_promotion|runtime_environment_lifecycle|managed_configuration`
PASS: две страницы, фильтр, отказ cursor при смене query, digest совпадает с
прежним byte-exact алгоритмом. Go/race managed transport/repository и Proto
lint/codegen PASS. Промежуточные FAIL новых fixtures (добавленный agent менял
VFS count; stale configuration version) исправлены порядком fixture и точным
readback текущей version, без ослабления assertions.

## История Assistant D1

`ListAssistantConversationsRequest.query = 3`, `state = 4`;
`AssistantConversation.state = 10`. Закрытый enum: ACTIVE, CLOSED, ARCHIVED;
UNSPECIFIED в запросе означает ACTIVE. Поиск по title/ref ограничен 200 Unicode
символами. SQL проверяет creator и актуальные project.view/organization.view
до LIMIT. Keyset по created_at/ref связан с actor, organization, authority
project и всеми фильтрами. История другого пользователя не выдаётся.

`ArchiveAssistantConversation{mutation, conversation_ref}` возвращает
`conversation`; операция `platform.assistant.conversations.archive`, policy 54.
Owner разрешается до OCC/idempotency; чужой conversation возвращает NotFound.
ACTIVE/CLOSED -> ARCHIVED допускается только без незавершённого run. В одной
транзакции pending plans переходят в REJECTED, сохраняются command receipt,
audit и SYSTEM_ASSISTANT_CHANGED. Повтор с исходным Idempotency-Key возвращает
исходный результат. Новые turns требуют ACTIVE; архив не удаляет историю.
Миграция: 20260904000622.

Локально PASS: PostgreSQL `system_assistant_proposes|assistant_history` (owner,
OCC, busy run, archive/replay, поиск, несколько страниц, подмена actor/filter);
Go assistant transport/repository, controlplaneclient/race, policy codegen,
Proto lint/codegen. Первые проверки FAIL из-за неподготовленного reader fixture
и старого ожидаемого номера policy в тесте; исправлены fixture и revision, без
ослабления проверок доступа. HTTP/browser путь: NOT RUN.

## Доступность Template Variables D3

`ListTemplateVariablesRequest.agent_ref = 4`, `runtime_revision_ref = 5`;
`TemplateVariable.available = 10`, `reason = 11`. Reason - закрытый enum:
AVAILABLE, PROJECT_CONTEXT_REQUIRED, AGENT_CONTEXT_REQUIRED,
RUNTIME_CONTEXT_REQUIRED, NOT_MATERIALIZED. UNSPECIFIED не выдаётся owner-кодом.

Без target доступны organization/user и разрешённый project context. Agent
разрешается через agent.view, его environment читается из текущего owner view.
Для exact RuntimeRevision SQL проверяет organization, authority project,
совпадение optional agent/project и актуальный run.view до чтения snapshot.
В ответ попадают только наличие переменной и reason, не значения, prompt,
credential descriptors или файлы. Пустой materialized collection и нулевой
счётчик доступны; отсутствующая/пустая строковая переменная disabled.
Доступность переменной не является runtime readiness или разрешением запуска.

Cursor теперь связан с tenant/actor/authority project, query и обоими context
refs. RuntimeRevision не подменяется последней revision при чтении. Каталог
замкнут и мал; его страница вычисляется в Go, без загрузки пользовательских
коллекций. Локально PASS: template variable unit/race, transport compile/vet,
Proto lint/codegen и PG `role_image_promotion|runtime_configuration_publish|
session_provider_affinity` с проверками global/agent/sealed context, запрета
без run.view, wrong agent и смены context в cursor. Первый узкий PG запуск
FAIL до новых assertions из-за отсутствия secondary provider fixture;
зависимость включена явно. HTTP/PWA: NOT RUN.

## Оставшаяся реализация

SkillBundle/Memory CRUD, bindings и реальные VFS context узлы реализованы:
см. `context-contracts-1046.md`. Остаются runtime pins/materialization,
рабочий scanner deploy/signature delivery, физический Memory retention GC,
полный VFS дерева сущностей;
сквозная credential matrix secret revisions; проверка Git lifecycle;
полная STT credential matrix; mail authorization producer #1037;
сквозная проверка external subject mapping и INTERACTION routing #1030;
полная негативная матрица, race и финальный exact-SHA review. Все восемь каталогов
выполняют eligibility до SQL LIMIT, с дополнительной per-result проверкой Go.
Это не завершённый unit. UNKNOWN_OUTCOME запрещает автоповтор; отдельный owner
reconciliation workflow ещё не реализован.

Live, deploy, push, PR, merge: NOT RUN, запрещены текущим заданием.
Значения секретов не публиковались. HTTP и PWA меняют отдельные владельцы.
