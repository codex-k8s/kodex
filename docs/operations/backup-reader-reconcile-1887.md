---
id: OPS-DOC-BACKUP-READER-1887
title: Адресное восстановление назначенной backup reader identity
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-10-25
---

# Адресный repair backup reader

Issue [#1887](https://github.com/codex-k8s/kodex/issues/1887), QA
[#1830](https://github.com/codex-k8s/kodex/issues/1830). Действующий Secret может
не совпадать с паролем уже существующей PostgreSQL роли. Ошибка аутентификации
не разрешает исключить БД из backup либо запустить общую ротацию прикладных ролей.

В текущем QA подтверждена более узкая причина: файл, созданный `openssl rand -hex`,
содержит завершающий LF. Штатный `deploy-local.sh` удаляет его при проекции пароля,
а прежний scoped backup drill переносил LF в JSON. Теперь оба пути одинаково
удаляют один завершающий LF только у PostgreSQL password. Общий decoder и S3 keys
не меняются. Сначала пересоздаётся собственная backup custody и проверяется backup;
рассогласование пароля самой БД пока не доказано. Адресный repair ниже применяется
только при независимо подтверждённой необходимости.

Оператор `tools/dev/scoped_backup_reader.py` предназначен только для проверенного
локального staging `k3d-kodex`. Он использует существующий reconciler в закрытом
`KODEX_RUNTIME_CREDENTIAL_SCOPE=backup-reader`. Обычный deployment сохраняет
прежний режим `all` и профили2/22 ролей. Адресный режим не создаёт роли и не
меняет их flags/членство: до записи проверяется существующая read-only backup
identity с каноническим BYPASSRLS. Проверяется наличие обеих заявленных БД и
Goose metadata до первой смены пароля. Затем меняются только пароль этой роли
на уже назначенное значение и канонические SELECT/sequence/default grants.

Материализация проходит через отдельный immutable Secret с двумя необходимыми
ключами: существующий PostgreSQL administrator нужен только операторскому Job,
назначенный backup reader — для адресного ALTER и собственного auth readback.
Значения не сохраняются на хост, не входят в argv/план/отчёт/пользовательский
prompt; модельным сотрудникам эти реквизиты не выдаются. Исходные Secrets не
меняются. Job использует verify-full/exact SNI/CA, pinned PostgreSQL image,
read-only mounts, noSAToken, штатную точную backup NetworkPolicy и backoff0.

| Переход | Условие и результат |
| --- | --- |
| plan | Только metadata read: exact cluster/ns/StatefulSet/source Secret/CA UID-RV и hashes штатного script/template. |
| apply preflight | Совпали plan hash и все текущие pins; отсутствуют активные backup consumers и одноимённые target resources. |
| custody | Прочитаны только два именованных ключа, повторно сверены source pins; fsync INTENT предшествует create. Immutable custody перечитывается в памяти; в журнал попадают лишь UID и digest. |
| dispatch | Создаются один immutable ConfigMap script и один bounded Job. Перед Job source pins и отсутствие активного backup проверяются ещё раз. |
| unknown create/timeout | INTENT запрещает повтор apply. Только metadata/UID/hash/status readback; нельзя создавать новую попытку, пока исход не установлен. Частичный набор receipts не объявляется успешным repair. |
| terminal success | Exact Job и inputs не сменились, pin script/template и scope сохранён; Job завершился успешно и подтвердил реальную аутентификацию reader в обеих БД. |
| terminal failure | Не повторять ALTER автоматически. Прочитать закрытые классы отказа; при возможном частичном эффекте проверить authoritative credentials/permissions. |
| cleanup | Только собственные Secret/ConfigMap/Job по receipts/UID через scoped backup drill после terminal всех consumers. |

Команды выполняются из чистого проверенного SHA, после PR/review и разрешения
на соответствующий staging repair. В полном QA такое адресное действие уже
входит в поручение владельца; production этим runbook не разрешён.

```sh
python3 tools/dev/scoped_backup_reader.py plan --run qa1830-reader \
  --output PRIVATE/reader-plan.json
python3 tools/dev/scoped_backup_reader.py apply --plan PRIVATE/reader-plan.json \
  --approved-plan-sha PLAN_SHA256 --output PRIVATE/reader-apply.json
python3 tools/dev/scoped_backup_reader.py readback --plan PRIVATE/reader-plan.json \
  --approved-plan-sha PLAN_SHA256 --receipt PRIVATE/reader-apply.json
```

PRIVATE — owned0700 каталог; создаваемые файлы0600, без symlink/hardlink.
Состояние `JOB_CREATED_NOT_VERIFIED` не равно успеху. При неизвестном исходе
первого create сохраняются `.intent` и уже полученные `.secret/.config/.job`
receipts; недостающий UID выясняется отдельным metadata readback, без повторного
apply. Успешная аутентификация не заменяет последующий полный backup и restore.

Откат к старому неизвестному паролю не выполняется и старое значение не
извлекается. Repair forward-only восстанавливает уже назначенную authoritative
revision; cleanup удаляет только временную проекцию и Job. Source snapshot,
прочие роли/приложения и writers сохраняются.

Проверка `make test-scoped-backup-reader` выполняет настоящий shell через
синтетические CLI и настоящий operator/render через fake Kubernetes transport.
Она доказывает target-only операции, ранний отказ,2/22 full-profile compatibility,
exact projections, stale/active-consumer отказ и отсутствие второго Job после
потери create ACK. Настоящая PostgreSQL аутентификация и backup/restore отмечаются
отдельно только после живого readback.

Readback связывает полный ответ Job с UID из receipt, а логи — с единственным
завершённым Pod, принадлежащим этой Job. После чтения логов UID Job и Pod
проверяются повторно: замена объекта с тем же именем не подтверждает repair.
