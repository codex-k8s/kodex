#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Integration cohort capture failed: %s\n' "$*" >&2; exit 1; }
repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
export GOTOOLCHAIN=local GOPROXY=off
bash "$repository_root/scripts/check-go-toolchain.sh"
directory=${KODEX_INTEGRATION_COHORT_CAPTURE_DIRECTORY:-}
requested_mode=${KODEX_INTEGRATION_COHORT_CAPTURE_MODE:-all}
[[ ${KODEX_INTEGRATION_COHORT_CAPTURE:-} == 1 ]] || fail 'explicit capture opt-in is required'
case "$requested_mode" in normal|backup|table-only-backup|all) ;; *) fail 'capture mode is invalid' ;; esac
[[ -n "$directory" ]] || fail 'an explicit new private output directory is required'
for key in KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE \
  KODEX_CONTROL_PLANE_TEST_DSN KODEX_CONTROL_PLANE_TEST_ADMIN_DSN KODEX_EMAIL_GATEWAY_TEST_BINARY; do
  [[ ! -v "$key" ]] || fail "external fixture input is forbidden: $key"
done
[[ -z $(git -C "$repository_root" status --porcelain --untracked-files=no) ]] || fail 'tracked source must be clean'
source_sha=$(git -C "$repository_root" rev-parse HEAD)
[[ "$source_sha" =~ ^[a-f0-9]{40}$ ]] || fail 'source commit is invalid'
python3 - "$directory" "$repository_root" <<'PY'
import os, pathlib, stat, sys
destination, repository = map(pathlib.Path, sys.argv[1:])
if not destination.is_absolute() or destination.parent.resolve(strict=True) != destination.parent or destination.is_relative_to(repository):
    raise SystemExit('Capture directory must be canonical, absolute and outside the repository')
parent = destination.parent.stat()
if parent.st_uid != os.geteuid() or parent.st_mode & 0o022:
    raise SystemExit('Capture parent must be owned and not group/world writable')
os.mkdir(destination, 0o700)
if stat.S_IMODE(destination.stat().st_mode) != 0o700:
    raise SystemExit('Capture directory permissions differ')
PY
export KODEX_INTEGRATION_COHORT_CAPTURE_DIRECTORY="$directory"
export KODEX_INTEGRATION_COHORT_CAPTURE_SOURCE_SHA="$source_sha"
export KODEX_TEST_POSTGRES_STORAGE=ram KODEX_TEST_POSTGRES_NETWORK=host
export KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 GOMAXPROCS=2 GOTOOLCHAIN=local GOPROXY=off
unset GOFLAGS

