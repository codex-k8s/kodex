---
id: OPS-HTTP-SURFACE-1045
title: Полнота HTTP поверхности MVP и зависимости producer
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница проверки

## Сверка принятого backlog MVP-UI-01–61 и CFG

Сверка относится к поверхности #1045, а не к приёмке UI, producer или
развёрнутого контура. Чисто визуальные требования реализует #1022.
Наличие операции ниже означает HTTP mapping существующего Proto; readiness
зависимого сервиса этим не подтверждается.

| Требования | Существующая HTTP/SDK поверхность | Оставшаяся граница |
| --- | --- | --- |
| CFG, RoleImage и IntegrationDefinition | Managed list/get/revisions/impact, специализированные create/save/validate/publish/discard/rebind, Git import/copy/detach/write-back, recipe build/promotion | Полная build/package execution и доставка exact published revision принадлежат producer; общий prepublication plan ещё отсутствует |
| 02–06, 10, 12–13 | Projects query/page, Runs states/page и Owner Gates state/page | Home и project projection должны давать server total/aggregates; Owner Gates query/total ожидает CP |
| 05, 19–23, 39–40 | Общие query/page для каталогов, provider accounts/readiness и integration definitions/connections/grants | Selector eligibility для конкретной operation/recipient не заменяется локальной фильтрацией; effective authority projection ожидает CP |
| 09 | Assistant conversations query/page/title/archive, context и plan lifecycle | Геометрия и browser lifecycle — PWA; server context остаётся authority |
| 11 | Same-origin session refresh и одноразовые realtime tickets | Длительная multi-tab/reconnect проверка принадлежит PWA/integrated baseline |
| 17–18, 21 | Model capability catalog с exact revision/digest; runtime config и canonical TOML draft/validate/publish/rollback | CP должен связать selected account/model catalog pins и effort из TOML, дать versioned allowed-fields schema и безопасные line/column/key diagnostics |
| 01, 07–08, 16, 48, 53–54 | Typed problems, prompt validate/preview diagnostics, environment readiness/agents, public search kind/ref/projectRef | Manifest/auth-proxy и browser routing проверяются в deploy/PWA; generic 404 не считается readiness |
| 24–29, 32–33, 38 | Runs states, organization catalogs с project filter, agent avatar command, workflow projection/launch | Layout — PWA; aggregate counts и avatar lifecycle доказываются producer, а не наличием route |
| 30–36 | Owner-scoped typed variables, prompt preview/service blocks и persisted RuntimeRevision diff | Requested/effective/unavailable authority projection и полный continuation notice/preview требуют CP; исторический diff не заменяет их |
| 37, 61 | VFS list/search, typed Skills/Memory lifecycle, exact revision bindings и artifacts | VFSNode пока без revision/lifecycle/scan/selection eligibility; запросы без active/trash/kind фильтров. MCP runtime и writable workspace — отдельные consumers |
| 41–42 | Typed mailbox draft/preview/validate/publish/discard/bind/unbind/read и credential receipt/list; остальные typed integrations через существующий registry | D5 owner/policy59 и exact delivery/readiness пока не завершены; universal proxy не добавляется |
| 43–45 | Schedule specification/preview/revisions и prompt preview, Environment read/readiness/agents | Cron materialization/continuation и eligibility принадлежат scheduler/CP; inspector — PWA |
| 46–50 | Environment draft lifecycle; Secret encrypted draft и immutable prepublication impact plan/per-item outcomes; exact Secret pin в Environment | Prepublication plan для Environment/Prompt/Instructions/RoleImage ещё нужен в CP. Postpublication impact/rebind не закрывает это требование |
| 51–52 | Provider delete/revoke/verify/reauthorize/device challenge и typed provider errors | Durable cleanup, blockers и real provider path требуют producer integration |
| 14–15, 55–60 | Typed STT settings/catalog/availability и bounded multipart transcription через protected client; инструкции и variables read | Размер редактора — PWA. Реальный provider smoke и итоговая readiness требуют разрешённого контура; локальные fixtures не заменяют его |

Недостающие CP контракты переданы владельцу #1046. Gateway не добавляет
самостоятельно новые RPC, eligibility, authorizations или успешные состояния
для закрытия этих строк.

## Exact Secret pin при редактировании Environment

MVP-UI-46/47: verified actor → create/publish Environment либо create/save draft
→ HTTP `secretBindings[].revision` → существующий Proto
`RuntimeSecretBinding.revision` → owner SQL `runtime_secret_resolve_binding`.
Владелец повторно разрешает tenant/project и ACTIVE Secret/revision; 0 либо
отсутствие означает current на момент materialization, положительное значение
сохраняет выбранную immutable revision. Draft readback возвращает тот же pin,
поэтому последующее сохранение других полей не переводит Secret на latest.
OCC/idempotency и события сохраняют существующий lifecycle Environment;
потребители получают exact descriptors из published RuntimeEnvironment, а
активный attempt продолжает прежнюю RuntimeRevision. Недопустимое число
отклоняется до RPC; повреждённый pin ответа — 502.
Published `secretDescriptors[].revision` также описан в SDK и проверяется как
положительный exact pin. Kubernetes namespace не входит в публичную projection.
Все existing-draft read/mutation ответы связываются с exact requested draftRef;
create дополнительно сохраняет projectRef и исходную пару Environment/version.
Несовпадение не выдаётся как успешный receipt другого ресурса.

## D5: typed mailbox, schema 056547091

Регистрация всех 12 публичных RPC и internal EMAIL readback подключена через
policy59 `78d700683`. Это устраняет structural profile gap, но не доказывает
исполнение owner, доставку конфигурации или READY.

Источники: #1045/#1046/#1018, MVP-UI-41, CFG и
`mailbox-owner-lifecycle-1046.md`. HTTP потребляет специализированные CP RPC,
не создаёт mailbox authority из browser payload. Полная producer/consumer
готовность требует executable CP/policy и #1029/#1037 checkpoints; одна схема
не означает READY или завершённый пользовательский сценарий.

