// Read-only EXPLAIN без ANALYZE: запрос каталога не исполняется и не пишет строки.
import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const root = new URL('../../', import.meta.url);
const path = new URL('services/internal/control-plane/internal/repository/postgres/platform/sql/runtime_files_capture_entries.sql', root);
const source = fs.readFileSync(path, 'utf8');
if (!source.startsWith('-- name: runtime_files_capture_entries :exec\n')) throw Error('QUERY_SCOPE_CHANGED');
const query = source.replaceAll('@catalog_id', "'00000000-0000-0000-0000-000000000000'")
    .replaceAll('@inputs', "'[]'").replaceAll('@skills', "'[]'");
const raw = execFileSync('kubectl', ['--context', 'k3d-kodex', '--request-timeout=10s', '-n', 'kodex-system',
    'exec', '-i', 'kodex-postgresql-0', '--', 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', 'control_plane'], {
    input: 'EXPLAIN (FORMAT JSON, ANALYZE FALSE, VERBOSE FALSE) ' + query,
    env: {HOME:'/home/s', PATH:'/home/s/.local/state/kodex-dev/tools/bin:/usr/local/bin:/usr/bin:/bin', LANG:'C.UTF-8', KUBECONFIG:'/home/s/.kube/config'},
    encoding:'utf8', timeout:20000, maxBuffer:4<<20, stdio:['pipe','pipe','pipe'],
});
const [plan] = JSON.parse(raw);
console.log(JSON.stringify({mode:'PLAN_ONLY_ADMIN_EMPTY_INPUT', sourceSHA256:createHash('sha256').update(source).digest('hex'),
    totalCost:plan.Plan['Total Cost'], estimatedRows:plan.Plan['Plan Rows'], jitFunctions:plan.JIT?.Functions??0, jitOptions:plan.JIT?.Options??null}));
