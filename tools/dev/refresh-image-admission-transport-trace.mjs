#!/usr/bin/env node
import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { digest, runRefreshCLI, verifyConfigMapBoundary } from './refresh-image-admission-diagnostics.mjs';

// Dev-only наблюдаемость одной прежней attempt; production script не меняется.
export const BASELINE_SCRIPT_SHA256 = '3d61890702c0157e944823a7282bb662865fdd7c333de05daf84840138db5e55';
export const CURRENT_SCRIPT_SHA256 = 'c6dacf274188f78643d3efa710d46cb20684e77eb3820b83363e1c7d76d0aad7';
export const CANDIDATE_SCRIPT_SHA256 = '8fcdcfda8893f2f8e54ecf8f7d828eedec0d4ace0e26db1dc279adb59d50e298';
export const FAILURE_INVOCATION = '  IMAGE_OWNER_ADMISSION_FAILURE_CODE="$failure_code" image-admission-bridge fail || return 1\n';
const quote = value => "'" + value.replaceAll("'", "'\\''") + "'";

// Ни raw record, ни его часть не становятся output; память filter хранит лишь
// закрытые booleans. EOF join предшествует возврату исходного callback status.
export const TRANSPORT_FILTER = `
{ line=tolower($0); seen=1
  if (line ~ /no such host|name resolution|dns.*(fail|error)|resolver.*(fail|error)/) dns=1
  if (line ~ /connection refused/) refused=1
  if (line ~ /i.o timeout|connection timed out|context deadline exceeded/) timed=1
  if (line ~ /server preface|http2.*(error|frame)|http.2.*(error|frame)|error reading.*preface/) preface=1
}
END {
  kind="OTHER"
  if (preface) kind="PREFACE"
  if (timed) kind="TIMEOUT"
  if (refused) kind="REFUSED"
  if (dns) kind="DNS"
  if (seen) printf "{\\"event\\":\\"IMAGE_ADMISSION_TRANSPORT_DIAGNOSTIC\\",\\"class\\":\\"%s\\"}\\n",kind
}`;
export const TRACE_CHILD = `set +x
set +e
ulimit -c 0 || exit 1
exec {trace_fd}> >(awk ${quote(TRANSPORT_FILTER)} >&2 2>/dev/null)
filter_pid=$!
sleep 2 || exit 1
GRPC_GO_LOG_SEVERITY_LEVEL=info GRPC_GO_LOG_VERBOSITY_LEVEL=2 GRPC_GO_LOG_FORMATTER= \\
  image-admission-bridge fail >/dev/null 2>&"$trace_fd"
callback_status=$?
exec {trace_fd}>&-
wait "$filter_pid" 2>/dev/null || true
exit "$callback_status"`;
export const TRACE_INVOCATION = '  IMAGE_OWNER_ADMISSION_FAILURE_CODE="$failure_code" BASH_ENV=/dev/null ENV=/dev/null /bin/bash --noprofile --norc -c ' + quote(TRACE_CHILD) + ' || return 1\n';
// Единственный разрешённый live preimage — уже установленный streaming trace.
// Это exact переход диагностики, не поддержка нескольких форматов script.
export const CURRENT_TRACE_INVOCATION = '  IMAGE_OWNER_ADMISSION_FAILURE_CODE="$failure_code" BASH_ENV=/dev/null ENV=/dev/null /bin/bash --noprofile --norc -c ' + quote(TRACE_CHILD.replace('sleep 2 || exit 1\n', '')) + ' || return 1\n';

export function traceCandidate(baseline) {
  if (typeof baseline !== 'string' || digest(baseline) !== BASELINE_SCRIPT_SHA256 || baseline.split(FAILURE_INVOCATION).length !== 2) throw new Error('SCRIPT_BASELINE_MISMATCH');
  return baseline.replace(FAILURE_INVOCATION, TRACE_INVOCATION);
}

export function candidateConfigMap(current, candidate, options) {
  verifyConfigMapBoundary(current, options);
  const script = current.data['image-admission.sh'];
  if (digest(script) !== CURRENT_SCRIPT_SHA256 || script.split(CURRENT_TRACE_INVOCATION).length !== 2) throw new Error('SCRIPT_BASELINE_MISMATCH');
  const approved = script.replace(CURRENT_TRACE_INVOCATION, TRACE_INVOCATION);
  if (candidate !== approved || digest(candidate) !== CANDIDATE_SCRIPT_SHA256) throw new Error('SCRIPT_CHANGE_NOT_APPROVED');
  const result = structuredClone(current);
  result.data['image-admission.sh'] = candidate;
  return result;
}

if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { runRefreshCLI('refresh-image-admission-transport-trace.mjs', traceCandidate, candidateConfigMap); }
  catch (error) {
    const code = /^[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'TRACE_FAILED';
    process.stderr.write(`Image admission transport trace refresh failed: ${code}\n`);
    process.exitCode = 1;
  }
}
