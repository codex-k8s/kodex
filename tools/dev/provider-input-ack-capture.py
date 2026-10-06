#!/usr/bin/env python3
"""Закрытый read-only capture раннего provider ACK без API session и raw input.

Источник: agent-runner/internal/codex/provider_input_proof.go и
runtime-controller/internal/workload/manager.go. Live запуск выполняет ROOT.
"""
import argparse
import hashlib
import json
import re
import subprocess
import sys
import time

CONTEXT = 'k3d-kodex'
KUBECONFIG = '/home/s/.kube/config'
NAMESPACE = 'kodex-runtime'
EVENT = 'PROVIDER_INPUT_ACKNOWLEDGED'
BINARY = '/usr/local/bin/kodex-agent-runner'
SELECTOR = 'runtime.kodex.dev/managed=true,runtime.kodex.dev/mode=turn'
PREFIX = 'runtime.kodex.dev/'
REF = r'[A-Za-z0-9_-]{8,128}'
HASH = r'[a-f0-9]{64}'
UUID = r'[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}'
IMAGE = r'[A-Za-z0-9./:_-]+@sha256:' + HASH


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def matches(pattern, value):
    return isinstance(value, str) and re.fullmatch(pattern, value) is not None


def sha(value):
    return hashlib.sha256(value.encode()).hexdigest()


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'DUPLICATE_JSON_KEY')
        result[key] = value
    return result


REF_FIELDS = ('run_ref', 'node_ref', 'session_ref', 'turn_ref', 'lease_ref',
              'agent_ref', 'organization_ref', 'project_ref', 'runtime_revision_ref',
              'runtime_config_ref', 'environment_ref', 'environment_binding_ref',
              'image_artifact_ref', 'image_recipe_ref', 'instruction_ref',
              'prompt_template_ref')
HASH_FIELDS = ('input_digest', 'runtime_revision_digest', 'runtime_config_digest',
               'environment_digest', 'environment_binding_digest', 'instruction_digest',
               'prompt_template_digest', 'prompt_materialization_digest', 'task_sha256',
               'provider_prompt_sha256', 'instructions_sha256', 'execution_binding_digest',
               'mcp_binding_digest')
NUMBER_FIELDS = ('attempt', 'lease_generation', 'runtime_revision_version',
                 'runtime_config_version', 'environment_version', 'environment_binding_version',
                 'image_recipe_generation', 'provider_prompt_bytes', 'instructions_bytes')


def project_ack(record, run_ref):
    """Никогда не возвращает log envelope, raw body, tool commands или grants."""
    if not isinstance(record, dict) or record.get('event') != EVENT:
        return None
    source = record.get('proof')
    require(isinstance(source, dict), 'ACK_SHAPE_INVALID')
    if source.get('run_ref') != run_ref:
        return None
    result = {}
    for field in REF_FIELDS:
        require(matches(REF, source.get(field)), 'ACK_REFERENCE_INVALID')
        result[field] = source[field]
    for field in HASH_FIELDS:
        require(matches(HASH, source.get(field)), 'ACK_DIGEST_INVALID')
        result[field] = source[field]
    for field in NUMBER_FIELDS:
        value = source.get(field)
        require(type(value) is int and 0 < value <= (1 << 53) - 1, 'ACK_NUMBER_INVALID')
        result[field] = value
    require(result['provider_prompt_bytes'] <= 1 << 20 and
            result['instructions_bytes'] <= 1 << 20, 'ACK_SIZE_INVALID')
    require(matches(IMAGE, source.get('image_reference')) and
            matches('sha256:' + HASH, source.get('image_manifest_digest')) and
            source['image_reference'].endswith('@' + source['image_manifest_digest']),
            'ACK_IMAGE_INVALID')
    result['image_reference'] = source['image_reference']
    result['image_manifest_digest'] = source['image_manifest_digest']
    for field in ('model', 'reasoning_effort', 'reasoning_mode'):
        require(matches(r'[A-Za-z0-9._-]{1,64}', source.get(field)), 'ACK_MODEL_INVALID')
        result[field] = source[field]
    require(type(source.get('task_in_prompt')) is bool, 'ACK_TASK_FLAG_INVALID')
    result['task_in_prompt'] = source['task_in_prompt']
    for prefix, expected in (('instructions_file', 'instructions_sha256'),
                             ('inbox_prompt', 'provider_prompt_sha256')):
        comparison = source.get(prefix + '_comparison')
        require(comparison in ('EQUAL', 'DIFFERENT', 'UNAVAILABLE'), 'ACK_COMPARISON_INVALID')
        digest = source.get(prefix + '_sha256', '')
        require((comparison == 'UNAVAILABLE' and digest == '') or matches(HASH, digest),
                'ACK_COMPARISON_DIGEST_INVALID')
        require(comparison != 'EQUAL' or digest == source[expected], 'ACK_EQUAL_DIGEST_MISMATCH')
        result[prefix + '_comparison'] = comparison
        if digest:
            result[prefix + '_sha256'] = digest
    for field in ('tools', 'grants', 'capabilities'):
        require(isinstance(source.get(field), list) and len(source[field]) <= 256, 'ACK_COLLECTION_INVALID')
        result[field + '_count'] = len(source[field])
    return result


