---
id: OPS-DOC-1450
title: Отдельная поставка frontend source и подготовленных зависимостей
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-09-12
---

# Граница

Переход из #1450 применяется только к `staff-control-center` в уже работающем
staging hot-reload профиле. Изменение `package.json` либо `package-lock.json`
по-прежнему запрещено обычным `scoped-release` source plan. Отдельный переход
атомарно меняет frontend source, prepared cache и application annotations.
Node image, wrapper scripts, security, shared resources и соседние Deployments
не меняются. Изменённые image или wrappers требуют отдельной поставки.

Это поддержка разработки. Portable image installation profile сохраняется;
данный механизм не объявляется способом обычной установки продукта.

# Подготовка

Используются exact clean source из штатного creator/prepare и существующий
cache root текущего host. Команды запускает один доверенный оператор из
проверенного checkout. Не передавать frontend production/test credentials.

```sh
./tools/dev/prime-frontend-cache.sh "$SOURCE" "$CACHE_ROOT"
```

Команда возвращает путь `frontend-v1/<identity>/node_modules`. Она использует
отдельный lock, exact Node image, `npm ci --ignore-scripts`, проверяет esbuild/Vite
и публикует новый readonly cache. Старый cache не изменяется. Удалять неполный
или старый каталог автоматически для зелёного результата запрещено.

В `CACHE` указывается возвращённый путь. Plan сверяет readonly manifests и
receipt, текущий image и runtime identity через краткоживущий Docker container
с readonly mounts, `--network none` и `--pull=never`. План не скачивает image и
не устанавливает packages. Секретные host env/npmrc в контейнер не передаются.

# Lifecycle

| Фаза | Проверка | Изменение и recovery |
| --- | --- | --- |
| Prepare | exact source, lock, Node image, npm manifests | Новый readonly cache, без изменения Pods |
| Plan | namespace UID, PWA UID/resourceVersion/spec, готовые старые replicas, drain, source/cache, wrappers и соседи | Private plan0600; server-side dry-run не применяет Deployment |
| Apply | Повторное вычисление canonical patch и cache proof, прежний spec/RV | Fsync immutable intent рядом с plan и отдельный journal до одного CAS patch |
| Observe | Тот же plan/intent, target spec, rollout readiness и прежние соседи | Readback без повторного patch; UNKNOWN не становится PASS по timeout |
| Rollback plan | Текущая PWA равна target spec прежнего plan, старые source/cache доступны и проверены | Новый plan с текущим CAS; исходный plan не применяется повторно |

Нужны private каталоги0700 того же operator UID. Plan, его `.intent` и journal
сохраняются после ошибки. Новый путь journal не разрешает повторную попытку
старого apply. При UNKNOWN оператор вызывает `observe` с тем же plan; если
виден прежний spec, результат остаётся UNKNOWN. Не запускать новый rollout
вслепую. Drift закрыто останавливает переход.

```sh
node tools/release/frontend-dependency-transition.mjs plan \
  --context "$CONTEXT" --k3s-sudo \
  --source "$SOURCE" --revision "$REVISION" --cache "$CACHE" \
  --output "$PRIVATE/frontend-plan.json"

node tools/release/frontend-dependency-transition.mjs apply \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/frontend-plan.json" \
  --evidence "$PRIVATE/frontend-evidence.jsonl" \
  --confirm APPLY-STAGING-FRONTEND-DEPENDENCIES

node tools/release/frontend-dependency-transition.mjs observe \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/frontend-plan.json"

node tools/release/frontend-dependency-transition.mjs rollback-plan \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/frontend-plan.json" \
  --output "$PRIVATE/frontend-rollback-plan.json"

node tools/release/frontend-dependency-transition.mjs apply \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/frontend-rollback-plan.json" \
  --evidence "$PRIVATE/frontend-rollback-evidence.jsonl" \
  --confirm APPLY-STAGING-FRONTEND-DEPENDENCIES
```

Без `--k3s-sudo` используется обычный `kubectl` выбранного context. Production
context закрыто отклоняется. Namespace фиксирован: `kodex-system`.

# Проверка результата

`PASS` инструмента означает exact Deployment rollout и сохранённые spec/UID
соседей. Он не заменяет HTTP/WS или продуктовую приёмку. После перехода нужны
fresh serving manifest, status/smoke, OIDC, две вкладки, Project/assistant и
затронутые сценарии. Во время rollout непрерывно наблюдаются авторизованные
HTTP/WS, Pod UIDs/restarts и пользовательские ошибки. Повторный login не должен
требоваться из-за source/cache transition.

Модели и CLI проверяются командой
`node --test tools/release/frontend-dependency-transition.test.mjs`.
Синтетический cache/runtime adapter в CLI tests не доказывает live npm cache:
его проверяет настоящий offline Node container перед plan и apply.
