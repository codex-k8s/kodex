---
id: OPS-DOC-1262
title: Пользовательская API-приёмка RoleImage и forward restore
type: acceptance-runbook
status: approved
owner: developer
version: 1.2.0
updated: 2026-09-08
---

# Приёмка RoleImage

Источники: #1262, #1031, #1223, CFG-01 и матрица PR #1245;
[переход runner policy](runner-policy-release.md), `GUIDE-DOC-006`,
`GOV-DOC-003`, OpenAPI `control-api-gateway/v1` и существующие команды PWA
`managed-configurations/api.ts`. Этот документ описывает процедуру,
а не утверждает, что новые образы уже собраны или проверены на staging.

`tools/dev/role-image-acceptance.mjs` использует существующий
`owner-session-client.mjs`: легитимные cookie/CSRF, естественный refresh и
принятие обновлённых cookies. QA не получает SSH, kubeconfig, PAT владельца
или provider credentials. Нужна отдельная private API storageState с доступом
к созданию проекта и управлению его agent, image source/build/promotion и
runtime environment. Одну session family не используют параллельные drivers.

Перед GO root завершает новую trusted base/policy и передаёт URL, fresh session
и безопасный manifest фактически обслуживаемых компонентов. Manifest является
отдельным SRE evidence: driver закрепляет SHA256 его точных bytes, но не выдаёт
чтение файла за проверку Kubernetes. State/session/manifest находятся вне source,
в private каталоге 0700, файлы 0600. Checkout должен быть чистым, exact source SHA
записывается в первый journal event и сохраняется между фазами.

## Последовательность и границы

| Фаза | Команда пользователя и владелец состояния | Доказательство и граница |
| --- | --- | --- |
| `prepare` | Новые Project/Agent; GET role-environments; UI ROLE_IMAGE draft → VALIDATE → PUBLISH | Actor из session; проект/роль разрешаются CP. PUBLISH сам атомарно создаёт build; дополнительный REQUEST_BUILD не отправляется |
| Build/admission | Worker claim → trusted base/build/scan/SBOM/provenance → admission | API candidate обязан соответствовать recipe/generation/configurationRevisionRef и COMPLETED build; обязательны provenance/SBOM/vulnerability digests |
| Promotion | POST recipe/promotions с exact artifact/provenance и If-Match | Readback требует ACCEPTED, promoted roles@digest, exact active artifact и promotion receipt SHA256 |
| Первое назначение | Новое runtime environment с admitted artifact; PUT agent/runtime-environment-binding | Проверяются exact artifact/reference и binding.versionRef текущей environment version. Прежние fixture ресурсы не меняются |
| `advance` | Новая forward UI revision того же recipe; к синтетическому Dockerfile добавляется фиксированный комментарий | Отдельные validate/publish/build/promotion. До publication published pointer сохраняется; до rebind прежняя agent binding остаётся точной |
| Выборочный rebind | PREPARE impact → все страницы → POST consumer-bindings | План обязан содержать минимум environment и agent; выбираются только новый fixture project/environment. При наличии чужих items закрытый отказ. Требуются APPLIED у всех выбранных items и итоговый exact artifact |
| `restore` | Чтение самой первой исторической revision → новый draft с тем же исходником | Никакой искусственной правки исторического source. Новый ref/монотонная revision/parent, прежний published pointer и pins; отдельный новый build/promotion и выборочный rebind |
| `inspect` | Только GET доступных owner projections | Показывает unresolved intent, matching projects/configurations/recipe/build states и историю без source. Не повторяет команду и не объявляет UNKNOWN успешным |

Источники authority, специализированные RPC, audit и domain events не меняются.
Owner transaction остаётся единственным владельцем revision/build/receipt/grant.
Для terminal/cancel/expiry driver читает состояние и останавливается; cancel,
retry, archive, delete или repair автоматически не вызываются. Существующие
immutable artifacts, history и pins не очищаются. `advance` и `restore`
каждый создают отдельную сборку: запускать их после принятого результата
предыдущей фазы, с собственным bounded бюджетом.

