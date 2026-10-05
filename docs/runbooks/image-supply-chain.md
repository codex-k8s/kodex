---
id: RUN-MC-018
title: Диагностика автоматического admission образов ролей
type: runbook
status: approved
owner: sre
version: 1.0.12
updated: 2026-10-05
---

# Диагностика автоматического admission образов ролей

Runbook разрешает только read-only диагностику. Он не разрешает deploy,
promotion, создание phase Job вручную, изменение owner state, ослабление
admission policy или чтение secret values.

## Контракт

`image-admission-controller` автоматически поддерживает одну цепочку
`claim → scan → sign → admit` и отдельный ожидающий `promote`. Kubernetes
Job/PVC — устойчивый reconcile cursor, но не источник business lifecycle.
`control-plane` server-side выбирает artifact и выдаёт fenced claim каждой
защищённой фазе.

Controller имеет только Kubernetes API token с коротким TTL. Он не монтирует
application grant, registry, signing, installation Secret либо control-plane credentials.
Каждая phase Job запускается под собственной identity, а fail-closed
`ValidatingAdmissionPolicy` разрешает controller создать только точные images,
commands, env, volumes и ServiceAccount из immutable typed parameter resource.
Runtime читает отдельную immutable `ConfigMap`-проекцию; release render обязан
доказать точное равенство её `data` и `spec` typed resource.

## Read-only preflight

1. Зафиксировать exact Git SHA и release-lock SHA-256.
2. Проверить, что Deployment использует exact digest `image-admission`, одну
   replica со стратегией `Recreate`, `automountServiceAccountToken: false` и
   projected Kubernetes token не дольше 10 минут.
3. Проверить `/healthz` и cached `/readyz`. Probe не должен обращаться к
   `control-plane`, registry или другой business service.
4. Сверить Role: exact get immutable typed parameters и runtime `ConfigMap`;
   get/list/create/delete Job; get/list/create/update/delete PVC. PVC update
   ограничен workspace VAP ровно двумя recovery annotations; spec и остальные
   metadata неизменны. Secret, Pod, Deployment, RoleBinding, list/watch parameters,
   patch и update других ресурсов отсутствуют.
   Проверить, что installer materializes registry identities через exact
   Kubernetes Secrets, а k3s `registries.yaml` содержит только pull-only
   credential для exact HTTPS host. Node runtime readback не должен
   использовать anonymous или plaintext fallback.
5. Сверить обе admission policy и binding: `failurePolicy: Fail`, действие
   `Deny`, exact controller username, namespace, typed `paramKind` и
   `parameterNotFoundAction: Deny`.
6. По metadata Job проверить одну активную admission chain, последовательность
   фаз, отдельный promotion и отсутствие чужих ServiceAccount. Не выводить env
   и volumes работающих Pod: достаточно сравнить canonical render в репозитории.

Диагностический render отдельной фазы не выполняет apply:

```bash
IMAGE_ADMISSION_POLICY_JSON='<read-only ConfigMap JSON without secrets>' \
  tools/render-image-admission-job.sh \
  production \
  v<UTC-YYYYMMDDHHMMSS>-<exact-release-git-sha> \
  claim \
  > /tmp/image-admission-claim.yaml
```

## Типовые отказы

- controller `/readyz` неготов: проверить только Kubernetes API reachability,
  RBAC readback и immutable policy revision. Соседний сервис не добавлять в
  readiness.
- Job отклонён admission policy: сравнить release-render с точным phase
  contract. Не расширять policy; исправить renderer либо несовпавший release
  material.
- `claim` завершён без работы: это bounded idle outcome. Controller создаст
  новую ожидающую phase после backoff; warning не должен повторяться на каждом
  опросе.
- `admit` failed: controller сохраняет exact predecessor UID/backoff в PVC и
  запускает bounded callback recovery. Только durable owner failure receipt
  разрешает marker `admission.failed` и cleanup. Отказ callback сохраняет
  workspace; Kubernetes Failed сам по себе не разрешает новый claim или cleanup.
- `promote` failed: admission workspace не восстанавливать. Следующая phase
  получает свежий one-time promotion claim и durable evidence по exact OCI
  manifest digest.
- policy revision изменилась: новый run ID обязан включать новую revision;
  Job предыдущей revision не переиспользуется.
- CSI доставил новый сертификат registry: guard сохраняет готовность только
  пока endpoint выдаёт последний доказанный applied DER с остатком действия
  не менее 15 минут. Mounted сертификат считается pending; перед окончанием
  окна guard закрывает готовность и перезапускает именно TLS-serving process.
  Для pull-registry это registry-pull-authorizer, для остальных защищённых
  registry endpoint — registry. После перезапуска готовность возвращается
  только при exact DER readback нового mounted сертификата. Частые рестарты
  backend registry при неизменном pull-authorizer означают drift process
  target, а не отказ хранилища.
