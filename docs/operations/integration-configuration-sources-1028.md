---
id: OPS-INTEGRATION-CONFIGURATION-SOURCES-1028
title: Исполнение UI/Git IntegrationDefinition и чтение Git source
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-05
---

# Граница поставки

Issue #1028 / PR #1064 реализует consumer часть CFG и MVP-UI-39–42.
Control-plane #1046 владеет source/set/actor/connection, immutable revisions,
OCC, idempotency, grants, claim/fence и publication. Integration-gateway
получает private typed work и выполняет ограниченный provider effect.
Содержимое Git файла, package bytes и credential descriptor не выдаются
публичным DTO или логам. Git write-back является отдельным обязательным
сценарием CFG; этот read-only consumer сам его не реализует.

Checkpoint `67ba4ed561aaea253097af596f0cab2e86b90577` включает исполняемый
owner SourceWork из control-plane `65cd95f1fc94b37f05d9bf3273879889b74b03dd`,
политику 63 и последующий owner input/test-budget `626ffe141c1ce7190e1da686dbb3aa9020d6f5a5`.
Claim/complete больше не являются отсутствующей producer-зависимостью.
GitHub/GitLab остаются единственными source readers; JSON/YAML принимаются
владельцем, который связывает RoleImage revision с очередью build.

## Сквозная карта и переходы

| Сценарий | Authority и owner state | Consumer | Ответ/readback |
| --- | --- | --- | --- |
| UI/Git package invocation | User/agent eligibility → CP exact connection/revision/grant → protected invocation claim | Строгий Parse private package, executable registry, key/version/digest; package остаётся внутри запроса | Существующий provider effect/inbox/receipt путь; неизвестный mutation не повторяется |
| Connection test | Owner specialized test command → exact package/connection/credential work | Health capability выбранной revision с её budgets и constraints | CP test result; встроенный пакет не заменяет выбранную revision |
| Source claim | Configure/refresh командует CP после owner/tenant/root eligibility; existing integration-gateway workload получает task | Claim одного source work, проверка claimant/fence/lease/deadline, package и exact repository | Private source snapshot; обычный Run/Turn не создаётся |
| Initial/unchanged read | Сервер назначил repository/ref/path и прежний commit | Git ref → exact SHA → regular tree entry → blob/content; прежний commit равен новому либо отсутствует | Complete с исходным lease, commit, content/SHA256 и INITIAL/UNCHANGED; даже unchanged возвращает exact bytes |
| Forward read | Server-pinned predecessor в том же source generation | GitHub merge-base/status или GitLab merge_base подтверждают predecessor; затем immutable file read | FAST_FORWARD; CP типизирует документ и атомарно принимает новую revision/build либо INVALID candidate |
| Diverged history | Previous commit не является предком нового | Отказ до blob read | DIVERGED, authoritative owner source state; force-forward не выполняется |
| Отозванный credential/permission | CP повторно проверяет actor/connection до claim/complete; materialization exact | Отказ credential read/provider 401/403; чужой repo/package отклоняется до provider | Закрытый failure enum; CP назначает retry либо SYNC_BLOCKED |
| Lost ACK | Read effect уже завершён, исходный claim неизменен | Не повторяет Complete и не создаёт новую operation id | CP authoritative claim/source receipt; следующее чтение требует новой owner claim |
| Cancel/expiry | Минимум operation budget, owner deadline и lease с резервом ACK | Provider context отменяется, partial bytes очищаются; expired claim не начинает provider call | Owner reaper закрывает прежний claim и назначает новый attempt/fence |
| Invalid/oversize source | 256 KiB content, 1 MiB HTTP response, закрытые path/type/digest limits | Отказ без частичного содержимого и без provider diagnostics | RESPONSE_INVALID/CONTENT_INVALID либо owner validation result |

Owner events, cardinality, receipt и retry граф описаны в
`configuration-source-lifecycle-1046.md`. Gateway не публикует NATS напрямую
и не читает CP PostgreSQL. Публичные HTTP/SDK/PWA потребляют только owner
проекцию source/revision/diagnostic.

## Пакет и безопасность чтения

