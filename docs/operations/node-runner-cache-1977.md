---
id: OPS-NODE-RUNNER-CACHE-1977
title: Адресная очистка воспроизводимого runner cache узлов
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-11-17
---

# Граница операции

Issue #1977, связанная доступность личного контура #2145. Инструмент
`tools/dev/node-runner-cache.py` обслуживает только два узла `k3d-kodex`.
Оператор передаёт kubeconfig/context, ожидаемый cluster UID и exact helper
`tools/dev/installation-runtime` с SHA256 и clean VCS provenance того же source.
Данные, PVC, registry objects и контейнеры в удаление не входят.

| Шаг | Проверки | Эффект |
| --- | --- | --- |
| Plan | Cluster/node UID, Docker container ID/cluster label, текущие workload refs и все aliases | Private план и временный проверенный helper; без image deletion |
| Export | Exact ref из плана, manifest/config/layer graph linux/amd64 | Новый owned0600 OCI, fsync файла/каталога и hash каждого blob |
| Apply | Тот же source/helper/target, план не старше2h, все archives проверены | Свежий inventory перед image, exact target deletion и durable intent/ABSENT journal |
| Lost ACK | Нет положительного ABSENT | Остановка, readback и новый plan; blind replay запрещён |

Helper сохраняет образы существующих контейнеров. Отдельно защищены refs
нетерминальных Pods, Deployment/StatefulSet/DaemonSet, Jobs и
CronJobs, включая init containers. Неизвестный alias сохраняет всю группу.
Атомарность Kubernetes create Pod и удаления image name не обещается:
containerd leases защищают занятый content; новый конкурентный consumer требует
обычного восстановления из registry/сохранённого archive.

Каждый режим получает `--kubectl`, `--kubeconfig`, `--context`, `--cluster-uid`,
`--helper`, `--helper-sha256`, `--plan`, `--archives`, `--journal`. После plan
оператор проверяет private bytes/SHA256; export/apply получают этот hash через
`--plan-sha256`. Каталог archives создаётся заранее с правами0700 и запасом
диска. Частичный archive не перезаписывается и не объявляется годным.

Возврат кеша: на выбранном узле выполнить `ctr -n k8s.io images import
--platform linux/amd64 --digests <archive>`, сверить manifest digest и вернуть
необходимые exact aliases из плана через `ctr images tag`. Это не rollback БД
и не supply-chain admission. Archives/journal сохраняются до installed приёмки;
global prune отсутствует.

`make test-node-runner-cache` использует synthetic OCI/metadata. Live export,
restore и DiskPressure recovery проверяются отдельно; unit PASS их не заменяет.
Context7 `/containerd/containerd/v2.2.1` подтверждает модель OCI descriptors;
flags дополнительно сверены с actual `ctr images export --help` containerd2.2.3.
Multi-platform index закрыто отклоняется: принимается полный linux/amd64 manifest.
