---
id: OPS-DOC-1198
title: Привязка materialization к runtime lease
type: operation
status: approved
owner: manager
version: 1.1.0
updated: 2026-09-08
---

# Привязка materialization к runtime lease

Issue: https://github.com/codex-k8s/kodex/issues/1198.
Источники: GUIDE-DOC-003, GUIDE-DOC-006 и контракт secretbroker/v1.
Документ определяет договор исправления, но не свидетельствует о готовности
реализации или успешной проверке staging.

## Сквозные сценарии

Пользователь создаёт запуск через control-api-gateway. Control-plane назначает
root actor и project, создаёт run/session/turn и immutable RuntimeRevision.
Runtime-controller получает lease через ClaimExecution, затем вызывает
MaterializeRuntimeCredentials у secret-broker. Producer proof разрешает actor
и project из активного исполнения control-plane, а не из worker credential.
Secret-broker проверяет подписанный context и повторно разрешает исполнение у
control-plane перед выдачей ограниченной credential projection. Потребитель
runtime-controller связывает projection с точными lease, revision и attempt.

System assistant проходит отдельный MaterializeSystemAssistantCredentials
с wrapper execution. Его project отсутствует; root actor принадлежит серверной
цепочке assistant session/turn. Обычный project request не может быть использован
как assistant request и наоборот.

Дайджест детерминированного protobuf request и точная operation фиксируются
в runtime_leases в той же транзакции, что lease и TURN_STARTED. Fence входит
в дайджест, но не сохраняется открытым текстом. Lookup по дайджесту не заменяет
проверки trusted workload, организации, срока lease, generation и всего графа.
ProjectRef является только проверяемым указателем на серверный project.

Proof использует RUNTIME_EXECUTION и серверный root actor. Его срок не превышает
сроки worker grant и lease. Неизвестный или старый lease без зарегистрированного
дайджеста отклоняется. Общий запрет выбора project произвольным worker сохраняется.

## Жизненный цикл пользовательского исполнения

| Переход | Состояние полномочий и событие |
| --- | --- |
| create | До claim права materialization отсутствуют; авторитетный read path control-plane |
| claim | Новый fence/generation и request digest атомарны с lease и TURN_STARTED; consumer runtime-controller |
| renew | Продлевается существующий живой lease; proof снова читает его срок, старый proof сам не продлевается; отдельного события регистрации нет |
| complete | Закрытый lease запрещает новые proof/projection; отдельного события регистрации нет, read path runtime_leases |
| cancel | Owner-транзакция закрывает исполнение и lease; сохранённый digest не разрешает повторную выдачу; отдельного события регистрации нет |
| delete | Удалённое или недоступное исполнение не разрешается; отдельного события регистрации нет, read path control-plane |
| retry | Новая attempt/revision/fence и новый digest; прежний lease недействителен; новый TURN_STARTED при claim |
| lease expiry | Истёкший lease не разрешается даже до фоновой уборки; отдельного события регистрации нет |
| dead-letter | Terminal graph не разрешает materialization; отдельного события регистрации нет |
| WAITING_OWNER | Нет действующего execution lease для выдачи; отдельного события регистрации нет |
| CHANGES_REQUESTED | Прежнее исполнение закрыто; продолжение требует нового claim; отдельного события регистрации нет |

## Жизненный цикл system assistant

| Переход | Состояние полномочий и событие |
| --- | --- |
| create | Project отсутствует; root actor/session назначает control-plane; права materialization ещё нет |
| claim | Проверяется assistant lineage; сохраняется digest wrapper request в транзакции lease и TURN_STARTED |
| renew | Проверяются активные assistant session/turn и lease; срок нового proof ограничен lease и worker grant |
| complete | Закрытые turn/lease запрещают выдачу; отдельного события регистрации нет, read path control-plane |
| cancel | Отзыв следует owner-транзакции закрытия assistant execution; отдельного события регистрации нет |
| delete | Отсутствующие или недоступные assistant session/turn запрещают выдачу; отдельного события регистрации нет |
| retry | Новые attempt/revision/fence и wrapper digest; старый digest не переносится; TURN_STARTED при claim |
| lease expiry | Проверка времени закрывает выдачу без ожидания уборки; отдельного события регистрации нет |
| dead-letter | Terminal assistant execution запрещает выдачу; отдельного события регистрации нет |
| WAITING_OWNER | Нет активного execution lease; отдельного события регистрации нет |
| CHANGES_REQUESTED | Нужен новый owner-approved execution и claim; отдельного события регистрации нет |

