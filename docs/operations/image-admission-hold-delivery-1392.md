---
id: OPS-DOC-1392
title: Управляемая доставка RoleImage Job hold
type: operations
status: approved
owner: sre
version: 1.0.2
updated: 2026-09-09
---

# Управляемая доставка RoleImage Job hold

Документ задаёт отдельный staging-переход для `image-admission-controller` и
двух admission resources из #1381. Он выполняется до включения bounded hold из
`OPS-DOC-1381` и не применяет общий render, не меняет worker grants, authority
generations, опубликованные RoleImage revisions/pins или пользовательские
приложения.

Переход использует один immutable plan и один fsync journal. Каждая mutating
фаза меняет ровно один Kubernetes resource. Это позволяет после потерянного ACK
прочитать фактическое состояние exact target и не повторять `PATCH` или
`CREATE`. Следующая фаза допускается только после `PASS` предыдущей.

## Границы и состояния

| Фаза | Предусловие | Единственное изменение | Readback |
| --- | --- | --- | --- |
| `pause` | один Ready controller Pod; hold выключен; нет terminating admission work | literal `IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS=true` | owner idle, существующие admission Jobs terminal, рабочих PVC нет |
| `reader` | pause, owner counters zero | exact immutable `image-admission` image и два выключенных hold env | imageID, CRI identity и SHA-256 фактического `/proc/PID/exe` |
| `policy-jobs` | reader proof | exact spec основной VAP из реализации #1381 | прежние правила и `failurePolicy: Fail` сохранены, новый reservation contract совпал |
| `policy-release` | основной VAP загружен | создать отсутствующую exact VAP release | допускается только `suspend=true -> false` без изменения Job identity/spec |
| `binding-release` | release VAP загружена | создать отсутствующий exact binding | `validationActions: [Deny]`, namespace selector `kodex-system` |
| `open` | все policy readbacks и reader proof | literal pause=false | один Ready controller Pod, прежний hold=false |

На каждой фазе закреплены cluster/namespace UID, target UID/resourceVersion,
before/after spec SHA-256, основной binding, текущие
`ImageAdmissionPolicyParameters` и immutable ConfigMap, все соседние Deployment
UID/spec digest, owner counters и digest опубликованных pins. Terminating
controller Pod не считается завершённым rollout. Plan закрепляет exact
Job UID/spec и PVC UID/spec. Во время `pause` существующая Job может перейти
только в terminal, а рабочий PVC — исчезнуть после cleanup. Terminating work,
новая/заменённая Job, удаление Job history либо новый/изменённый PVC закрыто
отклоняются.

Новые VAP создаются только по отсутствующему exact имени; API create является
name-CAS. Если имя уже существует, допустим только byte-equivalent spec из
точной реализации #1381. Partial либо чужой resource закрыто отклоняется.

## Подготовка capability

Сначала узко собрать и импортировать `image-admission` image из точного чистого
source. Пересборка соседних images не нужна:

```sh
./tools/dev/build-local-image-supply-chain.sh \
  --source-root "$SOURCE" --state-directory "$PRIVATE" \
  --component image-admission
IMAGE_ADMISSION_IMAGE=$(cat "$PRIVATE/image-admission-image")
node tools/release/image-admission-hold-capability.mjs \
  --source "$SOURCE" --revision "$REVISION" \
  --image "$IMAGE_ADMISSION_IMAGE" \
  --output "$PRIVATE/image-admission-hold-capability.json"
```

Capability повторяет точный Dockerfile build recipe и содержит только source
revision, immutable image manifest, Go version, recipe и executable SHA-256.
Значений Secret, Pod env или registry credentials в нём нет. При `reader`
оператор сравнивает этот digest с фактически запущенным `/proc/PID/exe`,
привязанным к CRI container ID, Pod UID и imageID.

## Inspect и plan

Команды выполняются на disposable k3s host из exact source. `PRIVATE` имеет
режим `0700`, каждый новый output — `0600`:

