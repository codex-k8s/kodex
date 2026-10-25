---
id: OPS-DOC-1859
title: Публикация CLEAN bundle исходников одним commit
type: operations
status: approved
owner: developer
version: 1.0.2
updated: 2026-10-25
---

# Контракт и границы #1859

Требование #1859: сотрудник передаёт подготовленные файлы, а не перепечатывает
base64 в MCP. Новый typed путь не меняет существующий `content.create`24KiB,
не включает code-mode и не передаёт PAT модели. Точный typed JSON bundle и canonical
manifest становятся immutable proposal. Отдельный Human Gate подтверждает
полный набор путей/размеров/SHA256, точную базу и repository/connection/grant
pins. Integration-gateway создаёт один single-parent commit и публикует его
одним exact CAS в новую назначенную сервером proposal branch. Source branch,
PR и merge этим решением не изменяются.

## Карта producer → owner → consumer

| Граница | Полномочия и immutable данные | Результат |
| --- | --- | --- |
| Runtime filesystem/capture | Файлы создаёт сотрудник; import/scanner назначает настоящие revision/version/digest/size и CLEAN | Archive + manifest доступны следующему turn через штатный file catalog |
| Native MCP prepare | Exact execution lease/fence/generation/RuntimeRevision; выбранный grant из immutable input | Компактные artifact pins и base commit, не inline bytes и не caller actor/project |
| CP prepare | Exact live execution, root initiator, project, grant/connection/package, artifact view/download eligibility | Полная byte-validation и frozen proposal, gate и audit одной owner transaction |
| Owner API/PWA | Текущие owner/resource permissions, proposal version и approval digest | Полный paginated manifest, APPROVE/REJECT/CANCEL; summary не подменяет полноту |
| CP work claim | Integration-gateway exact method/workload, durable attempt/generation/fence/expiry | EXECUTE либо только RECOVER_READ_ONLY; credential descriptor доступен лишь private worker |
| Private content read | Exact proposal/work lease + frozen artifact digests | Bounded verified bytes; runtime lease после завершения turn не используется как worker grant |
| IG prepare/Begin | Bounded Git workspace, immutable manifest/archive, deterministic author/time/message/base | Candidate commit/tree и durable Begin до первого внешнего эффекта |
| IG CAS/readback | Один refspec новой proposal branch, expected oldOID пустой | Максимум один push; независимый exact commit/parent/tree/every blob/bytes readback |
| CP complete | Exact lease/candidate и прежний Begin, owner receipt replay | SUCCEEDED либо UNKNOWN; совершённый эффект не повторяется после потери ACK |

Capability — `github.repository.bundle.publish`, WRITE,
`HUMAN_EACH_EFFECT`, exact configured repository. Ранее выданные
content.create/update grants сами по себе новую bulk capability не выдают.
GitHub API и Git destinations остаются exact HTTPS/TLS tuples shipped package.
Готовность новой capability требует обычной публикации/binding definition,
допуска credentials/network и fresh RuntimeRevision; schema сама readiness не
доказывает.

## Полный immutable набор

Manifest версии1: canonical UTF-8 JSON без whitespace, поля
`version,total_bytes,files`; entries отсортированы по path и содержат
`path,mode,bytes,sha256`. Все digest вычисляются по точным bytes, включая binary
и newline. Допускается256regular files,1MiB unpacked,1MiB encoded container,128KiB manifest,
512bytes/path. Бюджеты независимы от single-file capabilities. 108файлов/291KB укладываются в transport bounds; это не допуск форматов
либо подтверждение CLEAN каждого decoded файла. Реальный пакет #1859 с
обязательным HTML сейчас BLOCKED действующим native format policy; его файлы
не удаляются/переименовываются ради прохождения.

Отклоняются extra/missing entries, дубли/коллизии path, traversal, absolute
paths, Git metadata, credential filenames, encryption, symlink/hardlink/device,
неподдержанные mode/compression, неверные totals/digests. Архив не извлекается
по пользовательским путям и не исполняется. Hidden content не нормализуется и
не пропускается молча. Manifest не содержит credential values.

