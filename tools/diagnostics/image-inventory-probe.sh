#!/usr/bin/env bash
# Закрытая диагностика tools exact platform base без запуска агента и изменения rootfs.
set -euo pipefail
[[ ( $# == 3 || $# == 4 ) && $1 == k3d-kodex && $2 =~ ^kodex-buildkit-[a-z0-9-]+$ && $3 =~ ^sha256:[a-f0-9]{64}$ ]] || {
  printf 'Image inventory diagnostic arguments are invalid\n' >&2
  exit 1
}
context=$1
pod=$2
digest=$3
runner=${4:-}
remote_runner=''
if [[ -n "$runner" ]]; then
  [[ "$runner" == /home/s/.local/state/kodex-dev/* && -f "$runner" && -x "$runner" && ! -L "$runner" ]] || exit 1
  remote_runner=$(kubectl --context "$context" -n kodex-system exec "$pod" -c buildkitd -- mktemp /tmp/inventory-probe-runner.XXXXXX)
  [[ "$remote_runner" =~ ^/tmp/inventory-probe-runner\.[A-Za-z0-9]+$ ]] || exit 1
  trap 'kubectl --context "$context" -n kodex-system exec "$pod" -c buildkitd -- rm -f "$remote_runner" >/dev/null' EXIT
  kubectl --context "$context" -n kodex-system cp "$runner" "$pod:$remote_runner" -c buildkitd
  expected=$(sha256sum "$runner" | cut -d ' ' -f 1)
  actual=$(kubectl --context "$context" -n kodex-system exec "$pod" -c buildkitd -- sha256sum "$remote_runner")
  [[ ${actual%% *} == "$expected" ]] || exit 1
fi
kubectl --context "$context" -n kodex-system exec -i "$pod" -c buildkitd -- /bin/sh -s -- "$digest" "$remote_runner" <<'PROBE'
set -eu
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export DOCKER_CONFIG=/var/run/secrets/kodex/buildkit/tls
printf '# syntax=kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/dockerfile@sha256:%s\nFROM kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner@%s AS rootfs\nFROM rootfs AS diagnostic\nUSER root\nCOPY probe.sh /tmp/probe.sh\nCOPY --chmod=0555 probe-runner /tmp/probe-runner\nRUN --network=none --mount=type=bind,from=rootfs,source=/,target=/image,readonly /bin/sh /tmp/probe.sh\nFROM scratch\nCOPY --from=diagnostic /tmp/result.txt /result.txt\n' \
  "$KODEX_BUILDKIT_READINESS_FRONTEND_SHA256" "$1" >"$work/Dockerfile"
if [ -n "$2" ]; then cp "$2" "$work/probe-runner"; else printf baseline >"$work/probe-runner"; fi
cat >"$work/probe.sh" <<'SCRIPT'
set -eu
umask 077
: >/tmp/result.txt
if [ -x /tmp/probe-runner ] && [ "$(head -c 4 /tmp/probe-runner | od -An -tx1 | tr -d ' \n')" = 7f454c46 ]; then
  code=0
  /tmp/probe-runner image-tool-inventory "$(printf '%064d' 0)" "$(printf '%064d' 0)" "$(printf '%064d' 0)" >/tmp/observer.out 2>&1 || code=$?
  printf 'nativeObserverExit=%s\n' "$code" >>/tmp/result.txt
  if [ "$code" = 0 ]; then
    node -e 'const x=require("/tmp/kodex-tool-inventory.json"); for(const t of x.tools) console.log(JSON.stringify({name:t.name,status:t.status,required:t.required,path:t.path,version:t.version,sha256:t.sha256}));' >>/tmp/result.txt
  fi
  code=0
  /bin/bash -c 'set -o pipefail; timeout -k 1 3 setpriv --reuid=10001 --regid=10001 --clear-groups /tmp/probe-runner image-tool-probe-exec /usr/local/bin/npm --version </dev/null 2>&1 | head -c 4096' >/tmp/npm-probe.out 2>&1 || code=$?
  denied=false; sandbox=false; uring=false; pipe=false; terminal=false
  grep -Eq 'EPERM|EACCES|Operation not permitted|Permission denied' /tmp/npm-probe.out && denied=true
  grep -Eq 'image tool probe sandbox rejected' /tmp/npm-probe.out && sandbox=true
  grep -Eqi 'io_uring|uv_loop_init' /tmp/npm-probe.out && uring=true
  grep -Eqi 'uv_pipe|uv__|pipe|fchown|fchmod' /tmp/npm-probe.out && pipe=true
  grep -Eqi 'ioctl|tty|terminal' /tmp/npm-probe.out && terminal=true
  printf 'npmDiagnostic exit=%s denied=%s sandbox=%s uring=%s pipe=%s terminal=%s bytes=%s\n' "$code" "$denied" "$sandbox" "$uring" "$pipe" "$terminal" "$(wc -c </tmp/npm-probe.out)" >>/tmp/result.txt
  exit 0
fi
if [ -c /image/dev/null ]; then printf 'nullDevice=true\n' >>/tmp/result.txt; else printf 'nullDevice=false\n' >>/tmp/result.txt; fi
if [ -d /image/proc/self ]; then printf 'procSelf=true\n' >>/tmp/result.txt; else printf 'procSelf=false\n' >>/tmp/result.txt; fi
for name in git go grpcurl chromium; do
  case "$name" in
    git) path=/usr/bin/git; argument=--version ;;
    go) path=/usr/local/go/bin/go; argument=version ;;
    grpcurl) path=/usr/local/bin/grpcurl; argument=-version ;;
    chromium) path=/usr/lib/chromium/chromium; argument=--version ;;
  esac
  code=0
  timeout -k 1 2 chroot --userspec=10001:10001 /image /usr/bin/env -i \
    PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin HOME=/nonexistent LANG=C LC_ALL=C \
    NO_COLOR=1 COREPACK_ENABLE_NETWORK=0 GOTOOLCHAIN=local GOROOT=/usr/local/go \
    "$path" "$argument" >/tmp/probe.out 2>&1 || code=$?
  version=false; dev=false; banner=false; goroot=false; proc=false; missing=false; permission=false; sandbox=false; fatal=false
  grep -Eq '[0-9]+\.[0-9]+' /tmp/probe.out && version=true
  grep -Eq '/dev/null' /tmp/probe.out && dev=true
  grep -Eq 'dev build|no version set|\(devel\)' /tmp/probe.out && banner=true
  grep -Eq 'GOROOT|go binary is trimmed|cannot find.*go' /tmp/probe.out && goroot=true
  grep -Eq '/proc|readlink|read.*executable' /tmp/probe.out && proc=true
  grep -Eq 'No such file|not found' /tmp/probe.out && missing=true
  grep -Eq 'Permission denied|Operation not permitted' /tmp/probe.out && permission=true
  grep -Eqi 'sandbox|namespace|zygote' /tmp/probe.out && sandbox=true
  grep -Eq 'Check failed|FATAL|Invalid file descriptor' /tmp/probe.out && fatal=true
  printf '%s exit=%s version=%s nullError=%s devBanner=%s gorootError=%s procError=%s missing=%s permission=%s sandbox=%s fatal=%s bytes=%s\n' \
    "$name" "$code" "$version" "$dev" "$banner" "$goroot" "$proc" "$missing" "$permission" "$sandbox" "$fatal" "$(wc -c </tmp/probe.out)" >>/tmp/result.txt
  printf '%s sha256=%s\n' "$name" "$(sha256sum "/image$path" | cut -d ' ' -f 1)" >>/tmp/result.txt
  rm -f /tmp/probe.out
done
/usr/local/go/bin/go version -m /image/usr/local/bin/grpcurl >/tmp/buildinfo.out 2>&1
if grep -Eq 'mod[[:space:]]+github.com/fullstorydev/grpcurl[[:space:]]+v1\.9\.3' /tmp/buildinfo.out; then
  printf 'grpcurlExactBuildInfo=true\n' >>/tmp/result.txt
else printf 'grpcurlExactBuildInfo=false\n' >>/tmp/result.txt; fi
rm -f /tmp/buildinfo.out
for name in gh kubectl helm; do
  /usr/local/go/bin/go version -m "/image/usr/local/bin/$name" >/tmp/buildinfo.out 2>&1 || {
    printf 'Go CLI build info diagnostic failed\n' >&2
    exit 1
  }
  compiler=$(awk 'NR == 1 && $2 ~ /^go[0-9]+\.[0-9]+\.[0-9]+$/ {print $2}' /tmp/buildinfo.out)
  case "$compiler" in go[0-9]*.[0-9]*.[0-9]*) ;; *) exit 1 ;; esac
  printf '%s compiler=%s\n' "$name" "$compiler" >>/tmp/result.txt
done
rm -f /tmp/buildinfo.out
SCRIPT
timeout -k 5 180 buildctl --addr tcp://127.0.0.1:1234 \
  --tlscacert /var/run/secrets/kodex/buildkit/tls/ca.pem \
  --tlscert /var/run/secrets/kodex/buildkit/tls/probe.crt \
  --tlskey /var/run/secrets/kodex/buildkit/tls/probe.key \
  --tlsservername kodex-buildkit.kodex-system.svc.cluster.local \
  build --frontend dockerfile.v0 --local context="$work" --local dockerfile="$work" \
  --opt filename=Dockerfile --opt platform=linux/amd64 --output "type=local,dest=$work/result" \
  >"$work/build.log" 2>&1 || { printf 'Image inventory diagnostic build failed\n' >&2; exit 1; }
cat "$work/result/result.txt"
PROBE
