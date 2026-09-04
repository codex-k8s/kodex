---
id: SVC-MC-003
title: Внутренний сервис internal-rpc-authority
type: service
status: approved
owner: developer
version: 2.1.0
updated: 2026-09-04
---

# Внутренний сервис internal-rpc-authority

`internal-rpc-authority` — общий защищённый компонент короткоживущего
межсервисного контекста авторизации. Он не владеет пользователями, ролями,
проектами и бизнес-разрешениями: полномочие приходит только в подписанном
proof от домена-владельца и связывается с заранее утверждённой машинной
политикой.

## Исполняемый сценарий

1. Владелец доменного состояния разрешает actor, tenant и project внутри своей
   границы и выдаёт ES256 authority proof.
2. Приложение вызывает локальный
   `/internalrpcauthority.v1.AuthorizationIssuerService/IssueAuthorizationContext`
   через `/run/kodex/internal-rpc-authority/issuer.sock`.
3. Issuer проверяет Linux `SO_PEERCRED`, точную привязку операции, proof,
   происхождение и одноразовый `jti`, затем подписывает контекст текущим ключом
   workload.
4. Приложение передаёт контекст по целевому mTLS RPC. mTLS и контекст
   обязательны одновременно.
5. Целевое приложение передаёт локальному verifier фактически проверенные
   целевой SPIFFE и полный RPC. Verifier повторно проверяет JWS, точные
   workload/RPC/аудиторию/разрешение, TTL и устойчивое резервирование защиты
   от повтора.
6. PostgreSQL атомарно фиксирует верхнюю отметку снимка и одноразовое
   подтверждение.

Поля обычного запроса, произвольные идентификаторы клиента и сам mTLS не
являются источником полномочий.

## Карта lifecycle, trust и producer-client-consumer

Карта фиксирует границу Issue #1023 до изменения кода. `Proto SHA-256` ниже
вычисляется interceptor из фактически переданного protobuf-сообщения в
детерминированном режиме; caller не передаёт готовый digest как authority.

| Сценарий | Инициатор и trust | Producer / client / consumer | Exact binding | Durable переход |
| --- | --- | --- | --- | --- |
| Первичный unary RPC | workload mTLS + bearer/application credential; actor и tenant разрешает control-plane | control-plane proof resolver / локальный issuer / target verifier и domain handler | caller/target SPIFFE, полный method, operation, permission, actor/tenant/project provenance, `Proto SHA-256`, policy ABI и срок не более 30 секунд | issuer резервирует proof JTI и его monotonic revision; verifier одной PostgreSQL-командой принимает snapshot high-watermark и резервирует context JTI |
| Первичный streaming RPC | те же trust layers; request ещё не существует при открытии stream | proof resolver / issuer stream interceptor / verifier stream interceptor | exact stream method/operation и one-time session JTI; request binding profile обязан быть `STREAM_SESSION` | verifier резервирует session JTI до передачи stream handler; повторное открытие требует нового proof |
| STT policy continuation | уже принятый verifier контекст `platform.stt.transcribe`; locator из payload не является authority | `stt-tts-service` / continuation interceptor / control-plane projection | root actor/tenant/project и provenance наследуются без замены; parent target обязан быть `stt-tts-service`; child method и operation только `ResolveTranscriptionPolicy` / `platform.stt.policy.resolve`; child `Proto SHA-256`, request/correlation и `exp <= parent.exp` | issuer атомарно подтверждает наличие принятого parent JTI и резервирует один child JTI для `(parent, child operation, request)` |
| STT credential continuation | тот же принятый root context; policy result остаётся locator/effect input | `stt-tts-service` / continuation interceptor / secret-broker projection | тот же root lineage; child method и operation только `ProjectTranscriptionCredential` / `platform.stt.credential.project`; digest связывает provider/config revision и generation | отдельная reservation допускает ровно один credential child для того же parent/request; retry создаёт новый root request, скрытая переигровка запрещена |
| Device authorization materialization | OIDC session + gateway mTLS для command; control-plane platform worker grant для вызова secret-broker | gateway issuer/verifier -> control-plane -> control-plane issuer -> secret-broker verifier | первый RPC связан с полным Proto request; второй — с exact provider operation и активным grant, содержащим credential generation и ABI | control-plane принимает `(workload, credential generation, revision)` монотонно; несовместимый grant закрывает readiness до пользовательского RPC |

