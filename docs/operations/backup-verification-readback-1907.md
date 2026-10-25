---
id: OPS-DOC-BACKUP-VERIFY-1907
title: Чтение состояния после неизвестного исхода backup verify
type: operations
status: approved
owner: sre
version: 1.1.0
updated: 2026-10-26
---

# Чтение состояния после ошибки verify

Задача [#1907](https://github.com/codex-k8s/kodex/issues/1907), приёмка
[#1830](https://github.com/codex-k8s/kodex/issues/1830). Отдельный verify свежей
копии завершился `S3 operation failed`; перед ним inspect-backup вернул VERIFIED.
Это разные свидетельства. Причина отказа verify пока UNKNOWN. Прежняя
verification могла быть создана самим backup и не доказывает успех нового Job.

`inspect-verification` читает существующую связь manifest/verification, все
дампы, схемы и объекты по точным версиям, а также текущую блокировку до и после
чтения. Команда не вызывает AcquireOperationLock, EnsureVerification, PutJSON,
Release, retention или restore. Она не создаёт новых квитанций в репозитории.

## Карта и жизненный цикл

| Этап | Полномочия, состояние и результат |
| --- | --- |
| Инициатор | Оператор с уже разрешённой repository custody, точным staging context и каноническим backupId |
| CLI / render | Закрытая команда inspect-verification; scoped_backup_drill.py render-readback создаёт только адресный one-shot Job |
| Владелец данных | Прежний S3 repository prefix и immutable manifest/verification; caller не назначает новое состояние копии |
| Блокировка до чтения | Полный current inventory и точная версия; ABSENT только при подтверждённом отсутствии, не при ошибке доступа |
| Проверка данных | Прежние валидаторы связей и checksum всех точных версий; fail-closed при несовпадении |
| Блокировка после чтения | Закрытая проекция должна совпадать с исходной; смена или истечение во время чтения отклоняет весь результат |
| Успех | backup metadata, payloadsVerified=true, lock.state ABSENT/ACTIVE/EXPIRED и digest точной lock receipt; без object keys и operationId |
| Cancel / timeout / failure | Неполный результат не выводится; внешних записей и cleanup нет; сохраняются исходный exit и UID Job |
| Retry | Повторное чтение не выдаёт право повторить verify или удалить lock; каждый такой выбор требует fresh authoritative readback |
| События / grants | Новых событий, leases, grants и фоновых задач нет; stdout конкретного Job — read path результата |

Бюджет — прежний BACKUP_CONTROLLER_BACKUP_TIMEOUT (в QA15минут) и пределы
Job. Чтение использует repository credentials; source PostgreSQL и target
credentials ему не нужны. Readiness, сеть и права хранилища не расширяются.

ACTIVE/EXPIRED не дают права снять блокировку. Даже совпадение operationId
с backupId не доказывало бы владение конкретной попыткой. Снимок ABSENT — факт
момента чтения, а не lease и не гарантия отсутствия будущей конкурентной записи.

## Проверка

Render: `scoped_backup_drill.py render-readback --operation inspect-verification`
с точными run/image/prefix/backup-id/output. Выполнить один create и сохранить
UID; при неизвестном ответе сначала прочитать существующий объект. В логах
разрешены только закрытые поля результата. Исходный FAIL verify не заменяется
успехом диагностического чтения. После него решение о новой попытке verify
принимается отдельно; вся PDLC/DR приёмка не объявляется завершённой.

Регрессия перехватывает PUT/DELETE и доказывает отсутствие попыток записи для
ABSENT, ACTIVE, EXPIRED и повреждённого lock; отдельная проверка отклоняет
неканонический backupId. Наличие этих тестов не означает выполненный live PASS.

Возврат: прежний backup image поддерживает остальные команды; новые Jobs
чтения не запускать с ним. Shared Deployment не обновляется. Повреждённые либо
неизвестные lock вручную не удалять. Секреты в результат не входят.

Через Context7 проверены AWS SDK for Go v2 GetObject VersionId/IfMatch и
требование точных версионных чтений. Новых вызовов SDK не добавлено.

## Диагностика следующего отказа

Ошибка verify содержит закрытый этап: `lock_acquire`, `manifest_load`,
`payload_verify`, `receipt_ensure` либо `lock_release`. Одновременные ошибки
основной операции и освобождения блокировки сохраняются обе. Успех записи
verification при последующей ошибке освобождения не считается успешным verify.

Прежний общий `S3 operation failed` дополнен классом, полученным из типа ошибки
SDK: `access_denied`, `throttled`, `server`, `http_other`, `canceled`,
`deadline`, `network_timeout`, `network` или `unknown`. Исходная цепочка SDK
отбрасывается, не сериализуется и не возвращается через Unwrap. Прежние точные
sentinel для HTTP404/409/412 сохранены. Тексты ответов, request ID, object key,
URL, headers и credential не входят в классификацию. Через Context7 проверена
официальная документация AWS SDK Go v2 по errors.As и типизированным ошибкам.

Диагностика возвращается к прежней границе CLI; дополнительных логов, событий,
меток метрик, повторов, записей, изменений readiness или полномочий нет. Класс
`server` сам по себе не разрешает повтор: перед решением по-прежнему требуется
авторитетное чтение состояния. Историческую причину первой ошибки это изменение
не восстанавливает. Неизвестная причина остаётся UNKNOWN.
