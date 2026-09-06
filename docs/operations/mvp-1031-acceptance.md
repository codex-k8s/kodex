---
id: QA-MVP-1031
title: Сквозная приёмка доработок MVP
type: verification-plan
status: approved
owner: qa
version: 1.22.0
updated: 2026-09-07
---

# Приёмка #1031

## Постоянная STT fixture для HTTP smoke и микрофона (#1117)

`node tools/dev/stt-fixture-setup.mjs --expected-sha "$EXPECTED_SHA"`
выполняется **один раз** из точного чистого checkout после разрешённого deploy.
Это Node-only setup: он не запускает браузер, не отправляет audio и не является
проверкой транскрипции. Нужны действующие owner cookies в private storage,
HTTPS `KODEX_E2E_BASE_URL`, `KODEX_E2E_STATE_DIRECTORY`,
`KODEX_E2E_STORAGE_STATE` внутри этого каталога и уникальный
`KODEX_E2E_RESOURCE_PREFIX`. Каталог принадлежит текущему пользователю и имеет
режим 0700; storage — обычный файл 0600 без symlink/hardlink.
Обязательны `KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION`
и `KODEX_PROVIDER_E2E_API_KEY=1`. Credential поступает ровно из одного источника:
`OPENAI_API_KEY` **либо** `KODEX_PROVIDER_E2E_API_KEY_FILE` (абсолютный путь,
обычный owner-private файл 0600 без ссылок). Значение не передавать аргументом,
в браузер, trace, screenshot, Issue или receipt. Setup не печатает значения env
и содержимое ответов/исключений. Provider credential отправляется только одним
write-only POST через Node helper `provider-api-key-acceptance.mjs` (#1116).

| Фаза | Authority, версия и результат |
| --- | --- |
| Preflight | Exact source/origin/private storage; GET session и bootstrap OWNER; отсутствие effective STT и любых SYSTEM_STT sets. Чужой/GIT set требует отдельного решения, даже если пока не связан. |
| Account | Отдельный `openai-codex` account без agent bindings; свежий descriptor/ETag, Origin, CSRF, session и уникальный idempotency key для единственного authorization POST. |
| Draft → VALID → PUBLISHED | Текущий server catalog; typed specification; проверка content SHA256 и всех полей; перед каждым переходом свежая configuration.version из history. |
| Initial bind | Свежие impact digest и configuration.version; consumer STT_SERVICE/stt-tts-service с **expectedAbsent=true**, без revisionRef/version. Это контракт #1118/#1119; старый сервер использовать нельзя. Отдельный GET отсутствия не заменяет owner-transaction CAS. |
| Readback | Exact configuration/revision/digest/specification/account, authorized API_KEY descriptor, ready и пустые blockers, generation; свежий bootstrap speechTranscription READY с будущим validUntil. |
| UNKNOWN/412/частичный успех | Ни повторов mutation, ни автоматического удаления. Каждый эффект предваряется durable UNKNOWN; подтверждённый ответ меняет только свою фазу. После 412 сохранён UNKNOWN до отдельного авторитетного разбора. |

`<prefix>-stt-setup.json.reserved` создаётся через O_EXCL и fsync до эффектов;
`<prefix>-stt-setup.json` атомарно сохраняет безопасные refs, версии, digest,
catalog pin, phase и idempotency keys. Существующий journal/reservation закрыто
останавливает повтор. **Не удалять reservation и не выбирать новый prefix для
обхода UNKNOWN.** Потерянный ACK сначала разбирается через owner read paths
для уже записанных refs и списка accounts/configurations; setup не имеет
автоматического resume/replay. Credential, cookies, content и audio в journal
не сохраняются. Обновлённая при GET/PUT session авторизация записывается в тот
же private storage и используется последующими фазами и STT smoke.

После setup PASS ресурсы намеренно остаются. Затем отдельно запускаются
`stt-http-acceptance.mjs` и browser microphone acceptance из этого плана.
Setup PASS не означает provider inference, аудио или UI PASS. Если readback
не прошёл, journal содержит FAIL/UNKNOWN, а оставшиеся refs нужны для разбора.

Teardown — отдельное явно разрешённое действие владельца через штатный UI/API,
а не `finally` setup. Для связанного fixture сначала создать typed revision
того же SYSTEM_STT с `enabled=false`, validate/publish, получить свежий impact
и rebind с **прежними** exact consumer revisionRef/version; подтвердить disabled
readback. Если требуется восстановить прежнюю конфигурацию, владелец явно
выбирает её published revision и проверяет те же OCC/ownership fences;
setup автоматически чужую привязку не заменяет. Только после закрытия активных
STT consumers удалять fixture account через штатный lifecycle с fresh ETag,
idempotency и readback; blockers не обходить. Непривязанный account после
частичного setup также удаляется только отдельным действием после readback.
Private journal сохраняется как evidence; reservation не снимается автоматически.

Локальная bounded точка входа `make test-stt-fixture-setup` использует только
синтетические sentinel/cookies и проверяет first bind, fresh OCC, UNKNOWN каждой
фазы, запрет чужого/GIT binding, session refresh persistence и отсутствие
автоматического cleanup/audio. Реальный ключ и staging ей не нужны.

Источник: [эпик #1018](https://github.com/codex-k8s/kodex/issues/1018),
[приёмка #1031](https://github.com/codex-k8s/kodex/issues/1031), принятые
замечания владельца `MVP-UI-01`–`MVP-UI-61` и принятое UI-управление образами
и пакетами интеграций. Локальный каталог `.agents/mvp-finish` не является
CI-входом и не добавляется в Git.

Это план, а не отчёт о выполнении. Все флажки изначально пустые. `PASS` старого
`full-local-e2e` или discovery не доказывает выполнение всей этой матрицы.
Ни одна строка не исключается потому, что её ещё не покрывает автоматизация.

## Условия допуска

Дополнение #1086 к Node acceptance: `owner-session-client.mjs` связывает
аутентифицированный snapshot браузера с GET/PUT `/api/v1/session` и последующими
запросами RoleImage, runtime/workspace/artifact и STT. Первый GET подтверждает
серверные metadata; `renewAfter`, access/idle/absolute deadlines определяют
единственный PUT до следующего действия. В PUT передаются host-only cookies,
CSRF и Origin без bearer и `If-Match`. Сервер остаётся владельцем authority и
OIDC refresh token. Client не переигрывает бизнес-запрос после 401 или UNKNOWN.

| Состояние/переход | Результат оснастки |
| --- | --- |
| Свежий authenticated snapshot | GET metadata до первого бизнес-запроса. Нельзя начинать с истёкшего access без ранее сохранённой metadata: сначала повторить штатный browser login/API-session. |
| Наступил `renewAfter` при живых idle/absolute сроках и `BACKEND_REFRESH` | Сериализованный PUT, строгая пара Set-Cookie и metadata; новый Cookie/CSRF применяется до следующего запроса. |
| Новая фаза, GET401, прежняя cookie-bound metadata разрешает refresh | Один PUT до эффекта; неизвестный режим либо истёкший idle/absolute срок закрывает сценарий. |
| Потеря ACK, 401/503 refresh, повреждённая metadata/cookies | Фаза завершает preflight с ошибкой, включая все ожидающие запросы; автоматического повторного PUT нет. |
| Reveal/EMAIL сменили cookie generation | Новая пара принимается атомарно, затем GET metadata; следующий запрос не использует прежний CSRF. |
| Crash либо изменение authenticated state другим процессом | Независимый readback, cookie digest и private file guards запрещают blind overwrite/refresh. Фазы одного файла выполняются последовательно. |
| STT POST завершился ошибкой либо его ответ потерян | Один возможный платный POST; durable reservation сохраняется. Refresh не является поводом повторить audio upload. |

Cookie snapshot и соседний `<storage>.session.json` сохраняются атомарными
rename/fsync/readback в каталоге0700, файлах0600 без symlink/hardlink. Второй файл
содержит только metadata и cookie digest; он не заменяет проверку BFF. Cleanup
временной RoleImage сессии удаляет оба файла. Логи и отчёты не содержат cookies,
provider data или содержимого браузерного storage.

Локальный публичный вход: `make test-owner-session-acceptance`; тот же набор
входит в `make test-stt-http-acceptance`. Проверяются expiry/concurrent renew,
переход между фазами, lost ACK, cookie rotation и отрицательные transport/file
сценарии. Документация Node.js проверена через Context7 `/nodejs/node`:
fetch/Headers Set-Cookie, AbortSignal, private file descriptors и fsync/rename.
Эти проверки не заменяют общий baseline нового SHA и live acceptance.

Дополнение #1113: optional API-key fixture в `integration-path.spec.ts` передаёт
credential только в Node helper `provider-api-key-acceptance.mjs`. UI создаёт
account и проверяет descriptor/status; Node-only owner-session client выполняет
fresh GET с ETag и один write-only POST с Origin/CSRF/If-Match/idempotency.
Повтор при неизвестном исходе запрещён. Проверенная cookie-пара после renewal
возвращается браузеру отдельно, включая cleanup после потерянного ACK; key и
ответ authorization в browser context/storage не передаются. Ошибки фиксированы
и не содержат response body либо cause. Trace/video/screenshots остаются off.
Реальный Run, affinity и точный cleanup сохраняются в прежнем сценарии.
`make test-provider-api-key-acceptance` проверяет эту границу synthetic sentinel
и source AST без живого ключа. Прежнее private утверждение «ключ уже Node-only»
было неверным: до #1113 source вызывал DOM fill; live key scenario до исправления
не запускался. Локальный PASS не является live provider acceptance.

- [ ] Все unit эпика полностью реализованы в своих Issue/ветке/worktree/PR,
  прошли targeted проверки и включены точными commits в ветку #1031.
  Зафиксирован один полный 40-символьный SHA чистой интеграционной ветки.
- [ ] На этом интеграционном SHA выполнены все применимые проверки
  `GOV-DOC-003` и общий
  продуктовый, security и архитектурный цикл. Отдельные unit-review циклы
  исключены только на время ускоренной волны по решению владельца в #1018.
- [ ] Получено отдельное подтверждение владельца на слияние проверенного
  результата. После слияния unit в порядке зависимостей дерево чистого `main`
  сопоставлено с проверенным интеграционным деревом; изменения требуют новой
  проверки. Зафиксирован exact SHA `main` для развёртывания.
- [ ] Зафиксированы разрешение на disposable staging, точный context, namespace,
  UID кластера, digest render и фактически обслуживаемый SHA каждого workload.
  Production, чужие namespaces и прежний стенд не меняются неявно.
- [ ] Развёртывание и hot reload выполняются только repo-owned entrypoints.
  При наличии неразрешённого сетевого решения почтовый сценарий блокируется,
  а не получает прямой wildcard egress или отключённую TLS-проверку.
- [ ] Подготовлены синтетические данные и отдельные credentials тестовых
  провайдеров. QA не получает SRE/root credentials; операции установки
  выполняет разрешённый оператор. В отчёте остаются только имена secret keys.

## Профиль стенда

`dev.sh up` и `dev.sh full-e2e` принимают `--profile web-only|web-with-mattermost`.
Для новой установки по умолчанию используется `web-only`. В существующей
установке отсутствие флага сохраняет профиль из `kodex-dev-source-provenance`;
`status`, `smoke` и `e2e` используют то же правило. Старый render без поля
`deploymentProfile` относится к ранее единственному локальному `web-only`.

| Переход | Проверка и результат |
| --- | --- |
| Новая установка | Разрешены только два канонических профиля; выбранный профиль фиксируется в render, annotations workloads и evidence. |
| Повторный up/readback | Профиль наследуется либо явно совпадает; чужой, повреждённый или symlink render отклоняется. |
| Другой профиль без сброса | Отказ до обращения к Kubernetes: apply отсутствующего ресурса сам по себе не удаляет прежний workload. |
| Явный down | Существующее owner-подтверждение и проверки disposable context сохраняются. Только после успешного удаления namespaces удаляются локальные render/authority markers; ошибка сброса их сохраняет. |
| Новый up после сброса | Можно выбрать другой профиль; authority source fingerprint включает профиль и не смешивает его с прежним назначением ключей. |

В `web-with-mattermost` загружаются модули и запускаются hot-reload контейнеры
interaction-gateway, issuer и platform-worker-grant-agent, socket init и точное
key-delivery назначение. В `web-only` они отсутствуют. Общие компоненты,
включая session-archive и backup-controller, и общие назначения ключей совпадают
между профилями; устранено расхождение #1056. Статический выбор профиля не
подключает Mattermost автоматически и не выдаёт новые integration grants.

Проверки без обращения к живому кластеру:

Для этой рабочей среды `/tmp` нельзя выбирать по одному `df`: пользовательская
квота уже дала `EDQUOT`. Go и browser получают owner-private disk `TMPDIR`;
Go AF_UNIX fixtures дополнительно требуют короткий путь. Созданный каталог
сохраняется до фиксации безопасных логов, чужие temp/cache не удаляются.
Полный baseline выполняется последовательно в очищенном окружении: в него
не наследуются provider credentials, opt-in `*_E2E*`, внешние DSN и
`KODEX_CONTROL_PLANE_TEST_FILTER`. Для Go используются `GOMAXPROCS=2`,
`GOENV=off`, `GOWORK=off`, `GOTOOLCHAIN=local`; для render —
`KUBECONFIG=/dev/null`. Docker/PG, Go build и browser не конкурируют за один
слот. Установка зависимостей и скачивание toolchain не считаются запуском тестов.

```bash
BASELINE_TMPDIR=$(mktemp -d "$HOME/b1031.XXXXXX")
BASELINE_CACHE="$HOME/.cache/kodex-1031-render"
install -d -m 0700 "$BASELINE_CACHE"
export TMPDIR="$BASELINE_TMPDIR" GOTMPDIR="$BASELINE_TMPDIR"
make test-full-local-e2e-entrypoint test-web-only-release
timeout 240s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-only --cache-root "$BASELINE_CACHE"
timeout 240s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-with-mattermost --cache-root "$BASELINE_CACHE"
```

Через Context7 проверены официальные правила
[Kustomize](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/)
и [declarative configuration](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/).

## Текущий состав интеграции

На `177b307f247297629bec8115b5440e8a66d1037d` включены следующие
проверяемые зависимости. Это промежуточная интеграция полного scope,
а не завершённый `main` или допуск стенда.

| Unit | Включённый exact SHA | Граница checkpoint |
| --- | --- | --- |
| Control-plane #1046, PR #1071 | `bebb3e04cfd3633b2edfdf8f0f4d490dba3a8b5d` | D1–D7, SourceWork, account catalog/TOML, Session affinity v7, Env638, Mattermost637, RoleImage build/lineage/rebind643, prompt641, VFS/MCP642, impact644, binding645, stream и selectors72. OwnerGate intent1075 сохранён. Исполняемые19/33/43 включают шесть provider dimensions, account recheck после lock wait, Workflow readiness647 и runtime Automation preview с точными DRAFT/CURRENT_REVISION actor и revision pins. |
| Secret Broker #1068, PR #1069 | `af227acc60d9ca1bd0207429c6d8088fb9496af7` | Encrypted staged lifecycle и protected account model observer, fresh remote provenance, bounded actual Codex process и отказ refresh под read authority. |
| EMAIL #1037, PR #1062 | `f977638e10509d83ce1f27c8b386fa9bd7472842` | Managed exact pins и protected callback до публикации нового serving snapshot. |
| Egress #1029, PR #1065 | `5d8b177054dcf094830e32dceb7f9290d633920b` | Общие mail policy/DNS, admission и отзыв сетевых pins выключенного mailbox без потери общих активных endpoints. |
| HTTP/SDK #1045, PR #1066 | `f1af3df7e74c2d336d11e977edcbb6c858514c92` | D1–D7, selectors72, file catalogs, impact/binding plans и literal OwnerGate intent. CONFIGURE/LAUNCH передают шесть dimensions с проверкой context/profile/account version/health timestamps и явных false/zero. HTTP33/43 передают owner readiness и materialization: опубликованный body не разрешает запуск DRAFT; будущий preview не выдаётся за сохранённую ревизию. |
| PWA #1022, PR #1067 | `0f149e09c12b4b87740cc9879e045708205ffb98` | D2/TOML/source/overlay/capabilities, Home/NewRun, Files67, impact643/644, publication70/binding645, typed641/72, Workflow draft/preview и Git write-back. Consumer19 показывает шесть факторов, candidate runtime profile и отдельную допустимость выбора/submit. Consumer33 использует owner readiness и стабильную header action;43 показывает DRAFT/CURRENT preview, точную задачу automationText и сохраняет обычный input.task без скрытого переназначения. |
| Authority #1059, PR #1060 | `0a59ad482bd07b7019612de0f796932f90df70b7` | Exact EMAIL/Mattermost issuer/key projections и request-bound stream/signature перенесены в общий authority unit. Публичная PostgreSQL оснастка проверяет фактический Goose up/up, неизменность истории и узкие LOGIN/SET capabilities. |
| STT #1020, PR #1070 | `a54db5b325f1f39142119cd92c3479df2049d9ed` | Administrative adapter catalog до configuration/credentials, policy60, согласованные limits и актуальная STT model profile revision. #1074 устраняет false PASS при удалении внутренних символов из transcript: только регистр, Unicode whitespace и конечная пунктуация. |
| Runtime-controller #1025, PR #1063 | `297ea7a7e3be7233f521e5b4ded3c85a482c6a48` | Exact v7/context, четыре MCP file tools, initial-bound stream до512MiB, private unlink spool, проверка Complete/EOF/size/digest и свежая authority до выдачи HTTP body; оба profile render. |
| Agent-runner #1026, PR #1058 | `5613b045c4d63956de96819d8e42fc7b2c13b8d9` | Сохранён v7 mode/effort/workspace lifecycle; отдельный bounded file client и закрытый GET через оба локальных MCP bridges. Native effects и Usage сохраняются при post-execution failure. #1072 согласует canary/publish/reset/prompt межпроцессным bounded lock; #1073 сохраняет расход пяти failure branches и callback retry. Actual non-root loopback→SO_PEERCRED UDS→TLS1.3/mTLS/ticket передаёт33MiB+7 без выдачи execution ticket provider-процессу. Live agent/artifact ещё NOT RUN. |
| Integration-gateway #1028, PR #1064 | `b889673f04bd431788ac7553e1a80b033852e431` | SourceWork и исполняемый Git write-back: one-parent proposal commit, exact empty lease, separate branch/PR receipts, UNKNOWN read-only recovery, GitHub PR/GitLab MR readback; Git runtime/tmpfs/non-root и оба render. Live provider proof ещё NOT RUN. |
| Interaction-gateway #1030, PR #1061 | `c7d03d818ac4e6c5a226d49c6d14d7c6dc798b9e` | Actual package системных subscriptions/deliveries; OwnerGate до notification/mirror, exact input и approval claim, fail-closed discovery. |

Сохранено исправление #1056. Исходные Proto/OpenAPI/policy объединены по семантике,
generated Go/SDK/PWA validator получены повторной генерацией.

Дополнение72 включено без конфликтов. Source/Proto/operations совпадают
с проверенным CP0ef, OpenAPI и generated Go/TS — с проверенным HTT Pe66.
Это сравнение точных файлов не объявляется новым codegen либо полным baseline.
На CP0ef полный disposable Bootstrap **PASS**30.061s, race/vet/build,
SQL/Proto/policy72/ABI **PASS**. На HTT Pe66 full race **PASS**6.046s,
vet/build/OpenAPI/SDK/codegen/Proto/policy72/ABI **PASS**; первый compiler
запуск **FAIL** из-за пользовательской квоты `/tmp`, тот же код проверен
повторно с disk TMPDIR. На runner5613 full race/vet/build и public
`make test-agent-runner` **PASS**; отдельный TLS callback/retry regression
**PASS**6.696s, app full race15.846s. Длинный TMPDIR первоначально дал
**FAIL** старого AF_UNIX fixture, короткий private path прошёл.
Общие baseline/review/live checks нового интеграционного SHA — **NOT RUN**.

На `53ae573c6f1823137b04591a2e9a6bc3ed4429fc` локально **PASS**:
`lint-proto`, `build-proto`, `check-proto-codegen`,
`test-authority-policy-codegen` и `test-internal-rpc-authority-abi-render`;
tracked tree после codegen чистый. Безопасный лог —
`integration-53ae-contracts.log` в приватном evidence-каталоге.

HTTP unit 65b имеет полный race/vet/build, strict OpenAPI/SDK и canonical
Go/TS replay **PASS**. Первый full gateway **FAIL** выявил четыре отсутствующих
CP message IDs; оба locale registry исправлены, все используемые CP message IDs
сверены, полный повтор прошёл. PWA unit 2d747 имеет 1107/1107 unit в 209 файлах,
lint/format/build/typecheck/E2E TypeScript и canonical Go/TS replay **PASS**.
Их merge в 25243d6 выполнен без конфликтов; OpenAPI/SDK совпадают с HTTP 65b.
Интеграция сохраняет собственный regenerated package validator и более новый
Secret Broker Proto, поэтому равенство всех файлов PWA/Proto с unit-ветками
не заявляется. Общий baseline на 25243d6 ещё **NOT RUN**.

Историческое evidence пакета #1075:

- CP `a7761d6e074954308e129e39b0ebd65cb198b4b4` исправляет
  [#1075](https://github.com/codex-k8s/kodex/issues/1075): intent, bounded content
  preview, последствия решений и exact predecessor attachments доступны через
  single/list/event/resolve/replay. Подписанный project OWNER проверяется до
  count/OCC/receipt; terminal consequences сохраняют историческое состояние.
  Полный PostgreSQL Bootstrap35.159s и Avatar0.311s, CP race/vet/build,
  SQL boundary и diff check — **PASS**. Два исходных fixture **FAIL** исправлены
  и сохранены в unit evidence.
- HTTP `5726cd8dd7a9488501f7e0ed0202b88ff7cdd42c` сохраняет false значения
  последствий, literal content/scope и точные refs; семь consequence message IDs
  переведены в ru/en. На том же production source полный gateway race/vet/build
  **PASS**; после locale/test delta targeted HTTP/usertext **PASS**. Source
  OpenAPI/SDK этим пакетом не менялись.
- PWA `d73436127d683db045c51afbfe689d1a3124a9eb` включает OwnerGate preview,
  локализацию catalog state/source/failure, богатую подпись выбранной connection
  и ровно пять cron occurrences. На его предке52219 полный Node baseline
  **PASS**: 1114 unit в209 файлах, lint/format/build/typecheck/E2E TypeScript;
  Home/Gate390/2900 scoped browser4/4 **PASS**. После малой UI delta targeted9
  и typecheck **PASS**. Полный38 и общий integrated baseline — **NOT RUN**.

Локальное evidence текущего пакета:

- PWA0f149: 1145/1145 unit в215 файлах, lint/format/build/typecheck/E2E check
  — **PASS**. Полный публичный synthetic на exact0f149 —41/41 **PASS**,7 минут;
  проверены восемь Home viewport, сохранены14 контрольных снимков. Предыдущий8a0
  имел39/41 **FAIL**: Git source fixture не обслуживал реальный history GET;
  добавлен точный ответ и assertion. Два nullable prompt consumers больше не
  показывают ложный alert, намеренный unknown-error fallback сохранён.
  Исторические fixture/SSR failures находятся в PWA README.
- На интеграционном177b canonical integration-package validator replay
  и production build с vue-tsc — **PASS**, tracked tree после генерации чистый.
  Только этот generated JS отличался от PWA0f149 и сохранён как результат
  совместной source schema. Предупреждение Vite о chunk более500kB остаётся.
- CPbebb: полный PostgreSQL Bootstrap31.429s и Avatar0.392s, полный
  race/vet/build, SQL/Proto/canonical replay, policy72, ABI и оба release
  renders — **PASS** до последнего узкого lock follow-up. После него targeted
  PostgreSQL0.292s, repository race1.894s и SQL — **PASS**. Тест подтвердил
  ожидание через pg_blocking_pids и отказ после отзыва account. Полный Bootstrap
  после follow-up — **NOT RUN**. Исходный lock fixture **FAIL**0.650s из-за
  state/enabled constraint исправлен; история сохранена в unit документе.
- HTTPf1af: полный gateway race6.004s, vet/build, strict SDK и canonical
  Go/TS replay — **PASS** на consumer source до объединения CP; последующие
  coordinator/DRAFT regressions race2.038s — **PASS**. После exact CP merge
  Proto/policy72 replay — **PASS**; неизменные API/HTTP/SDK source повторно
  целиком не проверялись. Mapper не выводит readiness из опубликованного body.
- PWAad25: 1129/1129 unit в212 файлах, lint/format/build/typecheck/E2E TypeScript
  — **PASS**. Provider browser390/2900 на предшествующем e292 —2/2 **PASS**,
  снимки просмотрены; после малых lint/badge правок browser — **NOT RUN**.
  На интеграционном c9f531 production build с vue-tsc — **PASS**; прежнее
  предупреждение Vite о chunk более500kB сохраняется.
- На033fab bootstrap ConfigMap перенесён в control-plane, его единственного
  runtime consumer. Имя и содержимое сохранены; EMAIL использует отдельную
  Secret projection. Canonical objects до/после совпадают: web-only398 и
  web-with-mattermost408. Публичный email projection render обоих профилей
  — **PASS**. Verifier и mailpolicy fixture читают тот же source; на5a20c5
  mailpolicy race1.076s — **PASS**.

Предшествующее локальное evidence включённых unit:

- CP399cf: полный disposable Bootstrap33.432s и Avatar0.410s, targeted race,
  полный vet/build, SQL/Proto/codegen — **PASS**. Проверены candidate profile,
  exact actor/context, manage/launch, concurrent capacity и фактические fresh
  credentialed catalog observations. Первые permission fixture **FAIL**
  сохранены в unit документе.
- HTTP38c596: полный gateway race6.260s, vet/build, strict SDK, canonical
  Go/TS replay, Proto/policy72 — **PASS**. Первый targeted **FAIL** из-за старой
  fixture без usage исправлен; административный NOT_EVALUATED явно проверен.
- PWAaa804: 1118/1118 unit, lint/format/build/typecheck/E2E TypeScript — **PASS**.
  Предшествующий455b имел lint **FAIL**, устранённый явным сужением unknown.
  Полный новый synthetic — **NOT RUN**, прежний52219 scoped4/4 остаётся
  историческим результатом.
- Authoritybf93: публичный PostgreSQL **PASS**, actual Goose up/up/history0.167s
  и все LOGIN/SET/capability/promotion assertions. Head0a59 добавляет только
  документацию; production tree680c ранее прошёл полные race/vet/build и
  Proto/codegen. Install/render нового head — **NOT RUN**.

Producer/HTTP/PWA для [#1076](https://github.com/codex-k8s/kodex/issues/1076),
[#1077](https://github.com/codex-k8s/kodex/issues/1077) и
[#1078](https://github.com/codex-k8s/kodex/issues/1078) включены; Issues остаются
открытыми до сквозной приёмки. Перечень01–61/CFG не сокращён. Transient capacity и
UNKNOWN health не отменяют допустимый queued submission. Общий baseline,
общий review и deployed/provider acceptance на момент фиксации исходного177b
— **NOT RUN**; последующие результаты точного SHA фиксируются в PR #1051.

На code SHA `2eda37a2ebb07b03ba68a5e7b555b37526e47424` локально **PASS**
оба `local-role-image-render-contract-test.sh` профиля, включая положительный
EMAIL render и39 отрицательных проверок каждого профиля. Hot reload сохраняет
объявленные `ephemeral-storage` requests/limits controller, его disk spool2Gi
и единственный mount. EMAIL verifier проверяет точные Secret/Deployment/
NetworkPolicy/ConfigMap права publisher, отдельную readback роль и неизменённые
admission policy/binding с закрытым отказом. Предыдущий запуск на дереве9907801
был **FAIL**: verifier ожидал прежний Secret-only RBAC; ошибка исправлена без
изменения полномочий runtime. Логи `integration990-spool-*-fixed.log` и первый
`integration990-spool-web-only.log` сохранены в приватном evidence-каталоге.

На `e271f2c4034355bd9766c443d72ebc940ffa4f5a` canonical Go/TS OpenAPI replay
локально **PASS**; regenerated файлы совпадают. Это не полный baseline.
При объединении PWA d21 возник конфликт generated SDK. Исходный OpenAPI
сохранил полный контракт HTT Pe66, включая четыре72 endpoint; Go/TS повторно
сгенерированы из этого источника, а не выбраны из конфликтующей стороны.
На `29607deede87f906c46a90ff23ed2dc5f76e3ff3` production build с typecheck
и canonical Go/TS clean replay — **PASS**. Сохраняется предупреждение Vite
о chunk более500kB. Безопасные логи: `integration-296-pwa-build.log` и
`integration-296-openapi-replay.log` в приватном evidence-каталоге.
Общий baseline и вся сквозная приёмка — **NOT RUN**.

Spool ограничивается самим controller двумя файлами по512MiB и deadline.
Kubelet periodic scan не учитывает открытые unlink-файлы; `emptyDir.sizeLimit`
и `ephemeral-storage` не заменяют внутренний лимит. Правило проверено через
Context7 по официальной документации
[Kubernetes ephemeral storage](https://kubernetes.io/docs/concepts/storage/ephemeral-storage/).

На PWA unit `d21add2dd9e69d61d146f65e90eb7f9f060940fb` локально **PASS**:
1055 unit в201 файле, lint/format/build/typecheck и E2E TypeScript. Сохранены
исторические **FAIL**: scoped29787 —1/2 (390 прошёл,2900 Chromium crash при
EMAIL), full35 на50d5810 —34/35 с crash на2560. Установлена исчерпанная
пользовательская квота `/tmp`: запись4096 bytes дала EDQUOT122 при наличии
свободного места файловой системы. Playwright создавал profile/artifacts через
`os.tmpdir()`. На private disk TMPDIR неизменённый2900 сценарий прошёл
за55.1s, Chromium завершился с0. Browser flags и timeout не ослаблены.
Доказательства и точная команда записаны в PWA README; это устранение
проблемы среды, а не полный browser PASS. Полный35 повторяется после всех
оставшихся consumers; общий browser baseline нового SDK — **NOT RUN**.

## Нормализация unit перед общим gate

Read-only сверка относительно `8026633a9f92625df43163bf6dbef85df936289a`
подтвердила смешанные prerequisite histories: CP содержит части EMAIL,
integration-gateway, authority и broker, а HTTP/PWA/runner — реализацию
producer-зависимостей. Один rebase на новый `main` не гарантирует один unit
в diff. На момент исходного177b нормализация веток — **NOT RUN**. Подготовлены
1996 назначенных путей и десять общих файлов; проверка плана в памяти сохраняет
точные blobs/modes и полное итоговое дерево. Реальные результаты сборки цепочки,
проверок её SHA и публикации фиксируются отдельно в PR #1051.

Authority #1060 уже содержит source/signature и канонически полученные
Proto/generated из общей интеграции; source/runtime checks680c и дополнительный
Goose proofbf93 пройдены. Это первый подготовленный unit, но полная нормализация
всей цепочки ещё не выполнена. Дальнейшая локальная цепочка:
CP #1071 → broker #1069 → egress #1065 → STT #1070 → EMAIL #1062 →
integration #1064 → interaction #1061 → controller #1063 → runner #1058 →
HTTP #1066 → PWA #1067 → acceptance #1051. Scheduler уже присутствует в `main`.
Это порядок подготовки кода, не разрешение частичного deploy.

После последних fixes фиксируются целевой интеграционный commit и tree ID.
Каждый изменённый путь получает unit-владельца; shared Makefile, registry,
profiles и install/render scripts распределяются по смысловым изменениям.
Общие source-only prerequisites допустимы в producer-пакете по
`GUIDE-DOC-004`, но их происхождение сохраняется как
`исходный SHA → новый SHA → owner → пути`. Например, CP импортирует новые
emailbridgeapi/mailpolicy/dnsresolver, а EMAIL использует новые CP API;
перестановка веток сама по себе этот цикл исходных зависимостей не устраняет.
Handwritten implementation другого deployable не переносится вместе с такими
контрактами. Generated code воспроизводится из согласованного source.

Последовательная локальная сборка сохраняет исходные refs для восстановления,
проверяет allowlist каждого unit diff и его собственное subtree. Прежняя
смешанная ветка после этого не сливается обратно. Перед публикацией сверяются
точные remote head/base/body/draft и identity; переписанный ref публикуется
только с exact ожидаемым old SHA в `--force-with-lease`. Owner/main операции
остаются за отдельным human gate.

Финальное доказательство сохранения дерева:

```bash
git diff --exit-code "$TARGET_COMMIT" "$NORMALIZED_TIP"
test "$(git rev-parse "$TARGET_COMMIT^{tree}")" = \
  "$(git rev-parse "$NORMALIZED_TIP^{tree}")"
```

Затем выполняются targeted checks изменённых unit и общий Web-first baseline
на одном окончательном интеграционном SHA. Три направления общего review
проверяют тот же SHA. После owner gate и реального merge сравнение tree
повторяется для `main`; совпадение содержимого не выводится из количества
слитых PR или совпадения их названий.

## Настоящий файловый запуск в приёмке

В code `d52cb2321ae278668f19f0c3e1a239f1f1074da9` RoleImage phase после exact
image/imageID readback ждёт terminal Run и проверяет результат настоящего
provider/agent. `tools/dev/runtime-workspace-acceptance.mjs` передаёт модели
точный synthetic Node.js probe: create/read/atomic replace/read/delete своего
файла и отдельные отказы записи в input/source/context/Skills/Memory/credential,
symlink/traversal и путь вне workspace. Значения защищённых файлов не читаются.
Перед launch явно выдаётся Artifact capability и закрепляется разрешённый
account с точными catalogRevision/catalogDigest/providerDefinitionKey из fresh
account catalog; default reasoning effort не отправляется как входное поле.

Проверка требует успешный native `CODEX_SHELL` event, два synthetic artifact
и созданный runner `workspace-write-result.json`. HTTP download сверяется с
owner Artifact metadata, digest/size, Run/Project/Session и `AGENT_RESULT`;
provenance сравнивается с публичной authoritative RuntimeRevision identity
и текущей attempt. Чужой или quarantined artifact закрыто отклоняется,
pending scan ожидается в ограниченном бюджете. Содержимое, transcript,
credentials и сырые команды не попадают в итоговый evidence.

После успешного workspace контроля оснастка создаёт отдельный quota Run.
Модель создаёт 10001 пустой собственный regular file и на45 секунд оставляет
окно для проверки. Каталоги используют общую группу runtime (2770), а
положительные result files —0640: runner UID10001 должен читать результаты
provider UID10002. Это не даёт доступа за пределы workspace.

Инициатором остаётся owner web session с launch authority. POST Run создаёт
новые Session/attempt/RuntimeRevision; публичный revision-diff фиксирует
точный digest и owner binding. Оператор оснастки выбирает единственный Pod
по digest, project/session hash, attempt, promoted image и imageID. Через
repo-owned `kubectl exec` в точном `role-runtime` запускается существующий
защищённый `runtime-workspace-canary`; до и после сверяются Pod UID и
annotations. Принимается только `QUOTA_EXCEEDED`, без чтения содержимого
файлов. Другой denial, смена Pod или недоступная проверка завершают сценарий
ошибкой. После завершения модели authoritative Run должен иметь `FAILED`,
`RUNTIME_INPUT_INVALID`, ни одного result artifact и успешное native
shell event. Общего workspace error без exact canary недостаточно.
Это опубликованный safeErrorCode: внутренний `RUNTIME_WORKSPACE_INVALID`
нормализуется runner в `RUNTIME_INPUT_INVALID`. Оснастка не требует
несуществующего отдельного публичного workspace error code.

Runner `13c092f5131e17b8f1a7be4e1657f77bc1e88cde` сохраняет уже выполненные
native effects до post-execution quota check. На этом runner SHA **PASS**:
полный race/vet/build и `make test-agent-runner`; live provider —**NOT RUN**.
Два Run не повторяются при terminal failure. `passed` RoleImage записывается
только после положительного workspace контроля и отрицательного quota
контроля; до второго этапа `quota: NOT RUN` сохраняется. Execution volume
удаляет обычный runtime terminal cleanup, оснастка не исправляет Pod вручную.

На code SHA `adb21e4d3367049853f631673248b7a3e26e0044`
`make test-runtime-workspace-acceptance` локально **PASS**:38 проверок,
включая точные non-root probes, ошибочные owner/revision/attempt/Pod bindings,
отсутствие native execution и неверный terminal. Также **PASS**
`make test-full-local-e2e-entrypoint`, shell/Node syntax и форматирование.
Первый quota fixture test завершился **FAIL** по30s deadline из-за
незавершённого дочернего процесса; изолированная process group и bounded
cleanup исправлены, повтор38/38 прошёл. Эти проверки используют fixtures
и не доказывают live provider acceptance; staging и настоящая квота —
**NOT RUN**. Через Context7 проверены официальные
[Node.js24 fs](https://nodejs.org/docs/latest-v24.x/api/fs.html) и
[kubectl exec](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_exec/).

На code SHA `17c120887754cee3813a6974ce36d59b7476f2a8` локально **PASS**:
Go/TS SDK и Proto source generation/clean replay, policy65 codegen,
authority ABI render, оба integration-gateway/EMAIL projection render,
web-only release и полный PWA build/typecheck. Integration-gateway integration
и app race: 33.188s/1.065s. Исходные Proto ранее lint/build проверены на 72b9cd2,
VFS additive source — на b0bea6f; полный baseline нового SHA ещё NOT RUN.
Безопасные логи: `integration-17c-contracts.log`,
`integration-17c-pwa-build.log`, `integration-17c-gateway-race.log`
в приватном evidence-каталоге. Предупреждение
о JS chunk более500kB сохраняется. Полный baseline этим запуском не выполнен.

Исторический code `303f75b535cde2fd451d4d4ea22841becbaa488b` имел **FAIL**
PWA build: fixtures/consumer не удовлетворяли D2 input pins, output default и
overlaySchema. Новый запуск7f2 после PWA9bb подтверждает устранение этого FAIL;
старые логи `303-integration-contract-render.log`, `integration-ff7-openapi.log`
и `integration-catalog-source-pwa-build.log` сохранены.

При merge сохранены managed EMAIL callback, exact configuration/policy pins и
проверки CONNECT до provider bytes; старая prerequisite history их не откатила.
SourceWork owner и account catalog/Session affinity теперь включены; локальный
combined proof не выдаётся за deployed provider acceptance. Git write-back
HTTP/PWA/provider acceptance, полный RoleImage lifecycle и новые consumers
остаются обязательными.

На предыдущем exact `b4ef7be2976e528855c2a3f94e7094d28a1d00af` локально **PASS**:
PWA `npm run build` с typecheck, Proto lint/build/clean replay,
authority policy codegen, gateway AsyncAPI clean replay,
internal RPC authority ABI render, web-only release и оба EMAIL projection
render. HTTP transport, STT client и security boundary прошли targeted race/vet.
Ранее интеграционная сборка PWA была **FAIL** из-за неполных fixtures,
несовместимых typed pins и schema/generator constraints; новый запуск после
PWA `6931d7d52` подтверждает устранение этих ошибок. Безопасные логи текущих
проверок: `b4ef-integrated-pwa-build.log`, `b4ef-integrated-contracts.log` и
`b4ef-integrated-http-adapters.log` в приватном
локальном evidence-каталоге. Полный baseline, общий тройной review,
merge, deploy и сквозная матрица остаются **NOT RUN**.

Первый HTTP запуск ошибочно указал два несуществующих каталога adapter:
результат **FAIL (setup)**, HTTP transport в нём прошёл. Повтор с фактическими
`internal/sttclient` и `internal/security/boundary` прошёл. Сборка PWA сохраняет
предупреждение о размере JS chunk более 500 kB; оно не скрыто.

Результаты отдельных unit и synthetic fixtures не отмечают строки этой
матрицы выполненными. Следующая интеграция обязана включить актуальные HTTP/PWA
и остальные незавершённые функции 01–61 и CFG-01/02/03: model catalog/TOML UI,
immutable overlay history/publish, exact prompt preview, effective capabilities, VFS,
полный CFG execution, remaining impact plans и живая protocol-specific EMAIL
readiness. Последняя уже имеет локальный Provider.Probe; её staging proof ещё
NOT RUN. Все прежние и новые unit proofs оцениваются на своих exact SHA.

## Локальная подготовка EMAIL

EMAIL включён вместе с сохранённым #1056: optional session-archive,
backup-controller и точное назначение archive issuer не удаляются.

В обоих локальных профилях исполняются email-bridge, authority issuer и
platform-worker-grant-agent через закреплённые Go/Air, а CLI миграции через
`run-go-command.sh services/internal/email-bridge ./cmd/cli up`.
Host заранее загружает Go modules; source, modules, sumdb и Air внутри
контейнеров read-only. У каждого процесса отдельный writable build cache и
ограниченный tmp; root filesystem read-only. Socket init запускается сразу
с UID/GID 29000 без capabilities; bridge/migration сохраняют UID 10001,
issuer 29001, grant agent 29004. Writable grant volume остаётся доступен
только producer, consumer получает read-only mount.

| Сценарий | Авторитетный путь и результат |
| --- | --- |
| Подготовка без API | `render-local.sh` использует локальный Kustomize и сохраняет profile, source SHA и content digest. Новые credentials не выдаются. |
| Разрешённая установка оператором | Существующий installer материализует отдельные runtime/migration DB descriptors и сертификаты; `deploy-local.sh` ждёт Certificates, PostgreSQL StatefulSet, затем запускает точный migration Job и ждёт completion до application Deployments. |
| Повторная установка | Точное имя migration Job пересоздаётся bounded-командой; goose использует собственную таблицу версий EMAIL. Ошибка миграции прерывает установку. Итоговый readback включает EMAIL PostgreSQL и migration Job. |
| Bootstrap mailbox projection | До запуска CP/EMAIL installer создаёт отсутствующий canonical Secret ровно один раз. Существующий Secret проверяется по точным имени, namespace, UID, типу и owner label; его данные не перезаписываются. Гонка create завершается повторным readback, чужой owner и ошибка API прерывают установку. |
| Повторный render текущего source | Разрешённый `dev.sh up` читает только mailbox document из принадлежащего CP Secret; ошибка API не становится bootstrap fallback. `render-local.sh --mail-configuration` передаёт exact snapshot в реальный typed mail producer. Из одного результата берутся immutable ConfigMap, exact CNI NetworkPolicy, оба policy digest и annotations обоих Pod templates. Secret с credentials не копируется в render. |
| Source изменился до apply | Installer повторно читает документ и сравнивает canonical source hash с provenance до foundation apply. Несовпадение требует нового render; текущие данные CP не откатываются. После этой проверки возможен новый CP transition: runtime source/readback mismatch остаётся закрытым отказом до следующей согласованной доставки. |
| Hot reload | Air наблюдает модуль EMAIL и `libs/go`; не переписывает authority, TLS, Secret descriptors или immutable runner pins. Почтовые effects и unknown-outcome остаются ответственностью EMAIL owner. |
| Обновление mailbox projection | CP publisher обновляет целый `email-bridge-mailbox-projection` через exact `get/update` RBAC. EMAIL получает обязательный read-only Secret volume без `items`, `subPath` и `subPathExpr`; loader проверяет единое AtomicWriter поколение документа и credential keys. Пустой release bootstrap не выдаёт mailbox authority. |
| Недоступный producer | Ошибка CP publisher или недоступные credentials не заменяются allowlist. Нет фиктивных mailbox credentials или обхода readiness. |

Service остаётся внутренним HTTPS `443 -> https/8443`, CP authority идёт по
точному gRPC target с TLS. Mail CONNECT использует только отдельный
`egress-gateway.kodex-system.svc:8082`; general `8080` и STT `8081`
отклоняются render-проверкой. NetworkPolicy сохраняются из EMAIL dependency.
Новый Ingress для EMAIL не создаётся. Проверка сравнивает полные canonical
NetworkPolicy specs, issuer binding и имена Secret descriptors, не их значения.
Локальная замена Go image не меняет runtime-controller schema/policy и
передаваемый immutable runner image; соответствующие существующие renderer
assertions остаются обязательными.

Проверки подготовки без живого API:

```bash
make test-full-local-e2e-entrypoint test-local-go-cache-contract \
  test-local-image-cache-import-contract test-local-material-contract-revision \
  test-local-backup-controller-credentials-contract
make test-automation-scheduler
make test-web-only-release test-go-toolchain-contract check-integration-package-codegen
bash scripts/tests/local-mail-projection-contract-test.sh
timeout 300s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-only --cache-root "$BASELINE_CACHE"
timeout 300s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-with-mattermost --cache-root "$BASELINE_CACHE"
timeout 450s bash scripts/tests/local-email-process-contract-test.sh "$BASELINE_CACHE"
```

Оба renderer запускают EMAIL positive и 39 negative cases. Process fixture
требует заранее доступный закреплённый Go image и primed cache: Docker
`--network none`, non-root, read-only root/source/modules, отдельные tmpfs.
Она выполняет настоящий socket init, отказ CLI без DSN и сборку через Air;
не подключается к PostgreSQL, mailbox или Kubernetes и не заменяет live E2E.

Прежняя причина `test-web-only-release` FAIL, отсутствующий CP publisher,
устранена потреблением owner checkpoint `af74fc7dc`; новый результат определяется
запуском на интегрированном дереве, а не результатами отдельных PR.
Дополнительно через Context7 проверены официальные правила
[Secret volumes](https://kubernetes.io/docs/concepts/configuration/secret/):
обновление eventually consistent, `subPath` не получает обновления,
immutable Secret нельзя использовать как обновляемую проекцию.
Потреблён EMAIL source-header `ae24f4bc7`: runtime сравнивает policy и source
readback до TLS/provider bytes. `EMAIL_BRIDGE_EGRESS_POLICY_DIGEST` больше не
закрепляется старым bootstrap значением в локальном render: он пересчитывается
вместе с `EGRESS_GATEWAY_MAIL_POLICY_DIGEST` из актуального snapshot и DNS pins.
Локальные fixtures используют только пустые поколения, не обращаются к живым
mail hosts и не назначают owner allowlist. Для непустого source требуется
разрешённый оператором resolver и hosts; `--mail-resolv-conf` задаёт точный файл.
Исторический checkpoint требовал завершения CP owner D5 и новых HTTP/PWA
consumers. В текущий состав включены owner publication/delivery/readback,
EMAIL callback и согласованные egress pins; сквозная приёмка изменения mailbox
через UI ещё не выполнена. D2/D4/D6 также включены, их live acceptance остаётся
отдельной обязательной проверкой. Повторный разрешённый `up` реализует локальную
доставку и сам по себе не доказывает работу owner publisher после UI transition.
Локальный render не заменяет
общий gate на точном интегрированном SHA.
До разрешения владельца `up`, import в k3s, apply/deploy, SSH, live provider
и live E2E остаются **NOT RUN**.

## Локальная подготовка STT

`dev.sh up` собирает `tools/dev/Dockerfile.local-stt` через
`tools/dev/build-local-stt.sh` и передаёт в renderer точный
`--stt-hot-reload-image repository@sha256:digest`. Образ содержит закреплённые
Go 1.26.6 и FFmpeg 8.0.1; изменение Go-кода не требует повторной сборки образа.
OCI archive кэшируется по содержимому Dockerfile и повторно импортируется при
`up` и `e2e`, в том числе после очистки node image cache. Это локальный образ,
не артефакт production promotion.

Renderer сохраняет STT Deployment, issuer, verifier, socket init и оба
обязательных key-delivery назначения. Модули заранее загружаются доверенным
host-процессом; исходники, Go modules, sumdb и Air монтируются read-only.
У каждого контейнера отдельный writable build cache. Основной STT-контейнер
остаётся non-root с read-only root filesystem и отдельным ограниченным `/tmp`;
spool, TLS, authority и точный egress через порт 8081 сохраняются из base.
Лимит памяти development-контейнера 2 GiB учитывает компиляцию Go; production
лимиты не меняются.

`tools/dev/verify-local-stt-render.sh` проверяет итоговый render до apply.
Потеря сервиса, sidecar или key-delivery, лишний handler пробы, writable общий
кэш, другой образ и подмена STT egress отклоняются. Существующий deploy entrypoint
ожидает readiness всех Deployment из render, поэтому STT не исключается из
ожидания отдельным списком.

Локальная точка проверки:

```bash
timeout 240s bash scripts/tests/local-role-image-render-contract-test.sh \
  --cache-root "$BASELINE_CACHE"
make test-local-go-cache-contract test-local-image-cache-import-contract \
  test-local-kubernetes-api-egress-contract
```

Render-проверка использует синтетические image digests и не обращается к
Kubernetes API; она не доказывает успешный deploy. Импорт в k3s, запуск Pod,
protected availability и реальная OpenAI-транскрипция остаются отдельными
пунктами сквозной приёмки. Для OCI export проверена документация Docker через
Context7: [OCI exporter](https://docs.docker.com/build/exporters/oci-docker/).

## Fixtures и наблюдение

- [ ] Две организации, минимум три проекта: доступный полностью, доступный с
  ограничениями и недоступный. Участники: owner/admin, оператор, читатель,
  пользователь без STT, пользователь без управления проектом/делегирования.
- [ ] Для каждого пагинируемого каталога подготовлено больше двух страниц
  данных и длинные русские/английские названия; отдельно есть пустой каталог.
  В Kanban больше шести карточек в каждой проверяемой колонке.
- [ ] Есть опубликованные и черновые ревизии, текущие и устаревшие bindings,
  активный attempt, отозванное право, readiness failure и OCC-конфликт.
- [ ] Browser снимает `pageerror`, неожиданные console errors, `requestfailed`
  и HTTP failures. Ожидаемые 4xx помечаются точным сценарием/endpoint, а не
  общим исключением всех ошибок. URL query, cookies и payload не логируются.
- [ ] Скриншоты проверяемых экранов: `1280x900`, `1440x900`, `1920x1080`,
  `2560x1440`, `2900x1600`, `390x844` и `768x1024`. Проверяются геометрия,
  текст, scroll, dropdown/modal и focus, а не только открытие route.
- [ ] Screenshots и временные browser traces хранятся только в приватном
  evidence-каталоге. При вводе Secret/credential трассировка содержимого
  выключена; в общедоступный отчёт попадают только безопасные hashes и исходы.

## Матрица интерфейса

Флажок означает выполнение всех перечисленных вариантов строки на итоговом
SHA. Функциональные browser-проверки используют фактические сервисы стенда;
подмена HTTP успешным ответом не доказывает работоспособность endpoint.

| Проверка | Действие и ожидаемый результат |
| --- | --- |
| [ ] MVP-UI-01 | Проверить ru/en, literal и зарегистрированные динамические/server keys. `SYSTEM_BASE_ROLE_IMAGE`, `CONFIG_OVERLAY_INVALID_OR_PROTECTED`, `workboard.sourceHint` локализованы; отсутствующий ключ закрыто ломает статическую проверку. Сырой `i18n:` не виден. |
| [ ] MVP-UI-02 | Переключить loading/connected/reconnecting и длинную подпись статуса. Центры badge, решений и пользователя совпадают; высота header не меняется. |
| [ ] MVP-UI-03 | На всех контрольных viewport открыть PageFrame, двухколоночные детали, Template variables, RoleImage и FAB. Симметричные отступы, панели и текст в границах экрана; горизонтально не прокручивается вся страница. |
| [ ] MVP-UI-04 | На главной оставить один содержательный блок и несколько блоков. Нет пустой половины layout; решения/ошибки остаются раньше справочных данных. |
| [ ] MVP-UI-05 | В компактном списке видно не более шести строк; разворачивание открывает модалку с тем же порядком, серверным поиском и total. В обоих видах страницы 20–50, внутренняя догрузка без дублей и полного выгружания каталога. |
| [ ] MVP-UI-06 | Быстрые действия находятся сверху рядом с запуском. Проверить создание сотрудника/процесса и загрузку с выбранным проектом и без него; на узком экране доступно компактное меню. |
| [ ] MVP-UI-09 | Чат открывается большой модалкой: слева история с серверным поиском/пагинацией, создание/переименование/архивирование; по центру сообщения и composer. Проверить контекст разных сущностей, inspector, вложения/DnD, подтверждения, Escape и предупреждение о несохранённом вводе. Mobile полноэкранный, tablet history overlay. |
| [ ] MVP-UI-10 | Новый подзаголовок проекта, не более двух читаемых карточек в строке. Назначение, статус, агрегаты и свежесть получены с сервера; icon actions открывают нужные сущности/формы запуска без случайного клика всей карточки. |
| [ ] MVP-UI-12 | Селектор проекта расположен между логотипом и поиском; дублирующего control слева нет. Поиск занимает остаток, пользовательские действия сохраняют своё место. |
| [ ] MVP-UI-13 | Rich selector проекта показывает назначение/статус/агрегаты, серверный поиск, страницы 20–50 и infinite scroll. Проверить повтор cursor, duplicate rows, loading/error/empty, длинный текст, клавиатуру, clear, outside click, route change и focus return. |
| [ ] MVP-UI-14 | Во всех редактируемых textarea и Markdown/YAML/TOML/JSON/Dockerfile редакторах общий STT control; нет перекрытия текста/resize/send. В Secret, password, credential, private key, readonly и disabled он отсутствует даже у STT-enabled пользователя. |
| [ ] MVP-UI-15 | Editor, preview и materialized preview сохраняют общую высоту с каталогом переменных. Скролл внутренний, соседние секции не смещаются. |
| [ ] MVP-UI-19 | Rich provider selector: однострочный badge, стабильная ширина, server search/cursor, keyboard flow. Отдельно воспроизвести lifecycle, credential, health, model, capacity и permission причины; есть доступный переход исправления и возврат. Health подтверждает только свежую credentialed catalog reachability данной учётной записи, не provider-wide SLA: пустой verified catalog может иметь успешный health при отсутствии совместимой модели; stale/failed observer даёт UNKNOWN. Проверить current account/credential и candidate runtime profile pins, смену профиля одного provider, stale response/cursor. До выбора модели selectable не означает allowedToSubmit; исчерпанная capacity и UNKNOWN health не запрещают допустимый queued launch. Admin без контекста не выдаёт model/actor NOT_EVALUATED за готовность. |
| [ ] MVP-UI-20 | Tab/Shift+Tab выполняют indent/outdent в каждом code editor, включая выделение и undo. Доступная команда выхода фокуса работает; обычные textarea сохраняют стандартный Tab. |
| [ ] MVP-UI-22 | Один общий async selector поддерживает single/multi, search, loading/error/empty, pagination, clear, disabled reason, Escape, outside click и возврат фокуса без геометрических скачков. |
| [ ] MVP-UI-23 | Exact environment edit action виден при достаточном праве и возвращает к агенту. Без права кнопки нет, а прямой HTTP update отклоняется владельцем. |
| [ ] MVP-UI-24 | Runs имеет только Kanban. Не более шести карточек по высоте колонки, независимые cursors/scroll, строки названия/исполнителя не превращаются в один символ; timestamp/badge не растягивают карточку. Горизонтальный scroll только внутри board. |
| [ ] MVP-UI-25 | `trash-toolbar` есть только в корзине. Details появляется только при выборе файла; обычный режим без выбора занимает всю ширину, пустой столбец/зазор отсутствует. Selection и смена режима не ломают layout. |
| [ ] MVP-UI-26 | Empty files workspace занимает рабочую высоту с отступами со всех сторон. Upload и DnD работают во всей безопасной области; loading/empty/content не скачут. |
| [ ] MVP-UI-27 | Все восемь проектных разделов всегда доступны в верхнем меню. Режим всех проектов группирует только доступные сущности; файлы начинают с папок проектов. Выбор проекта меняет server filter и сбрасывает cursors/selection/realtime, чужие данные не выдаются. |
| [ ] MVP-UI-28 | У готового агента один итоговый badge, центрального «Сейчас / Готов» и зарезервированной пустоты нет. Реальная активная работа показывает компактную ссылку на запуск. |
| [ ] MVP-UI-32 | Карточки процессов не шире двух колонок: этапы, уникальные агенты, parallel groups, Human Gate, активные запуски/решения и свежесть. Нет N+1 чтения этапов/запусков; actions соблюдают permission. |
| [ ] MVP-UI-33 | Кнопка запуска справа в заголовке, центр совпадает с badge. Панели во всю высоту нет. Открывается полная форма, а не немедленный запуск; disabled reason различает неопубликованность, права и readiness. Действие сохраняет геометрию при dirty/busy/denied; owner aggregate совпадает с actual launch admission. Проверить Workflow-only launch permission без прямого agent.launch или files capability; transient provider/capacity состояние отображается отдельно от допустимости постановки в очередь. |
| [ ] MVP-UI-38 | Вкладка «Ожидают» и счётчик помещаются в одну строку, доступное полное имя сохранено. |
| [ ] MVP-UI-39 | Селектор подключений стилизован и ищет на сервере; scope/credential type/readiness/reason читаемы. Смена проекта очищает несовместимый selection и страницы. |
| [ ] MVP-UI-40 | Project/recipient/capability используют зависимые rich selectors с серверным поиском; короткий recipient-kind enum без поиска. Проверить очистку stale selection и повторную backend-проверку grant. |
| [ ] MVP-UI-44 | Каталог окружений не выбирает первую строку автоматически и не вызывает detail endpoints заранее. Click выбирает; повторный click/Escape/outside снимают выбор; double click и клавиатурное действие открывают редактор. Action click изолирован; незавершённые запросы отменяются. |
| [ ] MVP-UI-45 | Tabs не дублируются command bar. Добавить переменную/secret можно только в соответствующей вкладке; глобальные draft/validate/publish остаются на стабильном месте с точными disabled reasons. |
| [ ] MVP-UI-49 | STRING и Base64 вводятся в textarea, JSON в общем code editor. Проверить padding/whitespace/размер Base64, JSON diagnostics/format/Tab, подтверждение очистки при смене типа. Plaintext исчезает после успеха/close/unmount и не сохраняется в storage/telemetry. |

## API, runtime и права

Для каждой мутации проверяются exact actor/project/resource, OCC, повтор того
же idempotency key после потерянного ответа и безопасная ошибка. Прямой запрос
в обход disabled UI не должен расширять права. Query/list/search используют
одинаковую eligibility с detail read.

| Проверка | Действие и ожидаемый результат |
| --- | --- |
| [ ] MVP-UI-07 | В новой и авторизованной сессии manifest/icons/service worker возвращаются с корректным типом и origin без Keycloak redirect/CORS. Публичное исключение не открывает API/данные. Сверить render exact paths. |
| [ ] MVP-UI-08 | Workboard без warning; readiness/agents для живого окружения работают сквозь generated route/RPC/owner. Stale/hidden ref не запускает бесконечный polling; route skew не маскируется пустым результатом. |
| [ ] MVP-UI-11 | Прожить серверный expiry в нескольких вкладках: single-flight refresh с jitter, новый одноразовый WS ticket, сохранённый cursor без дублей. 401/403 завершает bounded retry одним re-auth. Нет tokens в JS storage/WS URL; provider expiry отличим от SSO. |
| [ ] MVP-UI-16 | `POST /api/v1/prompt-templates/preview`: plain text, все scopes, file/tool ranges, неизвестная/недоступная переменная, неверный тип, смена revision. Валидный preview без 500; пользовательская ошибка даёт line/column 4xx; secrets не материализуются. |
| [ ] MVP-UI-17 | Account-specific versioned model catalog: Astra, Sol/Terra/Luna, 5.5/5.4/mini и Spark только при реальной доступности. Spark не предлагается обычному API key; исчезнувшая выбранная модель не заменяется молча. |
| [ ] MVP-UI-18 | Effort/default получены из model capabilities. Поддерживаемая пара сохраняется в immutable runtime revision; несовместимая отклоняется перед turn. Проверить смену account/model и недоступный прежний effort. |
| [ ] MVP-UI-21 | Overlay allowlist: `model_reasoning_effort`, `personality`, `allow_login_shell=false`, `history.persistence`. Проверить диагностику, completion/hover и versioned schema. Provider/model/credentials/policy/MCP/environment overrides отклонены; draft/validate/publish/rollback сохраняют canonical digest. |
| [ ] MVP-UI-29 | Avatar upload: success, замена, неверный тип/размер, scan rejection, отказ после upload до binding, duplicate retry. До успешной атомарной привязки объект не виден ни в active, ни в trash; прежний avatar остаётся до успеха, cleanup bounded. |
| [ ] MVP-UI-30 | Variable catalog даёт authoritative available/disabled reason. Без files capability недоступны коллекция, count, dir и manifest. Смена Agent/role/runtime инвалидирует старую страницу и preview; validate/publish также отклоняют недоступную переменную. |
| [ ] MVP-UI-31 | Actor без project.manage не выдаёт это право агенту/процессу/интеграции. Проверить requested/effective/unavailable как пересечение actor, delegation ceiling, organization/project, agent grants, step requirements, runtime readiness и exact integration scope; свежая revision перед turn/retry/continuation. |
| [ ] MVP-UI-34 | Назначение и результат этапа используют общий template editor. Preview координатора/исполнителя показывает insertion points, порядок и provenance Agent/workflow/step/input/files/tools/integrations. Preview и реальный run имеют один renderer и exact revisions. |
| [ ] MVP-UI-35 | Шаблон со всеми semantic slots не получает дублей; пропущенные slots добавляются детерминированными versioned service blocks. Вложенный пользовательский текст не подменяет границы блока; digests шаблона/snapshot/итога совпадают с runtime. |
| [ ] MVP-UI-36 | Изменить инструкции/model/effort/image/environment/files/skills/memory/tools/MCP/grants/policy и продолжить ту же session. Preview и одно user notice описывают безопасный typed diff previous/current revision; retry не создаёт второй turn/message, секретов/locator нет. |
| [ ] MVP-UI-37 | Типизированный VFS: проекты, применимые сущности, avatar/inputs/skills/memory/run results; breadcrumbs/search/pagination/bulk selection с exact refs/revisions. SkillBundle с SKILL.md проходит structure/scan/provenance/policy, память отдельная versioned сущность. MCP search/metadata/bounded preview/manifest ограничены pinned grant, не принимают filesystem authority и не выдают quarantined/deleted/foreign данные. |
| [ ] MVP-UI-43 | Preset и custom cron имеют один ScheduleRevision. Preview пяти occurrence совпадает с scheduler для timezone/DST/minimum interval; неверный cron диагностируется. Task идёт агенту или root coordinator, scopes automation доступны по правилам. CONTINUE_ONE добавляет task+notice один раз. Materialized preview использует тот же renderer для AGENT/WORKFLOW и first/bound Session. DRAFT описывает будущую ревизию от текущего пользователя; automation.revision недоступна до сохранения даже при прежних значениях полей. CURRENT_REVISION связан с exact saved spec/revision и автором исполнения, при отдельных текущих правах viewer. Проверить другого manager, отозванные права автора, stale spec/version, отсутствие preview effects и неусиление capabilities viewer-полномочиями. |
| [ ] MVP-UI-46 | Несохранённый ввод, server draft, validated revision и published различимы. Save не меняет active binding; publish только exact validated revision с fresh permission. Проверить уход со страницы/re-auth. Secret staged encrypted revision без plaintext readback и с bounded cleanup. |
| [ ] MVP-UI-47 | До publish открыть impact modal всех доступных потребителей с поиском/пагинацией, допустимые отмечены. Проверить отмену, без замены, выборочную замену, истёкший plan, OCC/permission conflict и чужой binding. Есть поэлементные receipts; running attempt остаётся pinned, следующий turn получает новую revision. |
| [ ] MVP-UI-48 | `GET /api/v1/runtime-environments/{environmentRef}/readiness` и `/agents`: ready/not-ready, пустая/следующая страница, скрытый/удалённый ref. 404 только для отсутствующего/скрытого ресурса; ошибка inspector не роняет каталог. Проверить фактический deploy route. |
| [ ] MVP-UI-50 | Secret create/rotate/reload и потерянный ответ дают одну committed revision. Malformed page (`items` null/undefined), projection lag и readback error дают локальный problem/retry, не стирают строки и не вызывают length/emitsOptions/subTree исключений. |
| [ ] MVP-UI-51 | Disable/revoke/delete различимы. Active turn, warm consumer, agent/pool/automation binding видны как blockers; queued cleanup не удаляет credential до освобождения. После cleanup tombstone убран из обычного каталога, audit/pinned history сохранены, stale page не оживляет account. |
| [ ] MVP-UI-52 | Device auth: первоначальный code, проверка реального статуса, re-auth той же account, expiry/cancel/retryable503/потерянный ответ/active-turn blocker. Bounded polling и exact retry не создают новый challenge/account; новая credential revision атомарно active без замены pinned turn. |
| [ ] MVP-UI-53 | На 499 мс запрос ещё не отправлен, на 500 мс отправлен без Enter. Проверить reset, Enter flush, trim/minimum length, clear, unmount, abort и stale response. HTTP kind только PROJECT/AGENT/WORKFLOW/RUN, ru/en локализованы. |
| [ ] MVP-UI-54 | Найти и открыть каждый из четырёх типов: exact URL и detail endpoint совпадают с kind/ref/projectRef. AGENT не вызывает `/workflows/agt_*`. Proto enum, неизвестный kind и malformed projectRef закрыто блокируют переход. |
| [ ] MVP-UI-61 | Реальный agent-runner создаёт вложенный файл, читает, атомарно заменяет, удаляет и публикует другой результат с attempt provenance. Отдельно read-only inputs/skills/memory/credentials, quota, traversal/symlink и foreign session/project дают READ_ONLY/QUOTA_EXCEEDED/PATH_OUTSIDE_WORKSPACE/RUNTIME_IO_ERROR. Readiness canary убран, пользовательские файлы не тронуты. |

## Интеграции

- [ ] MVP-UI-41: в UI и YAML задать SMTP/IMAP и отдельно ограниченный POP3.
  Проверить exact host/port/SNI, TLS/STARTTLS, auth, from/reply-to, mailbox,
  timeout и Secret refs; неизвестные/protected поля отклонены. Readiness каждого
  протокола использует рабочий credential/network path. POP3 не симулирует
  search/folders/threads/flags. Выполнить mailbox list, message list/search/read,
  thread read, attachment list/read, mark read/unread, move/archive/delete,
  draft create/update/delete, send/reply/reply-all/forward. Три connection
  policies: read без gate/write с gate, всё с gate, допустимое без добавочного
  gate; minimum package/platform policy не ослабляется. Проверить unknown
  write outcome, lost reply, receipt, audit, revoke и запрещённый mailbox.
- [ ] MVP-UI-42: «Подробнее» открывает modal без роста карточки,
  zero-connection-notice отсутствует. Все операции ниже реально вызываются
  через grant -> MCP -> owner claim -> adapter -> provider -> receipt,
  а не только присутствуют в YAML. Для каждой есть positive fake-provider
  scenario, отрицательный exact scope, bounded pagination/output и failure path.

| Пакет | Обязательная проверяемая поверхность |
| --- | --- |
| Confluence | Spaces, page search/read/descendants/comments/attachments, create и OCC update в exact space. |
| GitHub | Repository metadata/content, branches/commits, issues/comments, pull requests/reviews/checks, разрешённые Actions в exact repository. |
| GitLab | Project/repository, branches/commits, issues/notes, merge requests, pipelines/jobs, разрешённые retry/cancel в exact project. |
| Jira | Projects, scoped users, JQL, issue read/create/update/transitions/comments/links/attachments. |
| Mattermost | Teams/channels, posts/threads/search/files/reactions/send/update; exact team/channel и server-owned identity. Проверить inbound, durable ACK, notification/result mirror, gate approve/reject/replay, revoke, restart, UNKNOWN_OUTCOME. |
| Synthetic HTTP | Только зарегистрированные method/path/schema/limits/secret bindings; arbitrary URL и redirect в иной origin запрещены. |
| Email | Полный перечень и protocol-specific ограничения MVP-UI-41. |

Подключение новой версии definition не расширяет старые grants и не меняет
минимальную Human Gate policy. Read-only вызов также проверяет actor, resource
и active grant. Fake-provider PASS не выдаётся за готовность живого адаптера;
для каждого реального optional provider в отчёте отдельный PASS/FAIL/NOT RUN.

## STT

- [ ] MVP-UI-55: самостоятельный stt-tts-service присутствует в фактическом
  render и registry; первый adapter только OpenAI transcription. TTS-заглушки
  нет. Audio/transcript не сохраняются, egress проходит exact gateway/TLS.
- [ ] MVP-UI-56: администратор создаёт/меняет versioned configuration через UI,
  проверяет OCC/retry/readback, credential binding и доступность. Проверить
  model-specific language/languages, keywords, prompt, temperature, chunking,
  stream policy и совместимость. Default берётся из versioned каталога;
  каталог adapter с `version/observedAt` доступен по праву управления системой
  до первой enabled configuration и credential. Это чтение не выдаёт
  microphone eligibility или provider READY.
  Неподдерживаемые параметры не уходят провайдеру, browser не выбирает key.
- [ ] MVP-UI-57: org-scoped permission с выбранным проектом и без него;
  session/CSRF/Origin -> bounded multipart -> version-bound grant -> STT ->
  provider. Проверить все девять поддерживаемых контейнеров, 10 MiB/120 s
  limits, unsupported/truncated audio, отсутствие permission, revoked key,
  недоступную модель, timeout/rate limit/cancel. После возможного billable
  POST нет автоматического retry.
- [ ] MVP-UI-58: реальные MediaRecorder capture/stop/cancel и вставка в
  textarea/CodeMirror в selection, одной undoable transaction с сохранением
  focus/scroll. Navigation/logout закрывают microphone tracks и запрос;
  browser deny/unsupported codec не вызывают console errors. Availability
  обновляется после отзыва права/config, sensitive/readonly поля исключены.
  Permissions-Policy допускает microphone только same-origin, не camera.
- [ ] MVP-UI-59: фактические mTLS/application identity, exact ingress/egress,
  credential/model probe без billable POST, свежая availability, startup/join,
  cancel cleanup, organization/subject quota, bounded metrics и alerts с
  HTTPS runbook. Логи/trace не содержат filename, audio, prompt/keywords,
  transcript или credentials, в том числе после ошибочного запроса.
- [ ] MVP-UI-60: реальный OpenAI smoke и сквозной HTTP/UI запрос распознают
  tracked MP3 fixture `services/internal/stt-tts-service/testdata/1-2-3-4-5.mp3`
  (46 364 bytes, SHA-256
  `56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e`).
  Нормализация допускает только регистр, пробелы и конечную пунктуацию;
  ожидается «раз два три четыре пять», не перестановка или пропуск слов.
  В отчёт записываются match/digest, не transcript. Тестовый credential
  задаётся отдельно; без него NOT RUN блокирует приёмку STT.

Direct adapter smoke подтверждает только adapter. Он не заменяет сквозную
проверку права/configuration/credential projection/gateway/UI на стенде.
Тестовый ключ не подставляется в браузер или из учётной записи агента.

### Защищённый HTTP smoke

Repo-owned `tools/dev/stt-http-acceptance.mjs` запускается после разрешённого
deploy и настройки STT через предусмотренный owner lifecycle. Он использует
точный tracked MP3, опубликованную конфигурацию и уже созданный приватный
authenticated browser snapshot. Bootstrap snapshot, из которого удалены
Kodex API cookies, здесь не подходит. Сценарий не создаёт конфигурацию,
не читает provider key и не меняет разрешения пользователя.

Карта: MVP-UI-57/60 → owner web session → fresh bootstrap eligibility и
`GET /api/v1/system-stt-configuration` → один multipart
`POST /api/v1/speech/transcriptions` → HTTP gateway → protected streaming
STT RPC → owner projection/configuration/credential → OpenAI → безопасный
receipt → проверка configuration readback. Дополнительный проектный маршрут
проверяется отдельным запуском с `--project-ref`; org authority остаётся
серверной. `ready` административной конфигурации не подменяет fresh
пользовательскую `speechTranscription`.

Используются существующие env: `KODEX_E2E_BASE_URL`,
`KODEX_E2E_STORAGE_STATE`, `KODEX_E2E_STATE_DIRECTORY`,
`KODEX_E2E_RESOURCE_PREFIX`, точное значение
`KODEX_E2E_CONFIRM_DISPOSABLE` и явный provider opt-in
`KODEX_PROVIDER_E2E_API_KEY=1`. Это включатель разрешённого платного теста,
не значение ключа. State directory должен быть owner-private0700,
authenticated storage file —0600 непосредственно в нём. Значения cookie/key
не передаются аргументами и не выводятся. Проверяется чистый checkout exact
SHA, HTTPS origin без redirect и только два exact host API cookie.

```bash
node tools/dev/stt-http-acceptance.mjs --expected-sha "$EXPECTED_SHA"
# Отдельный уникальный KODEX_E2E_RESOURCE_PREFIX для проектного варианта.
node tools/dev/stt-http-acceptance.mjs --expected-sha "$EXPECTED_SHA" \
  --project-ref "$PROJECT_REF"
```

До платного POST создаётся и синхронизируется reservation/evidence file
`<prefix>-stt-http.json`; существующий файл закрыто останавливает повтор
после crash или неизвестного результата. POST не повторяется при429/503,
timeout, redirect, потере ответа либо ошибочном receipt. Общий бюджет120s,
transcription response ограничен64KiB до полной буферизации. Source SHA,
fixture digest, match, safe receipt IDs и configuration revision/digest
сохраняются без audio/transcript/model prompt/cookie/provider credential.
До и после запроса сверяются конфигурация и credential generation;
receipt должен содержать exact revision/model и PROVIDER_COMPLETED.
Несовпадение не объявляется успешным после автоматического нового запроса.

Локальный `make test-stt-http-acceptance` проверяет org/project paths,
fresh eligibility, forged/stale receipts, неизвестные результаты без retry,
ограничение/отмену stream и private file boundaries. Нормализация допускает
только регистр, whitespace и конечную пунктуацию; символы внутри слов
не удаляются. Эти fixtures не являются настоящим OpenAI либо browser PASS.
Реальные MediaRecorder/вставка/отмена и отрицательные пользовательские
сценарии матрицы остаются отдельной обязательной проверкой после deploy.

При подготовке оснастки через Context7 проверен
[Node.js24 filesystem API](https://nodejs.org/docs/latest-v24.x/api/fs.html),
через OpenAI Docs — официальный
[file transcription guide](https://developers.openai.com/api/docs/guides/speech-to-text).
Модель и параметры выбирает опубликованная owner configuration.

На exact code `e49f963282baf005e11b389abc8fc02c55780c1d` локально
**PASS**: `make test-stt-http-acceptance`9/9,
`make test-runtime-workspace-acceptance`38/38,
`make test-full-local-e2e-entrypoint`, Node syntax, Prettier и diff check.
Общий bounded response reader теперь отменяет stream и при отклонении
завышенного/неверного Content-Length до чтения body. Log
`stt-http-exact.log` хранится в приватном evidence-каталоге.
Прямой OpenAI, protected deployed HTTP и реальный browser — **NOT RUN**.

Исправление #1074 включено exact STT unit SHA
`a54db5b325f1f39142119cd92c3479df2049d9ed`: targeted normalizer race1.018s,
full STT race/vet/build, service contract и security-negative — **PASS**.
Тестовый provider credential исключён из env; live и новый Docker build
не запускались. На integrated code
`bc6d9e99cb478ea7848ca263ce27a0128da0ad31` HTTP matcher использует те же
Unicode White_Space/Punctuation границы; HTTP9/9 — **PASS**, включая
чередование конечной пунктуации и пробелов. Внутренние символы, ведущая
пунктуация и форматирующий BOM не удаляются. Этот код не подменяет настоящий
provider match локальным преобразованием ошибочно распознанных слов.

## Образы и пакеты

Этот принятый блок не имеет отдельного номера MVP-UI и не теряется при
проверке диапазона 01–61.

- [ ] CFG-01: «Образы ИИ-сотрудников» в навигации, active catalog/new/detail
  routes; полный Dockerfile editor с diagnostics/diff/history. Без Git
  создать UI recipe, validate/publish, выполнить изолированный build/scan/
  SBOM/provenance/promotion и назначить окружению exact admitted image digest.
  Source требует отдельного права, secrets не проходят ARG/ENV/context/log.
- [ ] CFG-02: создать UI IntegrationDefinition через синхронные форму/YAML,
  прочитать полный source по праву, edit/validate/publish/archive/history/copy.
  Проверить schema/semantic registry, risk/Gate/network/credential slots,
  immutable revisions, OCC/retry, old connection pins и compatibility preview.
- [ ] CFG-03: SHIPPED загружается в пустую БД без Git и не редактируется на
  месте; UI copy разрешена. GIT импортируется с exact repo/ref/path/commit,
  loss/revoke credential даёт SYNC_BLOCKED без смены owner и прежних pins.
  Проверить explicit detach/copy, отдельный write-back plan/PR permission,
  sync после merge, audit и rollback как новую revision, не движение назад.

## Порядок выполнения

1. Завершить реализацию и targeted проверки всех unit, интегрировать exact
   commits, выполнить полный baseline и общий тройной review одного SHA.
   После отдельного owner human gate слить в проверенном порядке и сравнить
   итоговое дерево `main` с проверенным деревом. Зафиксировать допуск и fixtures
   выше. Подготовить приватный evidence каталог
   и записи ожидаемых 4xx. До разрешённого deploy не запускать команды среды.
2. Из чистого финального SHA выполнить read-only preflight и существующий
   code-first rollout по [runbook hot reload](../runbooks/remote-hot-reload.md).
   Получить deployment/render/readiness readback всех сервисов, включая новые.
3. Выполнить существующий полный контур как базу, а не замену матрицы:

   ```bash
   ./tools/dev/remote-dev.sh acceptance --env-file "$REMOTE_ENV" \
     --resource-prefix "$ACCEPTANCE_PREFIX" --run-timeout-ms 1800000 \
     --expected-sha "$EXPECTED_SHA"
   ```

4. Выполнить все строки матрицы через browser/API/runtime сценарии с
   синтетическими данными. Проверки реальных optional providers имеют
   отдельный разрешённый profile и отчёт, STT live PASS обязателен.
5. На каждый дефект создать отдельный bug Issue: severity, среда/SHA,
   воспроизведение, expected/actual, безопасное evidence и ссылка на строку.
   Исправлять в unit-владельце; временная hot-reload правка не считается
   принятой, пока не committed/merged и не перепроверена на новом exact SHA.
6. После исправлений повторить затронутые сценарии и полный итоговый baseline,
   получить согласованные результаты общей проверки нового SHA. Проверить
   чистоту checkout, обслуживаемые revisions и отсутствие новых ошибок.

## Отчёт владельцу

Одна запись evidence содержит: requirement ID, scenario/вариант, exact source
SHA, дату UTC, среду и допустимый profile, команду или последовательность UI,
expected/actual, PASS/FAIL/NOT RUN, безопасный artifact name/digest и bug Issue
при дефекте. Для multi-item операций прикладывается поэлементный итог.

Результаты другого SHA, skipped test, пустой test selection, отсутствие
GitHub checks, успешная компиляция и fake-provider fixture не превращаются в
PASS пользовательского сценария. Итоговая приёмка возможна только при
доказательствах для каждой строки и каждого обязательного варианта; общая
зелёная сводка без этой матрицы недостаточна.