Lifecycle continuation закрытый: parent сначала проходит обычную проверку и
одноразовое резервирование; child выдаётся только локальному target parent,
только по allowlist профиля и только пока parent действителен. Cancel, ошибка
projection или terminal STT request не продлевают parent и не освобождают
reservation. Повтор разрешён лишь новым первичным request/context; child
никогда не меняет root actor, tenant, project, permission provenance или
source snapshot. Unknown method, operation, ABI, request profile, отсутствующий
bearer/context либо mTLS всегда дают закрытый отказ.

## Состав

- `internal-rpc-authority-socket-init` создаёт приватный корень UDS с
  `uid=29000`, `gid=29000`, режимом `1770`;
- `internal-rpc-authority-issuer` слушает только именованный issuer UDS;
- `internal-rpc-authority-verifier` слушает только именованный verifier UDS;
- `internal-rpc-authority-publisher` по одному устойчивому publication intent
  публикует полный `auth/proof/manifest/snapshot` graph, обычные readback
  credentials и относящиеся к роли restore credentials из версионированного
  реестра целей; PostgreSQL фиксирует predecessor/history и promotion только
  после точного readback всех ролей;
- `internal-rpc-authority-readback-attestor` выдаёт устойчивый одноразовый
  challenge и атомарно сохраняет неизменяемое подтверждение аттестации;
- `internal-rpc-authority-restore-controller` координирует
  `OPEN → QUIESCING → PREPARED → RESTORING → COMPLETED`, а отдельные
  `restore-operator` и `restore-recovery` исполняют запущенную владельцем
  команду и серверное восстановление ограждения. Operator получает хэш
  immutable intent только из фактически обслуживаемого CNPG `Backup` и его
  source `Cluster`; PITR executor независимо повторяет readback, указывает
  exact `backupID`/timeline и подписывает completion только при полном
  совпадении;
- `internal-rpc-authority-cli up|status` применяет единственную fresh baseline
  goose без штатного отката. Legacy `expand|contract|deploy`, backfill и
  compatibility path отсутствуют; повторный `up` выполняет идемпотентный
  readback уже применённой baseline.

Issuer и verifier загружают подписанный канонический снимок через независимый
корень доверия манифеста. Обновление сначала проходит полную криптографическую
проверку, устойчивый однонаправленный CAS и проверку обслуживаемого состояния;
только затем атомарный указатель переключает рабочий RPC на новую неизменяемую
модель. Отклонённая проекция закрывает рабочий путь и готовность, не заменяя
ранее проверенную
модель.

## Конфигурация

Обязательные исполняемые значения:

| Переменная | Назначение |
| --- | --- |
| `INTERNAL_RPC_AUTHORITY_WORKLOAD_ID` | Точный workload из реестра возможностей |
| `INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE` | Файл DSN; значение не помещается в окружение |
| `INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME` | Точные TLS SNI/имя узла |
| `INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER` | Точный LOGIN principal текущего поколения |
| `INTERNAL_RPC_AUTHORITY_SNAPSHOT_JWS_FILE` | Подписанный снимок authority |
| `INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_PUBLIC_JWK_FILE` | Неизменяемый начальный публичный ключ независимого корня манифеста |
| `INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_JWS_FILE` | Однонаправленно подписанный пакет доверия ключа манифеста |
| `INTERNAL_RPC_AUTHORITY_SECRET_BACKEND` | Только `kubernetes`; иной backend закрыто отклоняется |
| `INTERNAL_RPC_AUTHORITY_WORKLOAD_CERTIFICATE_FILE` | Точный mTLS-сертификат workload для attestor/controller |

