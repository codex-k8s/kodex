---
id: OPS-DOC-1381
title: Управляемая задержка Job для executable proof RoleImage
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-09-09
---

# Управляемая задержка Job для executable proof RoleImage

Документ задаёт staging-переход, при котором watcher успевает закрепить точную
короткоживущую Job до запуска её Pod. Переход не создаёт provider effect и не
меняет опубликованный RoleImage pointer: контроллер создаёт обычные `claim` и
`promote` Job с серверными identity и `spec.suspend=true`, а оператор снимает
только этот hold после устойчивого `INTENT` watcher.

Режим выключен по умолчанию. Он включается абсолютным UTC deadline не более чем
на 30 минут. Просроченный или слишком дальний deadline закрыто останавливает
новую оркестрацию и делает readiness контроллера отрицательной; уже созданная
Job остаётся приостановленной и не превращается в скрытый эффект. Рестарт
контроллера сохраняет reservation в Kubernetes Job.

## Границы и состояния

| Состояние | Авторитетное условие | Разрешённый переход |
| --- | --- | --- |
| `DISABLED` | env hold=`false` | CAS rollout контроллера в `HELD` |
| `HELD` | hold=`true`, допустимый deadline | контроллер создаёт `claim`/`promote` с точным reservation |
| `WATCHING` | watcher journal содержит fsync `INTENT` того же plan | CAS release только совпавшей Job |
| `RELEASED` | та же Job UID, `suspend=false`, неизменный spec | только обычный terminal outcome |
| `UNKNOWN` | ответ mutation/readback потерян | только `resume` того же plan; повторный patch запрещён |
| `DISABLED` | обе reserved Job terminal или удалены | CAS rollout hold=`false` |

Reservation связывает `admission-run-id`, operation ID, phase и attempt `1` SHA-256
дайджестом. Допустимы только `claim` и `promote`; `scan`, `sign` и `admit`
остаются обычной последовательной цепочкой. ValidatingAdmissionPolicy разрешает
создать приостановленную Job только контроллеру и разрешает при update лишь
переход `suspend=true -> false` без смены identity, аннотаций, Pod template или
bounded lifecycle.

## Операторская последовательность

Все пути ниже находятся в новом owner-private каталоге режима `0700`, файлы
создаются режимом `0600`. `CONTEXT`, `CAPABILITY`, `SESSION`, `MANIFEST`,
`RUNNER_DIGEST`, `PREFIX` и `UNTIL` берутся из свежего readback. `UNTIL` —
однократный UTC timestamp в пределах 30 минут.

Сначала включить hold отдельным plan/apply и дождаться готового контроллера:

```sh
node tools/release/image-admission-proof-hold.mjs plan \
  --context "$CONTEXT" --k3s-sudo --action enable --until "$UNTIL" \
  --output "$PRIVATE/hold-enable-plan.json"
node tools/release/image-admission-proof-hold.mjs apply \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/hold-enable-plan.json" \
  --evidence "$PRIVATE/hold-enable.jsonl" \
  --confirm APPLY_STAGING_IMAGE_PROOF_HOLD
```

После появления двух Job прочитать только `metadata.name`, `metadata.uid`,
`resourceVersion`, hold annotations и `spec.suspend`. Для каждого exact имени
создать watcher plan. План версии 2 закрепляет UID, reservation digest, held и
released spec SHA-256:

```sh
node tools/release/authority-freshness-job-proof-watcher.mjs plan \
  --context "$CONTEXT" --k3s-sudo --job "$CLAIM_JOB" \
  --capability "$CAPABILITY" --timeout-seconds 840 \
  --output "$PRIVATE/claim-watch-plan.json"
node tools/release/authority-freshness-job-proof-watcher.mjs plan \
  --context "$CONTEXT" --k3s-sudo --job "$PROMOTE_JOB" \
  --capability "$CAPABILITY" --timeout-seconds 840 \
  --output "$PRIVATE/promote-watch-plan.json"
```

Запустить оба `watch` в отдельных контролируемых терминалах и убедиться, что в
каждом evidence первым появился `INTENT`. Затем в третьем терминале запустить
один provider-free `role-image-acceptance.mjs prepare` с новым prefix и прежним
promoted pin. Сначала снять hold `claim`:

```sh
node tools/release/image-admission-proof-hold.mjs plan \
  --context "$CONTEXT" --k3s-sudo --action release --job "$CLAIM_JOB" \
  --watcher-plan "$PRIVATE/claim-watch-plan.json" \
  --watcher-evidence "$PRIVATE/claim-watch.jsonl" \
  --output "$PRIVATE/claim-release-plan.json"
node tools/release/image-admission-proof-hold.mjs apply \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/claim-release-plan.json" \
  --evidence "$PRIVATE/claim-release.jsonl" \
  --confirm APPLY_STAGING_IMAGE_PROOF_HOLD
```

После `PASS` claim watcher дождаться в приватном RoleImage journal ACK шага
`prepare-promote`; проверка выводит только boolean, содержимое ACK не
публикуется. Затем теми же командами с `PROMOTE_JOB` и `promote-*` путями снять
второй hold. Успех требует `PASS` обоих watcher и `prepare-complete` RoleImage.

При `UNKNOWN` mutation не создавать новый plan и не применять patch повторно:

```sh
node tools/release/image-admission-proof-hold.mjs resume \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/claim-release-plan.json" \
  --evidence "$PRIVATE/claim-release.jsonl" \
  --confirm APPLY_STAGING_IMAGE_PROOF_HOLD
```

После terminal readback обеих reserved Job выключить режим отдельным
`--action disable` plan/apply. Если Job ещё активна, plan закрыто отказывает.
Rollback до release — выключение невозможно, пока suspended reservation
активна; её UID-guarded удаление относится к отдельному аварийному решению.
После release rollback приложения не возвращает эффект Job: ожидается terminal
readback либо `UNKNOWN`, а прежние published pointer и pins сохраняются.

## Доказательство

Общий evidence содержит только exact Job name/UID, spec/policy/capability
дайджесты, timestamps и закрытые коды. Pod env, application grant, cookies,
RoleImage source, SBOM payload и registry credentials не сохраняются. Два
watcher `PASS` доказывают executable issuer только этих Job и не заменяют
полную CFG-01 приёмку, node pull или runtime Job.