- Новый exact release обязан менять release revision в PodTemplate BuildKit.
  Это принудительно перезапускает daemon после обновления projected registry
  credentials и policy inputs; ручной rollout не является штатным способом
  применения новой revision. Совпадение хеша mounted Docker config при старом
  времени создания Pod не доказывает, что daemon перечитал credential.
- BuildKit readiness получает exact repository базового образа, его digest и
  digest Dockerfile frontend только из server-rendered аннотаций PodTemplate
  через Downward API. Эти значения должны совпадать с release lock и проходить
  закрытую проверку формата до вызова `buildctl`. BuildKit не монтирует общую
  `kodex-image-admission-policy`: постоянное имя такой проекции не связывает
  readiness с exact release. При диагностике сверить аннотации нового
  ReplicaSet и соответствующие `fieldRef`, не читать содержимое Secret и не
  выполнять ручной rollout.
- Actual build-path readiness имеет bounded budget 180 секунд. Этот бюджет
  покрывает первый authenticated pull exact `agent-runner` digest на холодном
  `emptyDir`, сборку probe-слоя и push в staging registry. Уменьшать его ниже
  времени холодного пути запрещено: установка не должна зависеть от частично
  накопленного cache после отменённых probe. Исчерпание бюджета остаётся
  закрытым отказом readiness и не ослабляет digest, mTLS или repository policy.
- Readiness Dockerfile использует exact `agent-runner` только как промежуточный
  `verify` stage: там выполняется проверка обязательных бинарей и создаётся
  маленький marker. Финальный `FROM scratch` содержит только marker, поэтому
  staging push доказывает рабочий authenticated write path, но не копирует
  многогигабайтные base layers. Делать verified base финальным stage запрещено:
  такой probe измеряет повторную репликацию образа вместо готовности BuildKit.
- Registry write authorizer сохраняет короткий 5-секундный budget чтения
  заголовков, но authenticated OCI request/response stream ограничивает 15
  минутами. Общий 30-секундный client/server timeout запрещён: он обрывает
  large blob PUT посередине тела и оставляет backend с `unexpected EOF`.
  Исчерпание 15 минут остаётся закрытым отказом; mTLS identity, repository и
  method policy проверяются до передачи body в backend.
- Registry guard сохраняет последний успешный ready marker только пока идёт
  ограниченный по времени readback текущего цикла. Медленный manifest readback
  не должен заранее исключать единственный endpoint из Service. Ошибка,
  несовпавший digest или исчерпание сетевого бюджета удаляют marker в том же
  цикле и закрыто снимают readiness.

## Восстановление и rollback

Исправление выполняется только новым release render и Deployment rollout после
отдельного owner approval. Для rollback вернуть controller image на ранее
утверждённый exact digest, не откатывая policy revision, promoted artifacts или
owner state. Незавершённые claims закрывает только специализированный
`control-plane` lifecycle.

### Локальная активация технического admission failure (#1797)

Следующая последовательность — инструкция для отдельно разрешённого owner/SRE
apply, а не разрешение deployment со стороны этого read-only runbook.

1. Зафиксировать clean application SHA, source и exact OCI digest readback.
   `tools/dev/build-local-image-supply-chain.sh --source-root "$application_source" --state-directory "$state_directory" --component authority-security --context k3d-kodex`
   собирает новые `image-admission` (bridge/controller) и authority binaries;
   ConfigMap-only обновление не добавляет RPC. Сборка требует canonical source
   checkout; ошибки source guards не обходятся правкой private `.env` или remote.
2. Из clean source, совпадающего с существующими trusted host mounts, выполнить
   `tools/dev/render-current-local.sh --context k3d-kodex --state-directory "$state_directory" --expected-sha "$application_sha"`.
   Использовать объявленный этой командой новый private render, не прежний файл.
3. `tools/dev/deploy-local.sh --context k3d-kodex --mode apply --security-profile trusted-cluster --stage supply-chain --render "$fresh_render" --state-directory "$state_directory"`:
   controller останавливается; exact managed Jobs/PVC inventory обязан быть пуст.
   Непустой/недоступный inventory закрывает apply без удаления workspace и resume.
   Далее идут проверка global registry → embedded exact CP policy, source-pinned
   `control-plane-migrate` (включая forward migration `20261005000100`), актуальные
   script/parameters/VAP/bindings, exact Role/RoleBinding readback, новый CP с
   readiness/policy/source-input readback и лишь затем новые controllers.
4. Выполнить ту же команду с `--mode readback`. Она сверяет exact RBAC/VAP,
   source revision/content и image inputs полностью завершённых Deployment rollout.
   Это не доказательство ELF работающего процесса и не live acceptance image flow:
   actual executable/source и owner readbacks проверяются отдельно.