| HTTP под verified Session/tenant | Владелец и fence | Read/event/consumer |
| --- | --- | --- |
| GET integration-connections/{connectionRef}/email-mailbox/configurations | ListEmailMailboxConfigurations, exact existing EMAIL connection; query/page передаются CP | Safe managed lineage/revision/spec/publication, server total/cursor |
| GET того же email-mailbox/configuration | GetEmailMailboxConfiguration, optional exact configurationRef/revisionRef | Authoritative selected snapshot после reload, без подмены latest |
| GET email-mailbox/credentials либо credential-receipt?idempotencyKey | ListEmailMailboxCredentials либо GetEmailMailboxCredentialReceipt; owner connection и original actor/intent | Safe descriptor/generation/kind/connection version, без значения/digest; unknown receipt 404 |
| POST email-mailbox/preview | PreviewEmailMailboxConfiguration, connection eligibility; CSRF | Ровно typed specification либо YAML; серверный strict parser, без записи |
| POST email-mailbox/drafts | CreateEmailMailboxDraft, integration.manage; idempotency; existing set требует If-Match | Server refs/UI lineage; incomplete draft разрешён, caller source/owner запрещены |
| POST email-mailbox-configurations/{configurationRef}/revisions/{revisionRef}/saves | SaveEmailMailboxDraft, If-Match set+idempotency | Новая immutable revision, прежняя закрыта; selected revision read |
| POST того же revision /validation, /publication, /discard | Специализированные Validate/Publish/DiscardEmailMailboxDraft, set OCC+idempotency | Exact revision; validation повторно проверяет credentials/TLS/policy, publish не означает READY |
| POST того же revision /binding | BindEmailMailboxConfiguration, set OCC и отдельная expectedConnectionVersion | PENDING publication; CP desired projection → shared policy/CNI/Deployment/Secret readback → EMAIL report → READY |
| DELETE email-mailbox/binding | UnbindEmailMailboxConfiguration, connection OCC+idempotency | Publication без configurationRevisionRef, безопасный новый connectionVersion |

Create/save/validate/publish/discard атомарно фиксируют owner state/receipt/audit,
без отдельного domain event. Bind/rebind/READY используют существующий
INTEGRATION_CONNECTION_CHANGED и owner outbox. EMAIL callback остаётся internal
RuntimeWorkService и не выдаётся через HTTP; readiness относится к принятому
consumer snapshot/rollout, не к SMTP provider health. Политика сети и delivery
принадлежат CP/shared mailpolicy/EMAIL consumer, новых gateway портов нет.

Typed specification допускает неполный DRAFT/INVALID. UNSPECIFIED enum выдаётся
как отсутствующее optional поле, неизвестный enum закрыто отклоняется. HTTP
сохраняет SMTP/IMAP/POP endpoints, exact credential refs/generation, limits,
folders/recipients и все 21 operation policies без преобразования значений.
Preview передаёт YAML серверу без локальной переписи. Неизвестные/protected/value
поля typed JSON отклоняются до RPC; raw malformed YAML не сохраняет CP.
Canonical stored revision имеет JSON format и bounded content. Diagnostics
принимаются только из закрытого кода/сообщения, без value echo; line/column
сохраняются. READY требует readyAt, PENDING его не имеет; SUPERSEDED может
сохранить исторический readyAt.

Все ответы no-store; mutations и GET selected view возвращают set ETag,
credential receipt и unbind — connection ETag. После unknown write receipt
проверяется тем же connection/actor/key, новый успех не синтезируется. Чужой
connection/config/revision, stale OCC, Git-owned mutation и revoked Session
сохраняют owner отказ. Git copy/detach/history остаются существующими managed
RPC и теперь сохраняют kind EMAIL_MAILBOX; specialized mailbox lifecycle
не заменяется универсальным mutation endpoint.

Ручная проверка: создать неполный draft, preview YAML ошибки без утечки текста,
выбрать доступные credentials, validate/publish, отдельно bind с обоими OCC;
дождаться authoritative READY после consumer readback. Перечитать selected
revision и original credential receipt после reload; повторить stale/чужой
ref и Git-owned изменение. Live SMTP/IMAP/POP и rollout проверяются отдельно.

## D6: зашифрованный черновик Secret

Зависимости: полный CP `8b4ac92f` принят merge; `78b812699` добавляет
authoritative `secret_version`, broker `d11a1e0d9` передаёт тот же pin.
Источники: #1045/#1046/#1068, MVP-UI-47, CFG-03 и
`runtime-secret-draft-lifecycle-1046.md`. Prepublication impact plan и выбор
заменяемых потребителей подключены ниже на producer `3ec3fe95b`; существующий
postpublication impact/rebind не заменяет требование UI-47.

| Инициатор и HTTP | RPC и полномочия | Fence, переход и результат |
| --- | --- | --- |
| Session actor → POST projects/{projectRef}/runtime-secret-drafts | PrepareSaveRuntimeSecretDraft, secret.create/fresh auth; project только locator | Idempotency-Key и exact value commitment; server refs → PREPARING → SaveSecretDraft → DRAFT |
| Session actor → POST runtime-secrets/{secretRef}/drafts | тот же PrepareSave, secret.rotate/fresh auth; existing owner разрешает CP | If-Match Secret, immutable отдельный draft; активная revision не меняется |
| Session actor → POST runtime-secret-drafts/{draftRef}/validate | PrepareValidate → ValidateSecretDraft, exact owner/fresh auth | If-Match Draft, idempotency; broker decrypt/validate → VALID |
| Session actor → POST runtime-secret-drafts/{draftRef}/publish | PreparePublish → PublishSecretDraft, exact owner/fresh auth | If-Match Draft и expectedSecretVersion из owner read; fenced activation → PUBLISHED + safe Secret |
| Session actor → POST runtime-secret-drafts/{draftRef}/discard | PrepareDiscard → DiscardSecretDraft | If-Match Draft, idempotency; owner закрывает grants до exact ciphertext cleanup → DISCARDED |
| Session actor → GET runtime-secret-drafts/{draftRef} | GetRuntimeSecretDraft; owner eligibility до чтения | Safe current state + version/secretVersion, без изменений или event |