## Двухфазный native producer и допуск содержимого

Producer turn сотрудника создаёт локальные `repository-bundle.json` и
`manifest.json`, затем завершает turn для штатного outbox capture. Каждый
outbox artifact не превышает1MiB; manifest ограничен128KiB. Следующий
immutable RuntimeRevision получает exact CLEAN ArtifactRevision pins через
обычные attachment/callback/file catalogs. Только этот свежий leased turn
вызывает `prepare_repository_bundle_publication` с11scalar source/base pins
и exact grant_ref. Завершённая model lease не продлевается ради новых refs;
будущие refs не выдумываются. Кодирование выполняет сам сотрудник над своими
исходниками; host не реализует бизнес-приложение и не передаёт source bytes
через model MCP arguments.

Canonical container имеет форму
`{"version":1,"manifest_sha256":"<64 lowercase hex>","files":[{"path":"main.go","mode":"100644","content_base64":"<standard padded Base64>"}]}`.
Порядок files равен полному manifest. UTF-8 JSON не содержит whitespace,
порядок полей точный; `<`, `>`, `&`, U+2028/U+2029 записываются через lowercase
`\uXXXX`, как в canonical Go JSON. Неподдержанная версия/дополнительные либо
повторные поля/неcanonical Base64 закрыто отклоняются. Размер encoded
container проверяется отдельно: верхний decoded limit сам по себе не
обещает, что любой такой набор поместится в ordinary outbox.

Outer JSON CLEAN не подтверждает decoded content. До создания invocation/Gate
CP для **каждого** actual decoded member вызывает прежний
`artifactpolicy.InspectReader` с настоящим filename и server-derived MIME,
требует CLEAN, затем вызывает обязательный `repository.scanArtifact` для
точных bytes/SHA256/size. Infected, недоступный/устаревший scanner, mismatch,
quarantine и unsupported content не создают Gate. Хранятся только metadata
формата и `clamav-instream-v1` receipt, привязанные к полному manifest и source
pins. HTML/SVG/XHTML, shebang/MZ/ELF и неподдержанный SQLite database binary
не проходят существующую policy. Исходники Go/SQL/обычный текст проходят
только при действительном AV admission. 108файлов с запрещённым member
честно BLOCKED; нельзя молча опустить, переименовать или перекодировать
этот member ради CLEAN. Typed container не меняет global artifact policy.

Правила: FR-042, DOM-MC-008, ARCH-MC-008 и GUIDE-DOC-003, раздел
«Антивирусный допуск точных байтов файла». PostgreSQL хранит только manifest
metadata, exact immutable ArtifactRevision/S3 receipt pins и per-member
scan receipts. Тела остаются в существующих S3 versions (ADR-MC-006), новых
Put либо prepared-object cleanup в этом пути нет. UNKNOWN DB commit
не удаляет sources и не разрешает новый publication key.

Publication является retention consumer обоих source revisions до terminal;
UNKNOWN продолжает удерживать pins после cancel/deadline/root terminal.
Owner PURGE impact использует тот же `artifact_has_retained_revisions`, что
physical purge guard: retained boolean входит в impact digest, а причина
`ARTIFACT_HAS_RETAINED_REVISIONS` запрещает PURGE в preview и VFS. Она не
превращает завершённый run в активный и не запрещает штатный soft DELETE.
Admission удерживает ACTIVE artifact HEAD до commit. Canonical owner purge
и retention worker сохраняют HEAD lock и fresh owner trigger recheck перед
physical Delete. Pin не является permission: pre-Begin private source read
повторяет fresh ACL/CLEAN/grant checks до и после exact S3 read. Read-only
UNKNOWN recovery не скачивает original source body вообще: полного metadata
manifest достаточно для независимой проверки remote file bytes/digests.
Terminal release снимает только этот consumer; другие native references
продолжают удерживать artifact. Только полный authoritative project purge
может удалить terminal publication; unresolved publication блокирует purge.