## Команды

После явного GO root; shell variables ниже содержат только пути, URL,
проверенный digest и новый безопасный prefix:

```bash
node tools/dev/role-image-acceptance.mjs prepare \
  --origin "$QA_ORIGIN" --storage-state "$QA_STORAGE_STATE" \
  --state "$QA_ROLE_IMAGE_JOURNAL" --prefix "$QA_FIXTURE_PREFIX" \
  --runner-digest "$EXPECTED_RUNNER_DIGEST" \
  --serving-manifest "$QA_SERVING_MANIFEST" --timeout-ms 1200000 \
  --confirm APPLY-STAGING-ROLE-IMAGE-FIXTURE
```

Для следующих фаз заменить только `prepare` на `advance`, затем `restore`.
Budget 60–1800 секунд на фазу, отдельный HTTP timeout не более 30 секунд;
GET polling ограничен общим сроком. Все операции выполняются последовательно.
Новый prefix нужен для нового fixture; он не разрешает повтор неизвестного
эффекта прежней попытки. Повтор уже завершённой фазы возвращает сохранённый
результат без новой проверки и без новых mutations.

При неизвестном исходе выполнить ту же команду с `inspect`, без `--confirm`:

```bash
node tools/dev/role-image-acceptance.mjs inspect \
  --origin "$QA_ORIGIN" --storage-state "$QA_STORAGE_STATE" \
  --state "$QA_ROLE_IMAGE_JOURNAL" --prefix "$QA_FIXTURE_PREFIX" \
  --runner-digest "$EXPECTED_RUNNER_DIGEST" \
  --serving-manifest "$QA_SERVING_MANIFEST" --timeout-ms 60000
```

Журнал append-only: fsync intent **до** отправки, ACK с whitelist metadata после
проверки ответа. Request body, Dockerfile, raw response/error и credentials
не записываются. HTTP 400/401/403/404/409/412/422 классифицируются REJECTED,
остальные неоднозначные ответы/ошибки — UNKNOWN. В обоих случаях новые mutations
блокируются до отдельного решения по owner readback; скрытого повторения нет.
Старый helper повторял тот же idempotency key внутри request; само это не
объявляется дефектом owner receipt. Новый driver дополнительно сохраняет intent
между процессами и проходит UI-managed draft/validation/publication.

Одновременно писать journal нельзя: exclusive lock. После аварии оставшийся
lock не удаляется автоматически; root сначала доказывает отсутствие живого
процесса. `inspect` открывает существующий journal read-only. Повреждение,
обрезанный хвост, symlink, доступ группы/других или другая source/origin/manifest
закрыто отклоняются. Не редактировать журнал для превращения UNKNOWN в PASS.

## Восстановление только первого Project create

Исправление #1269 добавляет shared session preflight до первого business intent.
При отсутствии app-proxy cookies/истёкшей сессии новые запуски останавливаются
без project POST и без UNKNOWN business intent. Header журнала может остаться;
после исправления session обычный `prepare` использует тот же журнал.

Старый вариант мог сохранить UNKNOWN до фактической отправки. Для строго
первого Project create предусмотрен отдельный `recover-project`:

```bash
node tools/dev/role-image-acceptance.mjs recover-project \
  --origin "$QA_ORIGIN" --storage-state "$QA_STORAGE_STATE" \
  --previous-state "$OLD_PROJECT_JOURNAL" \
  --previous-sha256 "$EXPECTED_OLD_JOURNAL_SHA256" \
  --state "$NEW_LINKED_JOURNAL" --prefix "$SAME_FIXTURE_PREFIX" \
  --runner-digest "$EXPECTED_RUNNER_DIGEST" \
  --serving-manifest "$QA_SERVING_MANIFEST" --timeout-ms 1200000 \
  --confirm RECOVER-SAME-STAGING-PROJECT-INTENT
```

