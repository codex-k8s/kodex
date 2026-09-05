---
id: QA-MVP-1031
title: Сквозная приёмка доработок MVP
type: verification-plan
status: approved
owner: qa
version: 1.5.0
updated: 2026-09-05
---

# Приёмка #1031

Источник: [эпик #1018](https://github.com/codex-k8s/kodex/issues/1018),
[приёмка #1031](https://github.com/codex-k8s/kodex/issues/1031), принятые
замечания владельца `MVP-UI-01`–`MVP-UI-61` и принятое UI-управление образами
и пакетами интеграций. Локальный каталог `.agents/mvp-finish` не является
CI-входом и не добавляется в Git.

Это план, а не отчёт о выполнении. Все флажки изначально пустые. `PASS` старого
`full-local-e2e` или discovery не доказывает выполнение всей этой матрицы.
Ни одна строка не исключается потому, что её ещё не покрывает автоматизация.

## Условия допуска

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

```bash
make test-full-local-e2e-entrypoint test-web-only-release
timeout 240s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-only --cache-root /tmp/kodex-1031-render-cache
timeout 240s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-with-mattermost --cache-root /tmp/kodex-1031-render-cache
```

Через Context7 проверены официальные правила
[Kustomize](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/)
и [declarative configuration](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/).

## Текущий состав интеграции

На `7f2f667b278aad5f11c2f03413d37f3dd92598dd` включены следующие
проверяемые зависимости. Это промежуточная интеграция полного scope,
а не завершённый `main` или допуск стенда.

| Unit | Включённый exact SHA | Граница checkpoint |
| --- | --- | --- |
| Control-plane #1046 | `b9402939a3ccdcef384d44cc2c04dfa5554f73b5` + `12f9eb26a4b4a3f329f49226c3a8cfc888c533c8` | Сохранены D1–D7, SourceWork, catalog/Session affinity v7, Home totals, Env638, Mattermost637; immutable overlay history/publish, RoleImage UI→build и managed lineage; effective capabilities и свежая authority до receipt, policy64. Selective rebind/writeback и prompt continuation ещё завершаются. |
| Secret Broker #1068, PR #1069 | `af227acc60d9ca1bd0207429c6d8088fb9496af7` | Encrypted staged lifecycle и protected account model observer, fresh remote provenance, bounded actual Codex process и отказ refresh под read authority. |
| EMAIL #1037, PR #1062 | `f977638e10509d83ce1f27c8b386fa9bd7472842` | Managed exact pins и protected callback до публикации нового serving snapshot. |
| Egress #1029, PR #1065 | `5d8b177054dcf094830e32dceb7f9290d633920b` | Общие mail policy/DNS, admission и отзыв сетевых pins выключенного mailbox без потери общих активных endpoints. |
| HTTP/SDK #1045, PR #1066 | `0fd625f0af80e4dbfb9d7a844b8c891acabec614` | D2 pins/status/TOML, Home/globalArtifacts total, Env metadata, overlay history и typed effective capabilities. RoleImage query/lineage и новые Session/prompt/writeback consumers ещё завершаются. |
| PWA #1022, PR #1067 | `9bb1197ff712f158bd674f9c1760d4bf79b1a458` | D2/TOML, Git source lifecycle, immutable overlay history/rollback, Home/Env metadata, исправлен async picker на390. Capability и новая Session eligibility ещё потребляются; общий build восстановлен. |
| STT #1020, PR #1070 | `6c23e653263643912e3e984802448b561a652615` | Administrative adapter catalog до configuration/credentials, policy 60, согласованные limits и актуальная STT model profile revision. |
| Runtime-controller #1025, PR #1063 | `21425ddbbc2196849c5378f8227bd18e13f2f8fb` | Exact v7 mode/effort/context, Pod/callback, schema/admission/render contract revision 2. |
| Agent-runner #1026, PR #1058 | `257c2f2c98e001d2422a3c8df20a95389a9b3850` | Server reasoning mode/effort во всех turns, UNSUPPORTED без параметра, сохранён bounded workspace/context lifecycle. |
| Integration-gateway #1028, PR #1064 | `31164f57b622bd0322a7b64d313173e9e3582b82` | Actual SourceWork owner/consumer/profile63, GitHub/GitLab exact commit/regular blob reader, scoped PostgreSQL и оба render. Write-back ещё открыт. |
| Interaction-gateway #1030, PR #1061 | `c7d03d818ac4e6c5a226d49c6d14d7c6dc798b9e` | Actual package системных subscriptions/deliveries; OwnerGate до notification/mirror, exact input и approval claim, fail-closed discovery. |

Сохранены включённые ранее authority `0765f3dad`
и исправление #1056. Исходные Proto/OpenAPI/policy объединены по семантике,
generated Go/SDK/PWA validator получены повторной генерацией.

На code SHA `7f2f667b278aad5f11c2f03413d37f3dd92598dd` локально **PASS**:
Proto lint/build/clean replay, policy64 codegen, authority ABI render, оба EMAIL
projection render, web-only release и полный PWA build/typecheck. Go/TS SDK и
Proto повторно получены из объединённых источников, clean readback подтверждён.
Безопасные логи: `integration-7f2-contracts.log`,
`integration-7f2-pwa-build.log` в приватном evidence-каталоге. Предупреждение
о JS chunk более500kB сохраняется. Полный baseline этим запуском не выполнен.

Исторический code `303f75b535cde2fd451d4d4ea22841becbaa488b` имел **FAIL**
PWA build: fixtures/consumer не удовлетворяли D2 input pins, output default и
overlaySchema. Новый запуск7f2 после PWA9bb подтверждает устранение этого FAIL;
старые логи `303-integration-contract-render.log`, `integration-ff7-openapi.log`
и `integration-catalog-source-pwa-build.log` сохранены.

При merge сохранены managed EMAIL callback, exact configuration/policy pins и
проверки CONNECT до provider bytes; старая prerequisite history их не откатила.
SourceWork owner и account catalog/Session affinity теперь включены; локальный
combined proof не выдаётся за deployed provider acceptance. Git write-back,
полный RoleImage UI lifecycle и новые HTTP/PWA consumers остаются обязательными.

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
  --profile web-only --cache-root /tmp/kodex-1031-email-render-cache
timeout 300s bash scripts/tests/local-role-image-render-contract-test.sh \
  --profile web-with-mattermost --cache-root /tmp/kodex-1031-email-render-cache
timeout 450s bash scripts/tests/local-email-process-contract-test.sh \
  /tmp/kodex-1031-email-render-cache
```

Оба renderer запускают EMAIL positive и 30 negative cases. Process fixture
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
Остаются отдельные зависимости полного прототипа: завершение CP owner D5,
новые HTTP/PWA consumers и автоматическая доставка mail policy после
UI transition D5. D2/D4/D6 уже включены в промежуточную интеграцию;
их сквозная приёмка ещё не выполнена. Повторный
разрешённый `up` реализует локальную доставку, но не является фоновым reconciler.
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
  --cache-root /tmp/kodex-1031-render-cache
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
| [ ] MVP-UI-19 | Rich provider selector: однострочный badge, стабильная ширина, server search/cursor, keyboard flow. Отдельно воспроизвести lifecycle, credential, health, model, capacity и permission причины; есть доступный переход исправления и возврат. |
| [ ] MVP-UI-20 | Tab/Shift+Tab выполняют indent/outdent в каждом code editor, включая выделение и undo. Доступная команда выхода фокуса работает; обычные textarea сохраняют стандартный Tab. |
| [ ] MVP-UI-22 | Один общий async selector поддерживает single/multi, search, loading/error/empty, pagination, clear, disabled reason, Escape, outside click и возврат фокуса без геометрических скачков. |
| [ ] MVP-UI-23 | Exact environment edit action виден при достаточном праве и возвращает к агенту. Без права кнопки нет, а прямой HTTP update отклоняется владельцем. |
| [ ] MVP-UI-24 | Runs имеет только Kanban. Не более шести карточек по высоте колонки, независимые cursors/scroll, строки названия/исполнителя не превращаются в один символ; timestamp/badge не растягивают карточку. Горизонтальный scroll только внутри board. |
| [ ] MVP-UI-25 | `trash-toolbar` есть только в корзине. Details появляется только при выборе файла; обычный режим без выбора занимает всю ширину, пустой столбец/зазор отсутствует. Selection и смена режима не ломают layout. |
| [ ] MVP-UI-26 | Empty files workspace занимает рабочую высоту с отступами со всех сторон. Upload и DnD работают во всей безопасной области; loading/empty/content не скачут. |
| [ ] MVP-UI-27 | Все восемь проектных разделов всегда доступны в верхнем меню. Режим всех проектов группирует только доступные сущности; файлы начинают с папок проектов. Выбор проекта меняет server filter и сбрасывает cursors/selection/realtime, чужие данные не выдаются. |
| [ ] MVP-UI-28 | У готового агента один итоговый badge, центрального «Сейчас / Готов» и зарезервированной пустоты нет. Реальная активная работа показывает компактную ссылку на запуск. |
| [ ] MVP-UI-32 | Карточки процессов не шире двух колонок: этапы, уникальные агенты, parallel groups, Human Gate, активные запуски/решения и свежесть. Нет N+1 чтения этапов/запусков; actions соблюдают permission. |
| [ ] MVP-UI-33 | Кнопка запуска справа в заголовке, центр совпадает с badge. Панели во всю высоту нет. Открывается полная форма, а не немедленный запуск; disabled reason различает неопубликованность, права и readiness. |
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
| [ ] MVP-UI-43 | Preset и custom cron имеют один ScheduleRevision. Preview пяти occurrence совпадает с scheduler для timezone/DST/minimum interval; неверный cron диагностируется. Task идёт агенту или root coordinator, scopes automation доступны по правилам. CONTINUE_ONE добавляет task+notice один раз. |
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
