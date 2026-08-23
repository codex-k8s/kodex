---
id: GO-LIB-CREDENTIALFS-001
title: Безопасное чтение material подключений
type: component
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-23
---

# credentialfs

Библиотека проверяет абсолютный root, bounded reference и filename, разрешает
symlink только внутри root и читает один regular file с точным read-only
режимом `0400`, `0440` либо `0444` и ограниченным размером. Режим `0444`
безопасен только внутри явно ограниченного read-only mount namespace целевого
контейнера. Библиотека не знает формат credential, не кэширует значение и не
логирует path либо содержимое. Вызвавший adapter обязан очистить возвращённый
буфер после создания точного provider client.