Все mutations проходят session/tenant/revocation/CSRF middleware. CP prepare
SAVE использует policy57 UNARY_PROTO_SHA256, metadata resource/version/attempt
FORBIDDEN, idempotency REQUIRED; остальные prepare привязаны к draft_ref и
mutation.expected_version. Idempotency не заменяет свежую owner проверку.
Владелец CP атомарно фиксирует state/receipt/audit; отдельного event нет,
authoritative public read — GetDraft/GetSecret, worker rejoin принадлежит broker.

Пять broker D6 RPC используют отдельное protected connection: exact mTLS
hostname/CA + OIDC proof control-plane.oidc-secret-draft + local issuer context,
digest фактического protobuf request. Эти RPC не маршрутизируются в CP.
Resource/version/attempt/idempotency metadata запрещены; одноразовый grant
остаётся внутри protobuf gateway→broker и никогда не выдаётся браузеру.
Перед effect gateway вызывает CheckSecretDraftReadiness через тот же protected
client с пользовательским OIDC context. Background gateway readiness не
подменяет пользователя; broker владеет своим storage/keyring/owner readiness.
Используются существующие gateway identity/mount/network destination; delivery
policy57 и broker encrypted namespace/keyring принадлежат #1068 и owner profile.

HTTP не сохраняет значение; plaintext byte buffer очищается после broker call,
в том числе ошибки. Ответы no-store и ETag Draft. Safe draft не содержит value,
display hint, digest, grant, key ID или storage locator. Unknown enum, чужой
ref/tenant/Secret, неверный generation/version, неполный terminal receipt и
неверный published pin отклоняются 502. Ошибки CP/broker сохраняют строгий
Problem mapping, без upstream diagnostics или синтетического успеха.

При потерянном SAVE ответе повторяют exact body и Idempotency-Key: CP возвращает
сохранённый terminal snapshot без нового broker effect либо fresh fenced grant.
CLAIMED/несовпавший intent дают Conflict. После terminal replay перед следующей
операцией выполняют GetDraft; secretVersion snapshot не подменяется latest.
При DISCARD completion версия может совпасть с prepare: owner уже закрыл draft
в prepare, broker только подтвердил cleanup. Отмена/timeout не доказывают откат
зафиксированного owner intent; recovery принадлежит CP/broker, UI читает owner.

Ручная проверка: сохранить значение, перечитать draft после reload, проверить
VALID, опубликовать с обоими pins, перечитать Secret; отдельно отменить draft.
Повторить запрос с прежним ключом, чужим ref, stale If-Match и revoked session;
проверить отсутствие значений/grants в responses. Перед publish подготовить
impact plan, выбрать строки и после ответа перечитать per-item outcomes.

### Prepublication Impact, schema e7944151f

Этот раздел дополняет первоначальный D6 checkpoint; executable CP
`3ec3fe95b` и policy58 приняты как prerequisite. Session actor → POST
`runtime-secret-drafts/{draftRef}/impact-plans` →
PrepareRuntimeSecretDraftImpact с If-Match Draft и Idempotency-Key → CP exact
owner/eligibility → immutable bounded plan с Draft/Secret versions, source
revision, digest, expiresAt. Публикация принимает обязательные impactPlanRef и
selectedItemRefs (явный пустой список означает отсутствие замен, максимум 1000
уникальных server item refs). Эти поля входят в exact intent prepare и не
назначают authority. Broker request остаётся server grant, без public plan
payload или повторной передачи plaintext.

GET `runtime-secret-draft-impact-plans/{planRef}` → GetRuntimeSecretDraftImpact
с query/page → CP current owner eligibility + immutable snapshot → safe items
и per-item outcome. Plan.total — исходное immutable количество до 1000;
page.total — текущий результат eligibility/query. Gateway только передаёт
фильтр, не фильтрует страницу после LIMIT. Cursor связан с plan digest/state,
query/actor; переход состояния требует начать итоговый отчёт с первой страницы.

При APPLIED plan нет PENDING: каждая выбранная строка имеет APPLIED, CONFLICT
либо FORBIDDEN, невыбранная NOT_SELECTED. Только APPLIED содержит новую
environmentVersionRef и, для agent, прежний bindingRef с большей version.
PREPARED/CANCELLED/EXPIRED допускают PENDING/NOT_SELECTED без result fields.
HTTP отклоняет противоречивые состояния, duplicate item refs, прежнюю result
revision, неверный binding pin и переполнение count. Отдельного event нет;
owner activation и результаты читаются через GetDraft/GetSecret/GetImpact.
Повтор publish с прежним intent не создаёт второй effect; per-item conflicts
не превращаются в ложное подтверждение изменения всех выбранных потребителей.

## D4, CP 8e532589e

Session actor → GET `runs/{runRef}/runtime-revision-diff` с optional
`currentRevisionRef` → generated GetRuntimeRevisionDiff → policy56
`platform.query.runtime-revisions.diff` (resource=run_ref, version/attempt/
idempotency FORBIDDEN) → CP run.view и predecessor eligibility → repeatable-read
owner query двух persisted revisions одной Session → safe typed identity/diff
→ PWA continuation view. Предыдущую ревизию выбирает только сервер. Это query
без события или изменения lifecycle. Новая materialization выполняется
существующим continuation RPC отдельно; diff не доказывает её readiness.

Публичный DTO не содержит worker snapshot, prompt, credential, локаторы или
значения конфигурации. HTTP проверяет exact requested run/revision, общую
session пары, known components и разрешённые поля каждого компонента.
Первая ревизия не содержит previous; отсутствие изменений даёт пустой список.
Неизвестные либо противоречивые upstream данные отклоняются 502; hidden/no
materialization CP возвращает 404. Используется прежний защищённый CP client
и deploy ownership gateway, без новых сетевых портов и Secrets.

## Дополнение D2/D3, CP 9ad5b58d1