## Readiness и доставка

CheckRuntimeCredentialProjectionReadiness не материализует секрет и не выбирает
произвольный project. Его projectless worker profile отделён от execution proof.
Новая самостоятельная workload не добавляется: владелец таблицы и миграции
control-plane, producer runtime-controller, consumer secret-broker. Изменение
policy требует увеличения revision и обычной доставки authority publisher.

## Необходимые доказательства

Требуются положительные user/assistant проверки и отказы при чужом project,
actor, workload, изменённом поле protobuf, неверном wrapper, expired/cancelled
lease, старой attempt и отсутствующем digest. После общей выкатки проверяются
настоящий CODEX_SHELL, workspace/artifacts и quota. До этих результатов Issue
не считается закрытым.

## Тематическое завершение runtime по #1237

Issue: https://github.com/codex-k8s/kodex/issues/1237. Требования определены
`docs/operations/mvp-1031-acceptance.md`; локальные проверки ниже не заменяют
приёмку развёрнутого `CODEX_SHELL` и не разрешают deployment.

Обнаруженный пробел: control-plane сохранял continuation notice последним
`USER` сообщением `sessionContext`, controller переносил сообщения в
`RunnerInput`, но runner не использовал их при сборке provider prompt.
Пользовательский continuation template и история после сброса provider thread
оставались без исполняемого потребителя.

Теперь `buildPrompt` доставляет bounded JSON context перед текущей задачей.
Свежий thread, включая cold restart после изменения Skills/Memory, получает
полную закреплённую историю. `thread/resume` получает только последнюю новую
typed notice, а старую историю читает из provider archive. Notice сверяется с
текущими revision/session/turn/attempt; mismatch закрывает запуск. Если notice
присутствует, дополнительный `runtime-revision-delta` не добавляется. Роли
сообщений остаются контекстными данными, а полномочия принадлежат проверенному
execution binding. Формат RPC и immutable snapshot не изменён.

На общей границе устранён второй блокер: `DecodePromptService` прежде
отклонял любой provider resume с базовым kind `AGENT`, `WORKFLOW_STAGE` или
`AUTOMATION`, хотя именно их выпускает control-plane. Теперь базовый kind
сохраняется, а общий `CurrentContinuationNotice` требует отдельную exact
notice для semantic resume. Проверяются closed JSON, обязательные slots,
effective capabilities, порядок компонентов, diff digest и полная identity.
Legacy prompt без semantic revision сохраняет прежний путь.

### Карта требований и локальных доказательств

Все пути начинаются с авторизованного пользователя control-api-gateway;
root actor, tenant/project и права назначает либо разрешает control-plane.
Runner получает только owner snapshot через controller и работает по exact
lease/revision/attempt. Callback возвращается через controller к
авторитетному состоянию control-plane и к читателю Run/Session.

