---
id: OPS-DOC-ARTIFACT-COMMIT-ACK-1889
title: Сохранность артефакта при неизвестном результате COMMIT
type: operations
status: approved
owner: manager
version: 1.0.1
updated: 2026-10-25
---

# Неизвестный COMMIT не разрешает удаление объекта

Issue [#1889](https://github.com/codex-k8s/kodex/issues/1889). PostgreSQL может
сохранить owner state, receipt, audit и события, даже если клиент не получил
ответ COMMIT. Удалять подготовленную exact object version по такой ошибке
нельзя: authoritative artifact уже может ссылаться на неё. Ошибка остаётся
ошибкой; успешный результат нельзя синтезировать. Повтор исходного намерения
читает существующий receipt через прежние authority/idempotency проверки.
Новые SQL retry, API, grants, migrations и фоновые очистители не добавляются.

Пути владельца: native CompleteExecution → проверенный transport principal →
domain service → preflight с canonical lock/receipt и прежними lease/fence/
generation/CLEAN checks → object PUT вне transaction → основной Execute →
artifact/content/usage/receipt/audit/events → COMMIT; обычный UploadArtifact →
owner eligibility и content-bound mutation → antivirus/CLEAN → object PUT →
authoritative artifact/content/receipt/audit/event → COMMIT. Readback и replay
сохраняют прежние tenant, actor, operation, key и intent boundaries.

| Состояние | Объект и результат |
| --- | --- |
| До попытки COMMIT: validation, authorization, begin/query failure | Объект не опубликован данной transaction; прежняя компенсация подготовленной версии сохраняется. |
| Успешный COMMIT | Сохранить принятую exact version; вернуть прежний owner result. |
| COMMIT вернул `pgx.ErrTxCommitRollback` | Подтверждён rollback; сохранить прежний cleanup неопубликованной версии и закрытый error mapping. |
| COMMIT вернул transport/context/иной неизвестный error | Сохранить потенциально опубликованную версию; вернуть прежнюю ошибку, без повторного COMMIT/PUT. `ErrTxClosed` сам по себе rollback не доказывает. |
| Cancel/deadline до либо во время неизвестного COMMIT | Cancel не доказывает отсутствие commit. Cleanup не должен удалять потенциально committed content. |
| Matching replay после неизвестного COMMIT | Fresh authority и прежний receipt; один artifact/content/audit/event, без нового PUT. Terminal root не открывается заново. |
| Иной intent, actor или tenant | Прежний закрытый отказ; чужой receipt не выдаётся, новый эффект не создаётся. |
| Неизвестный COMMIT без фактического сохранения | Возможен orphan, который безопаснее удаления опубликованного тела. Автоматическое удаление без authoritative доказательства отсутствия ownership не добавляется. |

Системные аналоги: `Execute` и `UploadArtifact` используют непосредственный
deferred cleanup и входят в исправление. Assistant proposal/file revision
используют отдельный устойчивый `prepared_content` ledger и атомарное attach;
их staging возвращает пустой список объектов для общего deferred cleanup.
Avatar compensation требует owner reservation и SQL `NOT EXISTS` immutable
artifact/content перед внешним Delete. Purge — самостоятельный owner lifecycle.
Эти механизмы не подменяются эвристикой ошибки COMMIT. Сама objectstorage
библиотека не знает authority БД: решение о Delete принадлежит CP.

`TestCommitAckArtifactComponent` — отдельная suite, не возобновление остановленных
#1836/#1838/#1848. Для COMPLETE используется настоящая ordinary-agent claim и
независимый protobuf decode каждого вызова; UPLOAD проходит публичный domain
service с новым reader. Test-only pgx `BuildFrontend` reader скрывает ровно
CommandComplete(COMMIT) и следующий ReadyForQuery(I), действительно полученные
от disposable PostgreSQL, после successful exact receipt INSERT. Он возвращает
EOF либо отмену caller только после этого; production interface не меняется.
Независимый pool читает committed state и exact content pins, а owner download
проверяет исходные bytes. Replay и отрицательные intent/tenant проверки не
повторяют PUT. Known rollback получается из намеренной ошибки только synthetic
transaction до COMMIT и настоящего server CommandComplete(ROLLBACK).

Вся suite имеет общий бюджет90s; canonical harness — внешний предел360s.
Wire unit заранее проверяет framing, явное arming, отсутствие изменений обычных
ответов, malformed/oversized frames и обязательный idle proof. Expected RED
до фактического PG не называется подтверждённым RED. No-DSN SKIP не является
component PASS. Тесты #1881 не меняются; их lost client reply после успешного
Execute не является потерей DB COMMIT ACK. Context7 недоступен (`Invalid API
key`); проверены официальный [pgx Tx contract](https://pkg.go.dev/github.com/jackc/pgx/v5@v5.10.0#Tx)
и закреплённый исходник `pgx/v5@v5.10.0/tx.go`.

RED подтверждён на test-only `5de17d527`: четыре неизвестных исхода COMMIT
сохранили DB cardinalities1, но Delete1 уничтожил exact bytes; server COMMIT/idle
действительно получен wrapper. Два known rollback сохранили counts0 и Delete1.
Исправление меняет только два final owner Commit-error branch: компенсация
подавляется, если ошибка не является `pgx.ErrTxCommitRollback`. Replay ветки
не меняются: новый проигравший prepared object не входит в прежний receipt.
Неизменённая suite должна подтвердить GREEN на следующем отдельном checkpoint.

Ручная проверка после разрешённой поставки: создать маленький CLEAN файл
обычным сотрудником и загрузкой владельца; проверить download, digest и
неизменность истории при matching replay. Fault injection в живую БД не входит
в проверку. Disposable PG, unit и review не доказывают live/full PDLC.
Rollback к предыдущему CP image не меняет схему, но возвращает известный риск
удаления опубликованных bytes. Секреты, DSN и произвольные error texts не
попадают в диагностические результаты теста.
