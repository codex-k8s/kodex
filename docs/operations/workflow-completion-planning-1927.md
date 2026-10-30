---
id: OPS-DOC-1927
title: Ограниченное завершение PDLC с обязательными файлами и квитанциями
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-30
---

# Завершение PDLC в существующем RPC-бюджете

Задача: [#1927](https://github.com/codex-k8s/kodex/issues/1927), эпик
[#1830](https://github.com/codex-k8s/kodex/issues/1830), общий PR
[#1970](https://github.com/codex-k8s/kodex/pull/1970).

На staging `f7f0bf367812126fe34a8ce0a5deb2cc1a58a8cc` аналитик сохранил
документы, комментарий PR и CLEAN-файлы, затем передал FILE, COMMENT и два
ARTIFACT. Шесть попыток фиксации одного completion исчерпали прежний пятисекундный
бюджет `OWNER_RPC`. Архитектор не получил принятого результата. Это подтверждённый
сбой завершения платформы; перегрузка модели не установлена.

Новый случай нельзя автоматически приравнять к прежнему медленному поиску
артефакта. PostgreSQL отменил `runtime_activity__terminal_tools` и
`workflow_result__integration`: последняя команда не обязательно расходовала весь
бюджет одна. Диагностический read-only вариант незавершённого lineage SELECT
показал planning368.474ms и execution1.718ms при нуле строк; удалённый в диагностике
row lock и незавершённый tuple исключают использование этого результата как
положительного native proof.

Причина повторена в native component с настоящими owner-командами и70
завершёнными операциями: auto не завершает итоговый completion за5s, при этом
откатывается вся транзакция. Тот же сценарий с generic planning завершается
за1.673s, exact replay — за4.439ms. Предыдущие тесты ограничивали бюджет доставки
outcomes, но не проверяли весь последующий commit четырёх обязательных результатов.

Исправление применяет существующую `withWorkflowEvidenceGenericPlan` к полной
owner-фиксации явных native evidence profiles. Domain evaluator, SQL journal
guard и dispatch используют повторно подготовленные планы внутри одной
транзакции. Прежний режим восстанавливается до последующей работы вызывающего;
ошибка требует rollback. Схема, SQL predicates и функции не меняются. Это не
кэширование решения о доступе: все строки, grants, leases и eligibility читаются
и проверяются заново.

## Карта полномочий и жизненного цикла

Источник требования — обязательные результаты опубликованного SERVER_DAG_V1.
Runner завершает точный lease/attempt/RR/input, callback сверяет immutable input
и вызывает `PlatformRuntimeService.CompleteExecution`. Межсервисный transport и
полномочия проверяются существующим путём. Переданные refs выбирают кандидатов,
но не являются правами. Авторитетный владелец — control-plane/PostgreSQL.

| Событие                     | Авторитетное действие                                                                                                                            | История и последствия                                                               |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------- |
| Физический completion       | Exact lease/fence/generation; закрытие lease, capture, usage и проверка всех published predicates в одной транзакции                             | Принятый журнал запускает готовый downstream; `TURN_COMPLETED` публикуется атомарно |
| FILE/COMMENT                | Канонические lineage/rejoin readers, current/frozen grants и точные receipts; FILE дополнительно сверяет bytes, PR/head/path/branch и хронологию | SUCCESS присоединяется к существующему receipt; новая запись не создаётся           |
| ARTIFACT                    | Exact capture/source pins, revision/digest/size, CLEAN, frozen catalog и текущая ACL                                                             | Принятые ссылки сохраняются; непроверенный файл не становится CLEAN                 |
| Deadline/ошибка SQL         | Вся owner-транзакция откатывается; callback не сообщает успешный commit                                                                          | Уже завершённые внешние действия и прежние captures остаются в собственных журналах |
| Повтор того же completion   | Прежний ключ и идентичный payload, свежая authority до receipt/OCC                                                                               | Единственный commit; никакого второго successor, файла или GitHub effect            |
| Cancel/delete/revoke/expiry | Существующий owner закрывает полный граф и leases/grants                                                                                         | Поздний completion не оживляет terminal root; история доступна владельцу            |
| Human Gate                  | Только опубликованная политика и подтверждённые факты                                                                                            | Оптимизация чтения не заменяет согласование merge/deploy                            |
| Адресная замена Pod         | История, captures и receipts читаются авторитетным путём                                                                                         | Сам факт Ready не доказывает отсутствие потерь либо успешный PDLC                   |

Оптимизация планирования обязана сохранить предикаты, cardinality и приоритет
rejoin перед обычной lineage, `VOLATILE`, `SECURITY INVOKER`, точный search_path и
`FOR SHARE NOWAIT`. Используется только transaction-local настройка с
восстановлением; общие PostgreSQL-настройки и RPC/account бюджеты не меняются.
Прежние процессы без native evidence profile сохраняют прежний путь. Если позже
потребуется изменение SQL-функций, оно потребует отдельной forward-only миграции
с exact preimage/catalog proof; применённые миграции не редактируются.

## Регрессия и возврат

Native component проходит штатные команды: FILE+COMMENT+два captured ARTIFACT,
большой READ history, полное защищённое продолжение, затем приёмка в прежние5s,
повтор идентичного completion и автоматический downstream. Отдельно проверяются
оба completion orders, current и frozen authority, неверные pins, неоднозначные
receipts, cancel/revoke/expiry. Три режима planner одноразовой БД не изменяют
staging: auto, force_custom_plan, force_generic_plan.

На рабочем дереве после исправления три полных native completion прошли за
1.624s/1.735s/3.049s; exact replay занял3.314ms/4.374ms/4.281ms. Во всех случаях
приняты четыре результата, запущен и завершён зависимый сотрудник, старое runtime
чтение закрыто. Дополнительный native scope-тест проверяет на одном физическом
соединении success, empty и настоящий SQL failure с восстановлением режима и
обязательным rollback. Эти результаты получены до финальной фиксации SHA;
clean qualification и staging-проверка учитываются отдельно. Первоначальная
ошибка оснастки с превышением create-content limit не является production RED.

Проверена Context7 `/websites/postgresql`: PL/pgSQL/SPI plan caching,
generic/custom plans, PREPARE и volatility. План не фиксирует результат predicate
и не заменяет повторную авторизацию.

Для live-проверки требуется чистый зафиксированный SHA и доказательство actual
image/schema. Повреждённый terminal root не возобновляется слепым retry.
Проверяются реальные файлы владельца, журнал принятых результатов и фактический
следующий сотрудник. Наличие декларации модели не является PASS.

Возврат компонента использует прежний immutable digest через штатный scoped
release; схема032 не меняется. Возврат восстанавливает известный медленный путь и
не переписывает прошлые бизнесовые решения. Production и общие
Kubernetes/ingress/firewall/auth не затрагиваются; значения секретов не публикуются.
