# role-image-builder

`role-image-builder` — единственный исполнитель одной fenced attempt сборки
ролевого образа. `control-plane` владеет рецептами, очередью сборок,
артефактами и eligibility, а отдельные `image-admission` и `image-promotion`
workload владеют evidence, verdict и переносом exact digest.

## Сквозной путь

1. Owner через `control-api-gateway` вызывает специализированный
   `ManageRoleImageRecipe`. Gateway не передаёт actor/tenant/owner как данные
   запроса: authority proof разрешает их в `control-plane`.
2. `control-plane` канонизирует полную спецификацию, включая multiline
   installation block, exact base/source/context/builder/frontend/platform/
   package/tool/toolchain/policy inputs, назначает generation и атомарно
   создаёт `ImageBuild`, idempotency receipt и audit.
3. Exact promoted artifact переиспользуется только при совпадении полного
   `spec_sha256`, актуальных policy revision/digest, admitted signature
   evidence и promotion readback. Иначе создаётся новая attempt.
4. Builder получает `ClaimImageBuild` без build ID в payload. Grant, lease и
   claim связаны с tenant/project/build/attempt/spec/input/generation/JTI/
   fence. Installation block возвращается только в этом ответе.
5. Trusted materializer по отдельной pull-only mTLS/application identity читает
   context/package/tool из одного server-configured OCI repository. Exact
   manifest содержит один слой утверждённого media type; descriptor и payload
   size/digest проверяются потоково в private bounded `emptyDir`, после чего тот
   же snapshot безопасно извлекается. Recipe/claim не содержит build credential
   refs: private external input публикуется owner-side до сборки. Secret values
   не входят в context, mounts или BuildKit.
6. Builder генерирует Dockerfile и обращается к вынесенному rootless BuildKit по
   exact mTLS/SNI/CA. Staging push credential/egress принадлежит только BuildKit;
   builder client имеет лишь input pull и BuildKit egress. Недоверенный
   installation block исполняется без credential material. После него protected
   runtime binaries восстанавливаются из exact trusted base, а output закрепляет
   exact ABI `USER`/entrypoint/commands и runtime contract revision/digest.
7. BuildKit публикует staging artifact, native SLSA provenance и labels полного
   immutable tuple. Builder возвращает только bounded digest/status evidence.
8. `image-admission` server-side claim связывает SBOM, vulnerability evidence,
   native provenance, signature и receipt с exact artifact, публикуя их одним
   immutable OCI evidence bundle в выделенный repository. Content digest receipt
   и фактически прочитанный OCI manifest digest записываются owner-side до verdict. Rejected evidence
   переводит artifact в `BLOCKED`; accepted evidence делает artifact доступным
   только специализированному promotion claim.
9. Отдельный `image-promotion` Pod/ServiceAccount получает fenced одноразовый
   server-selected claim без artifact ID из payload. Claim включает exact
   staging reference, admission revision и оба receipt digest; поэтому
   promotion восстанавливает evidence по exact OCI manifest digest без admission PVC. Workload копирует exact
   image digest, сначала расходует claim owner-side через
   `AuthorizeImagePromotion`, копирует тот же evidence manifest в закрытый
   promoted repository, выполняет readback обоих manifests и завершает
   одноразовым authorization token. Builder не
   получает signer/promotion/node-pull identity.
10. Свежая `RuntimeRevision` разрешает recipe и artifact внутри owner boundary,
   закрыто проверяет все версии/digests и содержит единственный immutable
   `repository@sha256` reference. `runtime-controller`, credential materializer
   и admission webhook сравнивают именно этот reference и evidence binding.

Admission и promotion не требуют ручного запуска. Отдельный
`image-admission-controller` автоматически создаёт одну последовательную
цепочку phase Job/PVC. Он не получает owner, registry, signing или Vault
credentials; каждая фаза сохраняет собственный ServiceAccount и secret
boundary. RBAC дополняется `ValidatingAdmissionPolicy`, которая по exact caller
identity отклоняет чужой image, command, env, volume либо ServiceAccount.
`render-image-admission-job.sh` остаётся встроенным deterministic renderer и
read-only способом сравнить будущий phase manifest.

События для этого пути не публикуются: producer, admission и runtime используют
авторитетные защищённые read/command RPC. Ложного AsyncAPI consumer нет.

## Health, readiness и отказ зависимостей

`/healthz` отражает только жизнь процесса. `/readyz` читает локальный
потокобезопасный снимок, который фоновый monitor рассчитывает по workload-local
issuer sidecar, authenticated input registry и реальному bounded BuildKit solve.
Probe не выполняет сетевых вызовов сам.

Для `image-admission-controller` действует тот же контракт: `/healthz`
проверяет только процесс, а `/readyz` читает рассчитанный фоновым monitor
снимок прямого Kubernetes API и immutable policy. Недоступность
`control-plane`, registry или phase workload не делает controller Pod
неготовым; рабочая phase Job получает типизированный отказ и повторяется через
ограниченный reconcile.

