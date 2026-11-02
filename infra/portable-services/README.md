# Опциональные portable services — #1986

Chart устанавливает изолированные S3/storage UI и bootstrap release registry.
По умолчанию оба выключены, render пустой; существующие внешние службы и
отдельные hostname сохраняются. Это инфраструктурный prerequisite #1977.

`exposure.mode=shared_host` размещает S3 API на `/s3/`, web UI на `/storage/`,
OCI на стандартном `/v2/`; корень, API/auth/WS/assets платформы принадлежат
прежнему renderer. Префиксы S3/UI настраиваются, пересечения запрещены.
`separate_hosts` использует отдельные явные DNS names, `disabled` не создаёт
внешние маршруты. UI включается отдельно через `storage.ui.enabled`.

S3 SDK получает HTTPS endpoint с canonical prefix и `UsePathStyle=true`.
Traefik удаляет prefix, фиксирует `X-Forwarded-Prefix`, Host/proto и сохраняет
SigV4 headers/query. Registry prefix `/v2/` сохраняется: Docker/OCI client не
обещает произвольный `/registry/` endpoint. UI сохраняет prefix до native
SeaweedFS4.51/54777d7fb, использует отдельный login/CSRF и прокси с Secure/HttpOnly/
SameSite=Strict cookies, ограничением Origin для unsafe методов. Platform
Authorization/cookies не выдаются S3/registry; UI получает только собственную
`admin-session` cookie, без Authorization платформы.

`exposure.tlsMode=managed` требует уже выданный `tlsSecret` и websecure.
`external` требует exact proxy CIDRs и выделенный web entrypoint; source
ограничивается до приложения, origin остаётся HTTPS. Backend S3, UI и registry
используют TLS с CA/SNI verification в обоих режимах. Старые mTLS identities
promoted registry/node pull этим chart не меняются. Raw master/filer и registry
backend слушают loopback внутри Pod; NetworkPolicy разрешает только выбранный
ingress controller и DNS, ServiceAccount не имеет token/RBAC.

`pki.mode=existing` использует явный Issuer/ClusterIssuer. `isolated` создаёт
только namespaced root/issuer и serving certificates для самостоятельного PoC.
CA keys остаются в cert-manager Secrets. Serving certs действуют30дней;
оператор проверяет обновление сертификата и выполняет контролируемый restart
registry/console gateway до истечения прежнего leaf. Автоматический restart
не заявлен. Root и данные не удаляются при смене route/image.

В shared-host режиме `storage.trustedProxyCIDRs` задаёт exact Pod IPs
проверенного ingress controller. Backend принимает forwarded prefix только
от этих TCP peers; NetworkPolicy дополнительно ограничивает selector. При
смене Pod IPs требуется новый план/values и controlled rollout, не trust-all.

Credentials создаются отдельно от Helm; values содержат только Secret refs
и nonsecret usernames/revision. Рекомендуются versioned immutable Secrets:

| Secret ref | Обязательные file keys |
|---|---|
| `storage.authSecret` | `s3.json`: непустой закрытый identities/credentials/actions contract |
| При включённом UI | `ui-user`, `ui-password` (не менее32байт), отдельная учётная запись оператора |
| `registry.authSecret` | `htpasswd`: ровно writer/reader из values, bcrypt cost10–16 |

Нет anonymous identity, пустого IAM/password или fallback. Init container
проверяет ограниченные файлы и публикует read-only0400 snapshots в private
memory volume; изменение Secret не обходит эту проверку после старта.
Для ротации выбираются новый Secret ref/credentialsRevision и новый Pod.
Writer/reader различны; reader имеет только GET/HEAD. Registry ограничен
выбранным repositoryPrefix, catalog и чужие repositories не публикуются.
Owner passwords, S3 keys и Docker auth не передаются в argv, image или values.

План без cluster mutation:

```sh
python3 infra/portable-services/plan.py --values /private/values.json \
  --release kodex-services --namespace kodex-trust --output /private/render.yaml
```

Parent output directory должен иметь0700, файл создаётся исключительно0600.
Values JSON проходит duplicate-key/host/exact-CIDR validation и Helm schema.
Проверенный chart archive/source, values/render hashes, target/cluster UID,
Secret refs, Certificate/PVC UIDs и imageIDs фиксируются в private release lock.
Apply выполняется Helm после preimage/ownership проверки, затем проверяются
Certificate Ready, exact serving config, настоящий S3 write/read/presign/
multipart, UI login/assets/logout, OCI401/reader403/writer publish/readback,
повтор apply/restart и данные. Timeout write требует reconciliation, не нового
создания объекта без readback. Secrets и signed URLs не печатаются.

Storage/registry —одна реплика с PVC и ограниченными ресурсами. Backup должен
включать согласованные metadata/objects, CA и credentials через утверждённый
backend; namespace/PVC delete, goose down и обещание rollback старым image
без проверки совместимости не используются.

Версии проверены по официальным pinned images: SeaweedFS4.51/source54777d7fb,
Python3.14.10, nginx1.30.6, Distribution2.8.3. Context7 и versioned исходники:
[S3 SigV4](https://github.com/seaweedfs/seaweedfs/blob/54777d7fb91c0ad9d63ae3d7c521f193f0c8c638/weed/s3api/auth_signature_v4.go),
[admin prefix](https://github.com/seaweedfs/seaweedfs/blob/54777d7fb91c0ad9d63ae3d7c521f193f0c8c638/weed/command/admin.go),
[session/CSRF](https://github.com/seaweedfs/seaweedfs/blob/54777d7fb91c0ad9d63ae3d7c521f193f0c8c638/weed/admin/dash/auth_middleware.go),
[AWS SDK endpoints](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html).
Native qualification и installed acceptance фиксируются отдельно от render.

Закреплённый upstream storage candidate ещё не является стабильным релизом:
`images.lock.json` фиксирует официальный dev image digest и source54777d7fb.
Он содержит исправление encoded-key SigV4/prefix
[ab204a03](https://github.com/seaweedfs/seaweedfs/commit/ab204a03e60f61c7293ba62ae939c9a7d540deb1),
которого нет в стабильных4.41/4.51. Использование выключено по умолчанию;
переход на выпущенную upstream версию требует нового digest и qualification.
Никаких локальных патчей SeaweedFS или отключения проверки подписи нет.

Воспроизводимая локальная проверка на rootless Docker, без доступа к кластеру:

```sh
python3 -m venv /tmp/kodex-synthetic-venv
/tmp/kodex-synthetic-venv/bin/pip install -r scripts/tests/portable-services-native-requirements.txt
timeout 300s /tmp/kodex-synthetic-venv/bin/python -B scripts/tests/portable-services-native-test.py
```

Smoke создаёт только свои контейнеры с synthetic credentials и ограничениями
CPU/memory, затем останавливает их. Порты bind только127.0.0.1; занятые порты
закрывают запуск. Kubernetes, внешние endpoints и credentials не принимаются.
Квитанция и synthetic данные остаются в новом каталоге0700 для диагностики.
