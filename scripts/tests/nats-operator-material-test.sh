#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'NATS operator material test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
materializer="$repository_root/tools/deploy/materialize-nats-operator-files.sh"
account_configurer="$repository_root/tools/deploy/configure-nats-application-account.sh"
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
mkdir -p "$temporary_directory/bin" "$temporary_directory/nsc" "$temporary_directory/output"

cat >"$temporary_directory/bin/nsc" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ " $* " == *' edit account '* ]]; then
  printf '%s\n' "$*" >"${FAKE_NSC_CALL_LOG:?}"
  exit 0
fi
output_file=""
kind=""
name=""
mode=""
field=""
while (($# > 0)); do
  case "$1" in
    describe) kind=${2:-}; shift 2 ;;
    --name) name=${2:-}; shift 2 ;;
    --raw) mode=raw; shift ;;
    --json) mode=json; shift ;;
    --field) field=${2:-}; shift 2 ;;
    --output-file) output_file=${2:-}; shift 2 ;;
    -H) shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$output_file" ]]
if [[ "${FAKE_NSC_MALFORMED:-false}" == true && "$name" == KODEX ]]; then
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
  if [[ -z "$field" ]]; then
    disk_storage=34359738368
    [[ "${FAKE_NSC_BAD_LIMITS:-false}" != true ]] || disk_storage=1
    jq -n --argjson disk_storage "$disk_storage" '{
      name: "APPLICATION",
      nats: {
        type: "account",
        limits: {
          mem_storage: 268435456,
          disk_storage: $disk_storage,
          streams: 8,
          consumer: 64,
          max_ack_pending: 100000,
          mem_max_stream_bytes: 268435456,
          disk_max_stream_bytes: 34359738368,
          max_bytes_required: true
        }
      }
    }' >"$output_file"
    exit 0
  fi
  printf '"A%s"\n' "$(printf 'P%.0s' {1..55})" >"$output_file"
  exit 0
fi
exit 1
EOF
chmod 0700 "$temporary_directory/bin/nsc"

FAKE_NSC_CALL_LOG="$temporary_directory/nsc-edit.log" PATH="$temporary_directory/bin:$PATH" \
  "$account_configurer" --nsc-home "$temporary_directory/nsc" >/dev/null
for expected_flag in \
  '--js-mem-storage 268435456' \
  '--js-disk-storage 34359738368' \
  '--js-streams 8' \
  '--js-consumer 64' \
  '--js-max-ack-pending 100000' \
  '--js-max-mem-stream 268435456' \
  '--js-max-disk-stream 34359738368' \
  '--js-max-bytes-required'; do
  grep -Fq -- "$expected_flag" "$temporary_directory/nsc-edit.log" ||
    fail "JetStream account edit omits $expected_flag"
done
if FAKE_NSC_BAD_LIMITS=true FAKE_NSC_CALL_LOG="$temporary_directory/nsc-edit-bad.log" \
  PATH="$temporary_directory/bin:$PATH" \
  "$account_configurer" --nsc-home "$temporary_directory/nsc" >/dev/null 2>&1; then
  fail 'JetStream account limit drift was accepted'
fi

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

generate_material="$repository_root/tools/install/generate-material.sh"
nats_user_policy="$repository_root/tools/install/nats-runtime-users.tsv"
account_disk_storage_bytes=34359738368
session_revocation_stream_bytes=16777216
control_plane_stream_bytes=$(sed -n \
  's/^[[:space:]]*- CONTROL_PLANE_NATS_MAX_BYTES=//p' \
  "$repository_root/deploy/k8s/base/control-plane/kustomization.yaml")
[[ "$control_plane_stream_bytes" =~ ^[1-9][0-9]*$ ]] ||
  fail 'control-plane stream byte budget is invalid'
((control_plane_stream_bytes + session_revocation_stream_bytes <= account_disk_storage_bytes)) ||
  fail 'mandatory JetStream reservations exceed the application account disk budget'
[[ $(awk -F'|' 'NF == 5 {count++} END {print count+0}' "$nats_user_policy") -eq 3 ]] ||
  fail 'NATS runtime user policy must contain exactly three complete rows'
if grep -Fq -- '$JS.API.>' "$nats_user_policy"; then
  fail 'runtime NATS credentials retain wildcard JetStream administration access'
fi
for permission_contract in \
  '$JS.API.STREAM.INFO.CONTROL_PLANE' \
  '$JS.API.STREAM.CREATE.CONTROL_PLANE' \
  '$JS.API.STREAM.UPDATE.CONTROL_PLANE' \
  '$JS.API.STREAM.INFO.CONTROL_API_SESSION_REVOCATIONS' \
  '$JS.API.STREAM.CREATE.CONTROL_API_SESSION_REVOCATIONS' \
  '$JS.API.STREAM.UPDATE.CONTROL_API_SESSION_REVOCATIONS' \
  '$JS.API.STREAM.MSG.GET.CONTROL_API_SESSION_REVOCATIONS' \
  'kodex.control_api.session_revocation.*' \
  'control_plane.run.*.*.events' \
  'control_plane.platform.*.events'; do
  grep -Fq -- "$permission_contract" "$nats_user_policy" ||
    fail "NATS least-privilege contract omits $permission_contract"
done

bash -n "$account_configurer" "$materializer" \
  "$generate_material" \
  "$repository_root/tools/install/materialize-secrets.sh"
printf 'NATS operator material tests passed\n'