Home/Kanban: session → `GET runs?states=RUNNING&states=QUEUED` и
`GET owner-gates?state=OPEN` → существующие CP ListRuns.states и
ListOwnerGates.state → owner eligibility и SQL filter до LIMIT → typed page.
State filter не даёт полномочий и не фильтруется после страницы в gateway;
чтение не меняет lifecycle и не создаёт event. Неизвестные/повторные states
отклоняются до RPC. Query/total OwnerGate требуют отдельного producer
дополнения #1046; HTTP их не выдумывает.

Карта до изменения: проверенная browser session/organization → GET
`model-capabilities` → generated Query.ListModelCapabilities → CP eligible
accounts/model catalog → server filtering/pagination → PWA model selector.
Read-only путь не меняет состояние и не создаёт событие. Exact catalog pin
не заменяет owner eligibility. CP связывает cursor с tenant, actor, фильтрами
и снимком; gateway передаёт его без локальной фильтрации. Новые workload,
Secrets и deploy resources не нужны: используется существующий защищённый
CP client с application identity, mTLS и signed context.

SDK принимает optional `expectedCatalogRevision/expectedCatalogDigest` только
парой (`mcat_` + SHA-256 и тот же lowercase digest). Ответ всегда содержит
обе части, включая пустой eligible catalog. Несовпавший ответ CP отклоняется
502; stale pin/cursor возвращает ошибку CP, без выбора другой модели.
При смене фильтра клиент сбрасывает cursor; при refresh явно снимает старый
pin, затем использует новую пару для следующих страниц.

D3 использует прежнюю owner-context карту ниже. Исправлено преобразование
producer `string/reference/integer/collection` в HTTP
`STRING/OPAQUE_REF/INTEGER/COLLECTION`. Сохраняются отдельные
`FILE_DESCRIPTOR/TOOL_DESCRIPTOR`, имена `artifact_ref/media_type` и источники
AUTOMATION/GATE/INPUT/RUN/SESSION/WORKFLOW. Неизвестный producer type/source
закрыто отклоняется; значения переменных не публикуются.

Context7: проверены `/getkin/kin-openapi`, CLI `cmd/validate` и полная
`Validate` без ослабляющих флагов. Проверки этого дополнения фиксируются
по exact checkpoint отдельно; наличие схемы не доказывает live acceptance.

Источники: #1045/#1021, #1018, утверждённая матрица #1031
`docs/operations/mvp-1031-acceptance.md`. Текущий Proto совпадает с CP
`67aa98d770ddaa24cecf01b188f006f087c7849d`; policy55 и generated client
потреблены из этого checkpoint, включая D3 из `c3446af76`.
D7 перенесён commit `88f4331ff` целиком.
CP SQL/handlers вручную не редактировались. PWA handwritten остаётся у Newton.

В таблице указана HTTP поддержка, а не PASS пользовательского сценария.
Проверка generated RPC surface охватывает 71 Query, 136 Command и 12 Assistant
методов. Для всех есть authority profile и HTTP consumer; event cursor
используется только WebSocket resume. Runtime/credential/email worker методы
не публикуются в browser API. Наличие consumer не доказывает CP SQL/runtime.

Все HTTP endpoints ниже имеют префикс `/api/v1`. Session/OIDC и tenant
устанавливает boundary; typed CP client передаёт application identity и signed
context. Ref/filter/OCC не заменяют owner eligibility. Мутации сохраняют
CSRF/idempotency/If-Match правила существующих специализированных команд.

## Все 61 требования

### Карта D1/D7, CP a241b73e1

| Сценарий | Actor/authority → HTTP → CP | State/OCC/readback |
| --- | --- | --- |
| История D1 | Проверенная browser session → GET assistant-conversations query/state/page → ListAssistantConversations | CP creator + organization/project eligibility до LIMIT; cursor связан с фильтрами; ACTIVE по умолчанию |
| Архив D1 | Session + CSRF → POST assistant-conversations/{ref}/archive → ArchiveAssistantConversation, policy54 | Owner до If-Match/idempotency; ACTIVE/CLOSED → ARCHIVED без busy run; CP атомарно сохраняет receipt/audit/SYSTEM_ASSISTANT_CHANGED и отклоняет pending plans |
| Impact D7 | Проверенная session → GET managed-configurations/{ref}/revisions/{revisionRef}/impact query/page → GetManagedConfigurationImpact | CP eligibility/search до SQL LIMIT; точный revision/digest, total/cursor; чтение без события и без изменения bindings |
| Variables D3 | Session → project template-variables либо global prompt-templates/catalog с optional agentRef/runtimeRevisionRef → ListTemplateVariables (c3446af76) | Owner agent.view/run.view и exact sealed context; query/context-bound cursor; только available/reason, не значения; чтение без события |
| EMAIL credential D5 | Session + CSRF → PUT integration-connections/{connectionRef}/email-mailbox/credential → ConfigureEmailMailboxCredential (67aa98d77, policy55) | CP integration.manage/CONFIGURE_CREDENTIAL и EMAIL definition до OCC; immutable key, idempotency receipt, новая connection version; safe descriptor без value/digest/Secret locator; публикация mailbox отдельно, read path owner command receipt |

Producer D7 переносится exact commit `88f4331ff`; D1 потребляет exact
Proto/generated client/policy54 из `a241b73e1`, без ручного изменения CP
SQL/handlers. Исполняемый CP `a241b73e1` обязателен при общей интеграции:
локальный HTTP fake RPC test не объявляется проверкой CP SQL или deployment.

