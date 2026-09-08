---
id: OPS-DOC-1313
title: Ограниченная свежесть authority и независимое обновление issuer
status: approved
type: operation-evidence
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Граница и источники

Задачи [#1313](https://github.com/codex-k8s/kodex/issues/1313) и
[#1328](https://github.com/codex-k8s/kodex/issues/1328), replacement LKG
[#1333](https://github.com/codex-k8s/kodex/issues/1333), решение владельца в
GUIDE-DOC-003 «Решение владельца о свежести отзыва, 2026-09-08».
Обычный authorization context сохраняет wire format и ровно 30 секунд TTL.
Раньше локальные metadata жили две минуты, refresh выполнялся раз в минуту,
а настоящий PostgreSQL receipt жил пять минут. Срок challenge в 30 секунд
не ограничивал срок принятого receipt.

Новый протокол ограничивает **действительность полномочий**, независимо от
`exp` токена: исходный receipt даёт не более `accepted_at + 30 seconds`.
Повтор receipt, новый receipt того же source, рестарт, ожидание SQL lock и
continuation не продлевают уже зарегистрированный context. При недоступной
БД отказ наступает сразу: in-memory replay fallback отсутствует. При отказе
attestor и доступной БД прежнее подтверждение работает до своего дедлайна.

Это не реализация emergency revoke. Канонический протокол сейчас содержит
нормальную ротацию с двумя окнами по 40 секунд; отсутствующий специализированный
переход разобран отдельно в [#1322](https://github.com/codex-k8s/kodex/issues/1322).
Успешная проверка 31 секунды не закрывает этот остаток и всю приёмку MVP.

# Матрица полномочий и жизненного цикла

| Сценарий | Инициатор и authority | Владелец и устойчивый переход | Результат / потребитель |
| --- | --- | --- | --- |
| Подтверждение снимка | Issuer/verifier с каноническими readback credential, possession proof и mTLS | Attestor выполняет существующие challenge/consume через свой LOGIN; назначает `accepted_at` и immutable receipt | Snapshot активируется после полной проверки; повтор idempotency возвращает прежний receipt |
| Выпуск context | UDS workload + проверенный domain proof + exact operation binding | Issuer резервирует proof, подписывает JWS, узкий SQL command связывает JTI/digest/caller/target/source/key/policy/signer с owner-selected receipt и deadline | JWS возвращается только после регистрации либо точного read-only readback того же binding; неразрешённый UNKNOWN не возвращает token и не запускает новый JTI автоматически |
| Приём context | mTLS caller, подпись, exact binding, tenant/actor/lineage и canonical JWS | Verifier атомарно проверяет original receipt binding и резервирует replay; повторная проверка после ожидания INSERT исключает поздний допуск | Доменный RPC вызывается только после успешного accept; ошибки не содержат token/payload |
| Continuation | Только проверенный и ранее принятый parent | Issuer связывает child с durable parent JTI/digest; deadline равен минимуму parent и собственного receipt | Новый receipt child не оживляет старый parent; прежние root/operation/request bindings сохраняются |
| Ошибка подписи, повреждение или rollback snapshot | Существующий loader/verifier | Новая revision не активируется; существующая closed failure policy сохраняется | Нет применения частичного состояния или unsafe fallback |
| Отказ attestor | Старые exact credentials, доступный verifier PostgreSQL | Прежний receipt остаётся неизменным | До deadline — разрешённый путь; после — закрытый отказ |
| Отказ PostgreSQL | Те же credentials; durable boundary недоступна | Reserve/Freshness/Accept не заменяются локальным кэшем | Немедленный закрытый отказ; процесс и liveness не должны зависеть от глобального состояния |
| Restart / lost ACK | Тот же source и original receipt / JTI | Перечитывается остаток по DB time; duplicate registration не обновляет дату | Истёкшие credentials не восстанавливают authority |
| Cleanup | Только собственный issuer LOGIN через узкую функцию | Удаляются только bindings, истёкшие более десяти минут назад; caller не задаёт cutoff | Активный JTI нельзя удалить и переоформить с новым deadline; restore fence сохраняется |
| Активация | Только migrator LOGIN и отдельный operator intent | CAS version1→2, один UUID; повтор exact UUID возвращает прежний результат; другой UUID конфликтует | Forward-only policy, без переписывания receipts/revocation/high-watermarks |

Функции принадлежат `internal_rpc_authority_readback_owner`, имеют закрытый
`search_path` и отозванный PUBLIC EXECUTE. Issuer имеет узкий EXECUTE регистрации,
а не прямой write в binding table. Verifier получает только проверку exact tuple.
Граница `session_user`/generation/lifecycle и restore fence остаётся обязательной.

Go переносит только **остаток** DB deadline на monotonic clock, начиная до
запроса: сетевой RTT расходует бюджет. Повтор того же receipt может уменьшать
локальный deadline, но не увеличивать его. DB clock остаётся доверенным временем
протокола; произвольный откат часов PostgreSQL не является поддержанным способом
эмуляции partition. Локальные смещения ±5 секунд проверяются отдельно от DB time.
Refresh receipt выполняется каждые 10 секунд, reload snapshot — не реже чем раз
в 5 секунд. Readiness использует тот же durable path, что рабочая авторизация.

# Две фазы SQL и порядок выкладки

1. Из точного нового source запустить отдельный migration Job с `up`.
   Additive policy имеет version1/300s, старые binaries продолжают читать
   существующий формат. Новые helpers уже ограничены 30 секундами; новый
   verifier временно допускает отсутствующий issued binding только в version1.
2. Собрать immutable authority image и exact issuer/verifier capability.
   Обновить все зарегистрированные verifier, затем issuer контейнеры. Это
   отдельный security rollout, не `scoped-release` приложения и не global `up`.
   Source/image приложения, signer/trust/grant generation, worker image и
   socket init сохраняются. Для RIB особенно сохраняется прежний worker image.
3. Для будущих image-admission/image-promotion Jobs выполнить переход #1328:
   совместимый controller/renderer → additive CRD/CEL → новая immutable policy
   и parameters → binding/consumers → возобновление controller. Новое optional
   поле `authorityIssuerImage` отделено от прежнего `authorityImage`, который
   продолжает закреплять socket-init и worker. Legacy policy без нового поля
   сохраняет прежний render и run identity. Права/egress/jobs scope не расширены.
4. Выполнить настоящий разрешённый build/admission/promotion и снять `/proc/PID/exe`
   issuer внутри работающих Jobs, затем дождаться их успешного завершения.
   Полная CFG приёмка содержит дополнительные требования; этот proof подтверждает
   только совместимость producer/issuer и фактическое выполнение выбранных Jobs.
5. План активации сверяет все issuer/verifier Pods по owner UID, source/image и
   actual executable; старые terminating consumers должны завершиться. Проверяются
   точный controller renderer, policy/parameters/binding и оба completed Job proof.
6. Отдельным `freshness-activate` выполнить CAS version1→2. Старые receipts
   ограничиваются исходным `accepted_at+30`, а не временем миграции/активации.
   Missing issued binding после этого закрыто отвергается. Старый security binary
   нельзя вернуть обычным rollback: нужен проверенный совместимый forward fix.

Оператор ведёт один supervisor этих фаз. Обычные параллельные rollout, ротация,
активация и rollback одной authority boundary в это окно не запускаются.
Сначала authoritative readback неизвестного результата, затем новое решение;
новый prefix не разрешает повторить неизвестный эффект.

# Поддержанная оснастка staging

Все JSON планы и evidence создаются вне source, с 0600 и новым именем.
`--context` — фактический staging context; `--k3s-sudo` использует ровно
`sudo -n k3s kubectl`. Значения Secret/DSN не копируются на host и не выводятся.

```sh
node tools/release/authority-freshness-capability.mjs \
  --source "$SOURCE" --revision "$SHA" --output "$PRIVATE/capability.json"

node tools/release/authority-freshness-transition.mjs plan \
  --context "$CONTEXT" --k3s-sudo --source "$SOURCE" --revision "$SHA" \
  --action up --output "$PRIVATE/up-plan.json"
node tools/release/authority-freshness-transition.mjs apply \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/up-plan.json" \
  --evidence "$PRIVATE/up-intent.jsonl" --confirm APPLY-STAGING-AUTHORITY-FRESHNESS
node tools/release/authority-freshness-transition.mjs observe \
  --context "$CONTEXT" --k3s-sudo --plan "$PRIVATE/up-plan.json"
```

Для immutable authority image сборка использует `--build-arg VERSION="$SHA"`.
Capability отдельно собирает hot-reload recipe и Dockerfile recipe с
`-ldflags="-s -w -X main.version=${revision}"`; `binaries` и `imageBinaries`
не взаимозаменяемы. Actual image hash проверяется только против второго набора.

Новый Job клонирует проверенный completed `internal-rpc-authority-migrate`,
сохраняет ServiceAccount, TLS/Secret mounts и securityContext. Меняются только
source и CLI action; backoff0, deadline300s, прежний Job не удаляется. Имя
назначено intent UUID, неизвестный create разрешается get того же имени.
`APPLIED/RUNNING` не равно успешной миграции; требуется `SUCCEEDED`.

Для `freshness-status` и `freshness-watch` используется тот же plan/apply.
`freshness-watch` выдаёт 45 безопасных metadata samples, один в секунду, без
receipt payload. Для sidecar mutation нужен его свежий sample (≤5 секунд).
Пример отдельного manifest не более чем для двух workloads:

```json
{"version":1,"capability":"/private/capability.json","targets":[
  {"name":"control-plane","profile":"source","roles":["verifier","issuer"],
   "source":{"path":"/srv/kodex-dev/EXACT_SOURCE","revision":"EXACT_40_HEX_SHA"}}
]}
```

Для immutable установки target имеет `profile:"image"`, `image:"repository@sha256:..."`
и выбранные `roles`; runtime image не подменяет исходники hot-reload контейнера.

```sh
node tools/release/authority-sidecar-rollout.mjs plan --context "$CONTEXT" \
  --k3s-sudo --manifest "$PRIVATE/sidecars.json" --output "$PRIVATE/sidecars-plan.json"
node tools/release/authority-sidecar-rollout.mjs apply --context "$CONTEXT" \
  --k3s-sudo --plan "$PRIVATE/sidecars-plan.json" --status-job "$WATCH_JOB" \
  --evidence "$PRIVATE/sidecars-intent.jsonl" --confirm APPLY-STAGING-AUTHORITY-SIDECARS
node tools/release/authority-sidecar-rollout.mjs observe --context "$CONTEXT" \
  --k3s-sudo --plan "$PRIVATE/sidecars-plan.json"
```

До activation отдельный `rollback` использует тот же exact план, новый evidence,
свежий watch и `--confirm ROLLBACK-STAGING-AUTHORITY-SIDECARS`. Drift не
перезаписывается. После activation этот rollback запрещён; обычный новый
forward apply требует capability протокола2 и actual binary readback.

Переход future Jobs использует `runner-policy-transition.mjs prepare` с
`--runner-digest` **нынешнего** runner и новым `--authority-issuer-image`;
фазы `maintenance, reader, schema, admission, resources, binding, control-plane,
role-image-builder, controller, resume, open`. Значения берутся из readback.
Catalog, runner, tools, worker/socket image и старые immutable resources остаются.
Для managed policy helper нужен штатный kubectl wrapper, как в его README.

```sh
node tools/release/authority-freshness-job-proof.mjs --context "$CONTEXT" \
  --k3s-sudo --job "$RUNNING_OWNER_JOB" --capability "$PRIVATE/capability.json" \
  --output "$PRIVATE/job-proof.json"
```

Перед activation `--job-proofs` указывает приватный JSON array двух путей:
один успешный image-admission Job, один image-promotion Job. Их actual executable
снят во время работы, завершение перепроверяется сейчас. В plan для
`--action freshness-activate` дополнительно передаются `--capability` и
`--job-proofs`. Сама activation выполняется тем же CLI apply и новым journal.

# Partition и восстановление

Профили `attestor` и `database` независимы. План перечисляет **все** применимые
additive egress allowances API Pod, убирает только точного peer и не оставляет
пустой `to` (это означало бы allow-all). Широкие альтернативные allow, ipBlock,
неизвестный selector и shared policy требуют отдельного плана. CAS содержит
UID, resourceVersion и полную исходную spec. До apply проверяется весь policy set.

```sh
node tools/release/authority-freshness-outage.mjs plan --context "$CONTEXT" \
  --k3s-sudo --pod "$CURRENT_API_POD" --mode attestor --output "$PRIVATE/outage-plan.json"
node tools/release/authority-freshness-outage.mjs apply --context "$CONTEXT" \
  --k3s-sudo --plan "$PRIVATE/outage-plan.json" --hold-seconds 40 \
  --evidence "$PRIVATE/outage-intent.jsonl" --confirm APPLY-STAGING-AUTHORITY-OUTAGE
```

Apply автоматически восстанавливает policy в `finally`. После crash отдельный
`restore` с тем же планом, новым journal и `RESTORE-STAGING-AUTHORITY-OUTAGE`
восстанавливает только exact mutated spec; чужой drift не затирается. `observe`
читает состояние. Нет изменения keys/trust, volume/namespace или DB state.

Изменённая NetworkPolicy не доказывает разрыв существующих соединений CNI.
Параллельно нужны настоящий protected readonly bootstrap RPC, metadata watch
receipt возрастов, direct liveness и Pod UID/restart readback. Если receipt
продолжает обновляться, partition не достигнут: фиксируется FAIL оснастки,
а не ложный PASS свежести. Для attestor outage прежний путь должен работать
до исходного deadline и закрыться не позже него. При потере DB доступ закрыт
сразу, даже если attestor доступен. После restore требуется fresh receipt и
успешный тот же RPC без нового обходного login или повторного внешнего эффекта.

Не начинать live partition во время другого HTTP/browser supervisor; root
назначает отдельное согласованное окно после его terminal. Внешний monitor
использует legitimate ограниченную session, не изготовленный JWT/cookie.

# Проверки и предел доказательства

Локальная матрица привязывается к точному SHA в PR. PostgreSQL запускается только
через `make test-internal-rpc-authority-postgres`: свой контейнер, динамический
loopback port, реальные LOGIN и production challenge/consume/register/accept SQL.
Owner publication остаётся синтетическим immutable fixture; подписи отдельно
проверяются Go tests. Реальный SQL clock проходит 31 секунду; timestamps receipt
не подменяются. Проверяются exact replay, CAS/lost ACK, restart, caller boundary,
receiptB против старого tokenA, continuation и lock wait через deadline.

Герметичные Go suites проверяют ±5s local skew, spent RTT, non-sliding receipt,
DB failure, signed context за deadline и отсутствие token при UNKNOWN регистрации.
Корректный, но не принятый replacement сохраняет старый snapshot только при временном transport failure attestor и успешном authoritative readback старого deadline; новый бюджет не выдаётся. Corruption, PermissionDenied, expiry и DB outage закрыты.

Node/CLI regressions проверяют source/image isolation, CAS/drift/UNKNOWN,
policy reader-before-writer, legacy render, отдельный issuer image и restoration.

Дополнительная стоимость рабочего пути: один Freshness query для Issue/Verify,
отдельная registration round trip перед выдачей JWS и SQL проверка fixed binding.
Readiness тоже использует durable boundary. Unit/PG PASS не доказывает допустимую
live latency, нагрузку, partition, rotation, emergency revoke или полный MVP.
Эти строки остаются NOT RUN до root evidence; #1322 остаётся отдельным блокером.

При разработке проверены Context7 PostgreSQL18 (`clock_timestamp`, privileges
SECURITY DEFINER) и pgx (`Scan`, NULL, context/connection lifecycle). Wire Proto,
OpenAPI и формат authority JWS не менялись.
