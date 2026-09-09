---
id: OPS-RUNNER-1382
title: Проверка runner binary из сохранённого OCI base
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-09-09
---

# Назначение

`tools/release/runner-binary-provenance.py` восстанавливает
`RUNNER_BINARY_PROVENANCE` для #1378 из **сохранённого exact OCI archive**.
Не запускает образ/entrypoint, не извлекает файловую систему на host, не читает
runtime Pod, не вызывает registry, BuildKit, Kubernetes или provider. Это
ожидаемый hash base runner до пользовательского Run; фактический работающий
runner затем отдельно сравнивается observer из OPS-EMAIL-1378.

Это локальное доказательство целостности доверенного оператором build cache,
а не подпись BuildKit/SLSA и не новый admission/promotion receipt. Filename,
input hash и чистый source связывают сохранённую локальную сборку с её inputs;
они не доказывают честность неизвестного сборщика. Произвольный чужой архив
нельзя превратить в trusted build простым переименованием. Product admission,
SBOM/provenance/signature, promoted RoleImage и node pull остаются прежними.

# Входы и команда

Нужны Linux, Python 3 standard library, Git и GNU tar. Новых Python packages
нет. Оператор сначала выбирает доверенные archive/manifest/source из release
journal и build metadata. Source должен быть exact clean checkout; untracked
файлы и ignored файлы внутри `services/jobs/agent-runner`/`libs/go` закрыто
отклоняются. Ignored cache вне этих build inputs не участвует в hash.

```bash
python3 -B tools/release/runner-binary-provenance.py input \
  --source-root "$RUNNER_BUILD_SOURCE" --revision "$RUNNER_BUILD_REVISION"
```

Результат `input` — только normalized SHA256. Алгоритм прежний: GNU tar
`--sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner`
для двух каталогов. Это не Git tree hash. `TAR_OPTIONS` не влияет на вычисление.
Проверки Git выполняются до и после; основная verify повторяет весь input hash
после OCI чтения. Исторический source revision нельзя подменять source текущего
release helper; helper можно выполнять из нового checkout с отдельным
`--source-root` старой сборки.

```bash
python3 -B tools/release/runner-binary-provenance.py verify \
  --archive "$RUNNER_ARCHIVE" \
  --expected-manifest "$RUNNER_BASE_MANIFEST" \
  --source-root "$RUNNER_BUILD_SOURCE" --revision "$RUNNER_BUILD_REVISION" \
  --expected-input-digest "$RUNNER_BUILD_INPUT_SHA256" \
  --repository registry.local.kodex/kodex/agent-runner \
  --output "$RUNNER_PROVENANCE"
```

Archive filename строго `agent-runner-<input SHA256>.oci.tar`. Дескриптор manifest
в OCI index обязан содержать оба annotations: `io.containerd.image.name` равен
`<repository>:local-<input SHA256>`, `org.opencontainers.image.ref.name` равен
`local-<input SHA256>`. Конфликтующие annotations самого index отклоняются.
Это штатные tags сохранённого Buildx archive; одного переименования файла
недостаточно. Manifest задаётся
отдельным exact `sha256:<64 hex>` из доверенного release/CFG journal. Каталог
output уже существует, принадлежит текущему UID, mode **0700**, без symlink;
output должен отсутствовать. `verify` создаёт deterministic JSON через private
временный файл, fsync, atomic exclusive link и directory fsync, mode **0600**.
Существующий файл, link или конкурирующий writer не перезаписывается.

Для повторной проверки тех же bytes заменить фазу `verify` на `check`, сохранив
все аргументы. `check` повторно проверяет source и весь OCI, затем сравнивает
полные deterministic bytes прежнего 0600 файла этого CLI. Не меняет output.
Исторический вручную созданный provenance может иметь иной JSON format/набор
полей: для него сначала создать новый absent output и отдельно сравнить
sourceRevision/baseImage/binaryPath/binarySHA256, сохранив оба evidence. Изменившиеся
параметры требуют отдельного отсутствующего output, а не правки истории.

stdout verify/check содержит только PASS, provenanceSHA256 и binarySHA256.
Ошибки — закрытые English codes без содержимого image config, env, archive
entries, stderr Git/tar или source. Сбой до публикации не создаёт результат.
При неопределённом завершении сначала `check` существующего output. Никаких
внешних эффектов/автоматических retries этих CLI не существует.