| Требование | Исполняемый путь | Локальное доказательство | Остаток приёмки |
| --- | --- | --- | --- |
| MVP-UI-30 | ListPromptContextVariables -> GetPromptPreviewContextSnapshot -> server availability/cursor pin -> общий Materialize; publish повторяет scope validation | CP template variables, prompt scope/semantic tests; все файловые descriptors блокируются вместе | Live NOT RUN: смена Agent/runtime, stale cursor/preview, disabled reason/action в UI, publish с недоступным descriptor |
| MVP-UI-31 | claimExecution -> текущие actor/grants/policies -> Intersection -> immutable RuntimeRevision -> BuildTurnInput -> runner validation | CP prompt Intersection, runtime materialization binding и authorityproof tests; controller context/revision drift negatives | Live NOT RUN: пользователь без точного permission, отозванный grant между preview и claim/retry, свежая revision каждого continuation |
| MVP-UI-34 | hydrateRuntimePromptContext -> workflow stage templates -> Materialize -> prompt service envelope -> BuildTurnInput -> AGENTS.md и task | CP canonical producer/runtime consumer, stage template tests; обычный workflow runTurn fixture | Live NOT RUN: editor/preview и prompt исполнителя/координатора с exact published revisions |
| MVP-UI-35 | общий semantic renderer -> typed slot provenance -> обязательные PLATFORM sections -> DecodePromptService -> runner | CP semantic anti-impersonation/duplicate/missing-slot tests; controller prompt provenance; runner semantic prompt tests | Live NOT RUN: UI показывает добавляемые блоки, порядок и audit digests; права чтения полного prompt |
| MVP-UI-36 | prepareRuntimeContinuationNotice -> session_continuation_notices/sessionContext -> CurrentContinuationNotice -> controller -> buildPrompt -> ExecuteViaBroker -> turn/start -> completion | Новые TestCanonicalContinuationConsumerPreservesBasePromptKind, TestSessionContextReachesFreshColdAndResumedPromptOnce, TestSessionContextRejectsStaleNoticeAndInvalidHistory, TestRuntimeTurnContextWorkspaceAndRetriedCompletion; CP continuation diff tests | Live NOT RUN: exact preview, изменение всех компонентов diff, пользовательский template у модели, один notice/turn при retry; PostgreSQL OCC/idempotency компонентные сценарии |
| MVP-UI-37 | CP typed VFS/catalog -> lease-bound file callbacks -> immutable contextfiles -> provider Skills discovery/native skill input и отдельная memory projection | Controller context admission/pins, callback files; runner contextfiles, context discovery/process, explicit empty memory и protected writes tests | Live NOT RUN: active/trash selection, breadcrumbs/search/pagination, skills scan/retention, MCP exact read/search, недоступный Project, immutable inputs после новой revision |
| MVP-UI-61 | CP workspace policy -> controller UID/GID/mount/sandbox -> обычный runTurn -> subprocess CRUD -> PublishResult -> mTLS Complete -> artifact consumer | Новый обычный runTurn fixture включает вложенный create/read/atomic-replace/read/delete; workspace quota/escape/read-only/FIFO tests; completion retry сохраняет usage и provenance | Live NOT RUN: реальный role image и provider, mount/seccomp/UID, quota rejection, Run result artifact readback и отсутствие секретов |

### Матрица доставки контекста

| Переход | Контекст provider | Локальный инвариант |
| --- | --- | --- |
| Первый turn без истории | Точная текущая task и опубликованные инструкции | Нет выдуманного continuation |
| Новый thread с историей | Полная bounded история как JSON data перед task | Сохраняются порядок и исходное содержимое; SYSTEM не становится transport authority |
| Continuation с прежним provider thread | Только текущая typed notice перед task | История не переигрывается, notice не дублируется дополнительным delta |
| Continuation после смены Skills/Memory | Полная история и текущая notice перед task нового thread | Сброс CodexSessionID не теряет прошлый разговор и не снимает exact pins |
| Stale revision/session/turn/attempt | Provider execute не вызывается | Terminal RUNTIME_INPUT_INVALID, без artifacts и измеренного provider usage |
| Потеря completion ACK | Повтор того же authenticated completion | Provider вызывается один раз, bytes receipt, usage и artifact provenance неизменны |
| Cancel/expiry до выполнения | Действуют прежние owner lease/grant gates | Изменение prompt не создаёт новых команд, grants или обходов terminal graph |