В trusted-cluster global publisher/sidecars намеренно отсутствуют: новая
машинная policy (для этого перехода revision 88) проверяется по source, а exact
service policy встроена в новый CP. Protected full также требует пустой managed
inventory, materializes publisher policy через foundation и ждёт publisher и CP
до image controller; профиль не смешивается
с trusted stage. При отказе migration/RBAC/policy/owner gate EXIT не возобновляет
прежний controller. Старые expired DB claims при пустом workspace закрывает
bounded owner hook первого свежего Claim, не Kubernetes cleanup и не read path.

### Устойчивый импорт локальных platform images

`tools/dev/import-local-image.sh` принимает только девять exact repositories
локального platform profile, а не произвольные пользовательские образы.
Для k3d registry узлов выбирается по exact `k3d.cluster`, сверяется с полным
Kubernetes node inventory и требует `linux/amd64` на каждом workload node.
OCI archive обязан содержать один ожидаемый descriptor и своё tagged имя.

Импорт в containerd namespace `k8s.io` создаёт tagged ref с labels
`io.cri-containerd.image=managed` и `io.cri-containerd.pinned=pinned`
атомарно, до CRI image event. После проверки exact source descriptor helper
публикует immutable digest alias через `ctr images tag --local --force`:
[local tag containerd v2.2.3](https://github.com/containerd/containerd/blob/v2.2.3/cmd/ctr/commands/images/tag.go)
копирует source Image целиком, включая Labels; metadata Create фиксирует labels
и descriptor одной транзакцией. Default transfer tag вместо `--local` не является
подтверждённым способом сохранения pin. Значение `pinned`
определено [containerd v2.2.3](https://github.com/containerd/containerd/blob/v2.2.3/internal/cri/labels/labels.go),
а [CRI ImageStatus](https://github.com/containerd/containerd/blob/v2.2.3/internal/cri/server/images/image_status.go)
возвращает фактический `pinned`. Один успешный import или cache pointer не
доказывает защиту образа от kubelet image GC.

На каждом узле helper сверяет exact manifest digest, native platform, обе
metadata labels, полный content и unpack, затем bounded CRI readback с
`pinned=true` и exact immutable `repoDigests`. Для повторного readback без записи:

```sh
tools/dev/import-local-image.sh --context k3d-kodex --mode readback \
  --repository "$repository" --tag "$tagged_reference" \
  --exact-reference "$immutable_reference"
```

Для восстановления отсутствующего ref повторить штатный `--mode import`
с прежним проверенным `--archive "$oci_archive"` и теми же exact refs; rebuild
или новая VERSION для этого не нужны. Ошибка любого узла закрывает успех.
Helper не отключает GC, не меняет admission/pull policy и не выполняет unpin
или удаление прежних releases. Их retire требует отдельного owner-процесса;
накопление pinned releases учитывается в бюджете диска.

## Полный отчёт об отклонённом образе без раскрытия сырых данных

Если краткий remediation пуст либо неполон при ненулевом blocking count,
`tools/diagnostics/read-image-vulnerabilities.py` читает сохранённый immutable
OCI evidence, а не повторяет scan и не меняет verdict. Владелец сначала
получает exact artifact/image/vulnerability SHA через защищённый read path;
recipe, generation и build связываются этим readback отдельно. Receipt не
содержит этих трёх идентификаторов, поэтому helper их не выдумывает.

```sh
python3 -B tools/diagnostics/read-image-vulnerabilities.py \
  --context k3d-kodex --kubeconfig /home/s/.kube/config \
  --deployment-uid "$expected_registry_deployment_uid" \
  --artifact-ref "$expected_artifact_ref" \
  --image-digest "$expected_image_digest" \
  --vulnerability-sha256 "$expected_vulnerability_sha256" \
  --evidence-manifest-digest "$expected_evidence_manifest_digest"

python3 -B -m unittest discover -s tools/diagnostics \
  -p test_read_image_vulnerabilities.py
```

Проверяются exact Ready Pod → ReplicaSet → Deployment UID, scope evidence,
registry image, ServiceAccount и PVC; regular owned0600 kubeconfig. Читаются
только canonical regular paths, без symlink, полного metadata/env и Secret.
Проверяются tag/revision link, manifest digest, все21 descriptors/hashes,
canonical chunks и логический vulnerability SHA, receipt и policy counts.
Бюджет180 секунд; part16MiB, общий evidence64MiB, итоговая проекция256KiB.

Raw report, URLs и locations остаются в памяти и не печатаются. Вывод содержит
ограниченные CVE/GHSA/GO identifiers, package/version/fix expressions и счётчики
отфильтрованных записей с закрытыми reason codes. `knownTools` выдаёт только
имена программ из закрытой таблицы exact paths; отсутствие location не
доказывает отсутствия уязвимого бинарника. Reported fix expression не считается
проверенным runtime pin. Ненулевой suppressed count требует дальнейшей
диагностики, а не объявления исправления или ослабления policy.

Формат хранения сверяется по
[Distribution2.8.3 paths.go](https://github.com/distribution/distribution/blob/v2.8.3/registry/storage/paths.go);
Context7 Distribution использован для официальной структуры registry API.