Соседний `control-plane` не входит в Kubernetes readiness builder. Его
недоступность переводит рабочий claim/build loop в отдельное degraded-состояние
с одним warning на отказ и одним сообщением на восстановление; Pod остаётся
готовым принимать работу после восстановления. Полный защищённый путь до
`control-plane` проверяется отдельной диагностикой и фактическими RPC.

## Lifecycle matrix

| Переход | Владелец и результат |
|---|---|
| create recipe | `control-plane`; server-owned owner/generation/policy, новая queued build или exact reuse |
| update recipe | `control-plane`; version CAS, generation++, новый canonical hash и build/reuse; прежние build/admission/promotion claims отзываются одной owner-транзакцией |
| archive/restore/delete recipe | `control-plane`; специализированная команда; незавершённые build и artifact закрываются, их lease/claims отзываются |
| resolve/reuse | `control-plane`; только exact `ACTIVE`, admitted, signed, promoted artifact с current policy/readback |
| claim build | `role-image-builder`; одна leased attempt, fence/generation/JTI и immutable input snapshot |
| renew/progress | `role-image-builder`; только current token/attempt/fence, закрытые stage и percent |
| complete/fail | `role-image-builder`; terminal owner transaction отзывает lease; complete создаёт immutable artifact |
| cancel | owner-команда `ManageImageBuild`; закрывает claim/lease, старый worker отвергается |
| retry | owner-команда; новая attempt/fence/generation и свежий grant, build evidence очищается |
| expiry | owner-команда после lease deadline; старый grant закрыт |
| dead letter | owner-команда после исчерпания maximum attempts; новых claims нет |
| claim admission | `image-admission`; одна lease/fence на exact artifact и current policy |
| admission accepted | durable OCI evidence bundle проходит exact readback; owner transaction фиксирует receipt content и реальный OCI manifest digest, promotion identity ещё не выдана |
| admission rejected | тот же durable evidence bundle фиксируется owner transaction, artifact переходит в `BLOCKED`; promotion неприменим |
| claim promotion | `image-promotion`; server-side queue выбирает exact artifact и возвращает staging reference, admission revision, receipt digests, fence/generation/JTI и bounded expiry |
| promotion expiry | следующий специализированный claim заменяет истёкший, повышает fence/generation и отзывает старый claim |
| authorize promotion | `control-plane`; owner-side расходует current claim до registry copy и выдаёт bounded одноразовый token |
| promotion/readback | `image-promotion`; exact image destination и неизменный OCI evidence manifest digest/readback, затем completion по token и artifact `ACTIVE` |
| RuntimeRevision | `control-plane`; только current promoted artifact с exact runtime ABI; missing/stale/mismatch закрыто отклоняется |

Отдельные `renew admission`, `retry admission` и универсальный CRUD намеренно
отсутствуют. Истёкший admission claim становится снова доступен через
server-side queue predicate; rejected artifact immutable и новый результат
требует новой build attempt. Promotion claim не продлевается: новый
специализированный claim для того же owner-resolved artifact повышает fence и
делает прежний token непригодным до любых registry-действий.

## Безопасность данных

Installation block необязателен; canonical empty value — `""`. Он хранится
только в PostgreSQL owner state и доступен через owner-scoped version-pinned
full recipe readback, но отсутствует в обычном status, audit payload, logs,
provenance и admission receipt. Secret refs возвращаются без values.

Context/package/tool producers публикуют single-layer OCI artifacts в exact
`roleImageInputRepository`; source OpenAPI/Proto передаёт immutable manifest ref
и ожидаемый payload digest. Registry client identity, CA/SNI, basic credential и
egress destination закреплены deployable. Materializer не следует redirect,
ограничивает manifest/payload, проверяет media type/size/digest и очищает private
workspace после attempt. Package/tool устанавливаются offline через закрытый
список `apk|apt|dnf|pip|npm`; context доступен installation step read-only.

Достижимые фазы: `MATERIALIZATION`, `CONTEXT_VALIDATION`, `BASE_PULL`,
`SOLVING`, `INSTALLATION`, `TRUSTED_RUNTIME_FINALIZATION`, `STAGING_PUSH`, `PROVENANCE`. Диагностика содержит
только закрытые `errorCode`/`diagnosticCode` и bounded безопасный summary; raw
BuildKit output и credential values отбрасываются.

## Локальная проверка

```bash
go test ./...
go build ./cmd/role-image-builder ./cmd/image-admission-controller ./cmd/image-admission-bridge
docker build --target runtime -f services/jobs/role-image-builder/Dockerfile .
docker build --target admission-runtime \
  --build-arg ADMISSION_TOOLS_IMAGE="$ADMISSION_TOOLS_IMAGE" \
  -f services/jobs/role-image-builder/Dockerfile .
```

Полные integration/E2E/deploy/lifecycle проверки отложены в Issue #216.

## Rollback

Остановить новые owner-команды и вернуть Deployment на предыдущий exact image
digest. Уже promoted digest не удалять и policy revision не откатывать.
Queued/claimed attempts закрыть только специализированными owner-командами;
ручное изменение PostgreSQL, claims, leases или registry tags запрещено.