def ack_from_logs(raw, run_ref):
    require(len(raw) <= 512 << 10, 'LOG_SIZE_LIMIT')
    found = []
    for line in raw.splitlines():
        # Неструктурированные и чужие события не декодируются и не выводятся.
        if len(line) > 65536 or not line.startswith(b'{') or EVENT.encode() not in line:
            continue
        try:
            record = json.loads(line, object_pairs_hook=unique)
        except (ValueError, UnicodeError):
            raise Failure('ACK_JSON_INVALID') from None
        proof = project_ack(record, run_ref)
        if proof is not None and proof not in found:
            found.append(proof)
    require(len(found) <= 1, 'MULTIPLE_ACK_TUPLES')
    return found[0] if found else None


POD_FIELDS = ['.metadata.namespace', '.metadata.name', '.metadata.uid', '.status.phase',
              '.metadata.labels.runtime\\.kodex\\.dev/managed',
              '.metadata.labels.runtime\\.kodex\\.dev/mode']
ANNOTATIONS = ('revision-digest', 'config-digest', 'environment-digest',
               'execution-binding-digest', 'mcp-binding-digest',
               'session-hash', 'turn-hash', 'attempt', 'lease-ref')
POD_FIELDS += ['.metadata.annotations.runtime\\.kodex\\.dev/' + key for key in ANNOTATIONS]
for container in ('provider-runtime', 'role-runtime'):
    POD_FIELDS += [f'.spec.containers[?(@.name=="{container}")].image',
                   f'.status.containerStatuses[?(@.name=="{container}")].imageID',
                   f'.status.containerStatuses[?(@.name=="{container}")].restartCount']
# kubectl получает только whitelisted columns, не spec.env, Secret refs или весь Pod.
POD_JSONPATH = '{range .items[*]}' + '{"|"}'.join('{' + path + '}' for path in POD_FIELDS) + '{"\\n"}{end}'


def pods_from_columns(raw):
    require(len(raw) <= 65536, 'POD_SIZE_LIMIT')
    rows = raw.decode('utf-8').splitlines()
    require(len(rows) <= 64, 'POD_COUNT_LIMIT')
    pods = []
    for row in rows:
        parts = row.split('|')
        require(len(parts) == len(POD_FIELDS), 'POD_COLUMNS_INVALID')
        ns, name, uid, phase, managed, mode = parts[:6]
        require(ns == NAMESPACE and matches(r'[a-z0-9-]{1,253}', name) and
                matches(UUID, uid) and phase in ('Pending', 'Running', 'Succeeded', 'Failed', 'Unknown') and
                managed == 'true' and mode == 'turn', 'POD_IDENTITY_INVALID')
        pods.append({'namespace': ns, 'name': name, 'uid': uid, 'phase': phase,
                     'annotations': dict(zip(ANNOTATIONS, parts[6:15])),
                     'containers': {name: {'image': parts[15 + index * 3],
                                          'image_id': parts[16 + index * 3],
                                          'restarts': parts[17 + index * 3]}
                                    for index, name in enumerate(('provider-runtime', 'role-runtime'))}})
    return pods