| MVP-UI | HTTP / контрактная поддержка и граница |
| --- | --- |
| 01 | `usertext` catalogs и safe Problem mapping; локализация layout у PWA. |
| 02 | Bootstrap/session/WebSocket status; геометрия badge у PWA. |
| 03 | Данные typed views; адаптивный layout у PWA. |
| 04 | Overview; композиция главной у PWA. |
| 05 | Bounded pages/cursors/total в поддерживающих их CP queries; компактный/modal list у PWA. |
| 06 | CreateAgent/CreateWorkflow и org/project upload; quick actions у PWA. |
| 07 | Публичные статические PWA assets не становятся исключением auth для API; ingress у root. |
| 08 | Environment readiness/agents endpoints, exact CP mapping. |
| 09 | Assistant create/title/turn/context, история query/state/pageSize/pageToken и archive с OCC/idempotency; HTTP D1 подключён к CP a241b73e1. |
| 10 | Project view/overview, серверные aggregates без N+1 у browser. |
| 11 | Session refresh, одноразовый websocket ticket, durable revocation и resume cursor. |
| 12 | Project list/search/cursor; размещение selector у PWA. |
| 13 | Project DTO и server filters; infinite scroll/focus у PWA. |
| 14 | Global speech endpoint + bootstrap availability; исключение sensitive editors у PWA. |
| 15 | Template preview/variables; размеры editor у PWA. |
| 16 | ValidatePromptTemplate/PreviewPromptTemplate, typed errors; renderer принадлежит CP. |
| 17 | Новый model-capabilities, account-specific filter и ProviderDefinition.models; catalog revision требует D2. |
| 18 | Supported/default reasoning effort передаётся без подмены; сохранение через runtime overlay CP. |
| 19 | Provider account/definition pages, lifecycle/readiness; rich selector у PWA. |
| 20 | Editor keyboard behavior не требует отдельного HTTP endpoint. |
| 21 | Config overlay draft/validate/publish/rollback, typed diagnostics и exact digest. |
| 22 | Общие query/page параметры; selector behavior у PWA. |
| 23 | Runtime environment commands; permission/OCC повторно проверяет CP. |
| 24 | Runs list/query/cursor; Kanban columns у PWA. |
| 25 | Artifact lifecycle filters/detail; selection/trash layout у PWA. |
| 26 | Org/project upload, bounded stream; workspace DnD у PWA. |
| 27 | Organization catalogs, optional project filters, typed VFS project roots. |
| 28 | Agent readiness/run refs; итоговый badge у PWA. |
| 29 | Atomic UploadAgentAvatar, size/type checks; scan/admission/cleanup у CP. |
| 30 | Оба TemplateVariable route передают agent/runtime context и обязательные available/reason/total из CP c3446af76. |
| 31 | Agent capability/integration grants и workflow commands; delegation intersection у CP. |
| 32 | Workflow typed views, aggregate counts и launch commands. |
| 33 | CreateRun/LaunchRun form mapping, ошибки publication/permission/readiness без auto retry. |
| 34 | Workflow step templates и materialized PreviewPromptTemplate; execution renderer у CP. |
| 35 | Preview сохраняет materialization/provenance; semantic slot logic у CP. |
| 36 | AddSessionTurn/LaunchRun и runtime bindings; безопасный previous/current diff требует D4. |
| 37 | VFS list/search, 24 Skill/Memory lifecycle/binding commands, exact source fields; runtime file I/O не browser RPC. |
| 38 | Owner gates list/count; подписи вкладок у PWA. |
| 39 | Integration connection list/query/page/readiness. |
| 40 | Membership/capability/connection selectors и grants, owner intersection у CP. |
| 41 | Общий connection config остаётся CP owned; typed EMAIL configuration/projection требует D5. |
| 42 | Integration definition/grants и safe email receipt/reconciliation; provider effects не выполняет HTTP. |
| 43 | Schedule create/update/revisions/preview/runs; occurrences и timezone logic у CP/scheduler. |
| 44 | Environment list/detail, без browser selection side effect в HTTP. |
| 45 | Environment/secret и managed draft commands; расположение tabs у PWA. |
| 46 | Восемь managed Save/Discard и environment drafts; отдельный staged Secret lifecycle требует D6. |
| 47 | Managed/environment/secret impact query/page/total и selective rebind; HTTP D7 подключён к CP 88f4331ff. |
| 48 | Environment readiness/agents, безопасные ошибки hidden/unavailable. |
| 49 | Runtime secret STRING/BASE64/JSON inputs, write-only payload и no-store. |
| 50 | Secret typed list/create/rotate/readback и безопасный Problem; materialization у CP/secret broker. |
| 51 | Provider disable/revoke/delete с blockers/cleanup states из CP, не локальный fake terminal. |
| 52 | Provider device start/observe/refresh/reauthorize, exact idempotency и ошибки. |
| 53 | Search min length/limit/cursor проверяются до RPC; debounce у PWA. |
| 54 | Четыре search kinds, opaque refs/project match, malformed upstream 502. |
| 55 | HTTP потребляет самостоятельный STT client; runtime/deploy принадлежат #1020. |
| 56 | Typed STT parameters/limits draft/readback и raw managed lifecycle; provider probe у STT. |
| 57 | Org speech route, MIME aliases, bounded multipart, permission/session/CSRF. |
| 58 | Speech API и TTL bootstrap; MediaRecorder/editor insertion у PWA. |
| 59 | Protected authenticated STT availability, READY/fresh validity; exact TLS/egress у STT/root. |
| 60 | Live OpenAI fixture smoke: NOT RUN, отдельный credential/staging допуск. |
| 61 | Artifact/VFS user read paths; runtime workspace I/O, immutable mounts и grants у CP/controller/runner. |

## Точные зависимости Bohr Через Root

Все позиции ниже найдены в текущем source Proto, не выведены из отсутствия
локального test. HTTP не добавляет успешные заглушки и не меняет owner contracts.

