# Immutable bundle исходников

Общий byte-validation контракт control-plane и integration-gateway для #1859.
Модуль не выдаёт полномочий, не читает Secret, не извлекает архив в filesystem и
не делает Git/network effects. Допуск CLEAN, tenant/actor/grant, Human Gate и
устойчивые квитанции принадлежат CP.

`ParseManifest` принимает canonical JSON версии1, возвращаемый
`CanonicalManifest`: UTF-8, поля `version,total_bytes,files` в данном порядке,
без внешнего whitespace, entries по возрастанию `path`, поля entry
`path,mode,bytes,sha256`. Exact raw digest входит в approval. Duplicate/unknown
JSON keys, trailing JSON, неполные totals и неоднозначные пути закрыто
отклоняются; никакого исправления или пропуска entry нет.

`Open` сверяет полный ZIP с manifest: только regular files, STORE/DEFLATE,
без encryption, directories, symlinks или дополнительных членов. Фактические
bytes ограничены declared entry size+1 и общим бюджетом. На результате можно
строить Git blobs, не исполняя и не записывая пути проекта. `Digest` сохраняет
точные bytes, включая binary и newline.

Пределы typed bundle:256files,1MiB uncompressed,1MiB encoded container,128KiB manifest,
512bytes/path. Они не меняют лимиты существующих content.create/update.
Ошибки не включают содержимое или имена недопустимого архива.

Typed JSON source container версии1 использует `version,manifest_sha256,files`
и entries `path,mode,content_base64` в exact manifest order. Base64 standard,
padded, canonical. Container≤1MiB, decoded files≤1MiB, manifest≤128KiB.
`CanonicalContainer` не является security admission: CP отдельно применяет
штатные format policy и обязательный AV ко всем decoded bytes. Outer JSON
CLEAN не является decoded-file CLEAN. Тела хранятся только в existing S3
versions; PG содержит metadata/pins/scan receipts.
