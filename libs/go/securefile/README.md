---
id: GO-LIB-SECUREFILE-001
title: Безопасные projected files и owned entries
type: component
status: approved
owner: architect
version: 1.2.0
updated: 2026-10-27
---

# securefile

Библиотека читает bounded regular files из read-only Kubernetes projection и
других явно ограниченных mount boundary. Допустимы только точные режимы
`0400`, `0440` и `0444`: владелец всегда имеет право чтения, а права записи и
исполнения отсутствуют у всех.

`Read` ограничивает symlink каталогом исходного mount path. `ReadWithin`
разрешает symlink только внутри переданного root. Ошибки не содержат путь или
содержимое файла. Библиотека не знает формат credential, не кэширует значение и
не логирует его; вызывающий код обязан очистить возвращенный буфер после
создания provider client, если это возможно.


## Owned records на Linux

Отдельные `ReadOwnedRecordAt` и `WriteOwnedRecordAt` обслуживают небольшие
неизменяемые metadata records через уже разрешённый descriptor private parent.
Они не определяют tenant, lease, generation, JSON-схему или полномочия записи.
Caller сначала доказывает этот контекст, сохраняет владение parent descriptor
и передаёт UID/GID только из доверенной process policy, не из payload.

Parent принадлежит ожидаемому UID и имеет ровно0700; его group не даёт доступа.
Имя — один ASCII component, без slash/dot traversal. Record имеет точные
UID/GID, mode0440, один regular inode/link и тот же device. Размер положительный,
не больше лимита caller и общего1MiB. Чтение не следует symlink, не блокируется
на FIFO, сверяет metadata до/после и отдельно возвращает ENOENT. Write разрешён
только текущему UID, использует O_EXCL, проверяет фактический mode, fsync файла
и parent, close и readback bytes. EEXIST не разрешает overwrite. Partial и
неопределённые записи не удаляются, не переименовываются и не принимаются как
готовое состояние; решение о read-only reconciliation принадлежит caller.

Read не доказывает предшествующий durable commit. Caller восстановления
повторно проверяет публикацию и необходимые durability barriers до owner ACK.
Каждая операция требует живого caller context; библиотека не создаёт скрытый
background context, не хранит state и не логирует private values/paths.
Прежние API чтения projected credentials сохраняют свои symlink/mode contracts;
новый owned-record API не является fallback для service-account token.

## Owned directories на Linux

`OpenOwnedDirectoryAt` открывает либо явно создаёт один component под уже
разрешённым parent descriptor. Допустимы только private0700 и shared2770;
UID назначает trusted process policy, shared directory требует exact GID.
Private group не даёт доступа и может отличаться. Parent mount/custody и его
access policy доказывает caller до вызова; helper сам не выдаёт authority.

`openat2` запрещает symlink и любой mount crossing, включая bind mount на том
же device. Новое создание использует mkdir, actual owner/mode readback и
fsync(child/parent); существующие entries не chmod/chown и не очищаются.
Read-only open отсутствующего directory возвращает ENOENT без create.
Returned FD имеет CLOEXEC и принадлежит caller; parent остаётся открыт.
При ошибке helper закрывает только свой FD, сохраняя даже partial directory.
Живой context обязателен. Linux syscall adapter использует уже закреплённый
сервисами `golang.org/x/sys v0.47.0`; внешний API библиотека не вызывает.
