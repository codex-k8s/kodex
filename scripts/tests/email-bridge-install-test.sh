#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary=$(mktemp -d)
name="kodex-email-install-${BASHPID}"
network="$name"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf -- "$temporary"
}
trap cleanup EXIT
fail() { printf 'Email bridge installation test failed: %s\n' "$1" >&2; exit 1; }

printf 'Generating isolated installation material\n'
if ! bash "$root/tools/install/generate-material.sh" --output-directory "$temporary/material" \
  --release-registry-host release.example.test --promoted-pull-host pull.example.test \
  >"$temporary/generation.log" 2>&1; then
  fail 'material generation'
fi
mkdir -p "$temporary/bootstrap" "$temporary/init" "$temporary/credentials" \
  "$temporary/tls" "$temporary/runtime" "$temporary/migration" "$temporary/bin"
node --input-type=module - "$root" "$temporary" <<'JS'
import { readFileSync, writeFileSync, copyFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
const [root, dir] = process.argv.slice(2);
const require = (condition, message) => { if (!condition) throw new Error(message); };
const projection = (secret, key) => join(dir, 'material/projections', secret, key);
const passwords = ['admin', 'runtime', 'migration'].map(role => {
  const path = projection('email-bridge-postgresql-bootstrap', `${role}-password`);
  const value = readFileSync(path, 'utf8');
  require(/^[a-f0-9]{64}$/.test(value) && (statSync(path).mode & 0o777) === 0o600, 'Invalid bootstrap material');
  copyFileSync(path, join(dir, 'credentials', `${role}-password`));
  return value;
});
require(new Set(passwords).size === 3, 'Database passwords are not independent');
require(!readdirSync(join(dir, 'material/postgresql/roles')).some(role => role.startsWith('email_bridge_')), 'Email roles leaked into the platform database');
for (const [index, role, user] of [[1, 'runtime', 'email_bridge_runtime'], [2, 'migration', 'email_bridge_migrator']]) {
  const source = projection(`email-bridge-${role}-database`, 'dsn');
  const uri = new URL(readFileSync(source, 'utf8'));
  require(uri.protocol === 'postgresql:' && uri.username === user && uri.password === passwords[index], 'Invalid database identity');
  require(uri.hostname === 'email-bridge-postgresql.kodex-system.svc.cluster.local' && uri.port === '5432' && uri.pathname === '/email_bridge', 'Invalid database destination');
  require(uri.searchParams.get('sslmode') === 'verify-full' && uri.searchParams.get('sslrootcert') === '/var/run/email/tls/ca.crt', 'Invalid database TLS');
  copyFileSync(source, join(dir, role, 'dsn'));
  if (role === 'runtime') {
    for (const mutation of ['valid', 'plaintext', 'wrong-host', 'wrong-password', 'wrong-ca']) {
      const variant = new URL(uri);
      if (mutation === 'plaintext') variant.searchParams.set('sslmode', 'disable');
      if (mutation === 'wrong-host') variant.hostname = 'localhost';
      if (mutation === 'wrong-password') variant.password = 'invalid';
      if (mutation === 'wrong-ca') variant.searchParams.set('sslrootcert', '/var/run/email/tls/untrusted.crt');
      const parameters = {host: variant.hostname, port: variant.port, dbname: variant.pathname.slice(1),
        user: variant.username, password: variant.password,
        sslmode: variant.searchParams.get('sslmode'), sslrootcert: variant.searchParams.get('sslrootcert')};
      writeFileSync(join(dir, role, mutation), '[probe]\n' + Object.entries(parameters).map(([key, value]) => `${key}=${value}\n`).join(''));
    }
  }
}
const source = JSON.parse(readFileSync(join(root, 'deploy/k8s/base/email-bridge-data/bootstrap.yaml'))).data;
for (const key of ['bootstrap.sql', 'pg_hba.conf']) writeFileSync(join(dir, 'bootstrap', key), source[key]);
writeFileSync(join(dir, 'init/bootstrap.sh'), source['bootstrap.sh']);
const installer = readFileSync(join(root, 'tools/install/deploy-platform.sh'), 'utf8');
const wait = installer.indexOf('  wait_statefulset email-bridge-postgresql');
const migrate = installer.indexOf('  apply_job email-bridge-migration');
const workloads = installer.indexOf('apply_render workloads-before-role-image-builder');
require(wait > 0 && migrate > wait && workloads > migrate, 'Email startup order is not enforced');
require(installer.slice(installer.indexOf('for job_name in')).includes('email-bridge-migration'), 'Email migration readback is absent');
JS

ca="$temporary/material/authorities/pki"
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout "$temporary/tls/tls.key" -out "$temporary/server.csr" \
  -subj '/CN=email-bridge-postgresql.kodex-system.svc.cluster.local' >/dev/null 2>&1
printf '%s\n' 'basicConstraints=critical,CA:FALSE' 'extendedKeyUsage=serverAuth' \
  'subjectAltName=DNS:email-bridge-postgresql.kodex-system.svc.cluster.local' >"$temporary/server.ext"
openssl x509 -req -in "$temporary/server.csr" -CA "$ca/ca.crt" -CAkey "$ca/ca.key" \
  -CAcreateserial -days 1 -extfile "$temporary/server.ext" -out "$temporary/tls/tls.crt" >/dev/null 2>&1
install -m 0444 "$ca/ca.crt" "$temporary/tls/ca.crt"
install -m 0444 "$temporary/material/authorities/pki-public/ca.crt" "$temporary/tls/untrusted.crt"
image=$(jq -er '.spec.template.spec.containers[0].image' "$root/deploy/k8s/base/email-bridge-data/statefulset.yaml")
chmod 0755 "$temporary/bootstrap" "$temporary/init" "$temporary/credentials" \
  "$temporary/tls" "$temporary/runtime" "$temporary/migration" "$temporary/bin"
chmod 0444 "$temporary/bootstrap/"*
chmod 0555 "$temporary/init/bootstrap.sh"
# Оснастка воспроизводит root-owned Secret с fsGroup, не добавляя init в production.
docker run --rm --network none --user 0 --mount "type=bind,src=$temporary,dst=/test" \
  --entrypoint /bin/sh "$image" -ec '
    chown 0:70 /test/credentials/* /test/tls/tls.key
    chmod 0440 /test/credentials/* /test/tls/tls.key
    chown 0:10001 /test/runtime/* /test/migration/dsn
    chmod 0440 /test/runtime/* /test/migration/dsn
    chmod 0444 /test/tls/tls.crt
  '
docker network create --internal "$network" >/dev/null
mapfile -t postgres_args < <(jq -er '.spec.template.spec.containers[0].args[]' "$root/deploy/k8s/base/email-bridge-data/statefulset.yaml")
docker run --rm -d --name "$name" --network "$network" \
  --network-alias email-bridge-postgresql.kodex-system.svc.cluster.local \
  --user 70:70 --cap-drop ALL --security-opt no-new-privileges --read-only \
  --tmpfs /var/lib/postgresql/data:uid=70,gid=70,mode=0700 \
  --tmpfs /var/run/postgresql:uid=70,gid=70,mode=0755 --tmpfs /tmp \
  --mount "type=bind,src=$temporary/bootstrap,dst=/bootstrap,readonly" \
  --mount "type=bind,src=$temporary/init,dst=/docker-entrypoint-initdb.d,readonly" \
  --mount "type=bind,src=$temporary/credentials,dst=/var/run/email-bootstrap,readonly" \
  --mount "type=bind,src=$temporary/tls,dst=/var/run/email-tls,readonly" \
  -e POSTGRES_PASSWORD_FILE=/var/run/email-bootstrap/admin-password \
  -e PGDATA=/var/lib/postgresql/data/pgdata "$image" "${postgres_args[@]}" >/dev/null
for _ in $(seq 1 45); do
  if docker exec "$name" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then break; fi
  [[ "$(docker inspect --format '{{.State.Running}}' "$name" 2>/dev/null)" == true ]] || fail 'database bootstrap'
  sleep 1
done
docker exec "$name" pg_isready -h 127.0.0.1 -U postgres >/dev/null || fail 'database readiness'
printf 'Bootstrap completed; checking TLS and database privileges\n'
(cd "$root/services/internal/email-bridge" && \
  env -u GOFLAGS GOENV=off GOWORK=off CGO_ENABLED=0 go build -o "$temporary/bin/migrate" ./cmd/cli)
chmod 0555 "$temporary/bin/migrate"
for action in up status up; do
  docker run --rm --network "$network" --user 10001:10001 --cap-drop ALL --read-only \
    --mount "type=bind,src=$temporary/bin,dst=/test,readonly" \
    --mount "type=bind,src=$temporary/migration,dst=/var/run/email/database,readonly" \
    --mount "type=bind,src=$temporary/tls,dst=/var/run/email/tls,readonly" \
    -e EMAIL_BRIDGE_MIGRATION_DSN_FILE=/var/run/email/database/dsn \
    --entrypoint /test/migrate "$image" "$action" >"$temporary/migration.log" 2>&1 || fail "migration $action"
done
sql() {
  local mutation=$1 query=$2
  docker run --rm --network "$network" --user 10001:10001 --cap-drop ALL --read-only \
    --mount "type=bind,src=$temporary/runtime,dst=/var/run/email/database,readonly" \
    --mount "type=bind,src=$temporary/tls,dst=/var/run/email/tls,readonly" \
    -e PGCONNECT_TIMEOUT=3 --entrypoint /bin/sh "$image" -ec '
      export PGSERVICEFILE="/var/run/email/database/$1" PGSERVICE=probe PGHOSTADDR="$3"
      exec psql -XAtv ON_ERROR_STOP=1 -c "$2"
    ' test "$mutation" "$query" "$address" 2>"$temporary/client.log"
}
address=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$name")
[[ "$(sql valid 'SELECT current_user')" == email_bridge_runtime ]] || fail 'runtime TLS authentication'
[[ "$(sql valid 'SELECT count(*) FROM email_bridge.receipts')" == 0 ]] || fail 'runtime schema access'
for query in 'CREATE TABLE public.forbidden(id int)' 'SET ROLE email_bridge_migrator' \
  'SELECT * FROM public.goose_db_version' 'DELETE FROM email_bridge.receipts'; do
  if sql valid "$query" >/dev/null; then fail 'runtime privilege escalation'; fi
done
for mutation in plaintext wrong-host wrong-password wrong-ca; do
  if sql "$mutation" 'SELECT 1' >/dev/null; then fail "accepted $mutation"; fi
done
printf 'Email bridge installation passed: isolated secrets, TLS bootstrap, migrations, runtime grants and negative paths\n'