Root completion учитывает pending publication. Оба порядка — сначала origin
complete либо сначала effect complete — сходятся через specific publication
owner join и прежнюю CONTINUES uniqueness. Replay, expiry и cancel не открывают
terminal root или прежний runtime grant. Старые generic integration paths
не получают нового общего completion fallback.

Для делегированного сотрудника callback исходному менеджеру удерживается до
завершения публикации и её immutable continuation. Если тот же producer
делегировал вложенную работу, новый kind использует существующий callback join:
terminal publication и все child receipts должны существовать одновременно.
Ни nested callback первым, ни Git readback первым не создают промежуточный turn;
последний prerequisite создаёт ровно один CONTINUES. Terminal wakeup выполняется
после сохранения owner ledger в той же транзакции. UNKNOWN продолжает удерживать
join. Новый Task содержит child artifact pins, полный aggregate outcome и точные
Gate ref/version/decision/comment; model summary не подтверждает Git write.

Final callback закрепляет исходное server-owned CALLBACK_TO/DELEGATED_TO и
exact final RuntimeRevision/artifacts. Допустимые промежуточные CONTINUES
проверяются по тому же tenant/root/child run/agent/session/attempt и завершённым
lease/revision; хотя бы один terminal witness именно нового kind обязателен.
Prepare после уже доставленного исходного callback отклоняется до создания
proposal/Gate/intent. Этот допуск не исправляет и не расширяет generic1848 path.
Settled FAILED/CANCELLED передаются менеджеру как owner-derived outcome даже
при model summary «published successfully». Полные counts и явно latest
доступны в parent input; latest не является списком всех исторических receipts.

Предел immutable Task — 32768 кодовых точек. Если полный owner callback input
нового kind превышает его, откатывается только presentation savepoint:
authoritative Git result, exact candidate/readback и COMPLETE receipt остаются
атомарно сохранены. Отдельные `continuationState=BLOCKED`,
`continuationCode=CALLBACK_INPUT_TOO_LARGE`, `continuationNodeRef` показывают
точный недоставленный join. SQL, authority и другие Conflict не превращаются
в этот исход. Сводки/refs не обрезаются, пропущенная информация не выдаётся за
полную, автоматический WRITE/retry либо новый ключ запрещены.

Owner Get manifest и PWA показывают причину отдельно от Git `SUCCEEDED`.
Полные original child runs/results/artifact pins остаются доступны через
существующий owner run graph/readback. В blocked состоянии нет нового model
turn: child/root completion counters и purge удерживают pending delivery.
Владелец использует штатный CancelRun: leases/grants закрываются, delivery
становится `CANCELLED`, а Git receipt/commit/pins не меняются. Автоматического
обрезания или возобновления oversized task нет.
Отзыв source/grant после terminal Git не снимает BLOCKED presentation barrier:
активных write grants уже нет, а недоставленный ввод не становится доставленным
от изменения полномочий. Его закрывает именно штатное закрытие root.
Если completed producer теряет fresh launch authority до terminal wakeup,
сохраняется отдельный `BLOCKED/CONTINUATION_AUTHORITY_CHANGED`: disabled agent
не получает turn, а исходный parent result не выдумывается. SQL отличает это
состояние от незавершённого producer, уже доставленного callback и terminal
root. Fresh authority deny достаточен для BLOCKED даже при ещё ожидаемых
вложенных детях; их завершение не утверждается. Оба порядка disable/origin
проверяются отдельно; недоставленный
late-disable result удерживается до owner CancelRun с полным outcome readback.
Тот же barrier сохраняется после read-only подтверждения Git past fact, если
отзыв source authority уже закрыл native invocation в UNKNOWN. Даже при
сохранённом `agent.launch` это не разрешает новый turn/grant: parent callback
остаётся withheld, owner видит Git SUCCEEDED отдельно от delivery BLOCKED.
Native CancelRun закрывает delivery и active-root input retention, не меняя
подтверждённый commit. UNKNOWN fixture завершает model turn до реального31s
backoff; длительность runtime lease не расширяется и heartbeat не подделывается.

