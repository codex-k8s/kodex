---
id: RUN-MC-018
title: Диагностика автоматического admission образов ролей
type: runbook
status: approved
owner: sre
version: 1.0.10
updated: 2026-08-26
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
   get/list/create Job; get/list/create/delete PVC. Secret, Pod, Deployment,
   RoleBinding, list/watch parameters и update/patch полномочия отсутствуют.
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
- `scan`, `sign` или `admit` failed: workspace удаляется guarded по UID,
  artifact остаётся непродвинутым, а повтор начинается с нового server-owned
  claim.
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
