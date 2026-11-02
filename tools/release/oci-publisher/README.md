# Ограниченная публикация OCI (#1989)

Опциональный publish unit для HTTPS registry за proxy с ограничением request
body. Direct `build-release.sh` сохраняется. Unit проверяет собственный OCI
archive, публикует blobs ограниченными chunks через regclient v0.11.5 и
подтверждает manifest digest. TLS/CA, authorization, admission, promotion и
node-pull mTLS не ослабляются. Источник build остаётся прежним exact checkout.

Адаптер `../buildctl-chunked.py` выбирается через поддержанный `BUILDCTL_PATH`.
Новый `KODEX_RELEASE_CHUNKED_CONFIG` — явная ссылка на private JSON config
(0400/0600), не прежний env API установщика. Значения credentials там не нужны.
Собрать CLI из этого pinned source:

```sh
cd tools/release/oci-publisher
GOWORK=off go build -trimpath -o /private/tools/oci-publisher ./cmd/oci-publisher
chmod 0550 /private/tools/oci-publisher
```

Config соответствует `chunked-config.schema.json`:

```json
{
  "schema_version": 1,
  "buildctl": {"path": "/private/tools/buildctl", "sha256": "<verified-sha256>"},
  "publisher": {"path": "/private/tools/oci-publisher", "sha256": "<verified-sha256>"},
  "chunk_bytes": 8388608,
  "max_archive_bytes": 8589934592,
  "timeout_seconds": 600,
  "ca_file": ""
}
```

`DOCKER_CONFIG/config.json` — отдельный owned regular0400/0600 auth file,
с inline login только для выбранного registry, без credential helpers.
Publisher читает выбранный entry, не использует другой аккаунт. Credentials
не входят в argv, URL или metadata. Системные CA используются по умолчанию;
`ca_file` добавляет публичный PEM bundle. Только HTTPS и выбранная authority,
без redirects/ambient proxy; cross-host token realms не поддерживаются этим
ограниченным режимом. Для иных схем остаётся прежний direct path; автоматического
fallback нет. Авторизационные ошибки не запускают import.

```sh
export BUILDCTL_PATH=/checked/source/tools/release/buildctl-chunked.py
export KODEX_RELEASE_CHUNKED_CONFIG=/private/chunked.json
export DOCKER_CONFIG=/private/registry-auth
# Затем штатный tools/release/build-release.sh с exact source и private endpoints.
```

Adapter принимает только публикационные `buildctl --addr unix:///... build
--frontend dockerfile.v0` вызовы существующего release builder. Дополнительные
secret/SSH mounts и insecure entitlements закрыты. Buildctl/publisher binaries
сверяются с SHA256, принадлежат caller/root и не допускают group/other write.
При shared init-tools сначала копируются в собственный private tool directory. Archive имеет предел8GiB и10000 tar entries; каждая blob
проверяется по SHA256 до сетевых эффектов. OCI layout содержит один root
manifest/index descriptor; его descendants, включая attestations, не должны
переписываться. Неизвестный/different digest закрывает результат.

Metadata появляется у caller только после registry readback и остаётся побайтно
исходной BuildKit metadata. Failure/timeout не является разрешением повторить
весь import: следующий запуск сначала читает destination digest. Совпадение
подтверждает прежний publish; другой digest даёт conflict. Ответ, потерянный
после commit manifest, сверяется чтением. CLI не выводит upstream payload или
signed upload queries. Budget:64KiB–16MiB chunks, явный retry limit1 без повторения всего
import, per-request timeout1m и общий CLI context10m;
adapter может установить меньший process timeout.

Caller обязан сериализовать публикацию одной пары repository/tag либо применять
registry immutable-tag policy. OCI tag PUT не предоставляет общего CAS;
предварительное чтение не выдаётся за distributed lock. Digest verification и
conflict сохраняются, но конкурентные writers не допускаются этим профилем.

Unit не подписывает неподтверждённую provenance, не создаёт admission receipt и
не устанавливает runtime images. Publisher/tool/source SHA, binary digests,
release lock и результаты admission фиксируются раздельно. После успеха archive
очищается только из собственного temporary directory. При failure/timeout этот
каталог0700 сохраняет archive, metadata и failure.json0600 со стадией и digest
для readback; оператор учитывает занимаемое место и удаляет его после сверки.
Созданная Buildctl metadata ограничивается до0600 после проверки владельца,
regular file и одной hardlink; требования к входным secret/config не ослабляются.
PVC/registry data не удаляются.

Проверки: `make test-oci-publisher`. Go suite использует настоящий disposable
OCI registry olareg0.2.1, synthetic auth и test CA, body limit128KiB; размер слоя
выше лимита, chunks64KiB. Проверяются digest/repeat/conflict/401/403,
metadata/blob corruption, secret symlink, foreign redirect, потерянный ответ
и отказ без CA. Adapter suite проверяет metadata handoff и отсутствие
credential/query payload в диагностике. Native large-archive квалификация
проводится отдельно, в разрешённом пространстве, с TLS и digest readback.

Источники: [regclient v0.11.5](https://github.com/regclient/regclient/tree/v0.11.5),
[BuildKit v0.32.2 OCI export](https://github.com/moby/buildkit/blob/v0.32.2/README.md),
[Buildx dial-stdio](https://docs.docker.com/reference/cli/docker/buildx/dial-stdio/).
Context7 и pinned APIs проверены; latest documentation сама по себе не доказывает
совместимость иных версий.