### Воспроизводимые локальные проверки

Из корня изолированного checkout при доступном `bwrap` и `TMPDIR=/tmp`
(каждый тест создаёт собственный приватный каталог), без project env и доступа
к кластеру. Более длинный TMPDIR может превысить предел Unix socket path:

```bash
export TMPDIR=/tmp
GOWORK=off GOMAXPROCS=4 go -C services/jobs/agent-runner test -p 2 -count=1 -timeout=180s ./...
GOWORK=off GOMAXPROCS=4 go -C services/internal/runtime-controller test -p 2 -count=1 -timeout=180s ./...
GOWORK=off GOMAXPROCS=4 go -C libs/go/runtimecontract test -p 2 -count=1 -timeout=180s ./...
GOWORK=off GOMAXPROCS=4 go -C services/internal/control-plane test -p 2 -count=1 -timeout=180s ./internal/domain/service/prompt/... ./internal/domain/service/authorityproof/... ./internal/transport/grpc/...
GOWORK=off GOMAXPROCS=4 go -C services/internal/control-plane test -p 2 -count=1 -timeout=180s ./internal/repository/postgres/platform -run 'Test(Runtime|Prompt|Template|Continuation)'
GOWORK=off GOMAXPROCS=4 go -C services/jobs/agent-runner build -p 2 -o /dev/null ./...
GOWORK=off GOMAXPROCS=4 go -C services/internal/runtime-controller build -p 2 -o /dev/null ./...
GOWORK=off GOMAXPROCS=4 go -C services/internal/control-plane build -p 2 -o /dev/null ./...
```

Точные SHA, команды и фактические результаты фиксируются в PR #1237.
Fixture заменяет provider и startup dependencies, но вызывает обычный
`runTurn`, настоящий subprocess с `bwrap`, файловые операции и TLS callback.
Это локальное доказательство доставки prompt/results и защиты повторов;
полный baseline, PostgreSQL component, browser E2E, cluster/render и live
provider в эту проверку не входят и остаются `NOT RUN`, пока нет отдельного
отчёта разрешённой проверки.

### Ручная проверка после разрешённого deployment

1. Зафиксировать source SHA, обслуживаемые role image/runtime/template revisions.
2. Запустить Agent, Workflow и assistant. Сопоставить user template, обязательные
   slots, immutable input descriptors и effective capabilities с exact preview.
3. В существующей Session изменить разрешённые инструкции, model/reasoning,
   image/environment, files, Skills/Memory, tools/MCP/integrations и policy.
   Проверить пользовательский continuation template и typed diff у модели,
   затем повторить с cold restart после смены context digest.
4. Повторить запрос с тем же ключом и получить один turn/notice. Подтвердить
   отказ stale preview/attempt и отсутствие расширения полномочий.
5. Выполнить вложенный create/read/atomic-replace/read/delete; получить artifact
   с exact revision/attempt/execution binding. Отдельно проверить immutable,
   чужой workspace, credentials, traversal/symlink и quota negatives.
6. Пройти VFS/Skills/Memory/MCP строки таблицы, включая pagination, active/trash,
   scan/retention, revoke и неизменность уже закреплённых входов.

Откат исправления выполняется revert тематического PR с новой обычной доставкой;
он возвращает известную потерю контекста continuation. Миграций и новых
provider API нет. Секреты и персональные данные в доказательства не включаются.
Открытые #1198/#1213/#1216/#1189 нельзя считать неактуальными только по успешным
локальным fixtures: их остаток deployment/live acceptance сохраняется.

## Полнота integration grant в digest RuntimeRevision (#1394)

CP `runtimeRevisionDigestFromSnapshot` материализует тот же `RunnerInput`,
который runtime-controller получает через `RuntimeRevisionSnapshot` и
`BuildTurnInput`. Проекция grant обязана включать `definitionVersion`,
`definitionDigest`, `operation`, `inputSchema`, `inputSchemaSha256` наряду
с остальными полями. Потеря этих пяти полей давала неправильный owner digest
при непустом integration grant; строгий consumer отвергал revision до создания
runtime Pod и до provider/MCP effect. Исправление сохраняет все поля при
вычислении исходного digest; проверка consumer не ослабляется.

