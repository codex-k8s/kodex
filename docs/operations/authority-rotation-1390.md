---
id: OPS-DOC-1390
title: Устойчивая ротация ключей internal RPC authority
status: approved
type: operation-evidence
owner: developer
version: 1.3.0
updated: 2026-09-09
---

# Граница и причина

Документ закрывает реализацию [#1390](https://github.com/codex-k8s/kodex/issues/1390).
Прежний publisher при новой revision одновременно переводил `NEXT` в `CURRENT`
и записывал новый `NEXT`; SQL переходил из `PREPARED` сразу в `PROMOTED`.
После частичной Kubernetes CAS-доставки не существовало устойчивого состояния,
которое позволяло отличить безопасный abort от обязательного resume.

Additive migrations сохраняют старый writer как `protocol_version=1`, а новый
writer связывает три неизменяемые публикации одной operation:

```text
DISTRIBUTING -> WAITING_SWITCH -> SWITCHING -> WAITING_RETIRE -> RETIRING -> RETIRED
      | publication #1       | publication #2          | publication #3
      | CURRENT+NEXT         | NEXT→CURRENT,+PREVIOUS  | -PREVIOUS
```

Каждая phase publication внутри использует прежний подтверждаемый граф
`PREPARED -> DELIVERING -> DELIVERED -> PROMOTED`. PostgreSQL назначает
`switch_not_before` и `previous_not_after` один раз. Повторный запуск читает
те же значения и не продлевает overlap. Для каждой публикации отдельно
сохраняются registry digest, publication-input digest и snapshot digest:
равенство между этими разными областями хеширования не предполагается.

`ABORTED` допустим только до первой доставки. После `DELIVERING` сбой, рестарт
или потерянный ответ ведут к readback и продолжению того же deterministic
intent. Нельзя создать параллельное намерение или пропустить source revision.
Payload JWS, private keys и credentials в таблицу намерений и операторский
readback не входят.

# Матрица lifecycle

| Переход | Инициатор и durable precondition | Действие и readback | Закрытый отказ |
| --- | --- | --- | --- |
| `PREPARED` | Единственный publisher, exact следующая source revision и digest registry; predecessor уже `PROMOTED/RETIRED`, его 40-секундное окно окончено | PostgreSQL фиксирует UUID, predecessor и точное число обязательных readers до Kubernetes CAS | Параллельный intent, пропуск revision, неизвестный predecessor, незавершённый overlap |
| `DELIVERING` | Тот же UUID/source/digest | Переход фиксируется до первой записи Secret; повтор после рестарта идемпотентен | После этого abort запрещён независимо от частичной доставки |
| `DELIVERED` | Snapshot history уже содержит exact publication; все записи и snapshot прочитаны обратно | Publisher фиксирует завершение CAS-доставки | Отсутствующий history, другой digest или частичная доставка |
| `PROMOTED` | `DELIVERED` и точный полный набор `(workload, role, generation)` readbacks | SQL атомарно фиксирует promotion и `overlap_until = DB time + 40 seconds` | Лишние динамические receipts не заменяют отсутствующий обязательный reader |
| `RETIRED` | Начинается следующая revision после окончания overlap | Predecessor переводится forward-only перед новым intent | Старый ключ нельзя вернуть в `CURRENT` обычным application rollback |
| `ABORTED` | Только `PREPARED`, history отсутствует, delivery ещё не начиналась | Publisher либо отдельная migrator job выполняет exact CAS и readback | `DELIVERING/DELIVERED/PROMOTED/RETIRED` нельзя отменить |

| Фаза operation | Авторитетная блокировка | Immutable publication и ключи | Recovery/следующая фаза |
| --- | --- | --- | --- |
| `DISTRIBUTING` | Одна active operation, exact следующая registry revision, текущий snapshot predecessor | #1 сохраняет прежние `CURRENT+NEXT`, но доставляет новый registry provenance всем readers | Потерянный CAS перечитывается; тот же phase intent продолжается до полного readback |
| `WAITING_SWITCH` | #1 `PROMOTED`; `switch_not_before` назначен временем PostgreSQL | Новых effects нет | До deadline остаётся та же publication; deadline не пересчитывается |
| `SWITCHING` | DB deadline наступил | #2 делает прежний `NEXT` новым `CURRENT`, создаёт новый `NEXT`, прежний `CURRENT` публикует как `PREVIOUS` с точным `NotAfter=previous_not_after` | Crash продолжает тот же UUID/phase; истечение deadline не продлевает доверие |
| `WAITING_RETIRE` | #2 `PROMOTED`; его intent закреплён тем же `previous_not_after` | `PREVIOUS` принимается только до указанного момента | До deadline mutation отсутствует |
| `RETIRING` | `previous_not_after` наступил | #3 удаляет `PREVIOUS`, не меняя `CURRENT/NEXT` | Partial fan-out остаётся тем же intent; promotion требует все exact readbacks |
| `RETIRED` | #3 `PROMOTED` | Три provenance rows и история сохраняются | Возврат прежнего ключа/operation и новая публикация той же registry revision запрещены |

# Порядок поставки и активации

1. Запустить additive migrations из точного source. Они не изменяют применённые
   migration bytes и сами не активируют новый writer.
2. Обновить publisher на совместимый бинарь через штатный authority
   infrastructure workflow. Обычный `scoped-release` приложения эту фазу не
   заменяет. В hot-reload staging пересборка immutable image не нужна, если
   контейнер, toolchain и встроенный binary не менялись; exact source и
   исполняемый процесс всё равно фиксируются.
3. Repo-owned transition создаёт случайный `intentID` оператора, а owner
   operation UUID канонически выводит из exact `source_revision` и SHA-256
   байтов нового registry тем же алгоритмом, что publisher. CAS разрешён
   только если registry revision возрастает ровно на единицу; snapshot
   revision остаётся отдельной последовательностью и за три публикации
   увеличивается на три. Transition применяет UID/resourceVersion CAS только к
   exact `internal-rpc-authority-publisher-target-registry`. Один атомарный
   ConfigMap CAS заменяет canonical base registry следующей revision и
   source-owned `authority-policy.json`; policy и registry не публикуются
   раздельно. Затем выполняется CAS неизменённого
   Deployment с отдельными intent и owner-operation annotations.
   Перезапускается только publisher;
   другие workloads и application revisions не меняются.
4. Наблюдать один operation UUID через три публикации и оба 40-секундных окна.
   Следующую registry revision нельзя публиковать раньше `RETIRED`.
5. Только `PREPARED` можно закрыть `rotation-abort`. После неизвестного исхода
   сначала выполнить `resume`/`rotation-status`, а не создавать новый intent.

Инструмент `tools/release/authority-rotation-transition.mjs` принимает только
точный staging context и новый private plan. Он не зависит от существования
завершённой live migration Job. В immutable installation profile repo-owned
status Job строится из канонического
`deploy/k8s/base/internal-rpc-authority-data/migration-job.yaml`, использует
exact digest уже обслуживаемого publisher image и запускает
`/usr/local/bin/internal-rpc-authority-cli rotation-watch --operation-id <OWNER_UUID>`.
В staging source profile transition принимает только завершённый exact
`OPS-DOC-1434` plan вместе с его durable migration UID receipt, повторно
сверяет тот же terminal Job и publisher `/proc/PID/exe`, затем строит Job из
закреплённого actual render с `run-go-command.sh` и offline caches.
Watch остаётся
активным до `RETIRED` и поэтому один immutable Job наблюдает все три фазы.
Имя Job закреплено за `intentID`, но terminal readback принимается только для
точного owner UUID; чужой `RETIRED` не завершает watch, включая `resume` после
неизвестного исхода. План версии 3 закрепляет оба UUID, namespace UID,
registry/Deployment UID+resourceVersion, source SHA, image digest и команды.
Оба Job builder (миграция и наблюдение ротации) используют общий набор
явных Kubernetes defaults до вычисления hash/intent. Заданные template values
сохраняются, посторонний spec drift по-прежнему закрыто отклоняется.
До записи plan выполняются `auth can-i create jobs`, безопасные metadata-only
проверки SA/Secret/CA, exact live NetworkPolicy selectors и server-side dry-run
создания итогового Job через admission.
Перед каждым ConfigMap CAS и publisher-only restart пишется fsync `INTENT`;
после неизвестного ответа `resume` сначала делает exact readback того же
operation и не создаёт новое намерение.

```bash
node tools/release/authority-rotation-transition.mjs plan \
  --context "$KODEX_RELEASE_CONTEXT" \
  --source /srv/kodex-dev/<EXACT_SOURCE> --revision <EXACT_SHA> \
  --action status --output /private/rotation-status-plan.json

node tools/release/authority-rotation-transition.mjs plan \
  --context "$KODEX_RELEASE_CONTEXT" \
  --source /srv/kodex-dev/<EXACT_SOURCE> --revision <EXACT_SHA> \
  --action rotate \
  --source-delivery-plan /private/authority-source-plan.json \
  --source-migration-receipt /private/authority-source-migration-receipt.json \
  --output /private/rotation-plan.json

node tools/release/authority-rotation-transition.mjs apply \
  --context "$KODEX_RELEASE_CONTEXT" \
  --plan /private/rotation-plan.json \
  --evidence /private/rotation.jsonl \
  --confirm APPLY-STAGING-AUTHORITY-ROTATION

node tools/release/authority-rotation-transition.mjs observe \
  --context "$KODEX_RELEASE_CONTEXT" \
  --plan /private/rotation-plan.json

node tools/release/authority-rotation-transition.mjs resume \
  --context "$KODEX_RELEASE_CONTEXT" \
  --plan /private/rotation-plan.json \
  --evidence /private/rotation.jsonl
```

Для abort plan дополнительно закрепляет `--intent-id`, `--source-revision` и
`--source-digest-sha256`, полученные из readback. Production context и другое
confirmation значение закрыто отклоняются.

# Crash, corruption и совместимость

- Crash до `DELIVERING` допускает exact abort либо повтор того же intent.
- Crash после `DELIVERING` требует resume. Уже записанный Secret сверяется по
  version/digest; несовпадение закрывает publisher readiness.
- Corruption JWK, snapshot, source binding или history не заменяется новой
  генерацией и не переводится в `PROMOTED`.
- Пропущенная registry revision закрыто отвергается до CAS.
- `CURRENT` новой revision всегда был `NEXT` предыдущей полностью
  подтверждённой revision. Поэтому mixed readers принимают его до switch;
  новый `NEXT` не становится signing key до следующей полной ротации.
- Issuer/verifier после рестарта загружают только полностью проверенный exact
  snapshot и собственную immutable projection. Last-known-good действует лишь
  до законного 30-секундного срока authorization context.
- Emergency revoke из #1322 не реализуется и не ослабляется этой state machine.
  Нормальные 40-секундные окна не разрешают fail-open, бессрочное доверие или
  автоматический rollback ключевого состояния.

# Доказательства реализации

PostgreSQL component harness проверяет migration, идемпотентный restart,
запрет promotion до полной доставки/readback, безопасный abort только до
delivery, запрет параллельной/пропущенной revision, bounded overlap и retire.
Go unit/build проверяют publisher и CLI; Node unit проверяет exact Job plan,
readback и закрытые статусы. Live activation, restart под нагрузкой и emergency
revoke остаются отдельными staging-сценариями #1223/#1322 и не считаются PASS
по локальным тестам.


# Восстановление legacy provenance (#1465)

Legacy writer сохранял snapshot digest в intent.source_digest_sha256. Migration
20260909000100 перенесла его в registry_source_digest_sha256, хотя это другая
область хеширования. Forward migration20260912000100 добавляет отдельную
owner-owned таблицу доказательств. Published history, прежние intents, keys,
revocation и replay boundaries не изменяются.

| Этап | Authority и проверка | Effect / повтор |
| --- | --- | --- |
| Подготовка | Root получает прежние raw policy/trust bundle и registry digest из точного сохранённого release evidence | Canonical JSON preimage должен совпасть с immutable publication_input_digest_sha256; несовпадение блокирует repair |
| Plan | Exact staging namespace, source, прежний completed migration UID, текущие registry и publisher spec соответствуют незавершённому rotation plan | Server dry-run Job без mutation |
| Apply | Migrator service account, прежние exact TLS/DSN mounts, bounded Job; fsync intent перед CREATE | Immutable proof ConfigMap и один Job с UID receipt, только additive migrations и repair function |
| Repair | PostgreSQL проверяет session_user migrator, protocol1, exact revision/snapshot и SHA256 полного preimage | Одна append-only provenance row; точный повтор идемпотентен, конфликт закрыт |
| Observe | Exact ConfigMap/Job spec, proof digest и сохранённые UID; неизвестный первый ACK разрешается authoritative readback | Чужой/replaced Job отвергается; новые ключи и новый rotation intent не создаются |
| Продолжение | Existing publisher использует подтверждённую historical registry binding | Resume того же normal rotation operation, обычный forward-only protocol |

Preimage — точные байты JCS объекта с тремя строковыми полями
manifest_bundle, policy, registry_digest_sha256. Proof file — canonical JSON
с sourceRevision, snapshotDigestSHA256, inputPreimage; private0600. Содержимое
не публикуется. Подписанный bundle и policy не являются секретными ключами,
но proof хранится только в закрытой операторской среде и immutable ConfigMap
migrator. Receipt хранит лишь digest, revision, UUID и время, не preimage.

Repo-owned инструмент tools/release/authority-legacy-provenance-repair.mjs:

```bash
node tools/release/authority-legacy-provenance-repair.mjs plan \
  --context default --source /srv/kodex-dev/<EXACT_NEW_SOURCE> --revision <SHA> \
  --rotation-plan /private/rotation-plan.json --source-plan /private/source-plan.json \
  --migration-receipt /private/migration-receipt.json --proof /private/proof.json \
  --output /private/repair-plan.json
node tools/release/authority-legacy-provenance-repair.mjs apply \
  --context default --plan /private/repair-plan.json --evidence /private/repair.jsonl \
  --confirm REPAIR-STAGING-LEGACY-PROVENANCE
node tools/release/authority-legacy-provenance-repair.mjs observe \
  --context default --plan /private/repair-plan.json --evidence /private/repair.jsonl
```

Не повторять apply после UNKNOWN и не менять registry/keys для обхода отказа.
Если authoritative readback не находит созданный Job, требуется отдельное
расследование durable intent; инструмент не создаёт новый Job автоматически.
Первый legacy upgrade требует доказать эту связь до заявления готовности
ротации. Наличие PostgreSQL migration само по себе её не доказывает.
