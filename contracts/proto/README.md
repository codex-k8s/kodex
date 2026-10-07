---
id: CONTRACT-DOC-002
title: Внутренние Proto/gRPC-контракты
type: contract-guide
status: approved
owner: architect
version: 1.0.2
updated: 2026-10-04
---

# Внутренние Proto/gRPC-контракты

Исходные контракты располагаются по пути:

```text
contracts/proto/<service>/v<major>/*.proto
```

## Собственная конфигурация помощника через managed MCP

Сценарий #1797 использует существующий `get_configuration_catalog` с закрытым
selector `assistant_configuration_catalog: {kind: CURRENT_CONFIGURATION,
assistant_ref: <собственный agent_ref>}`. `assistant_ref` — только locator,
не источник полномочий. Query, pagination, account/profile override и другие
selectors для этого вида запрещены. Новое имя инструмента или RPC не вводится.

| Этап | Проверка и владелец |
| --- | --- |
| Runtime → `/runtime/mcp`, `tools/call` | Runtime-controller проверяет execution ticket и exact binding; прежний `RecordRunToolCall` проверяет `platform.configuration.read` перед чтением |
| Controller → `RuntimeWorkService.SearchAssistantResources` | Тот же generated client и зарегистрированный workload/method; Proto kind `CURRENT_CONFIGURATION` |
| CP service → repository | Caller `runtime-controller`, permission `platform.runtime.assistant.resources.search`; ограниченные request поля |
| Authoritative CP snapshot | Lease/fence/generation, незавершённые run/node/session/turn и immutable revision lineage; текущие root initiator/membership, exact source helper и canonical configuration-view eligibility |
| Собственный target | Для SYSTEM и PROJECT требуется `assistant_ref == sourceRef` из lease; SYSTEM сохраняет ORGANIZATION, PROJECT — exact project/profile; project экран не меняет область SYSTEM |
| Ответ → MCP consumer | Новый `AssistantCurrentConfiguration` содержит только безопасную typed модель; controller закрыто проверяет scope/agent/binding, unknown fields/enums, версии, digest и размер |

`current_configuration` содержит свежие опубликованные config/overlay pins,
полный опубликованный шаблон инструкций, SYSTEM core/owner instructions и их
ревизии, environment/binding/image pins, tools, ресурсы/тома/сеть/RBAC,
несекретные environment values и каталог template variables. Доступность
переменных берётся из свежего canonical prompt-preview context, а не из
подставленных клиентом значений. Template digest относится к исходной immutable
ревизии шаблона; SYSTEM `published_instructions` дополнительно содержит owner
instructions, которые закреплены отдельной owner revision. Секреты представлены
только `name`, логическим `secret_ref`, `revision`; значения, Kubernetes
имена/namespace/key/UID/resourceVersion и content digest не выдаются.

Отдельный `execution_snapshot` содержит безопасные pins уже назначенного CP
immutable `RunnerInput` текущего exact run/node/session/turn/attempt. Он не
подменяет свежие настройки и не получает новый срок, lease или полномочия.
Изменение current во время хода не переписывает этот snapshot. Int64 внутри
`current_configuration` представлены каноническими ProtoJSON decimal strings;
`execution_snapshot` использует прежние bounded JSON числа.
У организационного SYSTEM bootstrap current image может быть полностью unset:
он остаётся пустым, а фактически выбранные image reference/digest читаются из
immutable `execution_snapshot`. Частичная image identity или пустой PROJECT
image закрыто отклоняются; current не восстанавливается из snapshot хода.

| Lifecycle | Исход чтения |
| --- | --- |
| Действующий собственный execution | Один `REPEATABLE READ` snapshot текущего состояния CP; сохранённый turn snapshot выдаётся отдельно |
| Foreign/missing target, scope/profile mismatch, revoked actor/membership | Закрытый NotFound/Forbidden; resource/version/payload не раскрываются |
| Terminal/cancel/delete/lease expiry | Прежний authoritative lease resolver не выдаёт состояние через неактивную lease |
| Retry/continuation | Новая server-owned lease/attempt/revision; старые fence/generation не подходят |
| Read/repeat/rejoin | Query не изменяет состояние, не создаёт idempotency receipt или domain event; существующие безопасные tool started/completed events сохраняются без полного результата |