## Матрица жизненного цикла

| Исходное состояние / событие | Переход и effects |
| --- | --- |
| Новый exact prepare | WAITING_APPROVAL; durable idempotency scope origin+key, input digest conflict при другой нагрузке |
| Повтор prepare | Та же proposal/gate, без второго create |
| APPROVE exact version/digest до expiry | QUEUED после fresh authority/pins check |
| REJECT/CANCEL/EXPIRED до Begin | Terminal, ноль внешних effects |
| Claim QUEUED | CLAIMED; новые attempt/generation/fence, immutable input без замены current artifact |
| Claim expired до Begin | Bounded новая attempt либо FAILED; прежняя lease недействительна |
| Begin exact candidate | EFFECT_STARTED + durable receipt, candidate identity больше не меняется |
| Begin ACK потерян | Push запрещён; последующий owner read определяет режим |
| Push ACK/Complete ACK потерян, lease истекла после Begin | UNKNOWN_OUTCOME; только read-only recovery, никогда новый push/key/commit |
| Exact ref/commit/parent/tree/blobs подтверждены | SUCCEEDED + durable receipt; повтор complete возвращает прежний результат |
| Terminal Git + oversized callback input | Git SUCCEEDED сохранён, delivery BLOCKED/CALLBACK_INPUT_TOO_LARGE; child/root не получают ложный terminal success |
| Owner CancelRun blocked graph | Delivery CANCELLED и leases/grants закрыты; Git fact/COMPLETE receipt неизменны, нового WRITE нет |
| Ref отсутствует/другой либо readback недоступен | UNKNOWN сохраняется; отсутствие не разрешает resend |
| Cancel/revoke/expiry после Begin | Authority на WRITE закрыта, состояние UNKNOWN до readback; не утверждается rollback внешнего действия |
| Root/run terminal до Begin | Закрытый отказ, прежний worker grant не начинает эффект |
| Root/run terminal после Begin | Только exact recovery прежнего эффекта, без открытия нового turn/run |

PostgreSQL владеет proposal, origin, idempotency, approval, work и receipt.
Ни actor/project из payload, ни manifest digest, ни TLS peer по отдельности не
дают authority. Decision не разрешает другой base, branch, archive или grant.
Один origin RuntimeRevision/node принимает одну bundle proposal; повтор не
создаёт второй commit/Gate. Новый осознанный запрос после решения владельца
получает свежий native turn/revision, а не новый key неизвестного эффекта.
Для одинаковых project/canonical repository/base/archive/manifest допускается
одна незавершённая proposal, включая UNKNOWN. Смена request key, runtime turn
либо connection alias не превращает неизвестный эффект в новый push.
Исходная ветка не CAS-проверяется как destination: CAS применяется только к
новой proposal ref. Обновление существующей ветки данным профилем запрещено.

## Профиль межсервисного доступа

Приёмка текущего staging выполняется только с явно настроенным
`KODEX_RPC_PROFILE=trusted-cluster` у CP, API, runtime-controller и
integration-gateway. CP загружает встроенный `service-identity-policy.json`,
а вызывающие компоненты используют generated registry из controlplaneclient.
Восемь новых bindings доставляются их неизменяемыми образами: owner Get
требует пользовательскую учётную запись, Prepare и шесть worker-операций
разрешают principal через владельца состояния. Проверки exact lease, grant,
поколения, проекта и закреплённых артефактов сохраняются. Этот профиль не
является доказательством mTLS либо допуска через sidecar verifier.

