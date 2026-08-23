---
id: GO-LIB-SECUREFILE-001
title: Безопасное чтение projected files
type: component
status: approved
owner: architect
version: 1.0.0
updated: 2026-08-23
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