Материализация переиспользует прежние RPC registration, client operation,
startup/readiness, exact NetworkPolicy и deployment controller/CP. Runner
получает descriptor динамически в том же каталоге managed MCP; UI JWT,
произвольное чтение и доступ к чужой БД отсутствуют. Самонастройка по-прежнему
требует отдельного typed plan и подтверждения владельца с его OCC pins.

Proto является источником истины для внутреннего синхронного API. Generated Go
code размещается рядом с потребителем и вручную не редактируется.

## Текущая readiness управляемого Context7

Инициатор фоновой проверки — CP в прежнем
`RuntimeWorkService.ClaimIntegrationConnectionTests`. Actor/org и workload
разрешаются сервером. Только exact `integration-gateway` и закрытый Context7
с активной парой READ/NONE grants получают `MANAGED_MCP_REFRESH`; ключ остаётся
в прежней защищённой credential projection gateway. Task фиксирует immutable
package/config/credential/connection pins, purpose, predecessor и attempt.
Прежние startup barrier, adapter.Test initialize/tools/list, lease/fence и
CompleteIntegrationConnectionTest используются без нового RPC или listener.
Refresh начинается после четырёх минут; допустимость proof остаётся пять минут.

| Переход | Owner result / событие / consumer |
| --- | --- |
| DUE → CLAIMED | Одна task/lease, новое generation и fence; ledger читается прежним claim RPC, события нет |
| CONNECTED refresh → SUCCEEDED | Новый настоящий receipt; connection state/version и RuntimeRevision неизменны; события нет, authoritative read — CP health ledger |
| Refresh → FAILED | DEGRADED закрывает readiness; прежние audit/idempotency/outbox INTEGRATION_CONNECTION_CHANGED в одной транзакции |
| Recovery → SUCCEEDED | Только собственный последний probe failure с теми же config/credential/package и активной парой grants; CONNECTED, audit/outbox прежнего события |
| Lease expiry | Прежняя task terminal FAILED, lease очищена; поздний complete отклонён. После 30 секунд новая task/predecessor, максимум три attempts; ledger read, события нет |
| Retry exhaustion | Новых tasks нет; свежесть не продлевается, startup/call закрыто отклоняются; явный owner Test остаётся поддерживаемым recovery path |
| Disable/revoke/delete/config drift | Незавершённый refresh CANCELLED; новое proof не публикуется и ресурс не воскресает; ledger read, прежние события соответствующей owner command |
| Первый/expired proof и exact собственный DUE/CLAIMED probe | Private pending оставляет только этот candidate в очереди, не создаёт RuntimeRevision/lease/Pod grant или событие; другие candidates продолжают. Общий wait не более 30 секунд от immutable created_at первой attempt этого цикла; retry/restart его не сбрасывают |
| Expired receipt / worker outage | Fresh RuntimeRevision не выдаётся; closed eligibility stage managed_mcp. Readiness worker не заменяет upstream receipt |
| Каждый MCP call | Прежний ResolveIntegrationInvocation читает CP-owned текущий proof, active execution lease и immutable/current config/credential/package/grant pins до постановки invocation; события чтения нет |
| Перед внешним READ | Прежний ClaimIntegrationInvocations повторяет тот же current-health check; selected ApprovalPolicy и invocation fences остаются обязательными |

Controller проверяет структуру immutable profile, а CP — текущую freshness.
Первоначальный receipt по-прежнему проверяется на runner startup. Нельзя
подменять старый immutable input новым временем, считать успешную прошлую
проверку бессрочной или продолжать call после отзыва одной части grant pair.
Отсутствующий exact probe, stale lease, истёкшее ожидание, failed refresh или
изменение pins немедленно дают прежний closed отказ. Enabled Context7 dependency
не исчезает молча из required startup при DEGRADED connection: callable authority
не расширяется, а запуск без обязательного профиля отклоняется.
Query/refresh не получают новую authority на shell, arbitrary MCP или credential.

## Package и версии

### Адресное чтение Workflow и назначенных сотрудников