Отдельный профиль с authority publisher требует согласованной доставки
policy revision, монотонного source revision, подписанного snapshot,
readback потребителей и отдельной проекции CP. Он не включается неявно
выкладкой пяти компонентов. Существующий оператор перехода, ограниченный
ревизиями76/77, не доказывает доставку policy98; такая поставка здесь
`NOT RUN` и требует отдельного подготовленного перехода. Общие ресурсы
authority publisher в текущем staging этим изменением не создаются.

## Доставка schema и потребителей

Изменены CP, integration-gateway, runtime-controller, control-api-gateway и
PWA; agent-runner input ABI не меняется. Штатный outbox producer уже умеет
captured regular files, новый runner image сам по себе не требуется. До
создания capability grant и model turn нужно доставить всю цепочку и authority
registry новой RPC service. Отсутствие исполнителя либо schema — закрытый отказ.

1. Root получает clean source SHA и exact опубликованный CP image с matching
   provenance. Отсутствие активных owner work проверяет existing migration
   preflight. Source schema HEAD — ровно20261025000200, predecessor Goose —
   ровно20261025000100; иные версии не угадываются и не изменяются вручную.
2. В private0700 каталоге сохранить исходный repo-owned render без подмены
   labels/Secret/config и без содержимого Secret.

```sh
umask 077
kubectl kustomize "$QA_SOURCE/deploy/k8s/overlays/staging/control-plane" > "$PRIVATE/control-plane-base.yaml"
node "$QA_SOURCE/tools/release/control-plane-migration.mjs" plan \
  --context k3d-kodex --profile repository-bundle \
  --source "$QA_SOURCE" --revision "$QA_SHA" \
  --base-render "$PRIVATE/control-plane-base.yaml" \
  --publication-plan "$PRIVATE/publication-plan.json" \
  --publication-evidence "$PRIVATE/publication-evidence.jsonl" \
  --plan "$PRIVATE/repository-bundle-migration.json"
```

3. Для того же immutable plan по очереди `check`, `stop`, `apply`, `inspect`,
   `resume`. Каждый вызов использует ровно эти arguments:

```sh
node "$QA_SOURCE/tools/release/control-plane-migration.mjs" check \
  --context k3d-kodex --profile repository-bundle \
  --plan "$PRIVATE/repository-bundle-migration.json" \
  --evidence "$PRIVATE/repository-bundle-migration.jsonl" \
  --confirm APPLY-STAGING-CP-MIGRATION
```

   Для `stop/apply/resume` меняется только phase. `inspect` имеет те же plan и
   evidence без `--confirm`. Не выполнять `stop` до authoritative check PASS
   exact Job/Pod/imageID/fixed stdout. `apply` разрешён только после остановки
   exact прежнего CP и fresh zero-owner proof; timeout/UNKNOWN требует inspect,
   а не новый key/Job. Единственный explicit attempt2 использует прежние
   predecessor plan/journal и knownfailed+resumed proof; automatic third attempt
   запрещена. Job300s/backoff0, native pg_isready init и existing migrator SA,
   Secret/CA pins, namespace boundary сохраняются. Host mounts и новые creds не
   добавляются. Полный stop/resume/crash protocol описан в
   [OPS-CP-IMMUTABLE-MIGRATION-1855](control-plane-immutable-migration-1855.md).
4. Schema PASS дополнительно требует profile-specific READ ONLY postcondition:
   exact Goose, table owner,13deferrableFK, отсутствие body columns, immutable
   guard, publication retention consumer и105node/289edge purge graph guard.
   Прямой psql/DDL и изменение Goose вручную не являются delivery path.
5. Дождаться authoritative migration Job terminal и прекращения его процессов;
   **в любом исходе** `resume` возвращает exact прежний CP image/replicas2.
   Postcondition failure не блокирует возврат старого приложения после proven
   terminal. Не возобновлять CP при ещё исполняемом migrator.
