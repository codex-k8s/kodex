---
id: OPS-DOC-GITHUB-SCOPE-2033
title: Личные репозитории GitHub и проверяемая область подключения
type: operations
status: approved
owner: developer
version: 1.0.1
updated: 2026-11-14
---

# Область GitHub и личный ключ инициатора

Источники: #2033, #1967, #1859, `GUIDE-DOC-003`, `GUIDE-DOC-006`.
Новый GitHub6.5 сохраняет47 исходных операций и добавляет
`github.identity.read` и `github.repository.list`. Старые6.0–6.4 остаются
полными точными историческими пакетами; новая область и новые политики им
не присваиваются. Ограниченный размер каталога увеличен с48до64 операций.

## Сценарий и полномочия

| Инициатор → внешний интерфейс | Owner и внутренний путь | Полномочия и результат |
| --- | --- | --- |
| Пользователь → создание подключения | `CreateConnection` → CP configuration owner | Новое6.5 создаётся с явным `ALL_ACCESSIBLE` и `INITIATOR_REQUIRED`; PAT не сохраняется как общий |
| Пользователь → «Мои подключения», добавить/заменить/отозвать | Прежние personal credential RPC → собственная tenant/user binding → Secret Broker | Значение write-only, descriptor не возвращается в браузер; shared fallback отсутствует |
| Пользователь → найти репозитории | Прежний `TestPersonalIntegrationCredential` с bounded `githubDiscoveryPage` → immutable personal test snapshot → Claim/Begin/Complete | Worker получает exact собственную revision; `/user` и `/user/repos`, одна страница≤100. Завершение связано с lease/fence, generation, binding version/revision и configuration digest |
| Пользователь → чтение результата | Прежний `GetPersonalIntegrationCredential` либо personal catalog | Безопасные ID/login, ID/fullName, тип владельца и наблюдаемые read/push/archived/private. Это не grant и не доказательство прав каждого endpoint PAT |
| Управляющий подключением → SELECTED | Прежний `UpdateConnection` → собственная актуальная discovery history | Сервер сверяет каждую пару ID/fullName со своими completed receipts того же actor, generation, revision, connection version и configuration/definition pins. Чужой admin не выбирает чужую binding |
| Агент/workflow → typed resource operation | Прежний managed MCP → runtime-controller → CP immutable invocation/personal pin → gateway Begin → typed GitHub adapter | Root initiator берётся из runs, не из payload/Gate approver. Текущие `/user`, bounded explicit-membership `/user/repos`, `/repos/{owner}/{repo}` и exact canonical ID/name проверяются до операции |
| Обычный project employee → source bundle | Прежний specialized Prepare/claim/Begin/CAS/recovery | Prepare содержит typed repo locator; owner фиксирует отдельную personal revision из native invocation pin. Snapshot/Gate включают ID/name; personal/shared revision колонки взаимоисключающие |
| Фоновый configuration source/writeback | Прежний native source/work owner → gateway | Только явно разрешённая service identity и PROJECT_SHARED. SELECTED выводит exact ID/name из сохранённой настройки; ALL_ACCESSIBLE не становится неявным фоновым назначением |

`ALL_ACCESSIBLE` означает актуальный явный доступ текущего ключа из
аутентифицированного `/user/repos` с `owner,collaborator,organization_member`.
Сам по себе публичный GET произвольного репозитория не даёт такого доступа.
Каждый resource input содержит `repository_id` и `repository_full_name`;
эти поля являются адресом требуемого ресурса, а не источником authority.
Gateway сверяет ID, canonical name, владельца, актуальное членство и read
permission. Для записи применяется закрытая матрица классов операций и archived;
сам endpoint провайдера остаётся обязательной границей точного права PAT.
Успешный health/discovery не помещает WRITE в verified capabilities.

SELECTED содержит1–100 уникальных пар ID/name. Дубликаты, aliases, URL,
traversal, неизвестные поля, потерянные IDs и подмена canonical name закрыто
отклоняются. Отказ провайдера не запускает расширяющий fallback.
Discovery использует только числовую страницу; provider Link URL не
открывается. Per-call поиск явного членства ограничен100страницами по100
репозиториев и общим deadline операции; превышение бюджета закрывается,
не выдавая неполный положительный результат. Это эксплуатационное ограничение
для аккаунтов с очень большим числом репозиториев.

## Политики и жизненный цикл

READ использует NONE. Обычные WRITE, включая comments/reviews/branches/PR,
по умолчанию HUMAN_SCOPED с возможностью HUMAN_EACH_EFFECT. Связь reusable
consent включает repository ID/name и прежние typed object/branch pins.
Merge, destructive/CI операции и `repository.bundle.publish` сохраняют
HUMAN_EACH_EFFECT без NONE либо scoped обхода. Исторические пакеты сохраняют
свои прежние ограничения. Никакие одобрения GitHub не создаются при проверке
credential или discovery.

