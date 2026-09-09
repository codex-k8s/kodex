# Независимое обновление приложений

Задача: #1222, общий план: #1204.

`scoped-release.mjs` обновляет digest образа или подготовленный source checkout
основного контейнера выбранного Deployment и собственную аннотацию релиза. Общие Secrets, trust, grants,
ConfigMaps, миграции, sidecar images, replicas и strategy не меняются.
Один target и группа используют одинаковый механизм.

## Выбор source и image

По [решению владельца #1343](https://github.com/codex-k8s/kodex/issues/1343)
hot reload — основной dev профиль на весь период разработки и последующих
ручных циклов, включая замечания после первой приёмки MVP. Для монтируемого
Go/Vue кода по умолчанию используется scoped source workflow ниже. Не требуется
пересобирать неизменный toolchain image или переводить все dev units на immutable
images ради нового Git SHA. Смена dev профиля — отдельное решение владельца;
[общие границы и сохранность данных](../../docs/runbooks/remote-hot-reload.md#профиль-разработки)
зафиксированы в runbook.

| Что изменилось | Необходимая подготовка и поставка |
| --- | --- |
| Только монтируемый application code | Проверенный новый source checkout, scoped plan/apply, actual source/process readback |
| Go/npm manifests или locks | Штатная подготовка exact dependencies/cache и проверка runtime-доступности; сама по себе не требует Docker build toolchain image |
| Dockerfile/base, OS packages, toolchain, native dependencies или встроенные assets | Сборка и доставка изменившегося образа с exact digest; незатронутые образы сохраняются |
| Встроенный runner/runtime/RoleImage binary | Явная image build/admission/promotion/digest цепочка до нового Job/Pod; прежние active attempt pins и immutable inputs сохраняются |

Подготовка кэша не снимает guards быстрого source release. Если manifests/locks
отличаются, требуется соответствующий dependency/config workflow; нельзя
обходить отказ CLI или выдавать старый кэш за подготовленный. Source update
не заменяет доставку встроенного binary, DB migration и config/security переход.
Обычные переносимые установки сохраняют immutable application image профиль
без hostPath; это не обязательная смена способа разработки на текущем сервере.

## Предварительные условия

- Node.js с поддержкой ES modules и `kubectl`; точный Kubernetes context.
- Уже подготовленный staging: `kodex.dev/local-profile=hot-reload` либо
  `kodex.dev/environment=staging`, `app.kubernetes.io/part-of=kodex`.
- `RollingUpdate`, `maxUnavailable: 0`, положительный числовой `maxSurge` и
  доступное текущее число replicas. Для workers уже активирован grant v2.
- Для image target образ собран, доставлен в registry и допущен существующим admission policy.
  Скрипт не обходит admission и не публикует неподтвержденные образы.
- Приложение совместимо с действующими API, событиями и схемой БД.
  Этот инструмент не выводит совместимость из Git SHA.

## Использование

Приватный JSON manifest версии 1 содержит `targets`: массив объектов с `name`
и выбранным `source` либо `image`. Для image target значение имеет вид
`repository@sha256:<64 hex>`. Перечень приложений закрытый; authority
infrastructure через этот путь обновлять нельзя.

В hot-reload профиле вместо `image` или вместе с ним можно задать
`source: {"path":"/srv/kodex-dev/workspace-new","revision":"<40 hex commit>"}`.
Скрипт запускается на доверенном dev host и проверяет оба checkout: точный root,
origin, commit и чистое дерево без приватного env внутри. Новые Go dependencies
должны быть заранее подготовлены штатным host prime; иначе новая replica не
сможет воспроизводимо собрать приложение. Для выбранных Go modules применяется
`python3 tools/dev/prime-go-cache.py plan|prime --profile staging-hot-reload`
с exact clean source/revision, shared cache root, явными `--module` и отдельным
private evidence. Полная команда и границы общего lock находятся в
[runbook](../../docs/runbooks/remote-hot-reload.md#подготовка-go-cache-перед-ограниченной-выкладкой).
Это не image rebuild и не запуск глобального render/up. Если cache не подготовлен,
новая replica не станет Ready, а старая останется доступной.

Go-приложение получает отдельный read-only `dev-application-source` mount.
Существующие mounts issuer/verifier/init остаются на прежнем checkout. Frontend
меняет только собственный source directory, не runner, runtime image или общий
кэш зависимостей. Изменение package.json/package-lock.json требует отдельной
подготовки runtime/cache и этим быстрым source-путём закрыто отклоняется.

Перед первым source release PWA новая чистая рабочая копия проходит отдельную
подготовку вложенных mountpoints. Команда не устанавливает зависимости и не
меняет Kubernetes: создаёт только отсутствующие Git-ignored каталоги под
проверенными дескрипторами Linux dev host. Существующие файлы и каталоги не
перезаписываются; symlink, чужая revision и изменения tracked files запрещены.

Создавайте новый source штатной командой, а не копированием рабочей среды:

```bash
node tools/release/create-application-source.mjs \
  --repository /srv/kodex-dev/workspace-current \
  --source /srv/kodex-dev/workspace-new --revision <40-hex-commit> \
  --confirm CREATE-STAGING-SOURCE
```

Команда создаёт только отсутствующий Git worktree на точной revision, с маской
022 для новых исходников и mountpoints. Исходная маска приватных журналов
восстанавливается при успехе и ошибке. Ignored files, env и credentials из
исходной рабочей копии не копируются; существующие файлы не получают chmod.
Если подготовка не завершилась, каталог остаётся для диагностики, а не
удаляется автоматически. Повтор в тот же каталог закрыто отклоняется.

Для этого Linux source-профиля tracked regular files должны быть читаемы,
tracked executables исполняемы, а каталоги читаемы и проходимы независимо от
UID/GID контейнера: проверяются соответствующие POSIX other-биты. Нечитаемая
рабочая копия, symlink или нестандартный tracked entry отвергаются до PATCH с
безопасным кодом ошибки. Проверка не использует привилегированную способность
оператора прочитать файл как доказательство runtime-доступа. Иные ACL, LSM или
нестандартные файловые системы требуют отдельной проверки профиля; права
исходников не заменяют live startup/readiness. Приватный каталог нельзя
исправлять массовым chmod: подготовьте новый чистый source.

```bash
node tools/release/prepare-application-source.mjs \
  --source /srv/kodex-dev/workspace-new --revision <40-hex-commit> \
  --confirm PREPARE-STAGING-SOURCE
```

`plan` и повторный preflight `apply` проверяют готовность `node_modules` и
`public/config` до PATCH. Неизвестный вложенный mount отклоняется до релиза,
а не обнаруживается после остановки контейнера. Read-only source и кэш
зависимостей остаются read-only.

HTTP-монитор `tools/dev/release-http-acceptance.mjs` запрашивает HTML-корень с
`Accept: text/html`, а JSON API с `Accept: application/json`. Cookie authority,
refresh и запись каждого реального отказа одинаковы; non-200 не превращается
в успешный результат ради прохождения проверки. Связанные дефекты: #1229, #1233.

```bash
node tools/release/scoped-release.mjs plan \
  --context staging --manifest /private/applications.json \
  --output /private/application-plan.json
node tools/release/scoped-release.mjs apply \
  --context staging --plan /private/application-plan.json \
  --evidence /private/application-release.jsonl --parallelism 2 \
  --timeout-seconds 300 --confirm APPLY-STAGING-APPLICATIONS
```

План закрепляет cluster UID, Deployment UID и digest полного текущего spec,
не сохраняя сам spec или значения env. PATCH повторно проверяет resourceVersion.
Конкурирующее изменение не перетирается. План и журнал создаются исключительно
как новые файлы, поэтому повтор после обрыва не запускает слепую мутацию.

Ошибка одного target фиксируется отдельно, другие targets продолжаются с тем же
лимитом параллелизма. Старые доступные экземпляры сохраняет Deployment controller.
Автоматического rollback, повторного PATCH или пересоздания Deployment нет.

В записи `PATCH_ATTEMPT` находится `rollback`: target для нового плана отката.
Он проверяет digest текущего приложения и точный предыдущий release ID.
Откатывается только приложение; sidecar, revocation и replay state остаются
действующими. После timeout/crash сначала нужен readback фактического состояния.

## Границы текущей реализации

- Production здесь намеренно запрещён. Для него требуется отдельное решение,
  runtime identity и проверка профиля; модель scoped patch переносима без SSH.
- Source rollout предназначен только для hot-reload dev host. В стандартном
  окружении используются исполняемые образы приложений с точным digest.
- Конфигурация, expand/contract migrations, включение grant v2 и security rotation
  выполняются отдельно, не маскируются под обычный application release.
- Render и локальные тесты не доказывают live zero-downtime. Групповые,
  повторные и rollback сценарии на staging относятся к #1223.

## Локальная проверка механизма

```bash
node --test tools/release/scoped-release.test.mjs tools/release/application-source.test.mjs tools/release/worker-grant-transition.test.mjs tools/release/image-writer-capability.test.mjs
```

Проверяются изоляция sidecar sources, сохранение trust/env, отказ небезопасной
стратегии, legacy grants и чужого профиля, source revision/rollback binding,
конкурентность группы и независимый исход каждого target. Полный bootstrap
`remote-dev.sh up` не является обычным application release и может заново
согласовать весь dev render; для повседневной выкладки используется этот
выборочный путь. Staging activation и пользовательская доступность проверяются
отдельно в #1223.

Переход Recreate/v1 → instance grants/v2 → RollingUpdate выполняет отдельный
`worker-grant-transition.mjs`: [порядок, ограничения и evidence](../../docs/operations/independent-releases.md#управляемая-активация-disposable-hot-reload).

Его exact active inventory исключает только terminal `Succeeded`/`Failed` Pods
из всех принадлежащих Deployment ReplicaSet; история не удаляется. Pod старого
RS с `replicas: 0` остаётся блокирующим, если он Running/Pending/Unknown.
`deletionTimestamp` сам по себе не исключает Pod: нетерминальный terminating
predecessor блокирует переход. Неизвестная/отсутствующая phase также не скрывается. Проверки
Ready, числа активных replicas и Deployment UID/resourceVersion/spec сохраняются.
Regression #1405 входит в `worker-grant-transition.test.mjs`.

Для единственного image writer `role-image-builder` используется закрытый
`role-image-builder-writer-capability.json`: exact image + CRI/process readback
через SRE host root. Новых CLI flags нет; CP reader/source guards сохраняются.


`runner-policy-transition.mjs`: [отдельная выкладка runner base и admission policy](../../docs/operations/runner-policy-release.md).
Профиль использует разрешённое окно остановки приложения, сохраняет прежние
immutable policies и опубликованные pins; обычный application release его не запускает.
Проверка application release охватывает также native grant sidecars в
`initContainers`; одной аннотации v2 без точного Pod UID каждого writer недостаточно.
CP проверяется адресными mTLS RPC обеих реплик; для runtime-controller
используется отдельный idle A→B→A proof из `runtime-leader-handoff.mjs`,
передаваемый как `--handoff-proof` и в plan, и в apply. Точные команды,
authority/lifecycle, пределы доказательства и запрет повторного сигнала после
неопределённого ACK приведены в том же runbook.

## Приёмка независимых версий

Issue #1253. `tools/dev/component-manifest.mjs` имеет три read-only режима:
`inventory` собирает фактические версии без заявления о совместимости,
`capture` создаёт кандидат manifest, `verify` проверяет заранее зафиксированный
manifest. Ни один режим не обновляет Kubernetes или источники. Это профиль
`component-revisions`; старый bootstrap/render profile сохраняется без флага.

Manifest включает cluster UID, namespace UID и полный набор Deployment,
StatefulSet и DaemonSet в `kodex-system`. Для каждого фиксируются UID, digest
полной спецификации, exact immutable image references, фактические imageIDs,
source roots/revisions отдельно для всех `/workspace` mounts и native sidecars.
Pods выбираются через controller ownerReferences; одноимённые labels у Job не
участвуют. Проверяются observed generation, число реплик, Ready, завершённые
init containers, совпадение Pod command/args/env/mounts/volumes и аннотаций с
шаблоном. У Air читается digest `/proc/PID/exe` единственного работающего
процесса, включая ещё работающий удалённый inode. Чтение `build/main` не
подменяет эту проверку. Начальная и конечная Kubernetes snapshots должны
совпасть. Pod UID и restart counts сохраняются как наблюдение; законная замена
Pod той же спецификации и binary digest не требует нового manifest.

Это проверка source mount, процесса и Kubernetes metadata, не криптографическая
аттестация сборки из исходников. Для PWA Vite фиксируются exact imageID и
read-only source mount; работа пользовательского пути доказывается browser
smoke/discovery отдельно. Ready и manifest сами по себе не закрывают QA64,
контракты провайдеров, runtime Jobs, schema, rotation или внешние эффекты.

Сначала `inventory --context "$CONTEXT" --output "$NEW_INVENTORY"` собирает
component names, source revisions и imageIDs для заполнения матрицы. Его статус
`INVENTORIED` не является PASS, такой файл нельзя передать в verify как manifest.
Перед capture release plan готовит private compatibility JSON:

```json
{
  "version": 1,
  "components": [
    {
      "component": "Deployment/control-plane",
      "revisions": {"source": ["<точные source revisions по возрастанию>"], "imageIDs": ["<точные imageIDs по возрастанию>"]},
      "provides": {"control-plane-rpc": "<64 hex contract digest>"},
      "requires": []
    },
    {
      "component": "Deployment/control-api-gateway",
      "revisions": {"source": ["<точные source revisions по возрастанию>"], "imageIDs": ["<точные imageIDs по возрастанию>"]},
      "provides": {},
      "requires": [{"component": "Deployment/control-plane", "contract": "control-plane-rpc", "acceptedSHA256": ["<64 hex supported contract digest>"]}]
    }
  ],
  "evidence": [{"path": "/private/approved-compatibility.md", "sha256": "<64 hex file digest>"}]
}
```

Пример показывает две записи, а не полный cluster profile: обязательна
ровно одна запись для **каждого** наблюдаемого workload, включая инфраструктуру.
`source` содержит sorted unique revisions всех его mounts; `imageIDs` — sorted
unique фактические IDs всех контейнеров, включая init. У image-only workload
`source: []`. Матрица содержит все затронутые wire/schema/runtime зависимости
и доказательства поддерживаемых digest для точных component revisions. Для
компонента без таких зависимостей допустимы пустые provides/requires с явным
обоснованием в evidence. Набор не выводится автоматически из Git SHA.

Инструмент сверяет полный набор компонентов, revisions и imageIDs матрицы,
наличие producer и попадание его contract digest в закрытый consumer список.
Файлы доказательств читаются и проверяются по SHA256. Содержание технических
утверждений и полноту графа проверяет автор release plan по каноническим
контрактам и выполненным проверкам; CLI не называет произвольную декларацию
новой доказанной совместимостью. После любого изменения версии требуется
актуализировать матрицу и доказательства, затем создать новый manifest.
Секреты, payload, provider inputs и персональные данные в эти файлы не входят.

```bash
node tools/dev/component-manifest.mjs capture --context "$CONTEXT" \
  --compatibility "$PRIVATE_COMPATIBILITY" --output "$NEW_MANIFEST"
node tools/dev/component-manifest.mjs verify --context "$CONTEXT" \
  --manifest "$NEW_MANIFEST" --output "$NEW_EVIDENCE"
./tools/dev/remote-dev.sh status --env-file "$REMOTE_ENV" \
  --expected-sha "$HARNESS_SHA" --component-manifest "$NEW_MANIFEST"
./tools/dev/remote-dev.sh smoke --env-file "$REMOTE_ENV" \
  --expected-sha "$HARNESS_SHA" --component-manifest "$NEW_MANIFEST"
./tools/dev/remote-dev.sh acceptance --env-file "$REMOTE_ENV" \
  --expected-sha "$HARNESS_SHA" --component-manifest "$NEW_MANIFEST" \
  --resource-prefix "$NEW_PREFIX" --run-timeout-ms 1800000
```

Общий бюджет readback — 10 минут, отдельного kubectl/git запуска — до 30 секунд
(у source inspector git — 10 секунд). Timeout закрыто отклоняет проверку.
Пути output всегда новые: перезапись manifest/evidence запрещена. `capture`
выдаёт `CAPTURED`, а не `PASS`. Оператор сверяет кандидат с согласованным
release plan до приёмки; изменившийся UID/spec/source/image/contract не
исправляется автоматическим recapture. `verify` закрыто отказывает и требует
диагностики. Возвращаемый `manifestSHA256` относится к закреплённому manifest,
а не к каждому последующему timestamp наблюдения.

`--expected-sha` в этом профиле закрепляет **оснастку**, а serving versions
берутся из component manifest. Browser report и visual evidence получают
`sourceRole: harness` и `servingManifestSHA256`; прежний `sourceSHA` сохраняется
как совместимое поле SHA оснастки. Full summary содержит `evidenceProfile` и
SHA256 файла manifest. Поддержанный readback выполняется до и после browser
и после всех batch, без глобального `up`, пересборки archive/STT образов или
перехода соседей на общий source. Тест механизма Air/Vite доступен отдельно
через явный `--batch hot-reload` без component manifest.

Локальная публичная проверка: `make test-full-local-e2e-entrypoint`. Она
включает Node negative/CLI fixtures и shell entrypoint contract, не обращается
к staging и не является live acceptance. Актуальная документация Kubernetes
Deployment/ReplicaSet/Pod readback проверена через Context7 `/kubernetes/website`.

## Authority freshness и отдельный issuer image

Полный порядок additive SQL → security sidecars → future Job producer →
CAS activation30s, capability/readback, независимые partition и точное
восстановление описаны в [OPS-DOC-1313](../../docs/operations/authority-freshness-1313.md).
Application-only scoped release не изменяет эти ресурсы. `runner-policy-transition`
дополнительно принимает `--authority-issuer-image` при сохранении текущего
`--runner-digest`; перед resource phase используются additive `schema` и `admission`.

Для #1313 узкий `tools/dev/build-local-image-supply-chain.sh --component authority-security`
с обязательным `--context` собирает только authority runtime и admission reader,
импортирует их в локальный k3s image store и проверяет manifest digests.
Точные аргументы и граница single-host профиля приведены в OPS-DOC-1313;
это не registry push и не global `up`.

Короткоживущие admission/promotion Jobs наблюдаются через
`authority-freshness-job-proof-watcher.mjs`. Для RoleImage перехода контроллер
сначала создаёт server-owned reservation с `spec.suspend=true` по
[OPS-DOC-1381](../../docs/operations/image-admission-proof-hold-1381.md).
Watcher plan версии 2 закрепляет exact Job name, UID, held/released spec,
policy и capability; `watch` пишет fsync intent до снятия hold и ждёт running
executable плюс terminal success. `image-admission-proof-hold.mjs` включает
bounded режим, снимает exact hold только при совпавшем watcher `INTENT` и
поддерживает readback-only `resume` после `UNKNOWN`. Старый plan до создания
Job остаётся совместимым, но не используется для нового RoleImage proof.
Полные команды, исходы и guards приведены в OPS-DOC-1313 и OPS-DOC-1381.
Одиночный `authority-freshness-job-proof.mjs`
поддерживается только когда exact Job уже находится в состоянии running.

## Серверное хранилище browser proxy session

Изменение backend OAuth2-proxy не является application-only release.
`proxy-session-store.mjs` готовит отдельную TLS/ACL/PVC dependency и выполняет
проверяемый CAS cutover двух control-center proxy replicas с одним штатным
повторным входом. Фазы install/readiness/maintenance/cutover/login, backup и
ограничения rollback описаны в
[OPS-DOC-1383](../../docs/operations/proxy-session-store-1383.md).
Сроки Keycloak/BFF и reuse не расширяются. Старый backup не восстанавливает
отозванные сессии; slow-provider lease defect отслеживается отдельно в #1388.