def bind_pod(pod, proof):
    annotations = pod['annotations']
    for annotation, field in (('revision-digest', 'runtime_revision_digest'),
                              ('config-digest', 'runtime_config_digest'),
                              ('environment-digest', 'environment_digest'),
                              ('execution-binding-digest', 'execution_binding_digest'),
                              ('mcp-binding-digest', 'mcp_binding_digest'),
                              ('lease-ref', 'lease_ref')):
        require(annotations[annotation] == proof[field], 'POD_ACK_PIN_MISMATCH')
    require(annotations['session-hash'] == sha(proof['session_ref'])[:16] and
            annotations['turn-hash'] == sha(proof['turn_ref'])[:16] and
            annotations['attempt'] == str(proof['attempt']), 'POD_ACK_TUPLE_MISMATCH')
    for container in pod['containers'].values():
        require(container['image'] == proof['image_reference'] and
                container['image_id'].endswith('@' + proof['image_manifest_digest']) and
                matches(IMAGE, container['image_id'].removeprefix('docker-pullable://')) and
                container['restarts'] == '0', 'POD_ACK_IMAGE_MISMATCH')
    return {key: pod[key] for key in ('namespace', 'name', 'uid', 'phase', 'containers')}


def kube(args):
    # Ошибки kubectl могут содержать приватные поля: stdout/stderr не пробрасываются.
    environment = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'KUBECONFIG': KUBECONFIG}
    try:
        result = subprocess.run(['kubectl', '--context', CONTEXT, '--request-timeout=5s', *args],
                                env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=8, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise Failure('KUBECTL_UNAVAILABLE') from None
    require(result.returncode == 0, 'KUBECTL_READ_FAILED')
    require(len(result.stdout) <= 512 << 10, 'KUBECTL_OUTPUT_LIMIT')
    return result.stdout


def list_pods(read):
    return pods_from_columns(read(['get', 'pods', '-n', NAMESPACE, '-l', SELECTOR,
                                  '--chunk-size=64', '-o', 'jsonpath=' + POD_JSONPATH]))


def capture(options, read=kube, now=time.monotonic, sleep=time.sleep, on_ack=None):
    deadline = now() + options.timeout_seconds
    while now() < deadline:
        selected = []
        for pod in list_pods(read):
            require(now() < deadline, 'CAPTURE_DEADLINE')
            if options.pod_name and pod['name'] != options.pod_name:
                continue
            if options.session_ref and pod['annotations']['session-hash'] != sha(options.session_ref)[:16]:
                continue
            if options.turn_ref and pod['annotations']['turn-hash'] != sha(options.turn_ref)[:16]:
                continue
            if options.attempt and pod['annotations']['attempt'] != str(options.attempt):
                continue
            try:
                raw = read(['logs', pod['name'], '-n', NAMESPACE, '-c', 'provider-runtime',
                            '--tail=256', '--limit-bytes=524288', '--timestamps=false'])
            except Failure:
                continue
            proof = ack_from_logs(raw, options.run_ref)
            if proof is not None:
                selected.append((pod, proof))
        require(len(selected) <= 1, 'MULTIPLE_ACK_PODS')
        if not selected:
            sleep(min(0.5, max(0, deadline - now())))
            continue
        pod, proof = selected[0]
        bound = bind_pod(pod, proof)
        for option, field in (('session_ref', 'session_ref'), ('turn_ref', 'turn_ref'),
                              ('attempt', 'attempt'), ('task_sha256', 'task_sha256'),
                              ('image_manifest', 'image_manifest_digest')):
            expected = getattr(options, option)
            require(expected is None or expected == proof[field], 'EXPECTED_ACK_PIN_MISMATCH')
        checks = {'task_expected_comparison': 'EQUAL' if options.task_sha256 else 'NOT_RUN',
                  'task_in_prompt': proof['task_in_prompt'],
                  'provider_inbox_comparison': proof['inbox_prompt_comparison'],
                  'instructions_file_comparison': proof['instructions_file_comparison']}
        # Ранний NDJSON checkpoint сохраняется до exec/cleanup; это ещё не rejoin proof.
        if on_ack is not None:
            on_ack({'version': 1, 'status': 'ACK_CAPTURED_POD_REJOIN_PENDING',
                    'event': EVENT, 'pod': bound, 'proof': proof, 'checks': checks})
        binary = {'status': 'NOT_RUN', 'scope': 'SAME_POD_IMAGE_FILE_NOT_SERVING_PROCESS'}
        try:
            raw = read(['exec', pod['name'], '-n', NAMESPACE, '-c', 'provider-runtime',
                        '--', 'sha256sum', BINARY])
            require(re.fullmatch(rb'[a-f0-9]{64}  /usr/local/bin/kodex-agent-runner\n?', raw),
                    'BINARY_HASH_INVALID')
            digest = raw[:64].decode('ascii')
            require(options.binary_sha256 is None or digest == options.binary_sha256, 'BINARY_PIN_MISMATCH')
            binary = {'status': 'CAPTURED', 'scope': binary['scope'], 'sha256': digest,
                      'expected_comparison': 'EQUAL' if options.binary_sha256 else 'NOT_RUN'}
        except Failure as error:
            if str(error) not in ('KUBECTL_READ_FAILED', 'KUBECTL_UNAVAILABLE'):
                raise
        # Повторное чтение same UID и полного закрытого tuple исключает замену Pod.
        current = [item for item in list_pods(read) if item['name'] == pod['name']]
        require(len(current) == 1 and
                {key: value for key, value in current[0].items() if key != 'phase'} ==
                {key: value for key, value in pod.items() if key != 'phase'}, 'POD_CHANGED_OR_CLEANED')
        bound['phase'] = current[0]['phase']
        return {'version': 1, 'status': 'CAPTURED', 'event': EVENT, 'pod': bound,
                'proof': proof, 'checks': checks, 'binary': binary}
    raise Failure('ACK_NOT_CAPTURED_BEFORE_DEADLINE')


class PrivateArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure('ARGUMENT_INVALID')


def main():
    parser = PrivateArgumentParser(description=__doc__)
    parser.add_argument('--run-ref', required=True)
    parser.add_argument('--pod-name')
    parser.add_argument('--session-ref')
    parser.add_argument('--turn-ref')
    parser.add_argument('--attempt', type=int)
    parser.add_argument('--task-sha256')
    parser.add_argument('--image-manifest')
    parser.add_argument('--binary-sha256')
    parser.add_argument('--timeout-seconds', type=int, default=30)
    options = parser.parse_args()
    require(matches(REF, options.run_ref), 'RUN_REFERENCE_INVALID')
    for field in ('session_ref', 'turn_ref'):
        require(getattr(options, field) is None or matches(REF, getattr(options, field)), 'REFERENCE_INVALID')
    require(options.pod_name is None or matches(r'[a-z0-9-]{1,253}', options.pod_name), 'POD_NAME_INVALID')
    require(options.attempt is None or options.attempt > 0, 'ATTEMPT_INVALID')
    for field in ('task_sha256', 'binary_sha256'):
        require(getattr(options, field) is None or matches(HASH, getattr(options, field)), 'EXPECTED_DIGEST_INVALID')
    require(options.image_manifest is None or matches('sha256:' + HASH, options.image_manifest), 'EXPECTED_IMAGE_INVALID')
    require(1 <= options.timeout_seconds <= 60, 'TIMEOUT_INVALID')
    emit = lambda value: print(json.dumps(value, sort_keys=True), flush=True)
    emit(capture(options, on_ack=emit))


if __name__ == '__main__':
    try:
        main()
    except (Failure, ValueError, UnicodeError, KeyError, TypeError):
        # Значения аргументов, исключений, raw logs и kubectl stderr не выводятся.
        print(json.dumps({'status': 'NOT_CAPTURED', 'code': 'PROVIDER_ACK_CAPTURE_FAILED'}), file=sys.stderr)
        sys.exit(1)