| Переход | Авторитетное изменение, событие и consumer |
| --- | --- |
| Добавление/замена PAT | Собственная binding, новая immutable revision/generation, CHECK_REQUIRED; прежние verification/discovery очищаются. Прежний audit/idempotency, отдельного domain event нет, read path — собственная credential |
| Test create → claim → Begin | Прежнее DUE → CLAIMED → PROBING, одна lease и одно расходование admission. Повтор Begin запрещён; новая arbitrary task/URL не создаётся. Read path — owner test ledger |
| Successful exact completion | READY и безопасная страница одной owner transaction; test SUCCEEDED, lease закрыта. Чтение через personal GET/catalog; отдельного domain event нет |
| Failed/expired/unavailable probe | FAILED либо отмена, lease закрыта; READY/discovery не остаются. Частичный provider ответ не принимается. Проверка другого пользователя не меняется |
| Configuration/definition/version drift | Прежний owner cancel закрывает jobs/leases, verification/discovery не используются. UpdateConnection публикует существующее INTEGRATION_CONNECTION_CHANGED; grants/dependencies сохраняют прежние fences |
| Revoke/rotation до внешнего bundle эффекта | Только consumers этой личной binding закрываются в той же owner transaction; Gate/invocation/task закрываются прежним specialized cancel path |
| Revoke/rotation после bundle Begin | Запись не повторяется: UNKNOWN сохраняет прежний candidate/manifest/tree/commit и read-only reconciliation; старый write lease отзывается. Другой ключ и новый idempotency key не разрешают новый push |
| Bundle claim/approve/Begin | Каждая новая работа сверяет current root initiator, own binding generation/revision, grants, source eligibility и exact native Gate. Shared key при personal policy не читается |
| Cancel/delete/root terminal/lease expiry | Прежний bundle lifecycle остаётся authoritative: до Begin known-no-effect closure, после Begin UNKNOWN/read-only. Receipt/rejoin сохраняют прежние exact source pins |
| Delegation/retry/wait/resume/restart | Прежний runs root и immutable invocation pin сохраняют инициатора; approver/последний собеседник его не заменяют. Каждый новый вызов проходит fresh personal Begin |

Shared-режим нового пакета требует явно указанного `service_identity`; gateway сверяет его с каноническим login свежего `/user`, поэтому метка не позволяет подменить служебную учётную запись.
Переключение сохраняет прежние disabled/OCC/no-active-consumer guards.
Фоновые source/writeback не получают личный PAT пользователя, настроившего
расписание. Эти операции не имеют интерактивного инициатора и используют
отдельную разрешённую service identity по #2033.

Пакет исходников `github.repository.bundle.publish` сохраняет прежний protected contract: только HUMAN_EACH_EFFECT с exact server-derived proposal/branch/manifest и отдельным Gate. Обычные branch/PR/comment/review WRITE допускают scoped consent по опубликованной матрице; это не распространяется на bundle либо merge. READ не создаёт Gate.

Проверка GitHub прав разделена по операциям. Code/content/branch/bundle/merge/actions требуют текущего repository push; issue/comment/review/PR collaboration используют текущую authenticated membership и окончательную проверку author/triage permissions точным provider endpoint. Наблюдаемый CanPush не объявляется универсальным правом записи. Архивный репозиторий закрыт для новых WRITE.

Управляемые сужения определения сохраняют personal default, discovery proof и явную service identity независимо от значения metadata.version. Worker resource kind берётся из закреплённого scope, а не из номера shipped release. Source/writeback effect validation добавляет exact ID/name из frozen owner configuration перед проверкой фактической schema; пользовательские IDs здесь не используются.

## Ручная проверка

В disposable проекте создать GitHub подключение, открыть «Мои подключения»,
добавить synthetic PAT и найти личные/организационные репозитории. Проверить
пустую последнюю страницу и переходы между страницами, выбрать несколько
репозиториев и сохранить SELECTED. До повторной проверки ключа запуск
закрыт. Проверить READ/NONE, scoped comment и отдельный merge Gate. Второй
пользователь не получает первую credential либо её discovery receipts.
После revoke/rotation повторный вызов, stale completion и новый bundle Begin
отклоняются; начатый неизвестный push только согласуется чтением.

Нативные GitHub вызовы в тестах направляются в local synthetic HTTP fixture.
Личный discovery для изменения выбора показывает все актуально доступные собственному ключу репозитории, включая ещё не выбранные; обычный agent `github.repository.list` ограничен текущим SELECTED. Наблюдение по-прежнему не выдаёт grant.

