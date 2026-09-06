---
id: OPS-VFS-ELIGIBILITY-1046
title: Виртуальные файлы и авторитетная доступность
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-05
---

# Карта MVP-UI-37/61

Вклад в полный CP unit #1046, Epic #1018. VFS path — только адрес проекции;
из него нельзя получать authority или storage key.

| Сценарий | Инициатор, контракт и владелец | Переход, точность и потребитель |
| --- | --- | --- |
| Дерево/поиск | Verified actor → HTTP VFS → ListVFSNodes/SearchVFS → CP RR transaction; текущие catalog_resource_visible/Skill/Memory rules | Read-only; scope/path/query/lifecycle/kind привязаны к cursor; SQL eligibility до LIMIT и count; typed source/ref/version/revision/scan/actions для PWA |
| Одиночный Artifact | Verified actor → GetArtifact → тот же artifact.view и exact target | Read-only; authorityProject и свежие bindings; actions вычисляются по отдельным permission, не legacy role |
| Скачивание/preview | Verified actor → DownloadArtifact → artifact.view + artifact.download в owner transaction | Exact content grant, audit и consume сохраняются; resource-instance role работает без legacy membership, отзыв закрывает доступ |
| Run/graph/event/receipt | Проверенный actor → read model либо повтор команды → текущая batch eligibility Artifact | Скрытые refs и metadata удаляются из ответа; историческая версия не получает actions текущей версии. Sequence/event history не переписываются |
| Множественный выбор | PWA сохраняет exact refs/revisions/version выбранных строк; существующие специализированные Artifact/Skill/Memory commands | Owner повторно проверяет authority до OCC/receipt; каждая строка имеет явный результат, отказ не игнорируется; irreversible purge использует существующий impact |
| Archive/delete/restore/purge | Только соответствующий типизированный owner command | Исторические immutable runtime inputs не меняются; tombstone/read paths и текущие audit/event invariants сохраняются |
| Runtime MCP | RuntimeRevision/lease/grant server context → специальные bounded search/metadata/preview/manifest operations | Только exact разрешённые revisions; ни путь, ни client projectRef не расширяет snapshot. Consumer/controller ещё подлежит реализации и проверке |

Дерево показывает только применимые папки после eligibility. Skills, Memory,
обычные артефакты и immutable run inputs имеют разные типы; отсутствие
полномочий либо недопустимый lifecycle задаёт причину недоступного выбора.
Query/count/page исполняются в одной ограниченной транзакции. События чтения
не создаются: authoritative GET/List являются readback. Runtime MCP audit
не содержит полного query/content, credentials или transcript.

Context7 `/jackc/pgx`: BeginTx, read-only RepeatableRead, StrictNamedArgs,
Close/Rollback и отмена context проверены.

## Проверки checkpoint

Code SHA `43c318b24426da3ae3d0f6fec8e88207f732adce`; следующий commit меняет
только этот документ. На идентичном проверенном code tree локально **PASS**:

- Race: repository/platform, domain/service/platform и transport/grpc;
  полный service vet и build `./cmd/...`.
- SQL boundary, Proto lint/build/clean replay и policy65 codegen.
- Полный disposable PostgreSQL `TestBootstrapComponent`: 22.332s.
  Exact resource role читает и скачивает только разрешённый Artifact;
  после revoke закрываются GET/download/VFS и вложенные Run/Graph/Event/receipt
  refs. Проверены literal search, cursor scope/kinds/lifecycle, metadata,
  active/trash и отказ удаления source file, удерживаемого Skill.

Исторические **FAIL** сохранены: широкое разрешение legacy membership в VFS
показывало Agent без `agent.view` (исправлено до первого PASS19.572s);
новая receipt fixture пропускала canonical ResolvePrincipal и получила
`forbidden` (fixture исправлена без ослабления owner boundary, повтор PASS
22.332s). Логи `vfs-envelope-full-pg.log`, `vfs-final-full-pg.log`,
`vfs-envelope-race.log`, `vfs-final-contracts.log`, `vfs-final-vet.log` и
`vfs-final-build.log` находятся только в приватном локальном evidence-каталоге.

Дополнение иерархии `bfe7dd679f91a820e8c9c0fcd3a5500132f4f1d5` прошло полный
Bootstrap23.953s, race/vet/build и SQL boundary: project entity folders находятся
непосредственно под проектом, Agent knowledge inputs имеют immutable leaves,
Skill bundle раскрывает exact `SKILL.md` и supporting files с deduplicated
вложенными папками. Checkbox/actions соответствуют типу и server eligibility.
Предыдущая попытка narrow PG пропустила prerequisite fixtures и завершилась
FAIL; доказательство получено полным Bootstrap, не ослаблением launch guards.

Это вклад в незавершённый unit. Полный Runtime MCP producer
и consumer, HTTP/SDK/PWA новых полей, live/runtime acceptance — NOT RUN.
Документ не объявляет MVP-UI-37/61 полностью выполненными.

## Объединение в основной unit

Вклад `43c318b24426da3ae3d0f6fec8e88207f732adce` и его ledger
`52a2eb696de832c9909f96a5fe8330e0a36b4282` объединены с CP
`2f5090c18f8134dcfd48413779b6049616f7c61f`. Сохранены resumable Sessions
и текущая ADD_TURN projection событий; resumable catalog использует общий
`readRunWithIncidents`, поэтому также закрывает отозванные Artifact refs.
На объединённом дереве полный Bootstrap PASS22.693s, три package race,
full vet/build, SQL boundary, Proto lint/build/generation и policy65 PASS.
Первый disposable запуск не дошёл до тестов из-за занятого случайного TCP
порта RootlessKit; повтор с новым выделенным портом завершился успешно.
