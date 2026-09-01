#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Local storage E2E reliability contract failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
source "$repository_root/scripts/tests/lib/local-kubernetes-e2e.sh"

temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
state_directory="$temporary_directory/state"
mkdir -m 0700 "$state_directory"
kodex_e2e_ensure_private_directory "$state_directory/e2e" || fail 'private state creation failed'
[[ $(stat -c '%a' "$state_directory/e2e") == 700 ]] || fail 'private state mode is not 0700'
ln -s "$state_directory/e2e" "$state_directory/linked-e2e"
if kodex_e2e_ensure_private_directory "$state_directory/linked-e2e"; then
  fail 'symlink state directory was accepted'
fi

endpoint_ready=true
kubectl() {
  case "$*" in
    '-n kodex-system get service/seaweedfs-s3 -o json')
      printf '%s\n' '{"metadata":{"name":"seaweedfs-s3","namespace":"kodex-system","labels":{"app.kubernetes.io/name":"seaweedfs","app.kubernetes.io/component":"object-storage","kodex.dev/local-profile":"hot-reload"}},"spec":{"ports":[{"name":"s3","protocol":"TCP","port":8333,"targetPort":"s3"}]}}'
      ;;
    '-n kodex-system get endpointslices.discovery.k8s.io -l kubernetes.io/service-name=seaweedfs-s3 -o json')
      if [[ "$endpoint_ready" == true ]]; then
        printf '%s\n' '{"items":[{"metadata":{"namespace":"kodex-system","labels":{"kubernetes.io/service-name":"seaweedfs-s3","app.kubernetes.io/name":"seaweedfs","app.kubernetes.io/component":"object-storage","kodex.dev/local-profile":"hot-reload"}},"ports":[{"name":"s3","protocol":"TCP","port":8333}],"endpoints":[{"conditions":{"ready":true,"serving":true,"terminating":false},"addresses":["10.42.0.10"]}]}]}'
      else
        printf '%s\n' '{"items":[]}'
      fi
      ;;
    '-n kodex-system get job/complete -o json')
      printf '%s\n' '{"metadata":{"namespace":"kodex-system","name":"complete"},"status":{"conditions":[{"type":"Complete","status":"True","reason":"Completed"}]}}'
      ;;
    '-n kodex-system get job/failed -o json')
      printf '%s\n' '{"metadata":{"namespace":"kodex-system","name":"failed"},"status":{"conditions":[{"type":"Failed","status":"True","reason":"DeadlineExceeded"}]}}'
      ;;
    '-n kodex-system get pods -l job-name=failed -o json')
      printf '%s\n' '{"items":[]}'
      ;;
    *) return 1 ;;
  esac
}

kodex_e2e_require_seaweedfs_s3_endpoint kodex-system || fail 'ready EndpointSlice was rejected'
endpoint_ready=false
if kodex_e2e_require_seaweedfs_s3_endpoint kodex-system; then
  fail 'absent EndpointSlice endpoint was accepted'
fi
kodex_e2e_wait_job_complete kodex-system complete 1 || fail 'Complete Job was rejected'
if kodex_e2e_wait_job_complete kodex-system failed 1 2>/dev/null; then
  fail 'Failed Job was accepted as Complete'
fi
unset -f kubectl

archive_wrapper="$repository_root/scripts/tests/local-session-archive-e2e.sh"
restore_wrapper="$repository_root/scripts/tests/local-backup-restore-e2e.sh"
backup_wrapper="$repository_root/scripts/tests/local-session-archive-backup-restore-e2e.sh"
for wrapper in "$archive_wrapper" "$restore_wrapper" "$backup_wrapper"; do
  rg -Fq 'kodex_e2e_ensure_private_directory "$state_directory/e2e"' "$wrapper" ||
    fail "private state contract is absent: $wrapper"
  rg -Fq 'kodex_e2e_start_seaweedfs_port_forward kodex-system' "$wrapper" ||
    fail "bounded port-forward contract is absent: $wrapper"
done
for wrapper in "$restore_wrapper" "$backup_wrapper"; do
  rg -Fq 'kodex_e2e_wait_job_complete kodex-system "$job_name" 900' "$wrapper" ||
    fail "terminal Job contract is absent: $wrapper"
  if rg -Fq 'wait --for=condition=Complete' "$wrapper"; then
    fail "Complete-only Job wait remains: $wrapper"
  fi
done
rg -Fq '.spec.activeDeadlineSeconds = 900' "$restore_wrapper" ||
  fail 'restore E2E Job deadline is absent'
rg -Fq 'activeDeadlineSeconds: 900' "$backup_wrapper" ||
  fail 'backup E2E Job deadline is absent'
rg -Fq 'kodex_e2e_delete_owned_jobs kodex-system "$stale_job_selector"' "$backup_wrapper" ||
  fail 'exact stale backup Job cleanup is absent'
rg -Fq -- '--wait=true --timeout="$timeout"' \
  "$repository_root/scripts/tests/lib/local-kubernetes-e2e.sh" ||
  fail 'bounded owned Job deletion is absent'

printf 'Local storage E2E reliability contract passed.\n'