Нужны отдельное явное решение root после диагностики и точный SHA256 старого
journal. Старый файл остаётся неизменным. Принимается только последовательность
HEADER → первый project INTENT → UNKNOWN без полученного HTTP status, других
commands и ACK. Проверяются origin/prefix/runner/manifest, исходный body digest
и принадлежность старого source истории нового checkout. Новый linked journal
должен отсутствовать и закрепляет predecessor SHA/source.

После fresh session preflight driver полностью читает owner project catalog
по прежнему prefix. Наличие matching project закрыто останавливает восстановление.
Отсутствие записывается как readback, **не как универсальное доказательство
отсутствия эффекта**. Разрешается только доставка первой Project create с
**тем же прежним idempotency key и тем же body**. Дедупликация принадлежит
существующей owner transaction, а не новому prefix. Затем продолжается обычный
`prepare`. Сбой/409/новый lost ACK снова останавливает работу без повтора.

Это исключение нельзя применять к build, promotion, provider, почте или любому
другому внешнему UNKNOWN. Их неопределённость сохраняет прежний общий stop.
При истёкшей сессии recovery не посылает business request. После начала
recovery существующий linked journal не переинициализируется; читать его
можно через `inspect` на том же новом exact source. Для продолжения после
успешных ACK применяется обычный `prepare`, а не второй recovery.

## Что ещё проверяется отдельно

API-проекция с digests доказывает owner readback соответствующих records.
Root отдельно связывает их с реальными scanner/SBOM/provenance/signature,
promotion и node-pull Jobs, UID и фактическим registry digest. Отдельный
разрешённый пользовательский Run должен доказать RuntimeRevision и imageID
нового runner Pod. Driver не запускает платный provider и не подменяет Run
canary/fixture; его output явно сохраняет `runtimeJob: NOT_RUN`.

Браузерный Dockerfile editor/history/restore, права source, SHIPPED/GIT copy,
archive, NOT_SELECTED/CONFLICT/FORBIDDEN варианты impact, revoke, no-secret
negatives и другие CFG-01/02/03 сценарии остаются в #1031/#1223. Поле
`browserUI: NOT_RUN` не позволяет выдать API-путь за UI-приёмку.

Локальные проверки:

```bash
node --check tools/dev/role-image-acceptance.mjs
node --test tools/dev/role-image-acceptance-cli.test.mjs tools/dev/role-image-acceptance.test.mjs tools/dev/owner-session-client.test.mjs
git diff --check
```

Тесты проверяют lost response/restart без нового effect, exact ACK, typed412 и
UNKNOWN503, private journal/corruption/concurrency, read-only inspect, полную
managed последовательность и отказ без exact completed build/SBOM/scan/promotion.
Fixture tests не заменяют staging. Актуальные Node.js fs exclusive open/write/fsync
проверены через Context7 `/websites/nodejs_latest-v24_x_api`.

Публичный CLI дополнительно проверяется в отдельном чистом Git checkout с private синтетическими session/manifest/journal и полностью подменённым transport: `prepare` либо `recover-project`, затем `advance`, `restore`, `inspect`. Проверка фиксирует прежний ключ recovery, три публикации, отсутствие provider Run, отказ неверных параметров, expired preflight и lost ACK без повторной отправки. Она не обращается к staging и не доказывает живую сборку образа.

## Настоящий runtime Pod из существующего fixture

`tools/dev/role-image-runtime-proof.mjs` продолжает существующий завершённый CFG
fixture из append-only journal. Он не создаёт ещё один project/image/environment.
Точный SHA256 исходного CFG journal проверяется при каждом вызове; исходный файл
открывается read-only. Новый отдельный runtime journal закрепляет source SHA,
origin, CFG journal digest и **новый фактический serving manifest**. Исторический
HEADER CFG не заменяется новым manifest. Между фазами runtime journal требует
того же чистого exact checkout и тех же bytes входных файлов.

