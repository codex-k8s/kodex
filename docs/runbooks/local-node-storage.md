---
id: RUN-DOC-1797-STORAGE
title: Перенос хранилища локальной ноды Kodex
type: runbook
status: approved
owner: SRE
version: 1.0.0
updated: 2026-10-09
---

# Граница операции

Issue #1797 / Draft PR #1807. Владелец отдельно разрешил перенос полного
хранилища одной server-ноды development-кластера на `/data` с сохранением
данных и образов. Этот runbook не применим к production, другой ноде или
другому volume; новый хост требует отдельного preflight и изменения кода.

`tools/dev/local-node-storage.py` закрепляет exact container ID, volume ID,
`KUBECONFIG=/home/s/.kube/config`, context `k3d-kodex`, источник и назначение.
Произвольные пути, container IDs и команды через аргументы не принимаются.
Переносится только `/var/lib/rancher/k3s` server-ноды; остальные volumes,
agent-нода, Docker daemon и его общесистемные настройки не изменяются.

Назначение: `/data/kodex-dev-storage/server-k3s`. Родитель принадлежит root
и имеет `0700`. Приватный root-owned журнал состояния расположен в
`/var/lib/kodex-dev-storage/server-0-migration.json`; он не содержит содержимого
файлов, credentials или сырых логов. Script выдаёт только закрытые статусы.

## Состояния и инварианты

| Фаза | Данные / допустимый следующий шаг |
| --- | --- |
| AUDITED | Source неизменён; core workloads 0, runtime terminal, target отсутствует |
| PREPARED / NODE_STOPPED | Нода остановлена; source остаётся полной исходной копией |
| COPY_VERIFIED | Offline rsync + checksum dry-run без изменений + syncfs завершены |
| SWITCHING / SWITCHED | Original переименован в точный backup; проверенный bind mount установлен |
| NODE_STARTING | Durable запись до start: original может стать устаревшим |
| VERIFIED | Нода использует exact device/inode назначения; Ready, исходные CRI pins сохранены |
| RETIRED | Разрешено удалить только исходную проверенную копию; актуальные данные остаются на /data |
| REVERSE_COPYING / REVERSE_VERIFIED | После запуска rollback только через новую offline reverse-copy |
| ROLLED_BACK | Нода снова использует root storage; destination copy сохраняется |

`FAIL_OR_UNKNOWN` после эффекта требует изучить фазу root-журнала; apply
никогда не повторяется автоматически. Для прерывания на SWITCHING до первого
старта есть отдельный `resume --expected-sha <CURRENT_SHA>
--expected-fingerprint <ORIGINAL_FINGERPRINT>`: он проверяет остановленную ноду,
точный original inode, installed units/guard и повторяет checksum-only сверку
готовой копии, не повторяя copy/rename. Исходный script SHA остаётся guard pin,
а новый published SHA сохраняется как resumeSourceSha. `systemd-analyze verify`
явно запускается с `--generators=yes`, чтобы видеть fstab-owned `data.mount`.
При неполной reverse-copy скрипт
закрыто останавливается; `/data` не удаляется. Перед первым стартом rollback
возвращает исходный inode. После любого возможного старта возврат к старой
копии запрещён: требуется свободное место на root для полного текущего
volume плюс 20 GiB, offline reverse-copy в новый точный каталог, checksum и
syncfs. При нехватке места текущие данные остаются на `/data`.

## Порядок

1. Проверить текущий чистый SHA и тот же Draft PR; получить отдельный owner YES.
   Убедиться, что core workloads уже quiesced штатным deploy-скриптом.
2. Выполнить `sudo -n python3 tools/dev/local-node-storage.py audit --expected-sha <SHA>`.
   Сохранить fingerprint. Audit проверяет root/data device, отсутствие target,
   эксклюзивность source volume, capacity и исходные durable CRI pins.
