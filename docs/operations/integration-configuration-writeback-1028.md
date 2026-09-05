---
id: OPS-INTEGRATION-CONFIGURATION-WRITEBACK-1028
title: Git write-back через proposal branch и PR/MR
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница исполнения

Issue #1028 / PR #1064 потребляет owner #1046 checkpoint
`2f5090c18f8134dcfd48413779b6049616f7c61f` через prerequisite
`f21dc646b2f996c478e318bb808b722d5a001d70`. Control-plane хранит proposal,
отдельное решение владельца, точные source/package/connection/content pins,
lease и две устойчивые квитанции. Gateway работает через generated
`ConfigurationWriteBacks` client и policy 65. Прямого чтения CP PostgreSQL нет.

Публикация создаёт только назначенную сервером proposal branch и PR/MR.
Исходная branch и активная RuntimeRevision не меняются. После внешнего merge
обычный SourceWork отдельно читает и валидирует новую revision.

| Сценарий | Авторитетное разрешение | Действие consumer и readback |
| --- | --- | --- |
| Prepare/approve | Специализированные CP команды, текущие actor/tenant/config/source permissions, OCC, idempotency и отдельный approval digest | До одобрения gateway не получает исполняемую claim; публичный Get/List и polling принадлежат CP |
| Claim | Integration-gateway workload, exact unary proof, назначенные owner attempt/generation/fence/lease | Одна работа за цикл, immutable private package и credential descriptor; текущий package обязательно проверяется |
| Подготовка BRANCH | EXECUTE, exact accepted base и отсутствие proposal ref | Локальный bare Git строит commit с единственным base parent, назначенными author/time/message и точными tree/blob/content hashes |
| Begin BRANCH | Owner повторно проверяет authority и фиксирует intent до внешнего действия | Потерянный Begin ACK запрещает push; already_started разрешает только чтение |
| Push | Единственный refspec proposal branch, пустой expected oldOID | Один HTTPS push с exact lease, затем независимое чтение commit/parent/tree/blob/content; source branch не входит в refspec |
| Complete BRANCH | Exact lease и immutable candidate | Owner сохраняет branch receipt; только новый owner work разрешает PR/MR |
| Begin/create PR | Exact approved proposal, подтверждённый commit и marker | Поиск существующего PR/MR, Begin, максимум один POST, независимый повторный поиск даже при потерянном POST ACK |
| Complete PR | Exact repository/head/base/marker/candidate и canonical provider URL | Owner сохраняет вторую квитанцию и SUCCEEDED; runtime остаётся прежним до merge/source sync |
| UNKNOWN_OUTCOME | Только RECOVER_READ_ONLY с прежним effect intent | Нет Prepare/push/POST/повторного Begin; подтверждается уже совершённое действие либо сохраняется неизвестный результат |
| PR recovery после удаления branch | Owner хранит candidate и branch receipt | Чтение файла по immutable commit, поиск PR/MR по точному marker; отсутствие branch не вызывает повторную публикацию |
| Cancel/revoke/expiry | Owner закрывает прежнюю authority и определяет terminal либо UNKNOWN | Consumer не повторяет эффект; timeout убивает всю Git process group, затем удаляет временные файлы |
| Потеря Complete ACK | Durable owner read/claim recovery | Совершённый эффект учитывается метрикой до ACK; новая claim не превращается в автоматический resend |

Событие для этих переходов отсутствует: authoritative Get/List, audit и receipts
описаны по каждому переходу в `configuration-writeback-lifecycle-1046.md`.
Gateway не создаёт фиктивный Run или собственный Human Gate.

# Runtime и ограничение полномочий

Runtime image содержит Git 2.52.0 из Alpine 3.23 и системные CA. UID/GID
приложения 10001, итоговый профиль задаёт supplementary fsGroup 29000.
Root filesystem read-only; `/tmp` — отдельный memory emptyDir 64 MiB.
Временные каталоги имеют mode 0700, credential file 0600; credential передаётся
Git через askpass-файл, а не URL, argv или значение environment variable.
Hooks, global/system Git config, redirects и произвольные protocols отключены.
TLS verification и configured exact CONNECT proxy обязательны. Git stderr не
выдаётся в diagnostics. Дедлайн ограничивает все subprocess и provider calls.

GitHub требует actual tuple `github.com:443` дополнительно к API destination.
Старый API-only managed package продолжает API операции, но write-back закрыт.
Managed network — только exact subset shipped tuples; capability input limits
проверяются до Begin. Для content.update допускается до 349528 base64 bytes
(256 KiB raw), JSON carrier до 512 KiB; более узкий managed package сохраняет
свои ограничения. Unsupported либо слишком большой Git fetch закрывается до
эффекта. Текущий reader проверяет SHA-1 provider Git objects.

Startup проверяет локальные Git binary и writable scratch; рабочий цикл
запускается после существующего startup barrier и участвует в cancel/join.
Kubernetes readiness остаётся локальной; свежесть полного owner cycle отражает
существующий work-path gauge. Локальная readiness не доказывает доступность
конкретного внешнего repository.

# Проверки

На consumer tree выполнены: полный gateway race/vet/build; PostgreSQL gateway
suite (5.058s); оба environment render; Proto, policy 65, package codegen;
Docker runtime build и nonroot/read-only-root/tmpfs smoke check.
Реальный локальный HTTPS git-http-backend fixture проверяет TLS, partial fetch,
single parent, неизменность source branch, exact empty lease и cleanup.
App fixtures проверяют 13 lost-ACK/recovery сценариев; provider fixtures —
GitHub/GitLab exact PR/MR identity и отсутствие повторного POST.

Первый Git fixture FAIL: его server запрещал reachable-object запрос partial
clone. Исправлена только fixture server capability; production TLS/lease не
ослаблялись. Первый расширенный render FAIL: assert ожидал base fsGroup10001,
хотя authority overlay назначает29000; исправлена проверка actual profile.

Live GitHub/GitLab, развёрнутый Kubernetes и пользовательская приёмка —
NOT RUN. Эти локальные проверки не заменяют общий baseline/triple review #1031
и отдельный owner gate. Секреты и private file contents не публиковались.

Проверены Context7 `/git/htmldocs` (resolve → query) и официальные документы:
[git-push exact lease](https://git-scm.com/docs/git-push),
[Git credentials](https://git-scm.com/docs/gitcredentials),
[Alpine Git package](https://pkgs.alpinelinux.org/package/v3.23/main/x86_64/git).