6. После schema PASS выполнить existing scoped immutable release: сначала CP,
   затем IG и RC, затем API/PWA. Новый IG polling требует нового CP RPC, поэтому
   IG-first не является разрешённым rolling порядком. Existing6.0 connection
   продолжает работать через exact known compatibility snapshot; unknown
   versions/digests отвергаются,6.0 не получает новую capability. Выдача нового
   grant разрешена лишь после готовности всех пяти новых компонентов и
   authoritative RPC policy. Rollback — прежние exact images и compatibility
   package без удаления additive schema; UNKNOWN receipts/source pins сохранять.

## Перепривязка существующего QA GitHub без доступа модели к PAT

Владелец использует обычный Integration Definition editor. Канонический
`contracts/integrations/v1/definitions/github.yaml`6.1.0 передаётся как YAML
content в `POST /api/v1/integration-definition-configurations/drafts`.
Затем exact revision проходит `.../revisions/{revisionRef}/validation` и
`.../publication`. Это команды CREATE/VALIDATE/PUBLISH_INTEGRATION_DEFINITION_DRAFT,
не прямой SQL. На каждом этапе readback возвращает configuration/revision refs,
version/digest и новый ETag; mutation использует fresh If-Match и stable key.

`POST .../revisions/{revisionRef}/consumer-bindings` получает authoritative
impactDigest и единственного consumer `kind=INTEGRATION_CONNECTION`,
`ref=<существующее QA connection>`. Для отсутствующей managed binding явно
`expectedAbsent=true`; иначе exact прежние binding revisionRef/version.
Проверять readback `definitionConfigurationBinding.state=MATCH` и exact6.1.0
package digest. Нельзя использовать expectedAbsent при существующей binding.

Штатный rebind **отключает прежние grants**, очищает credential binding и
возвращает connection в NOT_CONNECTED. Это ожидаемое поведение
`integration_package__bind_connection.sql`, а не сохранение прежней готовности.
Root повторно материализует только явно разрешённый `TEST_REPO_GITHUB_PAT`
через owner-native `PUT /api/v1/integration-connections/{connectionRef}/credential`
(`IntegrationCredentialInput.value`, writeOnly, fresh If-Match/idempotency/CSRF).
Значение читает лишь разрешённый custody-процесс, не модель, shell argv, лог,
manifest либо новый dotenv. Existing protected-secret-input CLI имеет другой
закрытый key mapping; его нельзя незаметно запускать с чужим key или выдавать
за поддержку TEST_REPO_GITHUB_PAT. Прямой Secret patch не используется.

После metadata-only readback новой server-owned credential revision root
выполняет `POST /api/v1/integration-connections/{connectionRef}/commands`
`{"action":"TEST"}` с fresh If-Match. Перед model run нужны authoritative
CONNECTED, exact definition6.1.0/digest и safe successful test receipt.
`POST .../grants` выдаёт только exact project employee новый
`github.repository.bundle.publish`, enabled=true, HUMAN_EACH_EFFECT.
По необходимости прежние READ/PR grants повторно выдаются явно с теми же
scopes; rebind не восстанавливает их автоматически. Модель получает лишь
opaque grant refs и fresh immutable RuntimeRevision. WRITE через обычный
`invoke_integration` для этой capability закрыт; catalog указывает specialized
`prepare_repository_bundle_publication`. Публикация PR после commit использует
прежний `github.pull_request.create` и отдельный Human Gate.

## Проверки и честный статус

Обязательны native owner PostgreSQL lifecycle/authority/revoke/expiry/replay,
два процесса и lost ACK в каждом окне, полный108-file byte roundtrip, hostile
archive/manifest, local HTTPS Git transport CAS/single-parent/no-source-change,
неизменность credential custody, bounded worker cancel/join, contracts/codegen,
итоговые environment renders и browser полный manifest Gate/readback.
До выполнения эти проверки имеют NOT RUN. One-commit live publication,
внешний PR/merge и полный PDLC данным документом не объявляются выполненными.