`AGENT_CONFIGURATION` использует тот же leased catalog только для текущего
`AGENT` context SYSTEM/PROJECT помощника. Запрос содержит собственный
`assistant_ref` и точную пару `entity_kind=AGENT`, `entity_ref` сохранённого
контекста; произвольный сотрудник, Workflow recipient, query, pagination и
account/profile override запрещены. `CURRENT_CONFIGURATION` остаётся own-only.
CP заново выводит actor из owner lease, проверяет source helper, organization/
project, `agent.view` и допустимость `CREATE_INSTRUCTION_DRAFT` либо
`UPDATE_AGENT` по `agent.manage`. Одно и то же намерение и agent version должны
совпасть в immutable RuntimeRevision и свежей owner context projection.

Typed `agent_configuration` несёт `agent_ref`, `project_ref`, OCC `version`,
canonical JSON и SHA256 точных bytes. Закрытая модель сохраняет name, purpose,
role, avatar, state, enabled и безопасные runtime labels. Native
`publishedInstructions` и фактически выбранные `effectiveInstructions` отдельно
содержат полный content, revision ref/number и digest; managed `PROMPT_TEMPLATE`
выбирается тем же SQL, что штатный `GetEffectivePromptTemplate`. Binding ref/
version/revision и effective flag не подменяют фактически выбранный шаблон.
Native content ограничен 64KiB, managed template — 256KiB, весь snapshot —
1MiB. Усечения и рендеринга исходного текста нет. Environment values, secrets,
provider credentials и hidden runtime materialization в модель не входят.
Consumer проверяет exact owner/context/version, закрытые поля и вложенные
модели, digest текста и canonical bytes; повреждение не становится частичным
успешным ответом. Чтение не заменяет существующие OCC/owner confirmation
последующего `CREATE_INSTRUCTION_DRAFT` или `UPDATE_AGENT`.

| Lifecycle адресного AGENT read | Авторитетный результат |
| --- | --- |
| Exact действующий SYSTEM/PROJECT helper и текущий AGENT | Один полный `REPEATABLE READ` owner snapshot |
| Ordinary agent, чужой helper/entity/organization/project, Workflow context | Закрытый отказ без instruction payload |
| Отзыв actor/view/manage, context version drift | Свежая eligibility отклоняет read |
| Cancel/terminal/expiry/stale fence/generation | Прежний owner lease resolver отклоняет read |
| Repeat/rejoin | Нет mutation, idempotency receipt, audit/domain event или новых grants |
| Последующая подготовка/Apply | Прежние специализированные операции, immutable plan и owner/OCC проверки |

Новый enum/message генерируется из Proto для CP и runtime-controller. Имя
инструмента, RPC, transport permission и runner operation profile неизменны.
Новый kind приходит в динамическом `tools/list` прежнего инструмента; публичная
wire-проверка producer/consumer покрывает SYSTEM/PROJECT AGENT context без
ослабления закрытого runner catalog и без изменения runner image.

`WORKFLOW_CONFIGURATION` расширяет тот же leased configuration catalog:
`assistant_ref` остаётся собственным помощником, а парные `entity_kind=WORKFLOW`
и `entity_ref` выбирают только Workflow сохранённого контекста execution.
Сервер повторно проверяет root actor, SYSTEM/PROJECT source, organization/project,
lease/fence/generation и совпадение immutable/current context version. В обеих
проекциях требуется `UPDATE_WORKFLOW`; состояние должно допускать штатный EDIT.
Query, pagination, account/profile override и неизвестные поля запрещены.

Ответ содержит typed envelope `workflow_ref`, `project_ref`, OCC `version`,
canonical `configuration_json` и SHA256 этих точных bytes. JSON ограничен 1MiB
и содержит полный authoritative before snapshot существующего UPDATE_WORKFLOW:
editable fields со стабильными step keys и полный draft с dependencies,
instructions, gates и defaults. Consumer проверяет закрытые поля, hash,
полный graph без усечения и соответствие editable projection исходному draft.
Этот snapshot не переписывает опубликованную Workflow revision либо Run input.

`RECIPIENT_INTEGRATION_GRANTS` принимает те же парные locators. Без них
получателем остаётся exact сохранённый AGENT/WORKFLOW context. Из Workflow
разрешён адресный AGENT только среди coordinator/Steps текущего draft с тем же
context version и project. Чтение дополнительно требует UPDATE_WORKFLOW,
canonical AGENT context eligibility и GRANT admission каждой записи. С другого
AGENT context нельзя читать произвольного сотрудника. Поля context entity
kind/ref/version ответа отдельно связывают исходный Workflow, а recipient pins
относятся к выбранному сотруднику. Locators и graph membership не выдают grants.