В этом профиле runtime-controller создаёт `corev1.Pod` напрямую (`mode=turn`),
а не Kubernetes Job. Build/admission Jobs — другая цепочка. Оснастка не должна
выдумывать Job UID или ownerReference у настоящего runtime Pod.

| Фаза | Разрешённый путь | Эффект / доказательство |
| --- | --- | --- |
| `plan` | GET agent, recipe, runtime-configuration, все страницы effective-capabilities, exact account model catalog | Проверяет ACCEPTED/promoted receipt, существующие environment/binding pins и capability `platform.artifact.manage`; никаких business mutations |
| `launch` | Fresh preflight + повтор GET плана; POST `/api/v1/runs` → CP owner CreateRun, server actor/project lineage, immutable Session/Turn/RuntimeRevision, outbox | Fsync INTENT до единственного POST, owner Idempotency-Key; ACK содержит только Run/Session/attempt/task digest. Асинхронный provider effect может начаться **до** HTTP ACK |
| `capture` | GET Run и `/runtime-revision-diff` | Exact project/agent/session/attempt, обязательный turnRef, revision ref/version/digest и IMAGE manifest digest; hash проекта/session/turn для сопоставления Pod |
| Root SRE readback | Реальный managed Pod в проверенном runtime namespace | UID, lease, immutable revision/attempt/hash annotations, exact spec images/imageIDs и SHA256 работающего runner binary; никаких env/credentials/runtime input |

`plan` требует существующую FIXED policy с одним аккаунтом, действующие exact
catalog pins, доступную модель openai-codex и эффективное право на результаты.
При отсутствии prerequisites возвращается `BLOCKED` с закрытыми кодами;
последующий `launch` отклоняется. Оснастка не выдаёт capability, не меняет
аккаунт/model/effort и не выполняет provider probe. Если plan выявил необходимость
настройки, она отдельно выполняется штатной пользовательской командой, затем
повторяется read-only `plan` до первого INTENT. Owner разрешает конкретный READY
план; `launch` повторно сравнивает его digest и закрыто отклоняет drift.

Для всех трёх фаз общие параметры одинаковы:

```bash
node tools/dev/role-image-runtime-proof.mjs plan \
  --origin "$QA_ORIGIN" --storage-state "$QA_STORAGE_STATE" \
  --fixture-state "$COMPLETED_CFG_JOURNAL" \
  --fixture-sha256 "$EXACT_CFG_JOURNAL_SHA256" \
  --state "$NEW_RUNTIME_JOURNAL" --serving-manifest "$CURRENT_SERVING_MANIFEST" \
  --timeout-ms 1200000
```

Только после отдельного GO заменить `plan` на `launch` и добавить
`--confirm START-ONE-STAGING-AGENT-RUN`. Затем заменить фазу на `capture`,
убрав `--confirm`. Budget 60–1800 секунд; HTTP до 30 секунд. `capture` ждёт выхода
из QUEUED, но не требует успешного результата провайдера: `runState` отражается
отдельно. Повтор `launch` после ACK возвращает прежний Run без нового POST.
Любой unresolved INTENT, в том числе lost ACK/409, запрещает следующий POST.
Повтор `plan` после INTENT тоже запрещён. Readback неопределённого Run root делает
по прежнему owner idempotency receipt; оснастка не угадывает Run по названию,
не меняет key, не вызывает retry/cancel/repair и не использует project recovery.

До GO можно выполнить только `plan`: поддержанного provider-free `dryRun` или
`suspend` у RunInput нет. Нельзя создавать Run и рассчитывать успеть отменить его
до provider effect. `launch` передаёт существующий `workspaceAcceptanceTask`
настоящему агенту; subprocess/canary не подменяют выполнение.

Root до `launch` включает ограниченный наблюдатель metadata runtime Pods, чтобы
не потерять короткоживущий Pod. Readback связывает:

