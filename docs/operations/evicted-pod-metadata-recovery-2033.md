---
id: OPS-DOC-EVICTED-METADATA-2033
title: Восстановление metadata Evicted Pods перед immutable migration
type: operation-guide
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-14
---

Refs [#2033](https://github.com/codex-k8s/kodex/issues/2033),
[#2139](https://github.com/codex-k8s/kodex/issues/2139).

`tools/release/evicted-pod-metadata-recovery.py` обслуживает только явно выбранный
`k3d-kodex`, namespace `kodex-system` и Deployment `control-plane`. Это отдельная
operator recovery для старых Evicted объектов, у которых отсутствуют все
container/init/ephemeral status arrays. Три уже доказанно остановленных Pod
не становятся кандидатами. Readiness и `isProvenStoppedPod` не изменяются.

План разрешает Deployment через cluster/namespace/installation UID и data hash
существующего portable target. Полная namespace metadata всех ReplicaSets/Pods
связывает controller lineage, включая изменённые labels; посторонний Pod без
owner references пропускается. CP должен сохранять исходный spec и три здоровые
реплики. План содержит private full preimage выбранных Pods и ReplicaSets,
Node UID, полный Docker container ID и exact owner lineage. Другие workloads,
DB, PVC, Secrets и business history не изменяются.

Перед каждым удалением helper заново проверяет scope/lineage и два полных
CRI `pods`/`ps -a` плюс containerd `tasks` snapshots на обоих зарегистрированных
локальных k3d nodes. Отсутствие любого target UID обязательно; неизвестный task,
потерянный sandbox, неполные labels либо другая node/container identity
закрывают доказательство. `--absence-attempts 2|3` явно разрешает ограниченное
повторное чтение после неполного snapshot; успех требует двух полностью
проверенных новых snapshots. Orphan не игнорируется и не помещается в allowlist.

Private plan, journal и cache находятся вне source checkout в owned0700
каталоге; JSON-файлы owned0600, regular, без symlink/hardlink. Plan закрепляет
единственный evidence path и clean executor path/SHA. Каждый durable INTENT
содержит сам bounded proof: Node UID, snapshot hashes/counts, target UID set,
attempt и время. Proof hash и свежесть до10секунд проверяются до эффекта;
fsync предшествует запросу. Сырые CRI environment/config не сохраняются.

Единственный mutation — generated exact Pod DELETE с
[UID/resourceVersion preconditions](https://kubernetes.io/docs/reference/kubernetes-api/definitions/preconditions-v1-meta/)
и `propagationPolicy=Orphan`. Нет collection delete, force, grace0, снятия
finalizers или создания Pod. Kubeconfig передаётся только явным path flag;
содержимое и Secret values не читаются/не выводятся. Child stdin/stdout/stderr
участвуют в nonblocking selector и общем deadline, включая child, не читающий
stdin. Процессы завершаются/join в конечном бюджете180секунд.

| Переход | Авторитетная проверка | Результат/восстановление |
| --- | --- | --- |
| plan | Actual target, owner lineage, exact count, два native snapshots | Immutable preimage и plan hash; эффектов нет |
| apply/BEFORE | Тот же source/target/spec, свежие UID/RV и native proof | INTENT/fsync, один адресный DELETE |
| UNKNOWN | Durable intent сохранён, mutation не повторяется | Отдельный status; свежий BEFORE допускает тот же UID/RV retry |
| DELETING | Тот же UID, terminal Evicted state, наш intent | Только наблюдение; finalizers/grace не обходятся |
| ABSENT | Отсутствие exact name после нашего intent | Receipt; другой UID не считается удалённым target |
| COMPLETE | Все запланированные UID ABSENT, scope неизменен | Нет новых effects; private preimage остаётся audit history |
| drift/corrupt journal | Fail closed | Сохранить файлы; нет автоматического truncate/нового journal или Pod restore |

Доменные события не публикуются. Kubernetes object readback и fsynced operator
journal — авторитетный read path результата этого maintenance command; они не
подменяют DB owner, migration receipts, authority rotation или installed turn.
При исключении после INTENT `status` разрешает исход только того же плана.
Отсутствие без нашего intent и подмена name новым UID закрыто отклоняются.

```bash
python3 tools/release/evicted-pod-metadata-recovery.py plan \
  --context k3d-kodex --kubeconfig /private/operator-kubeconfig \
  --target /private/target.json --expected-count 12 \
  --plan /private/recovery-plan.json --evidence /private/recovery.jsonl

python3 tools/release/evicted-pod-metadata-recovery.py apply \
  --context k3d-kodex --kubeconfig /private/operator-kubeconfig \
  --plan /private/recovery-plan.json --evidence /private/recovery.jsonl \
  --approved-plan-sha EXACT_SHA_FROM_PLAN_OUTPUT

python3 tools/release/evicted-pod-metadata-recovery.py status \
  --context k3d-kodex --kubeconfig /private/operator-kubeconfig \
  --plan /private/recovery-plan.json --evidence /private/recovery.jsonl
```

Ручная проверка: прочитать все выбранные Pod name/UID/RV из private plan;
подтвердить expected count и отсутствие трёх proven-stopped объектов в этом
наборе. После COMPLETE повторить прежний immutable migration check тем же
исходным journal/executor. Исторический preimage не восстанавливается созданием
Pod: новый UID мог бы запустить дополнительного writer.

Публичная герметичная проверка — `make test-evicted-pod-metadata-recovery`.
Она исполняет настоящий CLI с disposable backend, включая UID/RV/lineage drift,
source mismatch, orphan/runtime presence, lost ACK, status-before-retry,
standalone namespace Pod, durable proof и не читающий stdin child. Synthetic
PASS не означает live absence/deletion. Проверенные через Context7 Kubernetes
API Preconditions и DeleteOptions определяют UID/RV и Orphan semantics;
актуальное состояние каждого живого target проверяется отдельно.
