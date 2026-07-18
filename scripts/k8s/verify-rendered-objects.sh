#!/usr/bin/env bash

set -euo pipefail

RENDER_DIR=""
EXPECTED_FILES=""
EXPECTED_OBJECTS=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --render-dir)
      RENDER_DIR="$2"
      shift 2
      ;;
    --expected-files)
      EXPECTED_FILES="$2"
      shift 2
      ;;
    --expected-objects)
      EXPECTED_OBJECTS="$2"
      shift 2
      ;;
    *)
      printf 'неизвестный аргумент: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

if [ -z "$RENDER_DIR" ] || [ ! -d "$RENDER_DIR" ]; then
  printf 'render directory обязателен\n' >&2
  exit 1
fi
for command_name in yq kubectl; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'не найдена команда: %s\n' "$command_name" >&2
    exit 1
  fi
done

mapfile -t yaml_files < <(find "$RENDER_DIR" -maxdepth 1 -type f -name '*.yaml' -print | sort)
file_count="${#yaml_files[@]}"
if [ "$file_count" -eq 0 ]; then
  printf 'render directory не содержит YAML-файлов\n' >&2
  exit 1
fi

object_count=0
dry_run_count=0
for yaml_file in "${yaml_files[@]}"; do
  while IFS= read -r object_identity; do
    [ -n "$object_identity" ] || continue
    object_count=$((object_count + 1))
    printf '%s\t%s\n' "$(basename "$yaml_file")" "$object_identity"
  done < <(yq eval --no-doc 'select(. != null) | (.kind + "/" + .metadata.name)' "$yaml_file")

  client_output="$(kubectl create --dry-run=client --validate=false -f "$yaml_file" -o name)"
  if [ -n "$client_output" ]; then
    while IFS= read -r object_name; do
      [ -n "$object_name" ] || continue
      dry_run_count=$((dry_run_count + 1))
    done <<<"$client_output"
  fi
done

if [ "$object_count" -ne "$dry_run_count" ]; then
  printf 'число непустых YAML objects (%d) не совпадает с client dry-run (%d)\n' "$object_count" "$dry_run_count" >&2
  exit 1
fi
if [ -n "$EXPECTED_FILES" ] && [ "$file_count" -ne "$EXPECTED_FILES" ]; then
  printf 'число YAML-файлов: %d, ожидалось: %d\n' "$file_count" "$EXPECTED_FILES" >&2
  exit 1
fi
if [ -n "$EXPECTED_OBJECTS" ] && [ "$object_count" -ne "$EXPECTED_OBJECTS" ]; then
  printf 'число Kubernetes objects: %d, ожидалось: %d\n' "$object_count" "$EXPECTED_OBJECTS" >&2
  exit 1
fi

printf 'render evidence: files=%d objects=%d client_dry_run=%d\n' "$file_count" "$object_count" "$dry_run_count"