| ID | Недостающий producer контракт | Последствие |
| --- | --- | --- |
| D1 | Закрыт в HTTP/SDK: exact contracts a241b73e1, query/state и ArchiveAssistantConversation. | При общей интеграции нужен исполняемый CP a241b73e1 с SQL/handlers, а не только перенесённые contracts. |
| D2 | ModelCapability/ListModelCapabilitiesResponse не содержат catalog revision/digest. | Account/effort selection доступен; version-bound catalog readback нельзя выдумать. |
| D3 | Закрыт в HTTP/SDK: c3446af76, agentRef/runtimeRevisionRef и available/reason/total в обоих каталогах. | При общей интеграции обязателен исполняемый CP c3446af76; availability переменной не означает readiness запуска. |
| D4 | RuntimeRevisionSnapshot относится к worker execution; публичный previous/current typed diff перед continuation отсутствует. | Нельзя выдавать worker snapshot либо вычислять безопасный diff из неподтверждённых UI данных. |
| D5 | HTTP credential consumer 67aa98d77 подключён; snapshot delivery реализован в af74fc7dc. Публичный typed safe mailbox configuration lifecycle ещё отсутствует. | Receipt/reconcile и write-only credential доступны в SDK; публикацию/привязку mailbox descriptor нельзя заменить этими командами. |
| D6 | RuntimeSecret публично имеет PrepareCreate/Rotate/Reveal/Revoke, но не отдельные save/validate/publish/discard staged encrypted draft commands. | Не заявляется staged Secret acceptance по immediate create/rotate. |
| D7 | Закрыт в HTTP/SDK: managed query/page/total/cursor из 88f4331ff; environment/secret query из 98a71da1e. | CP выполняет filtering до SQL LIMIT; HTTP не фильтрует локально и сохраняет pinned digest/cursor. |

### Передача Newton/Root D1 И D7

Новый SDK `archiveAssistantConversation`: POST
`/api/v1/assistant-conversations/{conversationRef}/archive`, без body,
обязательные `If-Match`, `Idempotency-Key`, `X-CSRF-Token`. Возвращает
`AssistantConversation.state=ARCHIVED` и ETag. OCC → 412, busy → 409,
чужой owner → 404, недоступный CP → 503. Нет автоматического retry.
`listAssistantConversations` принимает query/state ACTIVE|CLOSED|ARCHIVED
(по умолчанию ACTIVE) вместе с прежними projectRef/pageSize/pageToken.
Смена query/state требует нового cursor, иначе CP возвращает 400.

`getManagedConfigurationImpact` принимает query/pageSize/pageToken, возвращает
прежние consumers/digest/refs плюс total/nextPageToken. Клиент не должен считать
первую страницу полным набором consumers. Выборочный rebind остаётся отдельной
командой с exact binding versions и digest; новая query не выполняет rebind.

Историческая сверка на CP a241b73e1 (D3 и EMAIL обновлены ниже): D2 остаётся
без catalog revision/digest; D3 без target agent/runtime context и
available/disabled reason; D4 без публичного previous/current runtime diff;
D5 без UI mailbox commands, write-only credential lifecycle и dynamic
projection readback; D6 без staged encrypted Secret save/validate/publish/
discard. Worker EMAIL APIs и immediate PrepareCreate/Rotate их не заменяют.

### Передача D3, CP c3446af76

`listTemplateVariables` и `listPromptTemplateVariables` принимают optional
`agentRef/runtimeRevisionRef`, сохраняют query/page и project context.
Каждый item содержит обязательные `available` (включая false) и `reason`:
AVAILABLE, PROJECT_CONTEXT_REQUIRED, AGENT_CONTEXT_REQUIRED,
RUNTIME_CONTEXT_REQUIRED, NOT_MATERIALIZED. Ответ содержит `total` и cursor.
При смене target/query cursor сбрасывается; CP mismatch возвращает 400.
HTTP не подменяет exact RuntimeRevision текущей и не вычисляет eligibility.
Неизвестный reason, противоречивый bool, oversized page/cursor или total
закрыто отклоняются 502. Значения переменных не добавлены в API.

Локально на изменённом дереве D3 PASS: focused race HTTP/boundary/app,
vet затронутых пакетов, gateway build, строгая OpenAPI validation,
Go/TS generation, strict SDK typecheck, Proto codegen replay. Проверены оба
HTTP route, все пять reasons, отсутствие project, malformed input до RPC,
некорректный upstream и owner errors. Полные неизменённые suites/Docker/PWA
не повторялись; live CP и browser NOT RUN.

Сверка следующего чистого CP `67aa98d77`: D2/D4/D6 остаются открытыми.
Для D5 snapshot delivery уже реализован в af74fc7dc; write-only credential
RPC появился в 67aa98d77 и подключён к HTTP ниже. Typed safe mailbox
configuration lifecycle пока не появился. Старые записи об отсутствии всей
projection/credential реализации ниже относятся к прежним checkpoint.

### Передача D5 Credential, CP 67aa98d77

SDK `configureEmailMailboxCredential`: PUT
`/api/v1/integration-connections/{connectionRef}/email-mailbox/credential`,
session, CSRF, If-Match connection version, Idempotency-Key.
Body `{kind,value}`: CA_CERTIFICATE/USERNAME/AUTH_SECRET, write-only value.
Лимиты байтов: 65536/320/16384; пустое значение запрещено, пробелы значимы.
USERNAME/AUTH_SECRET запрещают NUL/CR/LF; PEM CA проверяет authoritative CP.
Server-owned descriptor `{name,generation,kind,connectionRef,connectionVersion}`
и ETag новой connection version; value/digest/Secret locator отсутствуют.
HTTP сверяет exact connection/kind/version/generation перед ответом.
Повтор после потери ответа использует тот же idempotency key и исходный
If-Match; автоматического retry нет. OCC → 412, eligibility → 403/404,
state conflict → 409, CP unavailable → 503. Mailbox publication отдельно.

Локально PASS: focused race HTTP/boundary/app (D3, D5, все public RPC,
session/CSRF/tenant/revocation, exact policy operation), vet этих пакетов,
gateway build, strict OpenAPI validation, strict SDK typecheck, Go/TS
byte-identical codegen replay, Proto replay и authority policy55 codegen.
Первая компиляция FAIL из-за pointer-типа write-only value/expected_version;
исправлены getters, повторные проверки PASS. HTTP fixtures проверяют все
три kind, сохранение пробелов, ограничения до RPC, запрет caller owner fields,
safe descriptor/error, exact readback и OCC. Полный CP SQL/materialization
через HTTP, live mail и browser NOT RUN; неизменённые полные suites/Docker
не повторялись. Это не full #1045 acceptance.