Глобальный adapter registry содержит только compiled shipped baseline.
Private package каждой test/invocation/source claim разбирается отдельно и
проверяется `ValidateExecutableRevision`. Совпадение provider key не объединяет
tenant/connection revisions. Display metadata и разрешённые сужения не меняют
adapter ownership, transport route, credential/network/resource contracts.
Missing package или pins mismatch не включают shipped fallback.
Весь Execute получает deadline capability; health дополнительно сужает timeout
и число попыток по `healthCheck` выбранного package. Сужение действует и для
GitHub, Synthetic и EMAIL, а не только для общего vendor HTTP adapter.

Source требует file-read capability с `risk=READ` и `approvalPolicy=NONE`.
Усиленная политика READ без отдельной owner Human Gate цепочки закрыто
отклоняется; Configure source не выдаёт grant обхода gate. Credentials читаются
по точной revision/Secret identity/content digest и очищаются после операции.

GitHub ref resolution использует SHA media type. Далее чтение идёт через
exact Git commit/tree/blob, с проверкой SHA, regular file mode, размера и
Git object SHA-1. Symlink и submodule не разыменовываются. GitLab связывает
commit, merge base, tree entry и file metadata/content SHA256/blob SHA.
Путь ограничен 32 сегментами; directory lookup GitLab — 10 страниц по 100.
Partial/truncated/duplicate/out-of-scope response закрыто отклоняется.

Существующие exact HTTPS/egress clients используются без redirect и direct
fallback. Source выполняет только GET без скрытого retry. Диагностика содержит
закрытый outcome; чужой provider body, email и содержимое файла не выводятся.

## Composition и readiness

Source work входит в existing integration worker, startup barrier и cancel/join.
Заявка ограничена одной задачей; provider operation — 20 секундами или меньшим
настроенным budget, с отдельным резервом CP ACK. Полный cycle budget учитывает
test, invocation и source phases. Метрика `configuration_source` учитывает
завершённый provider read даже при последующей ошибке owner ACK.

По GUIDE-DOC-003 Kubernetes readiness проверяет local issuer; доступность
удалённого CP не добавляется в Pod probe. Отдельная диагностическая метрика
`kodex_integration_gateway_work_path_ready` требует свежего успешного полного
owner work cycle. Initial/failed/stale cycle даёт 0; successful protected cycle
восстанавливает 1. Положительный `CheckLocalAuthority` без такого цикла не
доказывает готовность рабочего пути. Source provider failure, принятый CP как
типизированный outcome, не подменяется неготовностью всего gateway:
пользователь видит owner source state. Эта метрика не доказывает live provider
availability или acceptance и не заменяет exact owner readback.

Новый deployable/listener/credential namespace не появляется. Existing
integration-gateway application grant, mTLS, sidecar operation profile и
credential projection должны включать исполняемые SourceWork RPC. Оба итоговых
environment render проверяются после объединения owner/profile.

## Проверенная документация и evidence

Context7: `/google/go-github`, `/websites/gitlab`. Дополнительно проверены
официальные [GitHub commit API](https://docs.github.com/en/rest/commits/commits),
[trees](https://docs.github.com/en/rest/git/trees),
[blobs](https://docs.github.com/en/rest/git/blobs),
[GitLab merge base](https://docs.gitlab.com/api/repositories/#get-merge-base)
и [file API](https://docs.gitlab.com/api/repository_files/).

Targeted fixtures покрывают concurrent UI/Git revisions, detached pins,
initial/unchanged/forward/diverged, symlink/submodule, malformed/oversize
response, exact digest, foreign repository, expired claim, lost ACK и cleanup.
Результат каждого запуска привязывается к exact SHA в PR #1064. Наличие этой
матрицы не означает PASS: до запуска owner/consumer/profile checks, полного
baseline и live provider acceptance соответствующие строки имеют NOT RUN.

На объединённых source/contracts полный gateway race (31.278s), vet/build,
Proto/policy/ABI/package replay и web-only render — PASS. На точном `67ba4ed`
canonical PostgreSQL target — PASS (3.028s), оба integration render — PASS.
Target включает catalog owner probe и version-bound catalog: без этой setup
зависимости первоначальный запуск дал FAIL (1.836s), поскольку pinned models
не были созданы. Повтор exact запуска сначала дал инфраструктурный FAIL Docker
из-за занятого случайного host port; следующий запуск прошёл без изменения кода.
Полный baseline, Docker build нового worker, live source/provider и write-back
остаются NOT RUN.
