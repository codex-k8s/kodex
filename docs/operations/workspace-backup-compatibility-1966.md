---
id: OPS-DOC-WORKSPACE-BACKUP-1966
title: Совместимость workspace migrations и backup reader
type: operations
status: approved
owner: sre
version: 1.0.1
updated: 2026-10-29
---

# Совместимость workspace migrations и backup reader

В #1966 фактическое обновление staging с010 завершило011–015 и остановилось
на016: штатный backup reconciler выдал новой таблице SELECT роли
`kodex_backup_reader`, а frozen guard015 допускает только owner/runtime.
Прежний control-plane восстановлен до двух Ready-реплик. Исторические миграции
и проверки не редактируются. Новый compatibility path является отдельной
операцией maintenance, а обычный `up` сохраняет закрытый отказ.

| Переход | Полномочия и эффект | Подтверждение и отказ |
| --- | --- | --- |
| Оператор → immutable migration Job | Exact staging cluster/namespace, source/image digest, проверенный backup и idle owner; прежние CP Pod положительно завершены. Только существующий migrator ServiceAccount и migration DSN | Private plan/journal; source/UID drift, работающие claims либо backup consumers останавливают путь |
| CLI → normalization | Только `session_user=control_plane_migrator`, database `control_plane`, canonical backup role, exact015 и пустой workspace. SERIALIZABLE owner transaction снимает только backup SELECT с трёх точных таблиц и schema-scoped default | Неизвестные роли, DML/grant option/column ACL и изменённый catalog вызывают rollback вместе с REVOKE; grant других ролей не меняется |
| Normalization → Goose | Только explicit `up-workspace-backup-compatible`, один вход в Goose до exact028; все прежние migrations исполняются неизменёнными | Ошибка либо неизвестный normalization commit запрещает запуск Goose. Blind retry отсутствует |
| Goose terminal → restoration | После подтверждённой normalization восстановление получает отдельный bounded context даже при отмене caller. Проверяются actual015/016/017/017+SSH/028 и frozen catalog соответствующей версии | Одна owner transaction восстанавливает только canonical backup SELECT на существующих custody-таблицах и прежний default. Неизвестный head либо неверный catalog закрыто отклоняется |
| Прерывание процесса | `restore-workspace-backup-acl` использует ту же закрытую проверку head/catalog и не запускает Goose | Сначала authoritative outcome/terminal readback. Неизвестный commit не считается rollback или успехом; новый execution grant не возникает |
| Возврат обслуживания | До resume должны быть подтверждены terminal migrator, восстановленные backup ACL, новая schema proof и ready serving replicas | Частичная migration не выдаётся за полный PASS. Backup controller и application rollout имеют отдельные readback |

Это DDL/DCL обслуживающего CLI, а не CRUD пользовательского проекта: event для
бизнес-данных не создаётся; авторитетные read paths — Goose ledger, actual
PostgreSQL catalog, Job/Pod UID и приватный журнал оператора. Пароли, credentials,
membership и настройки соседних приложений не меняются. Typed command не
принимает SQL, роль, схему или таблицы от caller.

Проверки: `make test-workspace-backup-upgrade-postgres` использует только
ограниченный disposable PostgreSQL со штатным bootstrap. Исходный отказ
воспроизведён на `5ac34568108de48677ca4ff50edf3cc385fd3a54`. На `4d085b9e8`
прошли настоящее обновление015→028, отказ лишних прав и отмена с независимым
cleanup; отдельно прошли55 CLI unit и73 проверки миграционного оператора.

Штатные OCI build/publication/check и migration Job профиля `workspace-backup`
на staging прошли с образом `4d085b9e89539c9db8cafe28b22261ca228e4a8c`.
Полный actual postcondition подтвердил028 и восстановленные backup ACL.
Первый resume выявил синтаксическую ошибку в отдельном минимальном SQL readback;
RESUME_INTENT тогда отсутствовал. Исправление `2a689563f` использует полный
pinned postcondition успешной Job перед resume. После этого возврат двух
Ready-реплик подтверждён. В `3d96895b1` исправлен и минимальный запрос: он
теперь исполняется native на015 и028. Проверки прошли4/0, без пропусков.

Адресное обновление serving control-plane на тот же immutable image прошло;
bootstrap, история146 запусков и каталог402 артефактов QA-проекта доступны.
Это подтверждает metadata readback после перезапуска. Байты всех402 файлов,
hard-kill в окне ACL maintenance, полная fresh installation и полный PDLC
этими результатами не принимаются. Остальной source cohort обновляется отдельно.

Откат не запускает `down` и не переписывает ledger. После terminal readback
восстанавливаются точные backup grants и прежний serving image; уже применённые
forward migrations сохраняются. Резервная копия с010 хранится приватно отдельно
от диагностики; её наличие и проверка оглавления не доказывают restore drill.

Context7 вернул отказ API key; проверена официальная документация PostgreSQL18:
[ALTER DEFAULT PRIVILEGES](https://www.postgresql.org/docs/18/sql-alterdefaultprivileges.html)
и [REVOKE](https://www.postgresql.org/docs/18/sql-revoke.html).