Issuer дополнительно требует
`INTERNAL_RPC_AUTHORITY_CONTEXT_PRIVATE_JWK_FILE` и
`INTERNAL_RPC_AUTHORITY_PROOF_TRUST_JWK_FILE`. Пути имеют безопасные значения
по умолчанию,
соответствующие Kustomize components. Значения DSN, token и private keys нельзя
передавать через окружение или выводить в лог.

Verifier того же workload `control-plane` дополнительно подтверждает роль
`AUTHORITY_PROOF_RESOLVER`: перед отдельным readback receipt он связывает
доставленный через Kubernetes Secret proof private key и его обслуживаемое
`current_generation` с exact `CURRENT` JWK, issuer, аудиторией, поколением и
source revision/digest proof trust. Поколение перечитывается из того же
версионированного Secret при каждой активации, поэтому forward-only rotation не
зависит от статического env. Receipt verifier не заменяет resolver receipt, а
publisher остаётся неготовым до полного набора.

Обычные readback credential, ключ владения, restore role credential и restore
ACK key issuer/verifier читают из точных Kubernetes Secret projections,
разрешённых только их роли workload. Вызывающая сторона не передаёт имя Secret,
workload, роль, поколение, аудиторию или TTL. Publisher обновляет только
закрытый набор Secrets через exact `resourceNames` RBAC и монотонную generation.
Snapshot создаётся заранее пустым ресурсом без
секретного материала; publisher имеет только `get/update/patch`, выполняет
`resourceVersion` CAS через exact Kubernetes API destination и после каждого
изменения читает фактически обслуживаемый JWS. Один snapshot действует не
более 180 дней; новая source revision должна быть опубликована и полностью
подтверждена до истечения этого окна, иначе publisher и consumers закрывают
readiness.

Тайм-ауты и ограниченный опрос задаются
`INTERNAL_RPC_AUTHORITY_STARTUP_TIMEOUT`,
`INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT`,
`INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT` и
`INTERNAL_RPC_AUTHORITY_SNAPSHOT_RELOAD_INTERVAL`. Очистка только истёкших
одноразовых резервирований использует
`INTERNAL_RPC_AUTHORITY_REPLAY_CLEANUP_INTERVAL` и фиксированный срок хранения:
issuer удаляет только резервирования authority proof, verifier — только
резервирования контекста авторизации. Устойчивая верхняя отметка не удаляется.

Restore admission закрыт уже при конструировании issuer/verifier. До активации
snapshot выполняется bounded синхронный poll внешнего controller, после
активации — повторный poll и served-state readback. UDS/technical listeners,
readiness и фоновые workers создаются только после обоих успешных этапов.
Timeout, cancel, отсутствующее/устаревшее/откаченное evidence либо фаза
`QUIESCING`/`PREPARED`/`RESTORING` оставляют admission закрытым и завершают
startup ошибкой.

Static PostgreSQL principals создаёт installation bootstrap, а одноразовая Job
сверяет закрытый реестр ролей и задаёт SCRAM passwords из Kubernetes Secret до
миграций. Runtime не получает `CREATEROLE` и не поддерживает параллельный
credential lifecycle. Имена Kubernetes и пути зафиксированы в deploy profile.
PostgreSQL DSN каждого
процесса обязан ссылаться на CA-файл из отдельного ConfigMap, подключённого
только для чтения, и
использовать `sslmode=verify-full`.
State-changing key publication проходит `domain/service → domain/repository
ports → PostgreSQL/Kubernetes adapters`; composition root только проверяет
конфигурацию и связывает зависимости.

## Поддерживаемая локальная проверка

```bash
find services/internal/internal-rpc-authority libs/go -name '*.go' -print0 \
  | xargs -0 gofmt -d
(cd services/internal/internal-rpc-authority && GOWORK=off go test ./...)
make test-internal-rpc-authority-postgres
make test-authority-policy-codegen
make test-web-only-release
buf format --diff --exit-code contracts/proto
buf build
git diff --check
```