Реальные GitHub writes, кластерные изменения, production и чужие credentials
не требуются. Полный установленный двухпользовательский сценарий и точные
immutable schema/image/backup pins квалифицируются отдельно от unit/synthetic
результатов; отсутствие такого запуска не является PASS.

## Проверенные внешние документы

Через Context7 `/websites/github_en_rest` проверены GET /user,
GET /user/repos, affiliation и pagination, GET repository и permissions.
Через `/websites/vuejs` проверены watch cleanup и onScopeDispose. Источники:
[GitHub repositories](https://docs.github.com/en/rest/repos/repos),
[GitHub users](https://docs.github.com/en/rest/users/users),
[Vue watchers](https://vuejs.org/guide/essentials/watchers),
[Vue scope disposal](https://vuejs.org/api/reactivity-advanced#onscopedispose).

## Точные backup ACL и неизменная миграция044

Исправление #2033/#2139 не редактирует `20261114120000_github_repository_scope.sql`
и не повторяет её для уже применённой044. Ошибка была в disposable capture:
он воспроизводил table SELECT, но пропускал действующие schema USAGE,
sequence SELECT/USAGE и sequence default ACL initializer
`deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh`.
Native fixture и генератор restore читают один отмеченный SQL-блок этого
initializer; manifest закрепляет путь и SHA256 его SQL. Это не копия ACL
произвольного установленного PostgreSQL.

`github-repository-scope-backup` различает две полные schema qualifications:
`TABLE_ONLY_BACKUP` (ранее закреплённый table-only catalog) и
`CANONICAL_BACKUP` (полный действующий initializer). Обе сравниваются целиком;
normal catalog, смесь этих вариантов, лишние права, grant option, посторонние
roles/memberships, неизвестные relation/function/trigger и изменённый purge
отклоняются. Это квалификации схемы, а не дополнительные runtime readers.
Новый postcondition явно возвращает `backupQualification`; один discriminator
без exact metadata не разрешает миграцию или resume.

| Переход | Авторитетный результат и восстановление |
| --- | --- |
| Остановить backup producers | Отдельный адресный maintenance plan/journal сохраняет UID/spec/replicas; полный namespace lineage и PostgreSQL `activeBackupSessions=0` подтверждают границу до normalization |
| Exact043 canonical/table-only → normalization | Зарегистрированная `up-github-repository-scope-backup-compatible` проверяет полный каталог и idle owner; одна transaction снимает только schema USAGE и sequence rU/default S до прежнего exact table-only043 |
| Подтверждённый normalize commit → Goose | Один `UpTo044` применяет неизменную опубликованную миграцию; нормализация не открывает общий `up` и не допускает произвольную версию |
| Goose PASS/FAIL/cancel | Независимый ограниченный cleanup читает фактически применённую043 либо044 и возвращает canonical ACL; исходная ошибка не превращается в PASS |
| Потерян normalize ACK | Goose не вызывается при неизвестном commit; отдельный readback определяет exact043, затем зарегистрированный restore восстанавливает canonical без повторения DDL |
| Потерян Goose ACK | Чтение версии и полного каталога различает043/044; explicit restore возвращает canonical, повтор команды на044 не исполняет Goose |
| Restore FAIL/UNKNOWN | Не повторять effects автоматически; backup worker остаётся остановленным до свежего exact canonical proof и readback прежней попытки |
| Уже установленная044 | Новый readback явно квалифицирует известный ACL-вариант; restore только переводит normal/table-only/canonical в canonical. Никакие другие установки не объявляются проверенными |

Restore сохраняет sequence ACL и schema/default S в нормализованном полном
каталоге. Исключены только отдельно проверенные table SELECT/default r;
UPDATE, grant option, чужой grantor и неизвестный объект не скрываются этой
нормализацией. Восстановление направлено только вперёд, не возвращает table-only
состояние и не редактирует business rows либо историю goose.

Безопасная локальная проверка:

```sh
make test-github-repository-scope-upgrade-postgres
make test-component-publication
make test-control-plane-migration
```

Capture выполняется только публичной disposable PostgreSQL-оснасткой с приватным
каталогом0700: для каждого `KODEX_GITHUB_SCHEMA_CAPTURE_MODE=normal|backup|table-only-backup`
задаются `KODEX_GITHUB_SCHEMA_CAPTURE_DIRECTORY` и
`--github-repository-scope-upgrade`, затем генератор получает
`--capture-directory` и проверяется `--check`. Ни captures, ни CLI tests
не используют live credentials или Kubernetes. Обязательные PostgreSQL18
GRANT/sequence/default-privilege semantics проверены через Context7
`/websites/postgresql/18`: [GRANT](https://www.postgresql.org/docs/18/sql-grant.html),
[ALTER DEFAULT PRIVILEGES](https://www.postgresql.org/docs/18/sql-alterdefaultprivileges.html).