Каталог отличает безопасный metadata read от исполняемости package. Exact
published binding читается только после strict Parse и совпадения key/version/
digest. Несовместимая с текущим adapter ревизия видна как
`reason=PACKAGE_UNAVAILABLE`, `grantable=false`; сохранённый
`current_grant_enabled` остаётся фактом конфигурации, а не правом исполнения.
Malformed content, неизвестный package, mismatch pins и отказ SQL не становятся
успешным unavailable snapshot. Exact unbound ревизии, отсутствующие в текущем
source registry, исключаются до LIMIT/OFFSET. Execution resolver, mutation
admission и owner/OCC checks не получают legacy compatibility либо fallback.

| Сценарий | Авторитетный результат |
| --- | --- |
| Действующий Workflow context | Один RR snapshot полного draft/OCC либо назначенного AGENT grant catalog |
| Foreign project/tenant, unassigned AGENT, другой Workflow | Закрытый отказ без target payload |
| Отзыв actor/EDIT/GRANT authority, context drift | Свежая owner eligibility отклоняет read |
| Cancel/terminal/expiry/stale fence/generation | Прежний exact lease resolver отклоняет read; retry требует новую lease |
| Повтор/rejoin | Read не создаёт mutation, receipt, event, lease или grant |
| Следующий UPDATE_WORKFLOW | Прежняя owner confirmation и snapshot/OCC; чтение не заменяет проверки Apply |

Producer → прежний generated RPC/client → CP owner snapshot → закрытый callback
caster материализуются вместе. Native runner получает descriptor динамически
из runtime-controller, без нового image, transport permission или credentials.

```proto
syntax = "proto3";

package example.v1;

option go_package =
  "<module>/services/internal/example/internal/generated/example/v1;examplev1";
```

- package содержит major version;
- несовместимое изменение создает новый `v<major>`;
- удаленные field numbers и names резервируются;
- field number не переиспользуется с другим смыслом;
- package owner зарегистрирован в `contracts/registry.yaml`.

## Service и методы

- Один service объединяет связную capability, а не все операции компонента.
- Method называется command/query намерением: `CreateRecord`, `GetRecord`,
  `ListRecords`, а не transport/SQL действием.
- Каждый method имеет собственные request/response messages.
- Generated `Empty` допустим только при фактическом отсутствии результата;
  успешная mutation обычно возвращает ID/version или безопасный snapshot.
- Batch/list имеют явные лимиты, стабильный порядок и pagination contract.

## Authority

Обычный request не содержит доверенные `actorId`, `tenantId`, `organizationId`,
`permission` или workload identity. Эти значения приходят из проверенной gRPC
metadata/signed context либо разрешаются владельцем данных.

Если идентификатор нужен как бизнесовый filter, контракт отдельно объясняет,
почему caller вправе его выбирать и как service проверяет ownership.

## Mutation

State-changing request включает:

- idempotency key;
- expected version для изменения существующего aggregate;
- только business input;
- immutable reference/version внешнего snapshot, если он утвержден сценарием.

Owner, server timestamp, aggregate version и event sequence назначаются
сервером. Idempotency и `expectedVersion` не заменяют authorization.

Unknown outcome после `Unavailable` повторяется только с тем же idempotency key
и тем же семантическим request.

## Presence и validation

- Optional field используется только когда нужно различить «не передано» и
  zero value.
- Невалидное нулевое значение enum не получает бизнесовый смысл по умолчанию.
- Закрытые enum отклоняют unknown на входной границе.
- Строки, collections и payload имеют max limits.
- Timestamp проверяется и нормализуется по утвержденной precision.
- UUID/ID/decimal/money получают устойчивое representation и domain caster.

Proto validation выполняется до domain handler. Validation schema не заменяет
domain constructors и cross-field invariants.

## Ошибки

Внутренний API использует типизированные domain errors, отображенные в
канонические gRPC codes по `GUIDE-DOC-005`:

- request validation -> `InvalidArgument`;
- отсутствующий ресурс -> `NotFound`;
- authority denial -> `PermissionDenied`;
- unauthenticated caller -> `Unauthenticated`;
- OCC/idempotency conflict -> `Aborted` или утвержденный conflict code;
- dependency outage -> `Unavailable`;
- unexpected defect -> `Internal`.