PostgreSQL-проверка использует только disposable container и не принимает
production DSN. Release-проверка рендерит оба профиля локально, не применяет
manifest и проверяет точные machine policy, probes и отсутствие unresolved
image inputs. Проверка, которая не запускалась на передаваемом SHA, в отчёте
обозначается `NOT RUN`, а не считается успешной по наличию test entrypoint.

Для ручного операционного render, а не автоматической test suite, необходим
фактически опубликованный неизменяемый образ:

```bash
KUBERNETES_API_CIDRS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh)"
KUBERNETES_API_PORTS="$(scripts/resolve-kubernetes-api-endpoint-cidrs.sh --output ports)"
test -n "$KUBERNETES_API_CIDRS"
test -n "$KUBERNETES_API_PORTS"
scripts/render-internal-rpc-authority.sh \
  --environment staging \
  --image-ref ghcr.io/codex-k8s/kodex/internal-rpc-authority@sha256:<digest> \
  --kubernetes-api-cidrs "$KUBERNETES_API_CIDRS" \
  --kubernetes-api-ports "$KUBERNETES_API_PORTS"
```

Нулевой digest в base является намеренным закрыто отклоняемым заполнителем
источника и никогда не проходит итоговый render.
Resolver материализует отсортированное объединение
`Service/default/kubernetes.spec.clusterIPs` и адресов только готовых
`EndpointSlice` как точные `/32` и `/128`. Он закрыто завершается при пустом,
невалидном или чрезмерном наборе. Отдельный режим `--output ports` связывает
Service port `443` и фактически обслуживаемые TCP-порты готовых EndpointSlice,
чтобы политика работала до и после DNAT. Команду нужно повторять непосредственно
перед каждым staging/production render: сохранённый набор после изменения
Service/EndpointSlice не является допустимым входом.

Issuer/verifier подключаются к workload через Kustomize components. Component
добавляет sidecar, PodMonitor и точные подключения UDS/Secret, но не может
задать `NetworkPolicy` отдельному контейнеру: политика выбирает весь pod.
Поэтому потребитель обязан включить направления issuer/verifier к PostgreSQL,
DNS и Prometheus в свой итоговый render с запретом по умолчанию. Отсутствие
этих точных правил является блокирующей ошибкой развёртывания потребителя, а
не поводом расширять исходящий трафик.

## Наблюдаемость и жизненный цикл

`/healthz`, `/readyz` и `/metrics` доступны только на техническом listener.
Готовность проверяет тот же устойчивый путь обслуживаемого снимка и защиты от
повтора, который использует рабочий RPC. Метки gRPC-метрик ограничены реестром
методов и каноническими кодами; произвольные значения нормализуются.

Каждый исполняемый процесс и восстановительная задача создаёт OTel trace
provider, включает серверные/клиентские gRPC spans и `otelpgx` hooks без
деталей SQL/соединения. Структурные записи `slog` получают
`trace_id`/`span_id`. OTLP разрешён только к
`otel-collector.observability.svc:4317` с TLS 1.3, точным SNI и отдельной CA.
Sentry DSN читается только из файла для чтения и допускает только
`sentry-relay.observability.svc:8443`; прямой исходящий трафик в интернет
запрещён. Панель, оповещения и абсолютный `runbook_url` входят в тот же
исполняемый компонент.

При `SIGTERM` готовность закрывается до остановки, затем независимо и в
ограниченные сроки завершаются gRPC, фоновые обработчики, технический HTTP и
PostgreSQL. Обновление подключённого Secret само по себе не доказывает смену
TLS/DSN: ротация учётных данных или сертификата завершается последовательным
перезапуском и повторной проверкой фактически обслуживаемой идентичности и
готовности. Порядок диагностики и восстановления приведён в `RUN-MC-006`.
