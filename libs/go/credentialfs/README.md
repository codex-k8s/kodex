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
symlink только внутри root и читает один regular file с закрытыми правами и
ограниченным размером. Она не знает формат credential, не кэширует значение и
не логирует path либо содержимое. Вызвавший adapter обязан очистить возвращённый
буфер после создания точного provider client.
