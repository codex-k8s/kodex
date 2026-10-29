---
id: OPS-DOC-GITHUB-AUTONOMOUS-PDLC-1967
title: Промежуточные действия GitHub без повторных согласований
type: operations
status: approved
owner: reviewer
version: 1.1.0
updated: 2026-10-29
---

# Промежуточные действия GitHub без повторных согласований

Источник: решение владельца о работе сотрудников в одном PR через MCP;
[задача #1967](https://github.com/codex-k8s/kodex/issues/1967).
Документ определяет требуемый контракт. Само его наличие не подтверждает
реализацию, развёртывание или успешную сквозную приёмку.

## Полномочия и маршрут

Владелец выбирает `NONE` для конкретного разрешённого grant сотрудника через
штатный API управления интеграцией. Actor и организация выводятся из сессии;
проект, подключение, сотрудник, версия пакета и область репозитория проверяются
control-plane. RuntimeRevision закрепляет точные версии grant и подключения.
Сотрудник вызывает MCP; runtime-controller передаёт подписанный контекст
владельцу состояния, integration-gateway выполняет только допущенную операцию.
Payload, имя репозитория или ссылка на PR сами по себе не дают полномочий.

Промежуточные операции включают создание и изменение Issue, комментариев,
PR и отдельных файлов через штатный MCP. Рецензия с
`NONE` допускает только `COMMENT`. `APPROVE`, `REQUEST_CHANGES`, слияние и
SSH-деплой не получают автономного допуска из этого режима. Default остаётся
`HUMAN_EACH_EFFECT`; разрешённый выбор не изменяет существующие grants.
Запись файла с `NONE` перед изменением получает свежие metadata репозитория
и существующей ветки: основная, защищённая, неоднозначная либо неизвестная
ветка отклоняется без WRITE. Защита веток GitHub остаётся независимой границей;
её настройки этот режим не меняет.
Отдельная массовая операция `github.repository.bundle.publish` сохраняет
прежний Human Gate и не используется в этом автономном PDLC. Её перевод
на `NONE` требует отдельного завершённого серверного пути; один новый пункт
в каталоге не считается реализацией. Файловые артефакты сотрудников при этом
публикуются и выдаются через обычный проверяемый контур Kodex.

Старые версии GitHub разрешаются только по закрытым неизменяемым снимкам с
точными version/digest. Нельзя выводить исторические права из нового пакета
удалением возможностей или подстановкой текущих approval policies.

## Матрица жизненного цикла

| Переход | Авторитетная проверка и результат |
| --- | --- |
| Создание grant | Owner API, exact package/recipient/repository; audit и OCC в owner-транзакции |
| Запуск/continuation | Свежая RuntimeRevision; разрешённая операция и выбранная policy закреплены сервером |
| Подготовка файлов | Exact artifact revisions, ownership, effective CLEAN, все decoded members и полный manifest; отказ любого члена закрывает набор |
| Подготовка invocation | Проверка живого grant и frozen input; существующий native intent возвращается по прежнему idempotency key |
| `NONE` | Durable intent разрешённой промежуточной MCP-операции ставится в очередь без создания или автоматического подтверждения Human Gate; bundle в этот набор не входит |
| `HUMAN_EACH_EFFECT` | Прежний Gate ожидает действительное решение; отказ/истечение закрывают зависимый эффект |
| Claim/renew/Begin | Проверка актуальных полномочий, версии, exact input, lease/fence и срока; новая policy не оживляет старую попытку |
| Успех | Native receipt и состояние эффекта фиксируются владельцем; результат доступен через защищённый read path |
| Неизвестный исход | Read-only reconciliation; запрет слепого повтора внешнего действия и нового idempotency key |
| Retry/rejoin | Новая runtime attempt сама по себе не разрешает новый WRITE; прежний эффект сопоставляется по immutable semantic intent |
| Cancel/delete/terminal | Закрытие очереди/leases/grants в owner boundary; уже подтверждённый внешний факт и история сохраняются |
| Revoke/смена grant | Старые pins не дают новый допуск; начатый uncertain effect сохраняет read-only recovery |
| Слияние | Отдельный Human Gate менеджера с exact PR/head; receipt не заменяется текстом модели |
| Деплой | Отдельный workflow и проверенная сервером передача merge receipt; отдельный deploy Gate и project SSH secret |

Эта задача не разрешает прямые записи состояний в БД, выдачу owner credentials
сотрудникам, обход scanner boundary или обход происхождения между workflow.

Для проверки GitHub API использованы официальные разделы
[Repositories](https://docs.github.com/en/rest/repos/repos#get-a-repository) и
локальный исходный код закреплённого `go-github/v74`. Context7 недоступен:
сервер отклонил настроенную авторизацию; ключи не выводились и не менялись.

## Работа в существующем PR

Выпуск GitHub6.3 добавляет отдельные `github.pull_request.comment.create`,
`list`, `read`, `update` и `github.pull_request.ready`. Старые
`github.issue.comment.*` сохраняют запрет PR. PR-операции проверяют выбранный
номер через repository-scoped provider read, а отдельный comment — также через
exact issue URL. Создание комментария использует прежний durable effect marker:
неопределённый ответ допускает только поиск подтверждённого комментария, без
второго POST.

`ready` принимает только номер PR и ожидаемый40-символьный head SHA. Входные
GraphQL, node ID, другой репозиторий, merge или review decision запрещены.
Node ID назначает свежий GitHub read; операция выполняет один закрытый
`markPullRequestReadyForReview`, затем читает состояние снова. GitHub не имеет
head CAS этой mutation, поэтому смена head после запроса сохраняет UNKNOWN.
Повтор при уже наблюдённом Ready и recovery не выполняют mutation.
Обязательный результат `PULL_REQUEST` по-прежнему получает свежий native read
и требует `draft=false`; перевод Ready сам по себе не доказывает качество.

| Инициатор и переход | Полномочия, состояние и результат |
| --- | --- |
| Сотрудник → PR comment | Runtime MCP → exact grant/connection/RR → owner durable invocation → integration-gateway → один repository/PR; native receipt и readback |
| Обычный Issue grant → PR | Закрытый отказ до comment read/write; права не наследуются |
| PR comment read/update | Exact выбранный PR и comment ID/issue URL; чужой comment закрыто отклоняется |
| Create retry/неизвестный ACK | Прежний intent/effect marker; read-only rejoin, новый POST не разрешается одним retry |
| Ready prepare/claim | Прежние owner actor/project/grant/lease/input pins; default Human, явный NONE только для этого WRITE |
| Ready → подтверждённый переход | Тот же PR/node/head и `open,draft=false` в авторитетном чтении; native receipt, прежний completion/outbox |
| Ready → отказ до mutation | Неверный head/объект/схема: без внешней записи; авторитетный invocation read |
| Ready → потеря ACK/drift | Read-only recovery; неподтверждённый результат UNKNOWN, повторного изменения нет |
| Cancel/revoke/delete | Прежний атомарный owner lifecycle отзывает допуск; подтверждённый внешний факт и история сохраняются |
| Merge/deploy | Прежние отдельные Human Gate; новые PR-возможности их не подтверждают |

Нативный маршрут остаётся `runtime MCP → runtime-controller → control-plane
command/owner transaction → integration-gateway claim/Begin → GitHub → owner
completion → защищённый readback результата`. Source actor приходит из
проверенного runtime-контекста, refs из payload — только locators. Idempotency
остаётся прежним intent/effect, версия новой попытки не даёт права на второй
внешний эффект. Не добавляется новый тип доменного события: invocation и
receipt используют существующие audit/outbox/response consumers.

Реальный аналитик выявил неполноту прежнего6.2: grant сохранялся, но
`integration_invocations_approval_check` и trigger `protect_email_mailbox_gate`
сохранили пять старых операций. Поэтому `ResolveIntegrationInvocation` возвращал
Unavailable до внешнего запроса, и сотрудник видел `TOOL_UNAVAILABLE`.
Регрессия проверяет actual durable READY для всех семи добавленных WRITE,
отсутствие Gate и rejoin прежнего invocation. Forward migration расширяет
только exact allowlist constraint/trigger, сохраняя COMMENT-only review,
тип риска, repository, operation=capability, mailbox и immutable grant checks.
Postcondition отдельно проверяет полный constraint обратной exact проекцией,
полный trigger body/ACL и включённый trigger. Успешный каталог без этой проверки
не является успешной миграцией. Прежний CHECK и отключённый trigger — отдельные
отрицательные native проверки.

Снимок6.2 сохранён побайтно; admission6.3 — отдельная forward migration
`20261029000200`. Профили `github-pr` и `github-pr-backup` проверяют exact029
до изменения и exact новый helper после него, сохраняя остальной schema proof.
Прежние migration/proofs не меняются. Штатная проверка перехода и повторного
`up`/восстановления backup ACL: `scripts/tests/control-plane-postgres-test.sh
--github-pr-upgrade`. Это локальная disposable проверка, не staging приёмка.

Context729.10 повторно отклонил авторизацию; использованы официальные
[PR mutations](https://docs.github.com/en/graphql/reference/pulls#markpullrequestreadyforreview)
и [общие comment endpoints](https://docs.github.com/en/rest/issues/comments).
Настройки Context7 и секреты не менялись.

## Проверка и возврат

Обязательны отрицательные проверки неизвестного пакета/digest, чужого проекта,
самовыдачи прав, `APPROVE`/`REQUEST_CHANGES` при `NONE`, merge/deploy при `NONE`,
заражённого или непроверенного member, устаревшего grant и повтора uncertain
WRITE. Положительный сценарий подтверждается native invocation/receipt,
авторитетным GitHub readback и доступным владельцу файловым артефактом.

Выкладка допускается только после готовности всех consumers и сохранения
совместимости старых pinned connections. Возврат сначала останавливает новые
исполнения выбранного QA-проекта, сохраняет начатые effects и историю, затем
возвращает предыдущие immutable images. Новые grants не переносятся на старый
пакет и автоматически не расширяют его права. До фактической проверки полный
PDLC, отдельный deployment workflow и immutable release остаются `NOT RUN`.
