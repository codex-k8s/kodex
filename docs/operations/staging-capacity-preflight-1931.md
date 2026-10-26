---
id: OPS-DOC-STAGING-CAPACITY-1931
title: Проверка фактической ёмкости staging перед работой
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-10-27
---

# Проверка фактической ёмкости staging перед работой

Задача [#1931](https://github.com/codex-k8s/kodex/issues/1931): не допускать
новые сборки или модельные прогоны на основании одного `df /`, если данные
k3d/containerd и общий Go cache находятся на другой файловой системе.
Сбой доступности, точная причина роста и полнота PDLC оцениваются отдельно.
Этот инструмент ничего не удаляет и не восстанавливает автоматически.

## Узкий профиль и наблюдение

`tools/dev/staging-capacity-preflight.py` обслуживает только context
`k3d-kodex`, namespace `kodex-system` и два узла
`k3d-kodex-server-0`/`k3d-kodex-agent-0`. Обязательны заранее проверенные
UID namespace `kube-system` (cluster identity pin), namespace приложения и
обоих Node. Это pins существующей среды, не присвоение ей полномочий.
Context name сам по себе не доказывает cluster identity.

Один запуск выполняет десять GET: namespace identity до/после, Node identity и
conditions до/после, по одному kubelet `configz` и `stats/summary` каждого узла.
Каждый запрос ограничен 5 s, дочерний процесс — 8 s, вся последовательность —
90 s. Повторов и polling нет. Нужные права чтения выдаёт оператор существующей
identity; инструмент не создаёт Role, grant, Job и не меняет kubeconfig.
Недоступный node proxy или 403 означает FAIL, а не пропуск проверки.

Проверяются свежие (не старше 120 s) `availableBytes`/`capacityBytes`
отдельно для `node.fs` и `node.runtime.imageFs`. Сигналы берутся из фактического
`kubeletconfig`, без подстановки Linux defaults:

```text
recoveryFloor = ceil(evictionHard) + ceil(evictionMinimumReclaim)
availableBytes - budgetBytes > recoveryFloor + reserveBytes
```

Проценты вычисляются от capacity соответствующего FS с округлением вверх.
Отсутствующий ключ minimum reclaim в существующей карте означает штатный 0;
отсутствующая карта или hard signal закрыто отклоняются. Поддерживаются
проценты, целые bytes и положительные decimal/binary SI quantities. Иные
формы отклоняются. Отдельный `containerfs.available` в effective hard policy
не поддерживается этим двух-FS профилем и приводит к FAIL.

Budget и reserve задаются явно в bytes. Reserve не может быть меньше 5 GiB.
Budget покрывает совокупный максимальный рост от всей одновременно разрешённой
работы, включая промежуточные outputs и кэши. Он консервативно вычитается из
каждого FS каждого узла: совпадение capacity не доказывает разные устройства,
а shared filesystem не получает повторного разрешения на тот же запас.
`--budget-bytes 0` допустим только для наблюдения recovery, не как доказательство
допуска положительного объёма будущей работы. Нехватка модели/памяти/inodes и
другие admission-проверки остаются отдельными.

PASS требует Ready=True и DiskPressure=False у обоих узлов. Missing/Unknown,
дубликат signal, повреждённый JSON, замена Node/namespace, недоступная или
устаревшая stat дают FAIL. Отчёт содержит только UID, conditions, числовые
FS-показатели и ограниченные policy-поля; pod/PV contents, credentials, raw
`configz`/summary/stderr не выводятся.

## Действия оператора

Перед сборкой оператор получает pins авторитетным readback именно разрешённой
среды и оценивает будущий budget. Пример формы вызова (значения переменных —
несекретные metadata, не credentials):

```bash
python3 tools/dev/staging-capacity-preflight.py \
  --expected-cluster-uid "$EXPECTED_CLUSTER_UID" \
  --expected-namespace-uid "$EXPECTED_NAMESPACE_UID" \
  --expected-server-node-uid "$EXPECTED_SERVER_NODE_UID" \
  --expected-agent-node-uid "$EXPECTED_AGENT_NODE_UID" \
  --budget-bytes "$FUTURE_WORK_BUDGET_BYTES" \
  --reserve-bytes 5368709120
```

Приватный отчёт сохраняется как новый файл0600 в каталоге0700. Повторное
использование старого PASS после другой записи/очистки или перед новой работой
не допускается. Это preflight snapshot, **не атомарная reservation**: оператор
координирует параллельные работы и повторяет чтение после изменения фактов.
Все существующие owner gates и release readback сохраняются. CLI не подключён
скрытым образом ко всем build entrypoints и сам ничего не запускает.

При FAIL сначала сохраняются safe metadata и статус фактически обслуживаемых
приложений. Для cold reproducible Go outputs применяется существующий
`tools/dev/local-go-build-cache.py`, отдельным разрешённым операторским действием:

```bash
python3 tools/dev/local-go-build-cache.py audit --profile shared \
  --minimum-age-hours 72 --output-plan "$PRIVATE_NEW_AUDIT_PLAN"
# После проверки exact нового плана, его fingerprint и разрешённого объёма:
python3 tools/dev/local-go-build-cache.py prune --profile shared \
  --minimum-age-hours 72 --plan "$PRIVATE_NEW_AUDIT_PLAN" \
  --expected-fingerprint "$VERIFIED_AUDIT_FINGERPRINT"
```

Никаких рекурсивных удалений, `docker system prune`, удаления образов/PV/backups,
модулей или исходников. Existing cleaner сверяет inode/owner/mode/nlink и все
времена каждого файла, ограничивает возраст плана и закрыто отклоняет drift.
Неизвестный результат prune не повторяется вслепую с тем же планом; сначала
readback и новый audit. Выбор отдельного возраста 24 h возможен только через
существующий явный профиль и новый план, а не изменение guard.

После очистки недостаточно пересечь hard threshold. Например, при фактических
5% hard и 10% reclaim recovery target равен 15% capacity, до добавления reserve
и будущего budget. Затем нужно наблюдать штатное снятие DiskPressure kubelet:
`evictionPressureTransitionPeriod` читается из configz (для исследованной среды
5 m), но само ожидание 5 m не гарантирует recovery. После False отдельно
проверяются PostgreSQL/NATS/CP readiness, вход/renew/reconnect и точные старые
файлы/граф. Preflight PASS не заменяет эти проверки и не доказывает availability.

## Воспроизводимая проверка без кластера

```bash
make test-staging-capacity-preflight
```

Stdlib Python fixtures подменяют все subprocess-вызовы. Проверяются фактические
FS вместо host-root, hard+reclaim+reserve+budget и строгая граница, проценты/bytes,
UID drift, pressure/Unknown, stale/missing stats, timeout/403, fixed GET/context
и отсутствие raw metadata в результате. Это unit PASS, не staging acceptance.

Документация Kubernetes проверена через Context7 app connector:
[Node-pressure Eviction](https://kubernetes.io/docs/concepts/scheduling-eviction/node-pressure-eviction/)
и [Summary API](https://kubernetes.io/docs/reference/instrumentation/understand-psi-metrics/).
Исторический отказ другого Context7 route в Issue не переносится на текущую
успешную проверку. Kubelet thresholds, topology, TLS и identity не изменяются;
rollback инструмента не возвращает удалённые cache outputs и не затрагивает PV.