```sh
node tools/release/image-admission-hold-delivery.mjs inspect \
  --context "$CONTEXT" --k3s-sudo \
  --output "$PRIVATE/hold-delivery-inspect.json"

node tools/release/image-admission-hold-delivery.mjs plan \
  --context "$CONTEXT" --k3s-sudo \
  --source "$SOURCE" --revision "$REVISION" \
  --capability "$PRIVATE/image-admission-hold-capability.json" \
  --output "$PRIVATE/hold-delivery-plan.json"
```

Source обязан быть чистым checkout `codex-k8s/kodex`, а revision — потомком
точной реализации #1381. CLI извлекает основной и release VAP из Git source и
сверяет их byte-equivalent spec с утверждённым commit #1381. Более широкий или
частично изменённый policy требует нового Issue/плана.

Для predecessor CLI принимает только закрытый набор представлений из точного
source: прямой разбор YAML и результат штатного `kubectl kustomize`, каждый до
и после документированных defaults Kubernetes API. К ним относятся
`matchPolicy: Equivalent`, пустые `namespaceSelector`/`objectSelector` и
`scope: "*"` для правила без явно заданного scope. Произвольная нормализация
или удаление whitespace в CEL запрещены: переводы строк внутри литералов и
любое иное отличие expression остаются drift. Исходный live spec сохраняется
в `before`, CAS test и rollback без преобразования.

Bundle v2 также явно сохраняет predecessor после штатной фазы `admission`
из `runner-policy-transition.mjs`. CLI читает policy из точного родителя
`28fc62259f36518f3602da7281c8ac27746aa016`, выполняет тот же Kustomize render и
тот же единственный issuer-image transition, что использует записывающий CLI.
Это отдельная каноническая форма: source YAML переносит conditional на новую
строку, тогда как ранее выполненный переход записал его в одну строку.
Сравнивается полный spec; другие изменения whitespace, литералов или правил
закрыто отклоняются. Уже сохранённые bundles v1 читаются в прежнем формате;
новый вариант не добавляется задним числом в их immutable plans/journals.

Readback #1421 от `2026-09-09T14:26:22.760Z` имеет полный spec fingerprint
`315bf2fdd46347af9695915052c54f0e0505f66d06d382e23ceb00a5544ac2e5`.
Source regression воспроизводит именно этот digest из Git и штатного перехода.
Это доказательство распознавания predecessor, но не live PASS остальных фаз.


Прямые `PATCH`/`CREATE` этого CLI отправляют exact source resource, а ожидаемый
readback закрепляют после тех же API defaults. Поэтому основная policy, release
policy и release binding не застревают после успешной записи из-за полей,
добавленных API server.

## Apply, observe и resume

Фазы выполняются последовательно одним plan и одним evidence:

```sh
set -e
for PHASE in pause reader policy-jobs policy-release binding-release open; do
  node tools/release/image-admission-hold-delivery.mjs apply \
    --context "$CONTEXT" --k3s-sudo \
    --plan "$PRIVATE/hold-delivery-plan.json" --phase "$PHASE" \
    --evidence "$PRIVATE/hold-delivery-evidence.jsonl" \
    --confirm APPLY_STAGING_IMAGE_ADMISSION_HOLD_DELIVERY
done
```

Если plan показывает, что одна из policy resources уже exact, соответствующая
фаза имеет `action=none`: команда выполняет только readback и записывает `PASS`
без `INTENT`. Это сохраняет полную последовательность одного journal. До каждой
mutation journal получает fsync `INTENT` с фактическим target resourceVersion.
После exact readback записываются `APPLIED` и `PASS`.

Read-only проверка конкретной фазы:

```sh
node tools/release/image-admission-hold-delivery.mjs observe \
  --context "$CONTEXT" --k3s-sudo \
  --plan "$PRIVATE/hold-delivery-plan.json" --phase "$PHASE" \
  --output "$PRIVATE/hold-delivery-observe-$PHASE.json"
```

