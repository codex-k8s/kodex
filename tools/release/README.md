# Независимое обновление приложений

Задача: #1222, общий план: #1204.

`scoped-release.mjs` обновляет digest образа или подготовленный source checkout
основного контейнера выбранного Deployment и собственную аннотацию релиза. Общие Secrets, trust, grants,
ConfigMaps, миграции, sidecar images, replicas и strategy не меняются.
Один target и группа используют одинаковый механизм.

## Предварительные условия

- Node.js с поддержкой ES modules и `kubectl`; точный Kubernetes context.
- Уже подготовленный staging: `kodex.dev/local-profile=hot-reload` либо
  `kodex.dev/environment=staging`, `app.kubernetes.io/part-of=kodex`.
- `RollingUpdate`, `maxUnavailable: 0`, положительный числовой `maxSurge` и
  доступное текущее число replicas. Для workers уже активирован grant v2.
- Образ собран, доставлен в registry и допущен существующим admission policy.
  Скрипт не обходит admission и не публикует неподтвержденные образы.
- Приложение совместимо с действующими API, событиями и схемой БД.
  Этот инструмент не выводит совместимость из Git SHA.

## Использование

Приватный JSON manifest версии 1 содержит `targets`: массив объектов `name` и
`image`, где image имеет вид `repository@sha256:<64 hex>`. Перечень приложений
закрытый; authority infrastructure через этот путь обновлять нельзя.

В hot-reload профиле вместо `image` или вместе с ним можно задать
`source: {"path":"/srv/kodex-dev/workspace-new","revision":"<40 hex commit>"}`.
Скрипт запускается на доверенном dev host и проверяет оба checkout: точный root,
origin, commit и чистое дерево без приватного env внутри. Новые Go dependencies
должны быть заранее подготовлены штатным host prime; иначе новая replica не
станет Ready, а старая останется доступной.

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

```bash
node tools/release/prepare-application-source.mjs \
  --source /srv/kodex-dev/workspace-new --revision <40-hex-commit> \
  --confirm PREPARE-STAGING-SOURCE
```

`plan` и повторный preflight `apply` проверяют готовность `node_modules` и
`public/config` до PATCH. Неизвестный вложенный mount отклоняется до релиза,
а не обнаруживается после остановки контейнера. Read-only source и кэш
зависимостей остаются read-only.

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
node --test tools/release/scoped-release.test.mjs tools/release/application-source.test.mjs
```

Проверяются изоляция sidecar sources, сохранение trust/env, отказ небезопасной
стратегии, legacy grants и чужого профиля, source revision/rollback binding,
конкурентность группы и независимый исход каждого target. Полный bootstrap
`remote-dev.sh up` не является обычным application release и может заново
согласовать весь dev render; для повседневной выкладки используется этот
выборочный путь. Staging activation и пользовательская доступность проверяются
отдельно в #1223.