Текст status безопасен, стабилен и не содержит SQL, token, PII или provider
diagnostics.

## Реализация

Server path:

```text
interceptors -> handler -> request caster -> domain service
             -> response caster -> status mapping
```

Client path:

```text
domain client port -> service adapter -> generated client
                   -> deadline + mTLS + signed context
```

Generated request не передается в домен, а generated client не импортируется
domain service напрямую. Полный профиль задают `GO-DOC-001` и `GO-DOC-005`.

## Ограниченный каталог для подтверждаемой настройки

`RuntimeWorkService.SearchAssistantResources` обслуживает три взаимоисключающих
режима: обычный поиск, каталог определений интеграций и типизированный
`assistant_configuration_catalog`. Новый режим использует тот же зарегистрированный
метод, mTLS, signed context и проверку точных lease/fence/generation. Ссылка
`assistant_ref` выбирает ресурс, но не выдаёт полномочий: сервер разрешает
инициатора, исходный диалог и сохранённую область целевого помощника до чтения.
PROJECT может настраивать только свой профиль, SYSTEM — себя либо доступный
проектный профиль. Обычный сотрудник не становится помощником из-за пустого
project ref.

Страничные виды каталога возвращают максимум десять записей закрытого вида с ограниченным
поиском и стабильным порядком; offset ограничен, нулевой next offset означает
конец. Аккаунты и модели читаются только для совместимого активного runtime
профиля. Ответ не содержит credential, auth refs, значения секретов, Dockerfile
или произвольные исполняемые параметры. Отдельный `CURRENT_CONFIGURATION`
возвращает не страницу, а описанную выше безопасную собственную typed модель.
Пустой каталог не означает разрешение
угадывать ссылки или объединять области организации и проекта.

Специализированный discovery не заменяет доменную eligibility обычного чтения:
`PROVIDER_ACCOUNTS` проверяет `provider.account.view` для каждого аккаунта,
включая отдельный instance ALLOW; `MODELS` проверяет `organization.view`, как
штатный ListModelCapabilities. Право управления помощником не назначает права
чтения другого вида ресурса. Совместимость аккаунта с runtime profile и
проверка current credential являются дополнительными условиями, не заменой
access policy. Ограничения схемы БД обязаны поддерживать тот же закрытый
набор scope/resource kind, что canonical registry, без фиктивного project.

Только записи `ASSISTANTS` могут содержать `runtime_environment_ref` текущего
опубликованного окружения. Это locator, не grant: изменение требует повторного
разрешения полного assistant/profile/project/environment tuple. SYSTEM может
подготовить окружение, инструкции или перепривязку проектного помощника через
`projectAssistantRef` независимо от открытого экрана; PROJECT — только себе.
Обычная операция сотрудника по-прежнему требует точного screen context.
Сохранённые owner/version pins неизменяемы при редактировании; изменение
состояния после подготовки не устраняется скрытым перепривязыванием.

Значения reasoning принадлежат exact каталогу модели и не ограничиваются
перечнем заранее известных имён. Producer и consumer используют canonical
структурную проверку, ограничение коллекции и проверяют уникальность и
принадлежность default к этому набору.

Чтение не меняет состояние и не создаёт событие. Последующая специализированная
операция заново разрешает actor/owner, снимки каталога, версии конфигурации и
ручного черновика. Подтверждение плана не заменяет эти проверки; прежний
каталожный ответ не является grant. Публикация модели и reasoning выполняется
одной транзакцией владельца и влияет только на следующие immutable runtime
ревизии, не переписывая уже материализованный запуск.

В подтверждённом плане из нескольких операций перенос версии допустим только
для точного эффекта предыдущей выбранной canonical операции над тем же
aggregate в той же транзакции владельца. Версия и зависимости берутся из
фактически возвращённого результата, не из caller payload или произвольного
свежего read. Исходная immutable revision, её digest и snapshots не меняются.
Внешний drift, отзыв прав, другой target и неизвестный эффект по-прежнему
закрыто отклоняются; последующая ошибка откатывает все эффекты плана.

Проект целевого помощника не заменяет область исходного диалога. Ответ,
окончательное событие плана и persisted conversation используют область
источника; audit и события изменённых объектов — область своего target.
Перепривязка диалога допустима только отдельным утверждённым переходом создания
проекта, а не как побочный эффект изменения проектного помощника.