| Путь / переход | Владелец и полномочия | Pin / результат / потребитель |
| --- | --- | --- |
| Пользователь запускает Agent через публичный Run endpoint | CP проверяет actor/project/Agent и текущие grants | Новый immutable RuntimeRevision; старые pins/history не меняются |
| Runtime-controller вызывает ClaimExecution по защищённому RPC | CP выдаёт server-owned lease/fence/generation и snapshot | Owner digest связывает полный grant, image/environment/provider, input и контекст |
| CP caster → Proto wire → BuildTurnInput | Controller читает защищённый claim; payload не назначает authority | Exact digest и MCP/execution binding; mismatch закрывает execution через прежний terminal path |
| Старый failed Run или повтор | Существующий terminal receipt/history остаётся | Никакого автоматического нового Run, продолжения старого provider effect или правки сохранённого digest |

Тесты используют один обезличенный полный набор
`services/internal/control-plane/testdata/runtime-snapshot/{snapshot,claim}.json`:
owner map → настоящий CP digest; тот же owner map → настоящий caster → Proto
marshal/unmarshal → полное равенство claim; этот claim → `BuildTurnInput` с
зафиксированным expected digest, без локального reseal. Набор содержит
RoleImage, environment policy/tools, provider binding, workspace/attachment и
email grant. Изменённые grant/schema/image/environment/provider pins закрыто
отклоняются. До исправления owner digest regression падает, caster проходит.

Disposable PostgreSQL-сценарий
`integration read and Human Gate decisions preserve effect cardinality`
дополнительно проверяет фактический `ClaimExecution`: оба ненулевых grant
содержат все пять pins, и изменение каждого меняет digest. Это локальное
доказательство owner pipeline, не подтверждение отправки vendor email.

Узкий PG запуск включает обязательные prerequisites `catalog owner probe` и
`model catalog is version bound`, а для affinity также
`runtime configuration publish validates canonical provider accounts`
(создание secondary account); без них сценарии
launch/affinity не имеют подготовленного account catalog.

Публичные локальные команды (Go `GOMAXPROCS=4 GOWORK=off TMPDIR=/tmp`, `-p 2`):

```sh
go -C services/internal/control-plane test -p 2 -count=1 ./...
go -C services/internal/runtime-controller test -p 2 -count=1 ./...
./scripts/tests/control-plane-postgres-test.sh '^TestBootstrapComponent$/(catalog_owner_probe|model_catalog_is_version_bound|integration_configuration_and_grants|integration_read_and_Human_Gate_decisions_preserve_effect_cardinality|runtime_configuration_publish_validates_canonical_provider_accounts|session_provider_affinity_survives_policy_mutation_and_fails_closed_on_revoke)$'
```

Deployment scope: только application CP. Proto/OpenAPI, миграции,
runtime-controller/runner, grants/sidecars и public DTO не меняются.
Исправленный CP совместим с существующим строгим runtime-controller; пустые
integration grants дают прежний digest. Старые уже сохранённые ошибочные
revisions с непустыми grants не чинятся задним числом. Откат CP возвращает
известный отказ новых таких запусков; безопасного fallback digest нет.
После targeted rollout нужен отдельный разрешённый новый Run с новым immutable
snapshot. Ни локальный PASS, ни исправление mapping не означают live acceptance.

Точный SHA, команды/выходы и оставшиеся NOT RUN фиксируются в PR, связанном с Issue #1394.
Актуальная документация protobuf-go по `proto.Marshal`/`proto.Unmarshal`,
`protojson.Unmarshal` и `proto.Equal` проверена через Context7
`/protocolbuffers/protobuf-go`; wire ABI и генерация не изменены.
