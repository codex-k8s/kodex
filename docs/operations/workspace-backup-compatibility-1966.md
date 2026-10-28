---
id: OPS-DOC-WORKSPACE-BACKUP-1966
title: Совместимость workspace migrations и backup reader
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-10-28
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
воспроизведён на `5ac34568108de48677ca4ff50edf3cc385fd3a54`; это red regression,
а не готовность исправления. Применение нового режима на staging пока NOT RUN.
До готовности plan/postcondition нового профиля его нельзя вызывать на живой БД.

Откат не запускает `down` и не переписывает ledger. После terminal readback
восстанавливаются точные backup grants и прежний serving image; уже применённые
forward migrations сохраняются. Резервная копия с010 хранится приватно отдельно
от диагностики; её наличие и проверка оглавления не доказывают restore drill.

Context7 вернул отказ API key; проверена официальная документация PostgreSQL18:
[ALTER DEFAULT PRIVILEGES](https://www.postgresql.org/docs/18/sql-alterdefaultprivileges.html)
и [REVOKE](https://www.postgresql.org/docs/18/sql-revoke.html).
