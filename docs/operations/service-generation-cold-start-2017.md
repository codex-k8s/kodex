# Cold-start служебного поколения — #2017

Подзадача #1977, совместимое исправление ordinary service-v1 из #1470.
Отделение optional proof dependencies от startup (#1528) остаётся самостоятельным.

На новой мигрированной базе уже существует server-owned регистрация разрешённых
workloads. Старый protected lookup требовал запись принятого signed grant, поэтому
первый service-v1 RPC не мог разрешить generation. Reader теперь читает включённую
регистрацию и `GREATEST(registration, accepted signed floor)`. Это не fallback на
default1: unknown, удалённая либо disabled registration не возвращает строки.
Историческое имя `trusted_workload_generations` сохраняется ради совместимости;
сама таблица не выдаёт identity либо разрешения метода и не включает trusted-cluster.

| Путь | Источник полномочий → владелец | Состояние/результат | Отказ/replay |
|---|---|---|---|
| Ordinary RPC → service reader | mTLS peer + exact SPIFFE/method policy → owner generation | Read-only enabled registration и больший signed floor; затем system actor/tenant и domain checks | Unknown/disabled → Forbidden, request не выбирает generation/profile |
| Signed proof → verified worker grant | Проверенная подпись, issuer/audience/workload/instance/time → owner acceptance | В одной native transaction продвигаются workload floor и per-instance watermark | Ниже registered/accepted floor, disabled, expired, чужой/изменённый envelope → closed failure |
| Restart / profile composition | Те же durable owner rows и явный transport selection | Одинаковый нижний предел сохраняется; нет новых grants или событий при чтении | Ошибка service-v1 не повторяется через legacy |
| Owner disable / generation advance | Привилегированный lifecycle существующей регистрации | Следующие SQL-команды видят committed enabled/floor; runtime имеет только SELECT | Повторное enable требует advance; downgrade запрещён existing trigger |

Проверка регистрации в signed acceptance выполняется в снимке одного SQL-command.
Уже начавшаяся операция может закончиться по своему прежнему снимку; обратная
отмена effects не обещается. Срок/отзыв сертификата, task/attempt leases,
пользовательские OIDC права, Human Gate и emergency revoke остаются прежними.
Во время смешанного rollout не менять registration для activation/revoke:
сначала подтвердить все новые owner readers. Старый reader не реализует новый
enabled/floor gate. Конфигурационный transport не переключается автоматически.

Новых таблиц, миграций, SQL privileges и ручной регистрации нет. Runtime SELECT
разрешён исходной схемой; INSERT/UPDATE/DELETE registration остаются закрыты.
Существующие signed floors и instance receipts не пересоздаются.

## Проверки

`make test-service-generation-postgres` использует штатную disposable PostgreSQL
оснастку, применяет schema до current head и проверяет полный schema postcondition.
Сценарий в отдельном clone проверяет cold registered workloads без watermark,
unknown/disabled, runtime write denial, signed floor, restart/profile switch,
re-enable/advance, lower/expired grant и сохранение floor после отказа. Существующий
worker-instance component доказывает replay/competing instances и committed-floor
row-lock race; authorityproof tests отдельно проверяют подпись и instance формат.
Это component доказательства, а не установленный runtime либо полная приёмка.

Для исправления частичного install собрать stock immutable control-plane image,
подтвердить exact source и неизменные schema/libs/contracts, node pull, затем
использовать optional component profile с прежним полным release lock и явными
`schema_heads`. Сохранить preimage/backup и выполнить полный preflight. Не
переигрывать bootstrap/migration Jobs. После rollout проверить actual ordinary
RPC session-archive/runtime-controller, system warm и дальнейший runtime путь.
Ready Pod и merge PR не заменяют эту проверку. Возврат старого image может снова
закрыть cold ordinary path; он не является успешным rollback функции.

Семантика statement snapshot/RLS проверена по официальной PostgreSQL18
документации через Context7; native qualification выполняется закреплённым18.3.