При `UNKNOWN` разрешён только readback того же plan/phase:

```sh
node tools/release/image-admission-hold-delivery.mjs resume \
  --context "$CONTEXT" --k3s-sudo \
  --plan "$PRIVATE/hold-delivery-plan.json" --phase "$PHASE" \
  --evidence "$PRIVATE/hold-delivery-evidence.jsonl" \
  --confirm APPLY_STAGING_IMAGE_ADMISSION_HOLD_DELIVERY
```

`resume` не выполняет mutation. Если target остался `BEFORE`, создаётся новый
plan только после того, как оператор закрыл неопределённый intent фактическим
readback. Drift, replacement и неполная policy остаются `FAIL/UNKNOWN`, а не
поводом для force или глобального `up`.

## Hold, watcher и release

После `open` применяется неизменённый flow `OPS-DOC-1381`:

1. `image-admission-proof-hold.mjs plan/apply --action enable` с deadline не
   более 30 минут.
2. Один provider-free RoleImage lifecycle создаёт suspended `claim` и
   `promote` Jobs с server-owned reservation.
3. Для каждой exact Job запускается
   `authority-freshness-job-proof-watcher.mjs watch`; release разрешён только
   после fsync `INTENT` этого watcher.
4. `image-admission-proof-hold.mjs --action release` снимает только exact
   `spec.suspend`; после неопределённого исхода используется его readback-only
   `resume`.
5. После двух terminal Jobs hold выключается. Старые published pins не
   перепривязываются автоматически.

## Ошибки и rollback

До включения bounded hold полный возврат выполняется тем же immutable plan и
отдельным rollback journal в обратном порядке:

```sh
set -e
for PHASE in rollback-pause rollback-binding-release \
  rollback-policy-release rollback-policy-jobs rollback-reader rollback-open; do
  node tools/release/image-admission-hold-delivery.mjs rollback \
    --context "$CONTEXT" --k3s-sudo \
    --plan "$PRIVATE/hold-delivery-plan.json" --phase "$PHASE" \
    --evidence "$PRIVATE/hold-delivery-rollback-evidence.jsonl" \
    --confirm APPLY_STAGING_IMAGE_ADMISSION_HOLD_DELIVERY
done
```

`rollback-pause` сначала возвращает controller в paused reader. Созданные этим
plan release binding/VAP удаляются Kubernetes DELETE с exact UID и
`resourceVersion` preconditions. Основной VAP и controller spec возвращаются
JSON Patch с `test` UID/resourceVersion/spec. Если ресурс уже находился в
исходном состоянии, фаза записывает read-only `PASS`. Для неопределённого
исхода используется `rollback-resume` с тем же plan, phase и journal;
повторная mutation запрещена.

```sh
node tools/release/image-admission-hold-delivery.mjs rollback-resume \
  --context "$CONTEXT" --k3s-sudo \
  --plan "$PRIVATE/hold-delivery-plan.json" --phase "$PHASE" \
  --evidence "$PRIVATE/hold-delivery-rollback-evidence.jsonl" \
  --confirm APPLY_STAGING_IMAGE_ADMISSION_HOLD_DELIVERY
```

После release Job rollback не повторяет Job и не откатывает внешний effect.
Ожидается terminal readback либо продолжение того же `UNKNOWN/resume`.
Удаление Jobs/PVC/history, ослабление `failurePolicy`, TTL, wildcard identity и
production context этим переходом не поддерживаются.

Актуальные правила Kubernetes `ValidatingAdmissionPolicy` PATCH, optimistic
concurrency `resourceVersion` и Job API проверены через Context7
`/kubernetes/website`. Точные defaulting functions в выдаче Context7 не
нашлись; дополнительно проверен официальный Kubernetes source
`pkg/apis/admissionregistration/v1/defaults.go`.
