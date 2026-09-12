---
id: OPS-DOC-1434
title: Точечная поставка source-профиля internal RPC authority
status: approved
type: operation-evidence
owner: developer
version: 1.1.2
updated: 2026-09-12
---

# Граница

Документ закрывает repo-owned prerequisite [#1434](https://github.com/codex-k8s/kodex/issues/1434)
для staging hot-reload. Он не меняет immutable installation profile: portable
профиль по-прежнему использует digest-pinned image и встроенные publisher/CLI.
Source-профиль допускается только для уже установленного staging с exact
`golang:1.26.6` image, `/workspace/tools/dev/run-go-hot-reload.sh` publisher и
локальными readonly module/sumdb/tool caches. Network fallback отсутствует.

# Lifecycle

| Фаза | Immutable input | Mutation | Readback и recovery |
| --- | --- | --- | --- |
| `PLAN` | clean source SHA; namespace UID; полный publisher spec/UID/RV; actual rendered migration Job; registry и соседние Deployment fingerprints; текущий `/proc/PID/exe`; target capability | Нет | Source, process, cache mounts, migrator SA/NetworkPolicy и server-side admission dry-run перечитываются до private plan; dry-run UID не становится live receipt |
| `MIGRATION_INTENT` | Plan hash и unique Job name; receipt отсутствует | Fsync evidence и immutable private intent marker, затем `CREATE` только нового additive migration Job | Marker запрещает повторный CREATE с другим evidence path даже после исчезновения Job; успешный CREATE response атомарно и с fsync публикует private receipt, а потерянный ACK даёт `UNKNOWN` |
| `MIGRATION_OBSERVED` | Exact name/spec/intent, durable marker и отсутствующий receipt | Только явный `observe-migration` может атомарно закрепить UID первого live readback после `UNKNOWN` | Конкурентные наблюдатели используют create-once publication: проигравший принимает только byte-identical receipt; foreign/corrupt receipt закрыто отвергается |
| `MIGRATION_SUCCEEDED` | Receipt связывает plan hash, intent, exact Job UID и semantic spec; тот же UID terminal `Succeeded` | Нет | `Failed`, missing Job, replacement UID до или после terminal, spec drift либо повреждение receipt не разрешают publisher update |
| `PUBLISHER_INTENT` | Успешная migration и её exact receipt; неизменные registry/соседи; exact publisher predecessor | Fsync `INTENT`, затем UID/RV/full-spec CAS только `dev-source` hostPath и source annotations | `observe-publisher` принимает только тот же migration UID, target publisher spec, стабильный owner Pod и target executable digest |
| `ROLLBACK` | Текущий spec равен exact planned target, ротация ещё не начиналась | Fsync `INTENT`; publisher-only CAS возвращает прежний source и прежние annotations | Старый executable digest обязан совпасть; migration остаётся forward-only |

Оператор не удаляет Job и не меняет caches, TTL, credentials, trust, sidecars,
соседние workloads или глобальный профиль. После начала registry rotation
application rollback запрещён: дальнейшее восстановление следует forward-only
state machine `OPS-DOC-1390`.

Каталог receipt заранее создаётся private (`0700`, тот же UID процесса), а
immutable intent marker и receipt остаются regular `0600` файлами и доступны
тому же operator process при recovery. Удаление marker для повторного CREATE не
является recovery-путём.

Source plan строится из actual результата `tools/dev/render-local.sh`, а не из
несуществующей live migration Job. Из render извлекается только
`internal-rpc-authority-migrate`; инструмент требует canonical
`run-go-command.sh`, source/mod/sumdb/tools mounts, отдельный writable build
cache, migrator credentials/CA, SA и exact NetworkPolicy. Секретные значения не
читаются и не записываются в evidence.

Kubernetes readback может опустить пустой `spec.ingress` у migrator
NetworkPolicy. Plan нормализует только этот отсутствующий deny-ingress список к
`[]` при сравнении с exact render; `null`, правило, неизвестное поле, selector,
destination или port закрыто отклоняются.
Для exact ingress-only `internal-rpc-authority-postgresql-from-migrator`
renderer добавляет пустой `egress: []`, который API также опускает. Только это
пустое поле при неизменном `policyTypes: [Ingress]` нормализуется к отсутствию.
Непустой egress, `null`, иной policyTypes и неизвестные поля остаются отказом.

```bash
node tools/release/authority-rotation-source-capability.mjs \
  --source /srv/kodex-dev/<EXACT_SOURCE> --revision <EXACT_SHA> \
  --output /private/authority-source-capability.json

node tools/release/authority-rotation-source-delivery.mjs plan \
  --context "$KODEX_RELEASE_CONTEXT" \
  --source /srv/kodex-dev/<EXACT_SOURCE> --revision <EXACT_SHA> \
  --render /private/actual-render.yaml \
  --capability /private/authority-source-capability.json \
  --output /private/authority-source-plan.json

node tools/release/authority-rotation-source-delivery.mjs apply-migration \
  --context "$KODEX_RELEASE_CONTEXT" --plan /private/authority-source-plan.json \
  --migration-receipt /private/authority-source-migration-receipt.json \
  --evidence /private/authority-source-migration.jsonl \
  --confirm APPLY-STAGING-AUTHORITY-SOURCE-MIGRATION

node tools/release/authority-rotation-source-delivery.mjs observe-migration \
  --context "$KODEX_RELEASE_CONTEXT" --plan /private/authority-source-plan.json \
  --migration-receipt /private/authority-source-migration-receipt.json

node tools/release/authority-rotation-source-delivery.mjs apply-publisher \
  --context "$KODEX_RELEASE_CONTEXT" --plan /private/authority-source-plan.json \
  --migration-receipt /private/authority-source-migration-receipt.json \
  --evidence /private/authority-source-publisher.jsonl \
  --confirm APPLY-STAGING-AUTHORITY-PUBLISHER-SOURCE

node tools/release/authority-rotation-source-delivery.mjs observe-publisher \
  --context "$KODEX_RELEASE_CONTEXT" --plan /private/authority-source-plan.json \
  --migration-receipt /private/authority-source-migration-receipt.json

# Только до начала ротации и только для exact planned publisher target.
node tools/release/authority-rotation-source-delivery.mjs rollback \
  --context "$KODEX_RELEASE_CONTEXT" --plan /private/authority-source-plan.json \
  --migration-receipt /private/authority-source-migration-receipt.json \
  --evidence /private/authority-source-rollback.jsonl \
  --confirm ROLLBACK-STAGING-AUTHORITY-PUBLISHER-SOURCE
```

# Совместная публикация registry и policy

Source rollout не публикует `authority-policy.json`. Только последующий
rotation plan атомарно заменяет оба data key одного
`internal-rpc-authority-publisher-target-registry` ConfigMap:

- новый registry выводится из exact live predecessor и canonical base с 16
  `(workload_id, role)` targets; `source_revision` увеличивается ровно на один,
  `interaction-gateway` не добавляется;
- policy берётся из того же exact source: revision 77, authority ABI 2, SHA-256
  `763028a7176c8c3394d0a01686b8d66a3a7cc465af90c2480a06064816b5e504`;
- binding `platform.command.integration-definitions.create-draft` имеет
  `project_required=false` и `request_profile.resource=FORBIDDEN`.

ConfigMap CAS закрепляет digest прежних и desired bytes. Затем transition
обязан завершить publisher-only restart до создания watch Job. Между CAS и
restart старый процесс может увидеть обновлённый projected policy при ещё
кэшированном registry; такой publish закрыто отклоняется. `resume` перечитывает
тот же ConfigMap и доводит bounded restart до стабильного exact executable; он
не создаёт новую registry revision или owner operation.