3. Запустить тот же опубликованный код:
   `sudo -n python3 tools/dev/local-node-storage.py apply --expected-sha <SHA> --expected-fingerprint <FINGERPRINT>`.
   Остановка касается только exact server-контейнера. После неё проверяются
   отсутствие descendant mounts, fd/cwd/root/mmap references. Копируются
   права/UID/GID, hardlinks, ACL, xattrs, sparse-файлы и symlinks.
   `--delete` используется только в dry-run checksum-проверке; actual copy
   не удаляет source или destination-файлы.
4. `sudo -n python3 tools/dev/local-node-storage.py readback`.
   Нода должна фактически видеть новый device/inode; pinned images не теряются.
   Readback не снимает quiesce и не запускает агентов.
5. Только после VERIFIED выполнить
   `sudo -n python3 tools/dev/local-node-storage.py retire --expected-fingerprint <FINGERPRINT>`.
   Удаляется только exact backup прежнего source, с проверкой inode и отсутствия
   mounts/references. Это удаление избыточной копии, не данных платформы;
   реконструкция возможна офлайн из актуального destination при наличии ёмкости.
6. Проверить обе ноды, DiskPressure, PostgreSQL/PVC и свежий owner ledger.
   Затем fresh clean-source render, canonical supply-chain apply/readback,
   source/Pod/image proof и пользовательские сценарии. Ready не заменяет
   readiness приложения или полный QA.

## Персистентность

Отдельные guard/mount/node systemd units устанавливает только опубликованный
скрипт. Guard требует настоящего `/data` mount, completed-copy phase и exact
destination inode; root-owned immutable копия скрипта сверяется по SHA256.
Mount зависит от guard и `data.mount`; node зависит от Docker и собственного
mount, `BindsTo` останавливает только эту ноду при утрате mount. Контейнерный
restart policy становится `no`, автозапуском этой ноды владеет systemd.
Зависимости общего `docker.service` не меняются. При отсутствии DATA guard
закрыто отклоняет старт, а не создаёт пустое runtime storage.

После реальной перезагрузки обнаружен дефект первоначального guard: Linux
изменил `MAJ:MIN` и `st_dev` DATA, хотя UUID и inode не изменились. Постоянная
проверка теперь закрепляет UUID filesystem, тип ext4, источник/mountpoint и
inode. Current device сравнивается между DATA, target, bind и контейнером,
а исторический номер устройства сохраняется в журнале как evidence.

Для единственного уже установленного guard предусмотрен `repair-boot
--expected-sha <PUBLISHED_CLEAN_SHA> --expected-fingerprint <ORIGINAL_FINGERPRINT>`.
Он допускает только VERIFIED/RETIRED, остановленную exact ноду, неизменные
unit-файлы, исходный guard hash либо записанный pending hash того же нового
кода, exact filesystem/inode и пустой unmounted source либо правильный bind.
Данные не копируются и не удаляются. Pending запись → atomic guard replace →
final journal update закрыто переживают crash; повторять можно только тот же
published source до запуска ноды. После старта проверяется readback, а не
слепой повтор repair. Общий Docker daemon не перезапускается.

Без перезагрузки хоста проверяются unit contents, enable/active, systemd verify
и actual mount/node readback. Проверка реальной перезагрузкой — отдельный
owner gate; её отсутствие не выдаётся за reboot PASS.

Rollback запускается тем же скриптом с режимом `rollback` и исходным fingerprint.
Он сохраняет DATA-копию и не удаляет чужие units; точное содержимое собственных
units проверяется до их удаления. После первого старта обратная копия обязательна.

## Проверки

Публичная герметичная точка входа:
`python3 tools/dev/test-local-node-storage.py`.
Она использует mocks и одноразовые файлы, не live volume, Docker или Kubernetes.
Context7 проверен для `/systemd/systemd` и `/rsyncproject/rsync`:
зависимости mount/service и shutdown ordering; отдельные `H/A/X`, dry-run и
checksum. Необходимые инструменты уже установлены; установка k3s не выполняется.
