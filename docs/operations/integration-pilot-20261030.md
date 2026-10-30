---
id: OPS-DOC-INTEGRATION-PILOT-20261030
title: Интеграционный пилот Kodex — проверенная конфигурация и порядок запуска
type: operations
status: approved
owner: qa
version: 1.0.0
updated: 2026-10-30
---

# Результат и область допуска

По решению владельца 30.10.2026 проверяется ограниченный интеграционный пилот:
отдельные сотрудники, авторизованный OpenAI Codex account, MCP, GitHub,
файлы и согласования. Эпик [#1830](https://github.com/codex-k8s/kodex/issues/1830),
общий Draft PR [#1970](https://github.com/codex-k8s/kodex/pull/1970).

**GO для ручного интеграционного пилота в проверенном staging** в описанных
ниже границах. **NO-GO для объявления готовой новой установки на другом
сервере:** единый immutable cohort и полный RoleImage не квалифицированы;
чистая установка текущей ветки на новой площадке не выполнена. Наличие двух
проверенных штатных образов не доказывает переносимость всей установки.

Полный автоматический PDLC, crash-resume неопубликованного workspace,
автоматический fallback, merge → SRE → согласованный deploy и disaster recovery
ещё не готовы. Их отдельный список:
[остаток полного PDLC](full-pdlc-debugging-backlog-20261030.md).
Согласование в изолированном документальном процессе подтверждает механизм
Human Gate; оно не означает личную приёмку владельцем бизнесового PR или выпуска.

## Контрольная точка

Проверенный код и source двух новых образов:
`38be75a8c6e6bddbf8b0fa3d3169d4f781ad72ab`.
Основной клон остался на `f73c3fd707b6fb162e69a427dc1fcf449346ddfd`;
изменения дедубликации сохранены. QA-ветка:
`qa/1830-stabilization-main-20261028`. Этот отчёт добавляется отдельным
документационным коммитом; SHA отчёта не подменяет source уже собранных образов.

Применимый backend baseline выполнен на
`7e506c8a024c015f59b25ca46311d314299f6e8f`: четыре Go unit-контура,
четыре `go vet`, SQL boundary, native overload-контур и весь baseline из 36
PostgreSQL suites — PASS. В этом SHA frontend build обнаружил TS1117:
дублирующийся ключ `PROVIDER_OVERLOADED`. В 38be75a8 удалён второй ключ и
переиспользован существующий перевод; полные frontend lint/unit/build с
typecheck — PASS. Backend build inputs между 7e506c8a и 38be75a8 идентичны;
повтор Go-команд на 38be75a8 не заявляется.

На 38be75a8 также PASS: `make test-web-only-release` и
`make test-install-contract`. Это локальные проверки render/контрактов;
они не являются фактической чистой установкой или полным CI. Browser E2E всей
платформы на отдельной disposable установке — NOT RUN.

## Карта фактически обслуживаемых компонентов

Снимок после адресного выпуска CP/PWA30.10.2026,03:58UTC:
context `k3d-kodex`, namespace `kodex-system`, schema032 без DDL.
Cluster UID `eecf7cd0-b13d-4782-941d-81327283ba08`, namespace UID
`3aaa56c8-6cc1-4709-b16a-f3d9c5d28b7d`.
Публичный вход — `https://platform.kodex.works`, его API `/api/v1` и
realtime обслуживаются существующим маршрутом PWA/API. Ingress, SSO и соседние
приложения текущим выпуском не менялись.

Повторный readback после адресного CP38be75a8 restart подтвердил ту же карту и неизменность spec остальных пяти Deployment. Все перечисленные Deployment имеют 2 готовые реплики. Digest ниже относится
к application container, а не к неизменённым security sidecars.

| Компонент            | Фактический source SHA                     | SHA256 образа                                                      |
| -------------------- | ------------------------------------------ | ------------------------------------------------------------------ |
| control-plane        | `38be75a8c6e6bddbf8b0fa3d3169d4f781ad72ab` | `9f96c987f0e2b67e3cfb2e60357970c61c32c903c2667e94d40341e980750e2c` |
| staff-control-center | `38be75a8c6e6bddbf8b0fa3d3169d4f781ad72ab` | `91526321d2c3a8ef8187e3631a476720b3fd3620fb5bf053c24f0987646ad627` |
| runtime-controller   | `78214f8502cc23d8cc489de53541929e2d16cbb1` | `2f3da3975a07db2e589573515c680529e04802455d9ceba74e204e95aab6f6cf` |
| control-api-gateway  | `daa00b444cb273795e0260dde56da999281b8097` | `0cdbb3d938f54446be14f32070cea44b09fdf7faefafc235ff7f204d533d5ebd` |
| session-archive      | `958d5cb6829c8c2526fecdd4356db4cd56fcd94e` | `5719932c81927edb53e9817778cae24b3d8416c143aef76302304697af95e394` |
| integration-gateway  | `bbb98cced7e541668ec4a695de23c2709b73801a` | `9f1ede13e0b0f3aaf2cfa15ed0ad744688e4df338d52bd6b1819f6ce05986045` |
| secret-broker        | `dbd8b0579b21dbc6ac3619670765385c267eaa13` | `4230d0732eb0dc74faa8441edf802c5d1a967c4c5ec3e62151e9fde4c6659434` |

Для session-archive Pod annotation содержит старый 26c5841; фактический source
установлен по точному build/publication digest. Аннотация сама по себе
не использована как доказательство.

Основной RoleImage ABI3:
`sha256:87a6bb447880b0f7726fbfab49883870342d87aa4c0138f2f3bacbf1573808fd`.
У отдельной ранее созданной QA-роли только для файлов image
`sha256:5f99a312c24a1db213b470c2ade509c4a17711ea8aa4198582b6e3a05f9c88b0`.
`SESSION_WORKSPACE_V2` выключен. CP/PWA читают immutable OCI, hostPath у них
отсутствует; `.git`, env и чужие worktree в приложение не монтируются.
Остальные пять Deployment не изменили UID/spec/images при этом выпуске.

## Что реально проверено

Данные относятся к указанным запускам и компонентам. Исторический PASS
не переносится автоматически на всю текущую платформу.

| Проверка                                                        | Статус                                               | Доказательство и граница                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Авторизованный Codex account и реальные Terra/medium исполнения | PASS                                                 | Та же FIXED-учётная запись, свежий каталог, фактические RuntimeRevision/grants; лимиты не повышались.                                                                                                                                                                                                                                                                                                                                                                                          |
| Sol/high при перегрузке                                         | FAIL выполнения, PASS диагностики                    | `run_jVrjowd93NdKUSsPNQoB6doJ`: native `server_overloaded`. Обычный новый запуск после явно разрешённого выбора Terra; это не автоматический fallback.                                                                                                                                                                                                                                                                                                                                         |
| GitHub чтение/запись документа/комментарий через сотрудника     | PASS                                                 | [Запуск аналитика](https://platform.kodex.works/projects/prj_NAKh_NrtthS8M2rg8CtWUMEM/runs/run_zrXe2rMon2BMWXN8fHHGWRWg): настоящий commit1583d5ee и [комментарий](https://github.com/codex-k8s/test-repo/pull/3#issuecomment-6513775429), native receipts и независимый readback. CP тогда 23dac184; IG/RoleImage с тех пор не менялись.                                                                                                                                                      |
| Двухшаговый SERVER_DAG аналитик → QA                            | PASS в узком профиле                                 | Два успешных прогона до/после CP23dac restart; на CP/PWA 38be75a8 [успешный прогон до restart](https://platform.kodex.works/projects/prj_NAKh_NrtthS8M2rg8CtWUMEM/runs/run_G1ldvJwgRBWQg3vDUQrvin18) и [после restart](https://platform.kodex.works/projects/prj_NAKh_NrtthS8M2rg8CtWUMEM/runs/run_x1JKnRacPZwk0t2Z0VDUOfpa). Сервер принимает обязательные ARTIFACT и запускает QA без модельного хода менеджера. Это документальный процесс, не GITHUB_PDLC.                                 |
| Отдельный продуктовый рецензент → GitHub review COMMENT         | PASS на 38be75a8                                     | [Запуск рецензента](https://platform.kodex.works/projects/prj_NAKh_NrtthS8M2rg8CtWUMEM/runs/run_NnHf9gaAT8v5dRjesIPmjh1c) создал [настоящее review](https://github.com/codex-k8s/test-repo/pull/3#pullrequestreview-5708140672) на 1583d5ee без Gate,8 точных grants с NONE и 4CLEAN файла. Найден содержательный пробел памятки; весь PR не принимался. URL теряется в native projection — [#1976](https://github.com/codex-k8s/kodex/issues/1976); host readback подтвердил ссылку отдельно. |
| Физическое продолжение QA                                       | PASS в одном сценарии                                | В последнем процессе 2 логических шага и 3 физических AGENT_EXECUTION: дополнительное CONTINUES завершилось в том же QA child run; оба обязательных результата приняты.                                                                                                                                                                                                                                                                                                                        |
| APPROVE через Chrome UI                                         | PASS механизма                                       | Gates `gat_lOJBa244aAtF0mU4fsPwqrlM` и `gat_vXqA2gy2u6_m7zTAMZaeFWPU` APPROVED, оба root SUCCEEDED;8 и 6CLEAN файлов. Второй UI POST/resolution HTTP200, Console без error/warn. Отдельные предыдущие сценарии REJECT/CANCEL/expiry проверены до выпуска 38be75a8. Полная матрица гонок — NOT RUN.                                                                                                                                                                                             |
| Более ранний deadline исполнения во время Gate                  | PASS отрицательной проверки                          | `run_HPuTMve5oIsVQeHidU9i7BvQ`: шаг исчерпал 300s раньше собственного срока Gate; root FAILED/RUNTIME_TIMEOUT, Gate CANCELLED. Это действующая политика [#1869](owner-gate-expiry-1869.md), а не неисправность expiry.                                                                                                                                                                                                                                                                         |
| Файлы сотрудника доступны владельцу                             | PASS                                                 | Точные CLEAN revisions скачаны с совпадающим SHA256; реальный UI preview/download `csv-export-review.md` на 38be75a8 HTTP200, Console без error/warn.                                                                                                                                                                                                                                                                                                                                          |
| Вложение доступно сотруднику                                    | PASS                                                 | `run_spjxXu-PiFGhv59d56S5RxNq`: штатный FINALIZED attachment set, MCP read до EOF,4CLEAN результата.                                                                                                                                                                                                                                                                                                                                                                                           |
| Подмена file locator материалом другого QA-проекта              | PASS только этой границы                             | Собственный файл → чужой artifact ref → собственный файл. Средний вызов закрыто отклонён, содержимое не выдано; host-корреляция показывает NotFound/control_notfound. Сам сотрудник честно оставил BLOCKED/UNKNOWN причины скрытой ошибки. Полная изоляция — NOT RUN.                                                                                                                                                                                                                          |
| Карантин и scanner                                              | PASS на 38be75a8                                     | Latest/exact DOWNLOAD и exact PREVIEW заблокированного HTML возвращают 403. На обоих новых CP scanner принял чистый образец и распознал безопасный EICAR через Unix INSTREAM; во внешние каналы образец не отправлялся.                                                                                                                                                                                                                                                                        |
| Сохранность опубликованных результатов                          | PASS в проверенном наборе                            | 28 точных файлов после CP23dac restart,34 после замены CP/PWA на 38be75a8 и 56 после адресного restart того же CP38be75a8; точные revisions/hashes сохранены. Не доказывает сохранность неопубликованного workspace.                                                                                                                                                                                                                                                                           |
| SSH через сотрудника SRE/проектный secret                       | PASS inspect, исторический                           | `run_bGsDmvt5RSbF6hVeC14KGLhr`: разрешённый inspect; чужой grant отклонён без внешнего вызова. Deploy не проверен этой операцией.                                                                                                                                                                                                                                                                                                                                                              |
| Вход/refresh/reload/reconnect/logout                            | PASS отдельных операций / UNKNOWN периодический MIME | Ранее выполнены login, natural refresh, WS reconnect/offline/reload/logout204/GET401. Свежая текущая вкладка подключена; причина периодического MIME-сбоя #1868/#1974 не доказана устранённой.                                                                                                                                                                                                                                                                                                 |
| Стандартные immutable CP/PWA build/publication/node pull        | PASS двух компонентов                                | Оба образа собраны штатным механизмом, опубликованы и реально обслуживаются на двух репликах с exact digest/source. Полный общий release и новая установка — NOT RUN.                                                                                                                                                                                                                                                                                                                          |
| Полный PDLC и новый сервер                                      | BLOCKED / NOT RUN                                    | Ограничения, причины и условия следующей приёмки перечислены в отдельном backlog.                                                                                                                                                                                                                                                                                                                                                                                                              |

## Ручной пилот в текущем staging

1. Открыть проект «Журнал обращений» и убедиться в Connected и доступности
   истории последнего успешного процесса. Выбрать отдельную небольшую задачу
   сотруднику, а не запускать старый незавершённый полный PDLC заново.
2. Проверить READY выбранного сотрудника, разрешённый проектный account/model,
   image и точные grants. После доказанной перегрузки Sol допускается явный
   Terra/medium; общий предел остаётся 2 исполнения. Настройка карточки не заменяет
   проверку effective RuntimeRevision конкретного запуска.
3. GitHub ограничить `codex-k8s/test-repo`; credential — `TEST_REPO_GITHUB_PAT`
   в проектной интеграции. Owner PAT и Kubernetes/SSH admin credentials
   сотрудникам не выдавать. Обычные разрешённые READ/WRITE/COMMENT идут с NONE;
   merge и deploy требуют собственных отдельных Gates.
4. Для файловой проверки передать собственное CLEAN вложение через штатный
   attachment set. Сотрудник должен прочитать вход, сохранить результат в
   `.kodex/outbox`, выполнить capture и вернуть настоящие назначенные refs.
   Владелец открывает «Файлы и знания» → Results, preview/download и версии.
5. Для процесса использовать опубликованную версию
   `wfl_5akvTM9TEYgKRGUL_AMNX9aV` v3. Проверить два принятых логических результата,
   серверный downstream и финальный Gate. Deadline исполнения продолжает идти
   во время Gate; открыть решение сразу после появления. Поздний закрытый Gate
   не оживлять и не подтверждать повторно.
6. При UNKNOWN внешней записи сначала проверить exact объект и native receipt.
   После Stop/timeout не считать рабочие файлы сохранёнными без capture.
   Завершённый отчёт и сохранённая ревизия — отдельные проверяемые результаты.

## Сборка, перенос и установка

Проверенные CP/PWA получены последовательно через
`tools/release/build-stable-local.sh` с exact clean source38be75a8,
`--component control-plane|staff-control-center` и отдельными новыми private
`--output-directory`. Build использует `git archive`, исключает `.git`, env,
untracked и секреты; build/cache/output не разделяются writable репликами.
Публикация выполнена штатным `stable-image-publication`, работающий node pull
доказан фактическими Pod imageID. Локальные build receipts с полями NOT_RUN
для следующих фаз не подменяют последующие publication/serving proofs.

| OCI-архив                    | SHA256 байтов архива                                               |
| ---------------------------- | ------------------------------------------------------------------ |
| control-plane.oci.tar        | `31a5bf444c2168b807a339eb06f119dd79b1568ce2f5abf21fc50fcfd92c3c44` |
| staff-control-center.oci.tar | `15300cb595197370d98b572c8fe4d38a6282dc86d8eec0c4f7bc0007e234d31a` |

Архивы и receipts сохранены в приватном каталоге оператора; credentials и
конфигурационные снимки в обычный диагностический архив не включены.

Для другой площадки применяется существующий
[runbook чистой установки](../runbooks/fresh-install.md): выбрать exact
Kubernetes context/install mode, заполнить принадлежащий оператору 0600
`.kodex-env`, подготовить TLS/OIDC/registry/S3 и минимальные credentials;
создать material штатным установщиком. Не копировать env, Secret, kubeconfig,
рабочие PVC и приватные backups текущего staging в новую установку.

Перед фактической установкой нужны отдельные exact target/configuration и
квалифицированный полный release, включая RoleImage, migration head и
node pull. Текущий смешанный набор нельзя молча подставить вместо проверенного
целостного install artifact. `make test-install-contract` подтверждает
контракт установщика, но не снимает этот NO-GO. Общий reset, reinstall k3s,
смена firewall/ingress и production этой проверкой не разрешаются.

## Проверка состояния и возврат

Текущий выпуск CP/PWA:
`49727caf-3486-4bf4-b9ae-48b659408aef`, без миграций.
Readback сверяет cluster/namespace/Deployment UID, actual imageID, source,
replicas, отсутствие hostPath, неизменённые пять Deployment, session API,
реальный процесс, выдачу файлов и Console/Network. Ready Pod сам по себе
не означает успешную приёмку.

Приватный исходный снимок и `rollback-manifest.json` находятся у оператора в
каталоге выпуска `release-pilot-consumers-38be75a8`. Для возврата использовать
**новый** `scoped-release.mjs plan` с этим manifest и затем apply нового плана
по [штатной процедуре](../../tools/release/README.md). Manifest проверяет
`rollbackOf`, ожидаемые текущие images и сохранённые предыдущие image/source:
CP23dac184 и PWA41c0c918dad16067c89e3ec0ed39dd3ae3e7cf21. Старый apply не повторять. После неизвестного
ответа сначала inspect/readback по его журналу; общий `rollout undo` не применять.
После возврата проверить session, историю, exact files/hash и отсутствие
запуска terminal workflow. Rollback возвращает исполняемый код и не удаляет
проекты, файлы, Gates, credentials или PVC.

Повторный same-source restart CP38be75a8 имеет ID
`fa0081c4-b97e-4a4d-926d-b07f6c4cb57f`; plan SHA256
`bd03d3e29edc394b939e71c622099c209b57dc96c4180cdbeb8ec14f4caf33c8`.
Два новых CP Pod UID: `39454787-8b20-4057-97ec-9e10a57c7198` и
`f0ce6475-d00a-4fed-a04c-b7b8babc33bf`, image/source прежние.

Для адресного restart применяется `application-restart.mjs plan/apply/readback`
с exact target/context и canonical approved plan hash. В проверенном прогоне
ACK первоначально сопровождался UNKNOWN из-за ещё завершающихся старых Pod;
отдельный readback стал PASS без повторного restart. Тайм-аут ответа команды
не даёт основания повторить внешний эффект.

## Что требуется от владельца

Для ручного пилота в текущем staging новых решений не требуется.
Перед новой установкой: назвать exact площадку/режим, согласовать допуск
квалифицированного release и принять перечисленные ограничения пилота.
Итоговый QA PR остаётся Draft, автоматического merge или production deploy нет.
Значения secrets, cookies, DSN, kubeconfig и credentials не опубликованы.
