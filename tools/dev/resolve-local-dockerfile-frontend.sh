#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex local Dockerfile frontend resolution failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --source-root <path> --format reference|digest\n' "$0" >&2
}

source_root=""
format=""
while (($# > 0)); do
  case "$1" in
    --source-root) source_root=${2:-}; shift 2 ;;
    --format) format=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$source_root" == /* && -f "$source_root/infra/dockerfile-frontend/Dockerfile" ]] ||
  fail 'source root does not contain the versioned Dockerfile frontend source'
case "$format" in reference|digest) ;; *) fail 'format is invalid' ;; esac

mapfile -t references < <(sed -nE \
  's|^FROM[[:space:]]+(docker\.io/docker/dockerfile:[^[:space:]@]+@sha256:[a-f0-9]{64})[[:space:]]*$|\1|p' \
  "$source_root/infra/dockerfile-frontend/Dockerfile")
((${#references[@]} == 1)) || fail 'versioned Dockerfile frontend must contain exactly one pinned FROM reference'
reference=${references[0]}
[[ "$reference" =~ ^docker\.io/docker/dockerfile:[a-zA-Z0-9._-]+@sha256:[a-f0-9]{64}$ ]] ||
  fail 'Dockerfile frontend reference is not pinned by an exact manifest digest'

case "$format" in
  reference) printf '%s\n' "$reference" ;;
  digest) printf '%s\n' "${reference#*@sha256:}" ;;
esac