- API revisionDigest, projectHash, sessionHash, turnHash, attempt с одноимёнными
  `runtime.kodex.dev/*` annotations, mode=turn и managed=true;
- один Pod UID и lease-ref, nodeName, timestamps/restarts; для
  `workspace-prepare`, `workspace-init`, `role-runtime`, `provider-runtime`
  spec.image равен `imageReference`, каждый фактический imageID сохраняется;
- imageID с digest promoted manifest. Если runtime возвращает platform child
  digest OCI index, нужен отдельный exact registry index → platform manifest
  proof; одинаковые imageIDs сами по себе равенство approved image не доказывают;
- SHA256 `/usr/local/bin/kodex-agent-runner` в том же Pod UID с эталонным runner
  binary SHA из exact trusted base. Чтение выполняет root repo-owned SRE tool;
  QA не получает exec, kubeconfig или новую browser authority.

API `capture: PASS` доказывает только owner projections. `runtimePod`,
`runningRunnerBinary`, `workspaceResult` сохраняются `NOT_RUN` до независимых
доказательств. Для workspace результата отдельно нужен успешный Run, реальные
CODEX_SHELL events и `verifyWorkspaceAcceptance` из существующей оснастки:
exact nonce/artifacts/provenance/CRUD/защищённые пути. Один imageID не закрывает
runtime, CFG, browser или весь MVP.

Локальный публичный CLI regression без сети:

```bash
node --check tools/dev/role-image-runtime-proof.mjs
node --test tools/dev/role-image-runtime-proof.test.mjs tools/dev/runtime-workspace-acceptance.test.mjs tools/dev/runtime-provider-catalog.test.mjs
git diff --check
```

Тесты запускают настоящий Node CLI в чистом temporary Git checkout, private
session/manifest/predecessor/new journal и подменённый transport. Проверяются
plan → launch → capture, единственный Run после ACK, preflight без dispatch,
UNKNOWN/409 без повтора, stale plan/pins/catalog/capability, изменённые
attempt/turn/image и deadline. Fixtures не выполняют vendor/provider effects.

CLI preload `--import` и subprocess API сверены через Context7
`/websites/nodejs_latest-v24_x_api`; локальные fixtures подменяют только transport
и не обходят production origin/session/CSRF или journal guards.

## Смена runner base существующего fixture (#1409)

`tools/dev/role-image-forward-upgrade.mjs` дополняет прежние три фазы отдельным
linked journal. Старый CFG journal не переписывается. Новые Project/Agent,
credentials, provider Run и HEALTH не создаются. Инструмент предназначен только
UI fixture с одной `FROM repository@sha256` и прежними CFG-комментариями;
произвольный Dockerfile закрыто отклоняется. Меняется только точная base image,
прочие поля source сохраняются. Schema/API/permissions не меняются.

Precondition: runner уже собран, OCI проверен через OPS-RUNNER-1382 и опубликован;
новая trusted base/policy/catalog выбрана через OPS-DOC-1258. Reader/tool/authority
images не пересобираются из-за одного runner diff. Root передаёт свежий manifest
и отдельную легитимную API session. Все inputs0600, parent0700, checkout чистый.

Private profile, immutable до plan:

```json
{
  "version": 1,
  "previousState": "/private/completed-cfg.jsonl",
  "previousSHA256": "<SHA256 неизменных bytes>",
  "runnerProvenance": "/private/new-runner.provenance.json",
  "runnerProvenanceSHA256": "<SHA256 новых provenance bytes>"
}
```

Предыдущий журнал заканчивается `prepare|advance|restore|upgrade-complete` PASS
и не имеет unresolved INTENT. SHA, project/Agent/environment/recipe и текущие
published/artifact/build/promotion pins сверяются. Цепочка допускает не более
восьми журналов. Source predecessor и новой provenance должны быть предками
текущего exact checkout. Новая база обязана отличаться от прежней.

