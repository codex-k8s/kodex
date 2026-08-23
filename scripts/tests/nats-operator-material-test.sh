#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS operator material test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
materializer="$repository_root/tools/deploy/materialize-nats-operator-files.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
mkdir -p "$temporary_directory/bin" "$temporary_directory/nsc" "$temporary_directory/output"

cat >"$temporary_directory/bin/nsc" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output_file=""
kind=""
name=""
mode=""
while (($# > 0)); do
  case "$1" in
    describe) kind=${2:-}; shift 2 ;;
    --name) name=${2:-}; shift 2 ;;
    --raw) mode=raw; shift ;;
    --json) mode=json; shift ;;
    --field) shift 2 ;;
    --output-file) output_file=${2:-}; shift 2 ;;
    -H) shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$output_file" ]]
if [[ "${FAKE_NSC_MALFORMED:-false}" == true && "$name" == MATTERCODEX ]]; then
  printf '%s\n' '-----BEGIN NATS OPERATOR JWT-----' 'not-a-jwt' '------END NATS OPERATOR JWT------' >"$output_file"
  exit 0
fi
if [[ "$mode" == raw ]]; then
  token=header.payload.signature
  [[ "$kind/$name" == account/SYS ]] && token=sysheader.syspayload.syssignature
  [[ "$kind/$name" == account/APPLICATION ]] && token=appheader.apppayload.appsignature
  printf '%s\n%s\n%s\n' "-----BEGIN NATS ${kind^^} JWT-----" "$token" "------END NATS ${kind^^} JWT------" >"$output_file"
  exit 0
fi
if [[ "$mode" == json && "$name" == SYS ]]; then
  printf '"A%s"\n' "$(printf 'S%.0s' {1..55})" >"$output_file"
  exit 0
fi
if [[ "$mode" == json && "$name" == APPLICATION ]]; then
  printf '"A%s"\n' "$(printf 'P%.0s' {1..55})" >"$output_file"
  exit 0
fi
exit 1
EOF
chmod 0700 "$temporary_directory/bin/nsc"

PATH="$temporary_directory/bin:$PATH" "$materializer" \
  --nsc-home "$temporary_directory/nsc" --output-directory "$temporary_directory/output" >/dev/null

[[ $(<"$temporary_directory/output/operator.jwt") == header.payload.signature ]] ||
  fail 'operator JWT was not extracted from the armoured output'
[[ $(<"$temporary_directory/output/system-account.jwt") == sysheader.syspayload.syssignature ]] ||
  fail 'system account JWT was not extracted from the armoured output'
[[ $(<"$temporary_directory/output/account.jwt") == appheader.apppayload.appsignature ]] ||
  fail 'application account JWT was not extracted from the armoured output'
grep -Eq '^A[A-Z2-7]{55}$' "$temporary_directory/output/system-account.public" ||
  fail 'system account nkey was not decoded from JSON'
grep -Eq '^A[A-Z2-7]{55}$' "$temporary_directory/output/account.public" ||
  fail 'application account nkey was not decoded from JSON'

before_sha=$(sha256sum "$temporary_directory/output/operator.jwt" | awk '{print $1}')
if FAKE_NSC_MALFORMED=true PATH="$temporary_directory/bin:$PATH" "$materializer" \
  --nsc-home "$temporary_directory/nsc" --output-directory "$temporary_directory/output" >/dev/null 2>&1; then
  fail 'malformed NATS output was accepted'
fi
after_sha=$(sha256sum "$temporary_directory/output/operator.jwt" | awk '{print $1}')
[[ "$before_sha" == "$after_sha" ]] || fail 'failed materialization changed existing canonical output'

bash -n "$materializer" "$repository_root/tools/deploy/generate-fresh-install-material.sh" \
  "$repository_root/tools/deploy/materialize-fresh-install-secrets.sh"
printf 'NATS operator material tests passed\n'
