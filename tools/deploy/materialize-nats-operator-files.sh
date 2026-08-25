#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS operator materialization failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf 'Usage: %s --nsc-home <path> --output-directory <path>\n' "$0" >&2
}

nsc_home=""
output_directory=""
while (($# > 0)); do
  case "$1" in
    --nsc-home) nsc_home="${2:-}"; shift 2 ;;
    --output-directory) output_directory="${2:-}"; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ -d "$nsc_home" && ! -L "$nsc_home" ]] || fail 'nsc home is invalid'
[[ -d "$output_directory" && ! -L "$output_directory" ]] || fail 'output directory is invalid'
for command_name in awk grep install jq mktemp nsc; do
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
done

umask 077
temporary_directory=$(mktemp -d "$output_directory/.nats-operator-material.XXXXXX")
trap 'rm -rf -- "$temporary_directory"' EXIT

extract_compact_jwt() {
  local input_file=$1 output_file=$2
  awk '
    /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/ {
      value = $0
      count++
    }
    END {
      if (count != 1) {
        exit 1
      }
      print value
    }
  ' "$input_file" >"$output_file" || fail 'nsc output does not contain exactly one compact JWT'
}

describe_jwt() {
  local kind=$1 name=$2 output_name=$3 armoured_file
  armoured_file="$temporary_directory/$output_name.armoured"
  nsc -H "$nsc_home" describe "$kind" --name "$name" --raw \
    --output-file "$armoured_file" >/dev/null 2>&1 || fail "nsc describe failed: $kind/$name"
  extract_compact_jwt "$armoured_file" "$temporary_directory/$output_name"
}

describe_account_public() {
  local name=$1 output_name=$2 json_file
  json_file="$temporary_directory/$output_name.json"
  nsc -H "$nsc_home" describe account --name "$name" --json --field sub \
    --output-file "$json_file" >/dev/null 2>&1 || fail "nsc account subject readback failed: $name"
  jq -er 'select(type == "string") | select(test("^A[A-Z2-7]{55}$"))' \
    "$json_file" >"$temporary_directory/$output_name" || fail "nsc account subject is invalid: $name"
}

describe_jwt operator KODEX operator.jwt
describe_jwt account SYS system-account.jwt
describe_account_public SYS system-account.public
describe_jwt account APPLICATION account.jwt
describe_account_public APPLICATION account.public

for output_name in operator.jwt system-account.jwt system-account.public account.jwt account.public; do
  [[ $(awk 'END {print NR}' "$temporary_directory/$output_name") -eq 1 ]] ||
    fail "canonical output is not a single line: $output_name"
done

for output_name in operator.jwt system-account.jwt account.jwt; do
  grep -Eq '^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$' \
    "$temporary_directory/$output_name" || fail "canonical JWT is invalid: $output_name"
done
for output_name in system-account.public account.public; do
  grep -Eq '^A[A-Z2-7]{55}$' "$temporary_directory/$output_name" ||
    fail "canonical account nkey is invalid: $output_name"
done

for output_name in operator.jwt system-account.jwt system-account.public account.jwt account.public; do
  install -m 0600 "$temporary_directory/$output_name" "$output_directory/$output_name"
done

printf 'NATS operator materialization completed without credential output\n'