```bash
UPGRADE_ARGS=(--origin https://control.kodex.works
  --storage-state "$UPGRADE_SESSION" --profile "$UPGRADE_PROFILE"
  --serving-manifest "$UPGRADE_MANIFEST" --state "$NEW_UPGRADE_JOURNAL"
  --timeout-ms 1200000)
node tools/dev/role-image-forward-upgrade.mjs plan "${UPGRADE_ARGS[@]}"
node tools/dev/role-image-forward-upgrade.mjs apply "${UPGRADE_ARGS[@]}" \
  --confirm APPLY-STAGING-ROLE-IMAGE-UPGRADE
node tools/dev/role-image-forward-upgrade.mjs inspect "${UPGRADE_ARGS[@]}"
```

`plan` выполняет только owner GET и создаёт отсутствующий журнал exclusive;
HEADER содержит predecessor digest/path, new runner digest/provenance и fresh
configuration/version/source/recipe/generation/binding pins. Повторный plan в
этот файл запрещён. `apply` перед первым INTENT повторяет план и требует точное
совпадение; для последующих фаз сохраняются прежние ACK и owner OCC. Пока
HEADER записан, но мутаций ещё нет, expired session не разрешает новый scope.

| Переход | Owner command/readback и cardinality |
| --- | --- |
| Draft | POST ROLE_IMAGE drafts того же configurationRef, fresh config If-Match; новая revision/parent, прежний published pointer |
| Validate / publish | Специализированные revision validation/publication. Publication сама создаёт один build; отдельного build POST нет |
| Build / admission | GET exact recipe/generation/revision; COMPLETED build, ACCEPTED candidate, новые artifact/build/digest и непустые SBOM/provenance/vulnerability evidence |
| Promotion | Один POST exact candidate/provenance, recipe If-Match; GET exact active artifact/promotion receipt; binding пока старая |
| Impact / rebind | Один POST impact, все страницы, ≥2 уникальных consumers только своего environment/project; один POST selected bindings; все APPLIED и новая binding/versionRef/exact promoted digest |
| Terminal | `upgrade-complete` только после полного readback; старые refs/history остаются. Provider/runtime Pod NOT RUN |
| Lost ACK / UNKNOWN | INTENT fsync до HTTP; apply закрыто блокируется. inspect только GET revisions с exact expected source digest, build/artifact/binding metadata; не сохраняет искусственный ACK, не повторяет POST/key |

Полученный inspect после UNKNOWN не является разрешением продолжить apply.
Если API не предоставляет однозначного owner receipt, root фиксирует результат
и согласует отдельное узкое восстановление; отсутствие ресурса не доказывает
отсутствие build/promotion effect. Новый prefix/journal не обходят эту границу.
Timeout после известного ACK допускает обычное продолжение apply того же файла:
мутация с ACK повторно не отправляется, pipeline читается дальше.

`existingFixture`/combined reader принимают только терминальный upgrade с
неизменным predecessor, всеми шестью ACK, новым artifact/build/revision/digest и
подтверждённым forward binding. Combined profile должен ссылаться на новый файл
и ту же provenance SHA, что закреплена в его HEADER. Старые standalone profiles
сохраняются. После plan/upgrade требуется отдельный GO на один provider/mail Run;
observer из OPS-EMAIL-1378 измеряет actual Pod imageID и binary SHA из новой базы.

Rollback — новый forward выбор ранее admitted artifact с fresh owner versions;
старый журнал/policy/schema/generation floor не откатываются. Старая база с
известным MCP defect не является исправной кандидатной версией.

Локальный entrypoint `make test-role-image-forward-upgrade`: public CLI actual
argv plan/apply/inspect, authoritative fake transport с формой реального API,
old/new substitution, foreign/stale/base drift, incomplete/missing rebind,
lost ACK без повторной publication и совместимость standalone/combined.
Эти fixtures не являются live scan/provider/node pull доказательством.