Порядок следующих зависимостей для PWA: D2 version/digest модельного каталога;
D4 безопасный публичный previous/current RuntimeRevision diff; D6 staged
encrypted Secret save/validate/publish/discard. На чистом CP 67aa98d77 этих
полей/RPC нет. Оставшаяся D5: typed safe mailbox configuration и привязка
выданного descriptor к публикации. CP/SQL вручную не редактировались.

Попытка cherry-pick D1 целиком остановилась на отсутствующих CP EMAIL
command/permissions dependencies и была отменена. CP owner-файлы не разрешались
вручную: D1 contracts/profile/generated скопированы без изменений из exact
checkpoint. Root должен интегрировать CP implementation a241b73e1; локальный
HTTP fake RPC suite не является проверкой CP runtime. D7 перенесён без конфликтов
как `8a49981d1`. Policy54 принадлежит Bohr, не назначалась HTTP самостоятельно.
Инструмента прямой межагентной отправки нет; этот файл является handoff root/Newton.

Локально PASS: targeted race HTTP/security boundary (`TestAssistant`,
`TestManagedImpact`, `TestManagedConfiguration`, `TestImpactSearch`,
`TestPublicRPCSurface`, session/CSRF и enum normalization), focused vet,
gateway build, strict OpenAPI validation (`kin-openapi` 0.135.0), strict
generated SDK typecheck, воспроизводимый Go/TS codegen, Proto replay и
authority policy codegen. Buf remote rate limit обработан существующим
fallback на exact local plugins, итог replay PASS.

Промежуточные FAIL новых fixtures: missing ACTIVE у старого history response
и неверные ожидаемые HTTP codes для Aborted/FailedPrecondition. Fixtures
исправлены под фактический producer enum и существующий Problem mapping:
Aborted→412, FailedPrecondition→409. Runtime authority/error boundary не ослаблялась.
NOT RUN: live CP assistant RPC и SQL/deployment этого checkpoint, browser UI,
общий baseline/Docker повторно не запускались. Проверенный ранее full gateway
и Docker контур остаётся в историческом отчёте d1a80b560, не переобозначается
как новый запуск. Full #1045 acceptance удерживается открытыми D2–D6.

### Историческая Сверка CP 98a71da1e

Потреблён exact `98a71da1e7da9d0ceee2470a6c16e7351eea2e53` cherry-pick
`192c56459`: исходный Proto, generated API и принадлежащая CP реализация
перенесены без ручных правок. Это доступный независимый checkpoint D7, не merge
всей ветки CP. Public Proto теперь совпадает с этим CP HEAD. Новая policy
revision не нужна; существующие exact operations и права сохранены.

HTTP `getRuntimeEnvironmentImpact`/`getRuntimeSecretImpact` принимают query,
сохраняют pageSize/pageToken/ETag/total, отклоняют более 200 Unicode-символов,
NUL и malformed UTF-8 до RPC. CP выполняет trim, literal case-insensitive
search до LIMIT и связывает cursor с нормализованным запросом. Другая строка
поиска со старым cursor даёт 400, не пустую успешную страницу.

Фактически просмотрены public Proto и следующие реализации в CP WT:
`email_authorization.go`, `email_receipts.go` транспорта, mailbox projection
handoff и diff `fed22b1f6`/`b20884535`. Итог по прежним зависимостям:

- D1: public assistant request всё ещё только page/project_ref; нет query,
  archive RPC и archived state. HTTP pagination уже сохранена.
- D2: ModelCapability/ListModelCapabilitiesResponse по-прежнему без catalog
  revision/digest; текущие model/effort/account поля HTTP передаёт полностью.
- D3: TemplateVariable и ListTemplateVariablesRequest не получили target
  context/available/reason. Обычный project/query/page каталог сохранён.
- D4: `b20884535` корректно очищает codexSessionID при смене context digest
  в worker snapshot. Это не публичный previous/current diff для UI; D4 открыт.
- D5: CP handlers ResolveEmailAuthorization/Report/ResolveReconciliation и
  Get/Reconcile receipts реализованы. Их отсутствие больше не blocker.
  `fed22b1f6` задаёт authoritative mailbox policy и EMAIL minimum NONE;
  HTTP schema уже допускает NONE и HUMAN_EACH_EFFECT, не назначает gate сама.
  Startup import `CONTROL_PLANE_EMAIL_CONFIGURATION_FILE` не является UI RPC.
  Остались typed mailbox commands, write-only credential lifecycle и dynamic
  projection delivery/readback. HTTP worker endpoints не создавались.
- D6: отдельных staged Secret draft/save/validate/publish/discard RPC всё ещё
  нет; existing PrepareCreate/Rotate/Reveal/Revoke не подменяют этот lifecycle.
- D7: environment/secret query закрыты в HTTP. Managed impact query/page остаются.

`fed22b1f6` и `b20884535` отдельно не переносились: это CP/worker/shared catalog
реализация, не новая HTTP dependency. Root сохраняет их при общей интеграции.
Старые абзацы producer handoff о незавершённых EMAIL handlers не используются
как актуальное доказательство. Никакой CP или handwritten PWA файл вручную
не изменялся; full неизменившиеся gateway/PG/Docker suites не повторялись.

Локальные проверки этого изменения: targeted HTTP race
`TestImpactSearch|TestSecretImpact|TestSecretRebind|TestIdentityEnvironment|TestPublicRPCSurface`
с outer timeout 180s и Go timeout 120s — PASS; focused HTTP vet, строгая
OpenAPI validation kin-openapi 0.135.0, Go/TS generation и strict generated SDK
typecheck — PASS. Новая fixture проверяет exact RPC, query/cursor/ETag,
200 Unicode-символов, literal `%_`, NUL/malformed UTF-8/oversize отказ до RPC,
400 для stale-filter cursor и безопасные 403/404/503. Это HTTP fake boundary,
не повтор CP PostgreSQL либо live проверки.

## Проверки И Передача