# Только cached RAM profile; никаких pull, disk volumes, live DSN либо внешнего mailbox.
resource_guard() {
  [[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
     "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
     "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'capture requires local Linux Docker'
  python3 - "$directory" "$1" <<'PY'
import json, os, pathlib, re, subprocess, sys, time
directory, phase = pathlib.Path(sys.argv[1]), sys.argv[2]
image = 'docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
# Между режимами Docker мог только что освободить предыдущую fixture.
# Ждём допустимого измерения ограниченное время с прежними порогами.
for attempt in range(15):
    filesystem = os.statvfs('/')
    available = filesystem.f_bavail * filesystem.f_frsize
    memory = int(next(line.split()[1] for line in pathlib.Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:')))
    if available >= 26250000000 and memory >= 12582912:
        break
    if attempt < 14:
        time.sleep(2)
else:
    raise SystemExit(f'Insufficient bounded capture disk/memory budget at {phase}: root bytes={available}, memory KiB={memory}')
metadata = json.loads(subprocess.check_output(['docker', 'image', 'inspect', '--format', '{"id":{{json .Id}},"digests":{{json .RepoDigests}}}', image], text=True))
if not re.fullmatch(r'sha256:[0-9a-f]{64}', metadata['id']) or not any(item.endswith('@'+image.split('@')[1]) for item in metadata['digests'] or []):
    raise SystemExit('Exact cached PostgreSQL identity is unavailable')
running = subprocess.check_output(['docker', 'ps', '-a', '--no-trunc', '--filter', 'name=kodex-control-plane-postgres-', '--format', '{{.ID}}'], text=True).strip()
if running:
    raise SystemExit('Another control-plane PostgreSQL fixture owns the slot')
path = directory / (phase+'.preflight.json')
fd = os.open(path, os.O_CREAT|os.O_EXCL|os.O_WRONLY, 0o600)
with os.fdopen(fd, 'w') as output:
    json.dump({'rootAvailableBytes':available,'memAvailableKiB':memory,'image':image,'cachedImageID':metadata['id'],'prefixEmpty':True,'storage':'ram','network':'host-loopback-ephemeral'}, output)
    output.flush()
    os.fsync(output.fileno())
PY
}

resource_guard prewarm
temporary=$(mktemp -d "${TMPDIR:-/tmp}/integration-cohort-capture.XXXXXXXX")
chmod 0700 "$temporary"
trap 'rm -rf -- "$temporary"' EXIT
run_private() {
  python3 - "$@" <<'PY'
import os, subprocess, sys
descriptor = os.open(sys.argv[1], os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
with os.fdopen(descriptor, 'wb') as output:
    result = subprocess.run(sys.argv[2:], stdout=output, stderr=subprocess.STDOUT, check=False)
    output.flush()
    os.fsync(output.fileno())
raise SystemExit(result.returncode if result.returncode >= 0 else 128-result.returncode)
PY
}
# Прогрев не является capture; native профиль всё равно выполняет свой current-source go test.
run_private "$directory/prewarm.log" timeout --kill-after=10s 300s env -u GOFLAGS GOENV=off GOWORK=off go -C "$repository_root/services/internal/control-plane" \
  test -c -p 1 -o "$temporary/cli.test" ./cmd/cli
modes=("$requested_mode")
if [[ "$requested_mode" == all ]]; then modes=(normal backup table-only-backup); fi
for mode in "${modes[@]}"; do
  [[ $(git -C "$repository_root" rev-parse HEAD) == "$source_sha" &&
     -z $(git -C "$repository_root" status --porcelain --untracked-files=no) ]] || fail 'source changed before capture'
  resource_guard "$mode"
  export KODEX_INTEGRATION_COHORT_CAPTURE_MODE="$mode"
  run_private "$directory/$mode.run.log" timeout --kill-after=10s 900s bash "$repository_root/scripts/tests/control-plane-postgres-test.sh" \
    --integration-cohort-schema-capture || fail 'native capture failed; private log retained'
  ids=$(docker ps -a --no-trunc --filter name=kodex-control-plane-postgres- --format '{{.ID}}') || fail 'capture cleanup readback failed'
  [[ -z "$ids" ]] || fail 'capture PostgreSQL cleanup is not confirmed'
  [[ -f "$directory/$mode.complete.json" && ! -L "$directory/$mode.complete.json" ]] || fail 'capture completion seal is missing'
done
[[ $(git -C "$repository_root" rev-parse HEAD) == "$source_sha" &&
   -z $(git -C "$repository_root" status --porcelain --untracked-files=no) ]] || fail 'source changed before final capture seal'
python3 - "$directory" "$source_sha" "${modes[@]}" <<'PY'
import hashlib, json, os, pathlib, stat, sys
directory, source, modes = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3:]
def read_owned(name):
    path = directory/name
    before = path.lstat()
    if not stat.S_ISREG(before.st_mode) or stat.S_IMODE(before.st_mode) != 0o600 or before.st_uid != os.geteuid() or before.st_nlink != 1:
        raise SystemExit('Capture artifact custody differs')
    descriptor = os.open(path, os.O_RDONLY|os.O_NOFOLLOW)
    with os.fdopen(descriptor,'rb') as value:
        after = os.fstat(value.fileno())
        if not os.path.samestat(before,after):
            raise SystemExit('Capture artifact identity changed')
        raw = value.read()
        if os.fstat(value.fileno()).st_size != len(raw) or len(raw) != before.st_size:
            raise SystemExit('Capture artifact size changed')
    return raw
names = ['prewarm.preflight.json', 'prewarm.log']
for mode in modes:
    marker = json.loads(read_owned(mode+'.complete.json'))
    if marker.get('sourceSHA') != source or marker.get('mode') != mode or marker.get('purpose') != 'SCHEMA_CAPTURE_ONLY' or marker.get('sealedGooseHead') != 20261114170000:
        raise SystemExit('Cohort mode completion seal differs')
    expected = [mode+'-'+stage+suffix for stage in ['046','047','048','049'] for suffix in ['.summary.json','.catalog.json','.seal.json']]
    if [item['file'] for item in marker['files']] != expected:
        raise SystemExit('Cohort mode artifact set differs')
    for item in marker['files']:
        raw = read_owned(item['file'])
        if hashlib.sha256(raw).hexdigest() != item['sha256'] or len(raw) != item['bytes']:
            raise SystemExit('Cohort mode artifact digest differs')
    names += [mode+'.preflight.json',mode+'.run.log',mode+'.complete.json']+expected
files = []
for name in names:
    raw = read_owned(name)
    files.append({'file':name,'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw)})
fd = os.open(directory/'capture.complete.json', os.O_CREAT|os.O_EXCL|os.O_WRONLY, 0o600)
with os.fdopen(fd,'w') as output:
    json.dump({'schema':'kodex.integration-cohort-capture.v1','purpose':'SCHEMA_CAPTURE_ONLY','sourceSHA':source,'modes':modes,'files':files},output,indent=2)
    output.flush()
    os.fsync(output.fileno())
descriptor = os.open(directory, os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
try:
    os.fsync(descriptor)
finally:
    os.close(descriptor)
PY
printf 'Integration cohort schema capture completed; artifacts remain private and are not release proofs\n'
