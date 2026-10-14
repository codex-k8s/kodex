---
title: Точная диагностика содержимого continuation
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-10
---

# Read-only диагностика continuation

Issue1797/PR1807: точный owner-authorized исторический RuntimeRevision, не live
claim, не actor authority, не исправление и не разрешение retry. Запуск только
после публикации кода и отдельного разрешения владельца на trusted-dev read.

`runtime-continuation-content-readback.sql` возвращает ровно один JSON document
в READ ONLY REPEATABLE READ transaction с10s statement/1s lock budgets. Все8 pins
передаются из native owner GET: organization, run, revision ref/version/digest,
session, turn, attempt. Отсутствие exacttuple даёт NOT_FOUND без чужих данных.

```bash
node tools/release/runtime-continuation-content-readback.mjs \
  --organization-ref org_EXACT_REF \
  --run-ref run_EXACT_REF \
  --revision-ref rrev_EXACT_REF \
  --revision-version 1 \
  --revision-digest EXACT_64_HEX_DIGEST \
  --session-ref ses_EXACT_REF \
  --turn-ref trn_EXACT_REF \
  --attempt 1
```

Transport только унаследованный разрешённый owner PostgreSQL: существующий
`healthReadbackEnvironment` разрешает exact PG keys и verify-full/socket,
не читает envfiles/Secrets, не передаёт PGOPTIONS и не назначает роль/RLS/GUC.
Скрипт не ищет credentials. SRE может выполнить тот же опубликованный SQL через
разрешённый штатный PostgreSQL transport с `psql -X -qAt --no-password -v
ON_ERROR_STOP=1` и указанными8 quoted variables, без изменения SQL.

Проекция содержит только refs/version/attempt, final role, bytes/codepoints,
trimEmpty/hasNUL/validUTF8 и cardinality/matches own persisted notice content/digest.
Сами content, history, source snapshot, notice digest и credential descriptors
не выходят. Роль вне USER/ASSISTANT/SYSTEM — UNSUPPORTED, не произвольная строка.
JSON schema/keys/pins/cardinality проверяются, extra fields и multiple JSON rows
закрыто отвергаются. Ответ ограничен8KiB, process budget15s; raw psql errors не
выводятся. NOT_FOUND не означает пригодность или успех.

В UTF8 PostgreSQL text/jsonb не представляют NUL, поэтому hasNUL=false — гарантия
типового источника, не поиск литерала `\\u0000`. UTF8flag проверяет database
encoding. char_length соответствует Unicode codepoints, octet_length — байтам;
trimEmpty использует точный Go unicode.IsSpace набор, не regexp locale.

При role=USER, bytes<=65536, codepoints>32768, trimEmpty=false, hasNUL=false,
validUTF8=true, noticeCount=1 и обоих matches=true наблюдение доказывает mismatch
server-owned continuation64KiB vs ordinary USER32768codepoints. Иначе причину не
подменяют гипотезой. Immutable historical failure не меняется. Корректирующие
USER_MESSAGE, truncate/history rewrite и blanket skip continuation запрещены.

Проверка: `node --test tools/release/runtime-continuation-content-readback.test.mjs`.
Синтетические tests не выполняют PostgreSQL или live effects. Проверены Context7
PostgreSQL18 `functions-string` и `datatype-json`: bytes/characters, explicit btrim,
jsonb NUL rejection. Disposable/live proof отмечается отдельно, не как unit PASS.