`055d8e050`: новый model route/SDK и search/VFS hardening; focused race
HTTP/security/app, vet и gateway build PASS. Strict generated SDK typecheck
PASS, повтор Go/TS generation воспроизводим. Общий PWA test/build не повторялся:
handwritten ownership Newton. CP producer SQL, реальные provider и browser
acceptance: NOT RUN. Нет push/PR/merge/deploy.

Для Newton: `listModelCapabilities` принимает providerDefinitionKey,
providerAccountRef, query, pageSize, pageToken; возвращает models с supported/
default effort, exact eligible accounts, available/blockers. Не выбирать другую
модель при исчезновении selection. `listAssistantConversations` принимает
pageSize/pageToken и возвращает nextPageToken; query/archive пока не обещаны.

Повторная проверка набора инструментов не обнаружила `send_input` или другого
thread-message API. Прямая отправка в тред Newton
`01a06dcf-baa3-73b1-8ead-f6425d3454f6` технически недоступна; передача не
объявляется выполненной. Этот tracked документ содержит готовый контракт для
чтения из HTTP WT без изменения handwritten PWA. Gateway/SDK не зависят от
нового cherry-pick CP: исходные Proto уже совпадают с указанным checkpoint.

После добавления assistant pagination повторены focused race HTTP/security/app,
focused vet, `go build ./...` gateway и strict generated SDK typecheck: PASS.
Матрица всех 61 строк проверена по QA источнику; это карта ownership/evidence,
не 61 успешный E2E. D1..D7 блокируют полное продуктовое acceptance.

## Полная Независимая Проверка Gateway

На чистом `11401f0acadefd139e0b61616405e4d353d6b7ca` выполнены локально:

- `go test -race ./... -count=1 -timeout=180s`: PASS по всему gateway, включая
  session, revocation boundary, ratelimit, websocket, observability и usertext.
  Пакеты без tests отмечены Go отдельно, не считаются проверенными сценариями.
- `timeout 120s go vet ./...` и `timeout 120s go build ./...`: PASS.
- `timeout 600s docker build --target runtime --build-arg VERSION=11401f0acadefd139e0b61616405e4d353d6b7ca -f services/external/control-api-gateway/Dockerfile -t kodex-control-api-gateway:1045-11401f0ac .`:
  PASS. Existing Dockerfile, UID/GID `10001:10001`, canonical binary entrypoint;
  локальный image digest `sha256:18ee5b52db51d39e362f907540780c1f42fd0db50c26df92043b6f4835865078`.
  Это local build, не registry push либо deploy.
- `timeout 120s make gen-control-api-gateway-openapi-go gen-openapi-ts`, затем
  `git diff --exit-code`: PASS, повторная генерация не меняет tracked files.
- Из PWA каталога только generated SDK:
  `timeout 120s ./node_modules/.bin/tsc --noEmit --strict --skipLibCheck --target ES2022 --module ESNext --moduleResolution bundler --lib ES2022,DOM src/shared/api/generated/openapi/index.ts`:
  PASS; handwritten PWA не проверялся и не редактировался.

Отдельная строгая validation:

```sh
timeout 120s go run github.com/getkin/kin-openapi/cmd/validate@v0.135.0 \
  contracts/openapi/control-api-gateway/v1/openapi.yaml
```

Первый запуск: FAIL, `SystemSTTConfigurationDraftInput` содержал лишнее YAML
поле `не произвольным LLM catalog.`. Причина: запятая внутри некавыченного
description в flow mapping. Исправлены кавычки в исходном OpenAPI, Go/TS
описания регенерированы. Повтор строгой validation: PASS; flags, отключающие
defaults/examples/patterns, не использовались. Runtime schema bounds и STT
policy не менялись. Проверены Context7 `/getkin/kin-openapi` (CLI и
`Validate`) и фактический source `cmd/validate` закреплённой версии 0.135.0,
совпадающей с dependency установленного oapi-codegen 2.7.1.

Итоговый проверенный code SHA:
`83609eeb0e85e73cb70f93679ea4da0e06b9428f`. После исправления на нём повторены
все перечисленные проверки: полный gateway race с внешним `timeout 300s` и
внутренним `-timeout=180s`, full vet/build, строгая OpenAPI validation,
Go/TS codegen с чистым readback и strict SDK typecheck — PASS.
Docker runtime повторно собран из того же Dockerfile с `VERSION`, равным
этому exact SHA: PASS; local image
`kodex-control-api-gateway:1045-83609eeb0`, digest
`sha256:ac22fd678349281cf8db9f0c8dcacac58ef1365b7d0e0c66b5f6ad827771212c`.
Последующая запись этого evidence меняет только документацию, не проверенный код.

NOT RUN: real CP protected integration по незавершённым D1..D7, browser E2E,
live provider, staging/cluster. Никаких новых acceptance-требований эта
проверка не вводит; код CP, handwritten PWA и Dockerfile не изменялись.

## Текст PR Для Root

Связь: #1045, #1021, #1018. Полная HTTP интеграция доступных CP Query/Command/
Assistant methods, managed lifecycle, global catalogs, Skills/Memory/bindings,
STT bootstrap/configuration, EMAIL safe receipt/fresh one-use reconciliation.
Дополнительно устранены потерянные model capabilities и assistant pagination,
закреплены schema/SDK и negative response validation.

Ручная проверка: открыть org catalogs без project; получить account model page
и следующую страницу; сохранить managed draft и использовать новый revision/ETag;
пройти Skill/Memory binding с agentVersion; прочитать EMAIL receipt и подтвердить
его exact digest через fresh receipt-bound session; проверить повторный отказ.
Недоступный CP возвращает ошибку, не пустой успешный результат.

Риски: новые обязательные typed поля уже существующих CP responses требуют
согласованного PWA consumption; cookie elevation одноразовая, после неизвестного
исхода сначала authoritative read. Rollback только согласованного gateway/SDK
release, без rollback CP revisions или выдачи более широкого grant. Новых
миграций/Secrets/портов этот HTTP пакет не вводит. Секреты не раскрывались.

PR не переводится в full acceptance до закрытия D1..D7 либо явного решения
владельца по каждой границе. Это не предложение исключить требования #1018.
