#!/usr/bin/env bash
set -euo pipefail

# Ни нового parser, ни установки утилит в controller image. Ниже генератор
# добавляет canonical renderer с одним изменённым SDK block; digest связывает
# его с настоящим штатным renderer этого immutable image до выдачи Job/PVC.
read -r kodex_renderer_source_sha256 _ < <(sha256sum /opt/kodex/render-image-admission-job.sh)
if [[ "$kodex_renderer_source_sha256" != __RENDERER_SOURCE_SHA256__ ]]; then
  printf '%s\n' 'Managed renderer source digest mismatch' >&2
  exit 78
fi
