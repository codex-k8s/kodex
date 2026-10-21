---
id: OPS-CODE-QUALITY-1823
title: Подготовка кода к построчному ревью
type: operations
status: approved
owner: developer
version: 1.8.0
updated: 2026-10-21
---

# Подготовка кода к построчному ревью

Задача: [#1823](https://github.com/codex-k8s/kodex/issues/1823).
Исходный main: `66c1a8ddafd3bb03d80ad6ff09b57759e582dfb5`.
Работа выполняется одним PR. После приёмки этой задачи владелец принимает
решение о слиянии, затем выполняет построчное ревью всего кода. Этот PR
самостоятельно не сливается и не является полным review/приёмкой прототипа.

## Границы и решения владельца

- По поручению21.10 один общий рефакторинг охватывает библиотеки, сервисы,
  шлюзы, PWA и инструменты; отдельные PR по слоям не создаются.
- Go-комментарии: английское пояснение, непосредственно следом русский
  перевод; смысл, ограничения и эффекты описываются по прочитанному коду.
  Это явное исключение из прежнего русского-only правила комментариев.
- Всё generated исключено из поиска и требования комментариев. Шаблоны
  генерации не меняются ради этой задачи. Handwritten генераторы остаются
  собственным кодом и проверяются, их output — нет.
- После изменения проверяются сборка и unit-тесты затронутых модулей.
  Полная приёмка, deploy, browser QA, реальные ИИ-запуски и component/live
  проверки не выполняются; их отсутствие не называется PASS.
- Поведение, authority, порядок проверок, wire и транзакции не изменяются.
  Применённые миграции неизменяемы. Похожая форма не доказывает общий смысл.

## Воспроизводимый анализ

`tools/code-quality/package-lock.json` закрепляет `jscpd@5.4.2` и нативный
пакет платформы. Документация проверена через Context7 `/kucherenko/jscpd`;
точные параметры подтверждены установленным `jscpd --help`.

```bash
npm ci --ignore-scripts --no-audit --no-fund --prefix tools/code-quality
node --test tools/code-quality/*.test.mjs
node tools/code-quality/scan-duplicates.mjs /absolute/private/report-directory
node tools/code-quality/summarize-report.mjs /absolute/private/report-directory
node tools/code-quality/clone-worklist.mjs \
  /absolute/private/report-directory/summary.gen.json \
  /absolute/private/report-directory/worklist.gen.json
node tools/code-quality/check-addressed-ignores.mjs \
  /absolute/private/report-directory/source-inventory.json
go test ./tools/code-quality/go-docs
go run ./tools/code-quality/go-docs -root . \
  -inventory /absolute/private/report-directory/source-inventory.json \
  -output /absolute/private/report-directory/go-docs.json -check
```

Go запускается с `GOENV=off GOWORK=off GOTOOLCHAIN=go1.26.6`.
Для реестра без дубликатного поиска есть `--inventory-only` третьим аргументом
`scan-duplicates.mjs`; для измерения неполного покрытия опускается `-check`.
Каждый отчёт содержит все непокрытые объявления, а не только удачные примеры.
Проверка языков машинная, точность и подробность текста проверяются чтением.

Охват — tracked и новые собственные source-файлы, включая тесты, SQL,
конфигурации и скрипты; исключения generated определяются устойчивым путём,
именем output/lockfile или ведущим generated заголовком до начала кода.
Исключения не выбираются по случайному слову в test string. Порог фиксирован:
100tokens/10lines, comments игнорируются, переименование идентификаторов
учитывается согласованно. Никаких embeddings/внешней отправки кода нет.

## Исходные результаты и оставшаяся работа

- [x] Созданы отдельные Issue/worktree/ветка и один контур измерения.
- [x] Baseline исходного main:6535 собственных файлов,179 generated/lockfile
  исключений; анализатор обработал5717 файлов выше собственных порогов,
  2138 clone groups. Полный нормализованный реестр без тел фрагментов —
  `code-quality-1823-baseline.gen.json`, SHA исходного raw report
  `704072f12077a73dfde1d4b9451f14de2232e9606b042f2083015d09418ed339`.
  Из них928 Go,259 TypeScript; остальные — scripts/SQL/configuration/markup.
- [x] Исходный Go-реестр:2414 файлов,20980 объявлений,0 с полным новым
  двуязычным форматом. Это не отсутствие существующих русских комментариев.
  Учтены также локальные именованные типы; прежнее измерение20901 без них
  было неполным и не используется как финальный denominator.
- [ ] Разобрать все группы: вынос либо адресное обоснованное исключение.
- [ ] Завершить подробные двуязычные комментарии и получить полный `-check`.
- [ ] Повторить анализ и build/unit всех затронутых модулей на конечном SHA.
- [ ] Передать единственный готовый PR владельцу; не объявлять частичную
  волну готовым результатом.

Первый вынос: `provider_diagnostic.go` / `provider_process.go` в runtimecontract
используют общий package-private `decodeCanonicalProviderObservation`.
Сохранены4096bytes/depth2, duplicate-key guard, exact keys, EOF, canonical
порядок, отдельные semantic validators и sentinels. Получатель заменяется
только после успеха. Это чистый wire helper, он не выдаёт authority и не
превращает diagnostic observation в business receipt. Added regression
проверяет сохранение обоих получателей при невалидном JSON.

Адресные исключения `jscpd:ignore-start/end`, не целые файлы:

- `websocket/server.go:queryClient` — явные сигнатуры разных generated RPC.
  Type-2 detector сравнил перекрывающиеся части одного интерфейса; общий
  dispatcher потерял бы типы запросов/ответов, исполняемого повтора здесь нет.
- `controlplaneclient/client.go:bindServices` — отдельные типизированные
  конструкторы разных сервисов на одном protected connection. Упаковка
  в нетипизированный реестр ухудшила бы явность состава и type safety.
  Исключены только строки конструкторов, не проверки или весь client.

Двуязычно описаны эти два контракта и их тестовые функции. Общий Go build/unit
runtimecontract — PASS LOCAL; точный SHA привязывается в квитанции коммита/PR.
Полностью описан собственный код и тесты модуля httpserver, его build/unit —
PASS LOCAL. Поведение этого модуля не менялось, добавлены только комментарии.
Unit нового inventory/Go checker — PASS LOCAL. Полный контур остальной
задачи пока NOT RUN; цель остаётся незавершённой.

## Вторая волна — промежуточный результат

На исходниках после0086a4257:

- Полностью описаны оба файла `oidcidentity` и оба файла `i18n`. Проверены
  фактические ветки fallback/ошибок и UUID/opaque namespace; поведение не менялось.
- Статистика Go с local aliases:20994 объявления,85 соответствуют новому
  формату,20909 ещё требуют обработки. Полностью покрыты httpserver15/15,
  oidcidentity11/11, i18n17/17 и checker12/12. Это не общий PASS комментариев.
- Адресные inline исключения двух typed-composition групп реально принимаются
  закреплённым jscpd. Отдельный unit запускает exact binary: без marker на
  синтетическом Go-фрагменте есть clone, с marker —0 в том же weak режиме.
- Повтор detector working-2:2135 групп, из них925 Go вместо исходных928;
  baseline не переписан и все оставшиеся группы пока не объявлены разобранными.
- Полные build/unit модулей controlplaneclient, oidcidentity, i18n — PASS LOCAL;
  полный gateway go test/build — PASS LOCAL, HTTP11.751s, WS3.738s.
  Runtimecontract build/unit и checker повторены; новых live действий нет.

Следующий этап: продолжить разбор baseline, вынос поведения и подробные
комментарии остальных Go-пакетов. Единый PR остаётся Draft до полного объёма.

## Третья волна — HTTP context, lifecycle runtime и PWA grant

Общие transport-проверки вынесены только внутри gateway:

| Путь | Сохранённый порядок до прежнего RPC |
| --- | --- |
| Archive/Restore/Purge SkillBundle —3 команды | resource ref → versioned headers → отдельный typed RPC → прежний view/ETag |
| Archive/Restore/Purge MemoryRecord —3 команды | resource ref → versioned headers → отдельный typed RPC → прежний memory/redaction view |
| Bind/Unbind SkillBundle —2 команды | decode body → agent/resource/revision refs и binding version → mutation headers → прежний bind/unbind RPC → exact response binding |
| Bind/Unbind MemoryRecord —2 команды | тот же локальный синтаксический порядок, но собственные memory RPC и resource refs |
| Validate/Publish/Discard SkillDraft —3 команды | bundle/revision refs → прежний body decoder → digest → headers → отдельная специализированная команда |

`requireContextResourceMutation`, `requireContextBindingMutation` и
`requireSkillRevisionDigestMutation` не выбирают actor/tenant, не выдают grants
и не создают события. Проверенный transport context, owner RPC, owner transaction,
OCC/idempotency/audit/event path не менялись. ReviewSkillBundleDraft с иным body
и decision остаётся отдельным. Typed RPC/response mapping не скрыт универсальным
dispatcher. Новые unit-тесты проверяют отказ до RPC и сохранение точных pins;
существующая таблица всех маршрутов проверяет реальные generated request mappings.

В PWA совпадающая wire-часть SYSTEM/PROJECT grant plans вынесена в
`integration-grant-plan-fields.ts`. Owner/scope/project/profile/agent-version
проверки остаются в отдельных variant-specific функциях. Общие проверки
before/after, закрытых ключей/policies, exact connection/version и APPLIED receipt
не стали серверной authority и не разрешают effect. Оба прежних публичных API
сохранены и используются с явным типом операции.

PASS LOCAL:8 существующих PWA unit tests, selected ESLint, typecheck/production
build. Воспроизводимая дополнительная проверка
`node tools/code-quality/check-integration-grant-refactor.mjs` сравнивает9001
вариант со source dc881e2e8 из Git: owner/fresh/applied/receipt, поля всех
снимков, чужие pins, неизвестные ключи, типы и числовые границы. Отдельные
положительные fixtures исключают ложный успех «обе стороны всегда отвергают».
Compiler diagnostics отклоняются до исполнения. Это конечная выборка, не
формальное доказательство всех возможных входов. Начальные ошибки оснастки
structuredClone/ESLint cwd и временный syntax после правки устранены; они
не выдавались за product PASS. Build сохраняет предупреждение о крупном chunk.

Полностью двуязычно описаны serviceruntime20/20 и HTTP context resource family
63/63 (commands/endpoints/views/fixture tests/new helpers). Runtime context,
cancel/join и cooperative shutdown бюджеты описаны по фактическому коду;
никакой принудительной отмены callback, которой нет в коде, не обещается.
Unit/build serviceruntime и полный gateway — PASS LOCAL; последний HTTP11.358s.

Working-4:21000 Go-объявлений,168 с новым форматом,20832 пока без полного
формата. Detector:2132 группы,926 Go/255 TypeScript. Число групп не обязано
монотонно падать: фиксированный minLines учитывает физический span, а подробные
комментарии увеличивают число строк между теми же code tokens. Порог не повышался.
Остальные группы и Go-пакеты остаются в работе; полный `-check` ещё не PASS.

## Четвёртая волна — журнал ошибок браузерных тестов и Go-библиотеки

У двух независимых наблюдателей `console.error` и `pageerror` вынесены общие
границы данных в `e2e/error-diagnostic-journal.ts`: ограничения полей и времени,
SHA-256, закрытая классификация origin, порядок событий, стадии, переполнение,
копирование snapshot и наблюдаемый номер документа. Способы получения источника
остались различными: у console — browser-reported location, у pageerror —
недоверенная строка stack. Прежние публичные имена классов и установщиков
сохранены. Две короткие типизированные обёртки классов адресно исключены
inline-маркерами `jscpd`: общий обработчик скрыл бы разное происхождение
location и stack. Тела браузерных обработчиков не исключены.

До и после изменения прошли те же26 unit-тестов двух наблюдателей. После
исправления циклической аннотации TypeScript отдельно прошли
`tsc --noEmit -p tsconfig.e2e.json` и selected ESLint. Это локальные проверки
тестового контура. Полная `npm run build` PWA также прошла с прежним
предупреждением о размере крупного chunk; браузерный сценарий здесь не запускался.

Полностью описаны собственные объявления и тесты `dnsresolver` (63/63) и
`credentialfs` (11/11). Комментарии сверены с кодом: минимум TTL не продлевает
авторитетный срок, `ReadKey` допускает projected-путь внутри корня с точным
конечным именем, а не обязательно непосредственно под корнем. Поведение и
граница чтения credentials не менялись. Оба модуля прошли полный локальный
`go test ./...` и `go build ./...` с Go1.26.6.

Working-7:6550 собственных исходников,181 generated/lockfile исключение,
2129 clone groups, из них926 Go и252 TypeScript. Целевые файлы двух
наблюдателей больше не имеют найденных групп. Go-реестр:21000 объявлений,
242 полностью двуязычных,20758 ещё требуют обработки. Число остальных
групп не закрыто и полный `-check` пока не проходит. После этой волны единый
PR остаётся Draft и не передаётся как готовый результат.

## Пятая волна — чтение защищённых файлов

В `libs/go/securefile` описаны все24 собственных объявления, включая
проверки разрешённых путей и точных режимов, исключение для projected token
`0640`, пробу `EROFS`, тестовые функции и реализацию `os.FileInfo` в fixture.
Пояснения не обещают запрета перехода в другой mount: фактическая проверка
ограничивает разрешённый путь заданным корнем. Файловое поведение не менялось.

PASS LOCAL: `go test ./...` и `go build ./...` модуля с Go1.26.6, формат
`gofmt`, `git diff --check`. Working-8:2129 clone groups; комментарии не
создали новых групп. Go-реестр:266/21000 двуязычных объявлений,
20734 ещё не покрыты. Итоговый `-check` и полный разбор дубликатов остаются
незавершёнными; PR остаётся Draft.

## Шестая волна — адресное исключение декларативного store API

В `src/features/access/store.ts` detector нашёл69 групп с участием файла:
66 сравнений большого списка возвращаемых Pinia-полей с импортами и другим
списком полей `platform/store.ts`, а также три перекрывающихся сравнения.
Повторяемого алгоритма в этом декларативном `return`-объекте нет. Только он
заключён в `jscpd:ignore-start/end`; состояние, очистка, команды и обработчики
store остаются в поиске. После повторного сканирования групп с участием
`access/store.ts` нет. Общий счётчик изменился с2129 до2070; это не
объявляет остальные группы разобранными.

Проверяемость исключений теперь закреплена в `tools/code-quality/ignored-clones.json`
и `check-addressed-ignores.mjs`: все5 точных областей имеют причину, максимальный
размер и SHA-256 содержимого. Проверка отвергает новый, исчезнувший, вложенный,
расширенный или изменившийся marker. Синтетический unit дополнительно
проверяет, что закреплённый jscpd действительно учитывает inline markers.
Маркер одного Pinia `return` не служит разрешением исключать другие stores.

`clone-worklist.mjs` группирует перекрывающиеся находки по точной паре файлов
и координатам обоих фрагментов, при этом сохраняет индекс и fingerprint каждой
сырой группы и машинно проверяет их полный взаимно-однозначный охват. Это
только навигация: кластер не назначает общее решение по смыслу. Для baseline
2138 групп →1153 пары файлов/1986 кластеров; для working-10 с новыми
инструментами 6555 исходников и2070 групп →1147 пар/1977 кластеров. Повтор
working-11 после комментариев `runtimesecret` сохранил эти числа; SHA-256
сырого отчёта `1cc2378cd79c8dc72491a8a1d072b6a5e9bf13f71410d1bafc507d0d64af761e`.
Отчёты хранятся локально вместе с
сырой записью анализатора; группы без решения не скрываются.

PASS LOCAL:7 Node unit-тестов инструментария, 10 unit-тестов access store,
selected ESLint и `npm run build` PWA. Vite сохранил прежнее предупреждение
о размере chunk. Полная приёмка и браузер здесь NOT RUN; итоговые карта
решений и Go-comment `-check` всё ещё незавершены.

Дополнительно полностью двуязычно описан модуль `runtimesecret` (10/10
объявлений): проверка server-owned scope tuple, ограничение ссылок, стабильное
имя immutable Kubernetes Secret и тесты. Изменены только комментарии;
`go test ./...` и `go build ./...` модуля с Go1.26.6 — PASS LOCAL.
Дополнительно описан `integrationgatewayauth` (37/37 объявлений, включая
тестовые): назначаемые сервером claims, exact keyset rotation, срок жизни,
проверка отдельного public snapshot и fixture-помощники. Изменены только
комментарии; `go test ./...` и `go build ./...` модуля с Go1.26.6 — PASS LOCAL.
Go-реестр на текущем дереве:313/21000 объявлений с форматом EN→RU,
20687 остаются без него. Проверка адресных исключений на inventory
working-11 — PASS (5/5), Node unit-тесты — PASS (7/7). Никакая из этих
локальных проверок не означает готовности всего PR.

## Седьмая волна — граница кэша

В `libs/go/cache` и `redisstore` подробно двуязычно описаны все33 собственных
объявления, включая восемь методов интерфейсов, и package docs. В первом
коммите волны интерфейсные методы были пропущены: прежняя запись «25/25»
была неверной; AST-проверка показала 25/33, затем комментарии добавлены.
Комментарии фиксируют фактическую границу: `Engine`
принимает успешно декодированный hit, поэтому проверка tenant/scope/version и
provenance должна выполняться конкретным сервисным `Codec`; библиотека не
подтверждает их сама. При ошибке кэша либо декодера чтение возвращается к
авторитетному `Source`, а ошибка записи кэша не отменяет удачное чтение.
Redis-клиент использует TLS1.3 с точным SNI и безопасно прочитанным CA.

Изменены только комментарии. `go build ./...` модуля с Go1.26.6 — PASS LOCAL;
`go test ./...` завершился успешно, однако в пакете и его Redis-подпакете
нет тестовых файлов, поэтому это лишь проверка компиляции, не проверка
поведения. `gofmt` и `git diff --check` — PASS LOCAL. Текущий Go-реестр:
346/21000 с форматом EN→RU;20654 пока не покрыты. Полный checker всё ещё
НЕ PASS; группы дубликатов этой волной не разбирались.

## Восьмая волна — общий выбор create/update в PWA

В `features/platform/save-versioned-resource.ts` вынесен общий выбор между
однократным OCC-обновлением существующего ресурса и созданием. Семь методов
`platform/store.ts` теперь передают ему свои типизированные generated-запросы:
project membership, platform membership, project, agent, role image recipe,
workflow и schedule. Точные path/body, versioned/create headers, signal,
readback и обновление соответствующего store остались на прежних местах.
При ошибке обновления создание не запускается. Новая общая функция не выбирает
actor, tenant, resource authority, retry или server decision.

PASS LOCAL на текущем рабочем дереве: 67 unit-тестов двух выбранных PWA suites
(включая три проверки helper), selected ESLint, TypeScript typecheck,
`npm run build`, Prettier и `git diff --check`. В Vite по-прежнему есть
предупреждение о большом chunk. Повтор анализатора working-15: 6557
собственных source-файлов, 181 generated/lockfile исключение, 2068 clone
groups против2070 на предыдущем чистом SHA; worklist1146 пар/1975 кластеров.
Это промежуточное измерение грязного дерева, не результат итогового SHA.
Адресные исключения5/5 проходят проверку. Остальные группы и Go-комментарии
не объявляются готовыми; браузер/deploy/live NOT RUN по текущему scope.

## Девятая волна — датированный каталог STT

В `libs/go/sttapi` полностью двуязычно описаны24/24 собственных объявления,
включая package docs, границу защищённого availability stream, безопасное
преобразование каталога, параметры и закрытую проверку model hints, ошибки
и оба теста. Generated protobuf-файлы не менялись. Проверена текущая
[документация API транскрипции](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create)
через Context7 `/websites/developers_openai_api_reference`: локальный реестр
с `ObservedAt` от05.09.2026 остаётся именно датированным снимком, не
live-проверкой аккаунта и не обещанием текущего перечня внешних моделей.
Поведение адаптера этой задачей не менялось.

`go test ./...` и `go build ./...` модуля с Go1.26.6 — PASS LOCAL; тесты
реально есть только в `modelprofile`, остальные handwritten-пакеты и generated
protobuf компилируются без тестовых файлов. `gofmt` и `git diff --check` —
PASS LOCAL. Go-реестр на текущем дереве:370/21000 EN→RU,20630 остаются.
Повтор clone detector после этих комментариев и итоговый `-check` ещё не
выполнены; весь PR по-прежнему в работе.