# Проверяемая цепочка

| Граница | Проверка / отказ |
| --- | --- |
| source | Exact HEAD, clean tracked/untracked state, отсутствие ignored build inputs, одинаковый normalized hash до/после |
| archive | Absolute regular non-symlink path, single hardlink; inode/device/mtime/ctime/size до/после; SHA256 archive bytes |
| tar | Канонические относительные paths, без traversal/дубликатов/sparse; обязательный zero terminator, запрет скрытого tar после terminator |
| OCI layout/index | layout1.0.0, schemaVersion2, один exact OCI image manifest; неизвестные media types и несколько платформ закрыто отклоняются |
| descriptors | SHA256 bytes каждого blob и size каждого manifest/config/layer descriptor; remote URLs/inline data не используются |
| config | Linux/amd64, rootfs layers и точный ordered diff_ids для decompressed layers |
| layers | OCI tar или tar+gzip; ordered overlay, whiteouts только нижних слоёв, opaque не удаляет новые entries того же слоя |
| executable | Итоговый `/usr/local/bin/kodex-agent-runner` — непустой regular executable без setuid/setgid; target и ancestry не могут быть symlink/hardlink/special |
| output | Exact source/base/input/archive/config/layers/binary metadata, atomic exclusive0600; standalone consumer #1378 принимает прежние обязательные поля |

Внешние symlink/hardlink Debian base **никогда не разыменовываются**: они
учитываются как inert записи при overlay. Иначе обычный distro `/bin` symlink
сделал бы весь base непроверяемым. Любой child под не-directory, неоднозначная
смена parent, link/special target или ancestor закрыто отклоняется. Сам target
не может быть ссылкой даже в промежуточном слое; такой layout не объявляется
поддержанным. Whiteout/replacement не оставляет прежние children или digest.

Ограничения: archive и expanded layer до 8 GiB, до 128 layers, до 500000 entries
на tar, JSON до 4 MiB. Expanded layer проходит через закрытый anonymous temporary
file; свободное место должно вмещать один expanded layer. Образ целиком на host
не распаковывается. Не поддерживаются zstd/foreign layers, image index с несколькими
manifest, Docker media types и необычные runner links; unsupported — FAIL,
а не fallback. Для оператора рекомендуется внешний bounded handle
`timeout 300s python3 -B ...`; истёкший budget не разрешает перезапись output.

# Интеграция с dev builder

`tools/dev/build-local-runner.sh` получает input hash этим же CLI. Cache hit
сохраняет прежний archive и image digest, не вызывает Buildx build или bootstrap
builder. Для нового archive прежний build выполняется один раз. Перед import
оба пути вызывают verify/check и сохраняют рядом:

`cache/agent-runner-<input SHA256>-<source revision>.provenance.json`.

Новый source revision с неизменными inputs получает отдельный provenance того
же image; прежнее evidence сохраняется. Existing provenance проверяется
полностью. Несоответствие не удаляет cache и не запускает rebuild. Сам builder
по-прежнему является build/import mutator; для read-only восстановления
использовать standalone CLI, не запускать builder ради одного hash.

# Локальная проверка и выпуск

`make test-runner-binary-provenance` проверяет public CLI на disposable Git/OCI
fixtures: gzip/tar, corruption/size/diffID, wrong source/input/manifest,
whiteout/opaque/replacement, links/traversal/duplicates/terminator, executable,
readback и atomic race. Также проверяется прежний image-cache import contract.
Никаких образов/контейнеров, live архивов, credentials или provider calls.
Это локальная корректность оснастки, не live acceptance.

Tools-only выпуск: application/sidecar/Proto/OpenAPI/SQL/images не меняются.
Старые six-field RUNNER_BINARY_PROVENANCE остаются совместимы с #1378;
новые дополнительные поля дают artifact chain. Rollback helper не откатывает
образы и не удаляет provenance. Руководство OPS-EMAIL-1378 описывает дальнейший
combined plan/один отдельно разрешённый Run и runtime observer.

Через Context7 проверены [OCI image layout](https://github.com/opencontainers/image-spec/blob/main/image-layout.md),
[whiteout/opaque semantics](https://github.com/opencontainers/image-spec/blob/main/layer.md)
и [ordered rootfs diff_ids](https://github.com/opencontainers/image-spec/blob/main/config.md).
