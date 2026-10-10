#!/usr/bin/env python3
"""Ограниченный local read-only capture закрытой диагностики provider-runtime.

Run связывается exact provider ACK либо отдельным trusted workload receipt.
Сырые строки, stderr и provider input никогда не становятся результатом.
Источники enum: agent-runner/internal/codex/{broker,process,parser}.go.
"""
import argparse
import copy
from datetime import datetime, timezone
import importlib.util
import json
import os
from pathlib import Path
import re
import selectors
import signal
import subprocess
import sys
import tempfile
import time

_SPEC = importlib.util.spec_from_file_location(
    'provider_ack_metadata', Path(__file__).with_name('provider-input-ack-capture.py'))
ACK = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(ACK)
Failure, require = ACK.Failure, ACK.require
MAX_BYTES = 512 << 10
MAX_LINE = 4096
MAX_FOLLOW_REJOINS = 2
STAGES = frozenset('SELECTION CONTEXT BROKER_REQUEST AUTH_READ MCP_BINDING MCP_BRIDGE '
                   'HOME_PREPARE ACCOUNT_PIN ARCHIVE_RESTORE PROCESS_START INITIALIZE SKILLS '
                   'ACCOUNT_READ THREAD_CALL THREAD_BIND MCP_READINESS USAGE_BASELINE '
                   'TURN_PARAMETERS TURN_START TERMINAL_WAIT THREAD_READ PROCESS_STOP '
                   'TERMINAL_RESULT ARCHIVE_CAPTURE UNKNOWN'.split())
CLASSES = frozenset('AUTHENTICATION AUTHORITY MCP CONFIGURATION PROVIDER ACCOUNT_RESPONSE_SCHEMA'.split())
DETAILS = frozenset('NONE REQUEST_WRITE CONTEXT_CANCELLED STREAM_CLOSED STREAM_INVALID '
                    'NOTIFICATION_INVALID REQUEST_REJECTED RESPONSE_CORRELATION MESSAGE_KIND RPC_ERROR '
                    'RESUME_SOURCE_SCHEMA RESUME_SOURCE_ID RESUME_SOURCE_LOCATOR RESUME_SOURCE_OPEN '
                    'RESUME_SOURCE_METADATA RESUME_SOURCE_IDENTITY'.split())
ACCOUNT_READ = frozenset('NONE UNKNOWN REQUIREMENTS_LOAD MISSING_ACCOUNT_ID DISCOVERY_CANCELLED '
                         'ACCOUNT_CHANGED DISCOVERY_UNAUTHORIZED DISCOVERY_FAILED MISSING_WORKSPACE '
                         'DUPLICATE_WORKSPACE REQUIREMENTS_RELOAD CONFIGURATION_CHANGED SHUTDOWN '
                         'DISCOVERY_TIMEOUT MISSING_BACKEND_ORIGIN INVALID_ROUTING_OVERRIDE '
                         'BACKEND_IS_NOT_ORIGIN BACKEND_CONFLICT INVALID_BACKEND_URL INVALID_BACKEND_ORIGIN'.split())
TOKEN_USAGE_ERRORS = frozenset('TOKEN_USAGE_STRUCTURE TOKEN_USAGE_REQUIRED_MISSING TOKEN_USAGE_REQUIRED_NULL '
                              'TOKEN_USAGE_REQUIRED_TYPE TOKEN_USAGE_OPTIONAL_NULL TOKEN_USAGE_OPTIONAL_TYPE '
                              'TOKEN_USAGE_NEGATIVE TOKEN_USAGE_TOTAL_ARITHMETIC TOKEN_USAGE_CACHE_INPUT_BOUND '
                              'TOKEN_USAGE_REASONING_OUTPUT_BOUND TOKEN_USAGE_LAST_EXCEEDS_TOTAL '
                              'TOKEN_USAGE_RECEIPT_CONFLICT TOKEN_USAGE_RECEIPT_LIMIT TOKEN_USAGE_OVERFLOW'.split())
TOKEN_USAGE_METHODS = frozenset(('thread/tokenUsage/updated', 'rawResponse/completed'))
NOTIFICATION_ERRORS = frozenset('NONE UNKNOWN METHOD ENVELOPE TUPLE ITEM TIMESTAMP MESSAGE '
                                'TOKEN_USAGE TERMINAL LIFECYCLE MCP PROVIDER_ERROR'.split()) | TOKEN_USAGE_ERRORS
NOTIFICATIONS = frozenset('''NONE UNKNOWN error thread/started thread/status/changed thread/archived
thread/deleted thread/unarchived thread/closed skills/changed thread/name/updated thread/goal/updated
thread/goal/cleared thread/settings/updated thread/tokenUsage/updated turn/started hook/started
turn/completed hook/completed turn/diff/updated turn/plan/updated item/started
item/autoApprovalReview/started item/autoApprovalReview/completed item/completed item/agentMessage/delta
item/plan/delta command/exec/outputDelta process/outputDelta process/exited item/commandExecution/outputDelta
item/commandExecution/terminalInteraction item/fileChange/outputDelta item/fileChange/patchUpdated
serverRequest/resolved item/mcpToolCall/progress mcpServer/oauthLogin/completed rawResponseItem/completed
rawResponse/completed mcpServer/startupStatus/updated account/updated account/rateLimits/updated
app/list/updated remoteControl/status/changed externalAgentConfig/import/progress
externalAgentConfig/import/completed fs/changed item/reasoning/summaryTextDelta item/reasoning/summaryPartAdded
item/reasoning/textDelta thread/compacted model/rerouted model/verification turn/moderationMetadata
model/safetyBuffering/updated warning guardianWarning deprecationNotice configWarning
fuzzyFileSearch/sessionUpdated fuzzyFileSearch/sessionCompleted thread/realtime/started
thread/realtime/itemAdded thread/realtime/transcript/delta thread/realtime/transcript/done
thread/realtime/outputAudio/delta thread/realtime/sdp thread/realtime/error thread/realtime/closed
windows/worldWritableWarning windowsSandbox/setupCompleted account/login/completed
thread/reverted thread/attachment/updated thread/queue/changed project/changed thread/project/updated
thread/environment/connected thread/environment/disconnected autoApprovalReview/strictReviewRequired
mcpServer/event/stream/notification account/gatewayOAuth/changed modelProvider/authRecoveryStarted
modelProvider/authRecoveryCompleted thread/realtime/item/started thread/realtime/item/transcript/delta
thread/realtime/item/completed'''.split())
TERMINAL_CODES = frozenset('''provider_error_info_invalid server_overloaded usage_limit_exceeded unauthorized
cyber_policy context_window_exceeded session_budget_exceeded provider_internal_error provider_bad_request
thread_rollback_failed provider_sandbox_error provider_other_error active_turn_not_steerable
provider_transport_failure provider_interrupted RUNTIME_ARTIFACT_INVALID'''.split())
REQUEST_PREFIX = 'Codex provider request failed at safe stage: '
TERMINAL_PREFIX = 'Codex provider turn completed with safe failure code: '
STAMP = re.compile(r'^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} ')
REQUEST = re.compile(r'^' + re.escape(REQUEST_PREFIX) +
                     r'([^;]+); class: ([^;]+); detail: ([^;]+); rpc_code: (-?[0-9]{1,19}); '
                     r'notification: ([^;]+); account_read: ([^;]+); notification_error: ([^;]+)$')


def parse_line(raw):
    """Только две точные producer-строки; недоверенный текст не отражается."""
    try:
        line = raw.decode('utf-8')
    except UnicodeDecodeError:
        return None
    line = STAMP.sub('', line.rstrip('\n'), count=1)
    if line.startswith(TERMINAL_PREFIX):
        code = line[len(TERMINAL_PREFIX):]
        require(code in TERMINAL_CODES, 'PROVIDER_DIAGNOSTIC_INVALID')
        return {'kind': 'TERMINAL_FAILURE', 'failure_code': code}
    if not line.startswith(REQUEST_PREFIX):
        return None
    require(len(raw) <= MAX_LINE, 'PROVIDER_DIAGNOSTIC_INVALID')
    match = REQUEST.fullmatch(line)
    require(match is not None, 'PROVIDER_DIAGNOSTIC_INVALID')
    stage, category, detail, code, notification, account, notification_error = match.groups()
    code = int(code)
    require(stage in STAGES and category in CLASSES and detail in DETAILS and
            notification in NOTIFICATIONS and account in ACCOUNT_READ and
            notification_error in NOTIFICATION_ERRORS and -(1 << 63) <= code < (1 << 63),
            'PROVIDER_DIAGNOSTIC_INVALID')
    require(detail == 'RPC_ERROR' or code == 0, 'PROVIDER_DIAGNOSTIC_INVALID')
    require(not detail.startswith('RESUME_SOURCE_') or (stage == 'THREAD_READ' and category == 'PROVIDER'),
            'PROVIDER_DIAGNOSTIC_INVALID')
    require(detail == 'NOTIFICATION_INVALID' or
            (notification == 'NONE' and notification_error == 'NONE'), 'PROVIDER_DIAGNOSTIC_INVALID')
    require(detail != 'NOTIFICATION_INVALID' or
            (notification != 'NONE' and notification_error != 'NONE'), 'PROVIDER_DIAGNOSTIC_INVALID')
    require(notification_error not in TOKEN_USAGE_ERRORS or notification in TOKEN_USAGE_METHODS,
            'PROVIDER_DIAGNOSTIC_INVALID')
    require((stage == 'ACCOUNT_READ' and detail == 'RPC_ERROR' and code == -32603) or account == 'NONE',
            'PROVIDER_DIAGNOSTIC_INVALID')
    return {'kind': 'REQUEST_FAILURE', 'stage': stage, 'class': category, 'detail': detail,
            'rpc_code': code, 'notification': notification, 'account_read': account,
            'notification_error': notification_error}


def validate_options(options):
    for field in ('run_ref', 'session_ref', 'turn_ref'):
        require(ACK.matches(ACK.REF, getattr(options, field)), 'REFERENCE_INVALID')
    require(type(options.attempt) is int and 0 < options.attempt <= (1 << 53) - 1, 'ATTEMPT_INVALID')
    require(ACK.matches('sha256:' + ACK.HASH, options.image_manifest), 'IMAGE_DIGEST_INVALID')
    require(options.assistant_scope in ('NONE', 'PROJECT', 'SYSTEM'), 'ASSISTANT_SCOPE_INVALID')
    expected_project_ref = getattr(options, 'expected_project_ref', None)
    require(expected_project_ref is None or ACK.matches(ACK.REF, expected_project_ref),
            'EXPECTED_PROJECT_REFERENCE_INVALID')
    require(type(options.timeout_seconds) is int and 30 <= options.timeout_seconds <= 3600,
            'TIMEOUT_INVALID')
    require(options.pod_name is None or ACK.matches(r'[a-z0-9-]{1,253}', options.pod_name), 'POD_NAME_INVALID')
    require(options.pod_uid is None or ACK.matches(ACK.UUID, options.pod_uid), 'POD_UID_INVALID')


def selected_pod(read, options, pending_identity=False):
    candidates = [pod for pod in ACK.list_pods(read)
                  if pod['annotations']['session-hash'] == ACK.sha(options.session_ref)[:16]
                  and pod['annotations']['turn-hash'] == ACK.sha(options.turn_ref)[:16]
                  and pod['annotations']['attempt'] == str(options.attempt)
                  and (options.pod_name is None or pod['name'] == options.pod_name)]
    require(len(candidates) <= 1, 'MULTIPLE_MATCHING_PODS')
    if not candidates:
        return None
    pod = candidates[0]
    require(options.pod_uid is None or pod['uid'] == options.pod_uid, 'POD_UID_MISMATCH')
    for field in ACK.ANNOTATIONS[:5]:
        require(ACK.matches(ACK.HASH, pod['annotations'][field]), 'POD_PIN_INVALID')
    require(ACK.matches(ACK.REF, pod['annotations']['lease-ref']), 'POD_PIN_INVALID')
    pending = False
    for container in pod['containers'].values():
        require(ACK.matches(ACK.IMAGE, container['image']) and
                container['image'].endswith('@' + options.image_manifest), 'POD_IMAGE_MISMATCH')
        if not container['image_id'] and pod['phase'] == 'Pending':
            pending = True
            continue
        require(ACK.matches(ACK.IMAGE, container['image_id'].removeprefix('docker-pullable://')) and
                container['image_id'].endswith('@' + options.image_manifest) and
                container['restarts'] == '0', 'POD_IMAGE_MISMATCH')
    return None if pending and not pending_identity else pod


def validate_workload_options(options):
    validate_options(options)
    require(options.pod_name is not None and options.pod_uid is not None,
            'WORKLOAD_EXACT_POD_REQUIRED')
    require(ACK.matches(ACK.REF, options.node_ref), 'WORKLOAD_NODE_INVALID')
    require(options.assistant_scope == 'NONE' and
            ACK.matches(ACK.REF, options.expected_project_ref), 'WORKLOAD_SCOPE_INVALID')
    require(options.expected_input_digest is None or
            ACK.matches(ACK.HASH, options.expected_input_digest), 'WORKLOAD_INPUT_PIN_INVALID')


def workload_timestamp(value):
    require(isinstance(value, str) and
            ACK.matches(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z', value),
            'WORKLOAD_TIMESTAMP_INVALID')
    try:
        return datetime.fromisoformat(value.removesuffix('Z')).replace(tzinfo=timezone.utc)
    except ValueError:
        raise Failure('WORKLOAD_TIMESTAMP_INVALID') from None


def workload_projection(read, options):
    """Только server-created fresh turn; отсутствие ACK не становится ACK."""
    pod = selected_pod(read, options)
    if pod is None:
        return None
    extra_paths = ['.metadata.resourceVersion', '.metadata.creationTimestamp',
                   '.spec.restartPolicy',
                   '.spec.volumes[?(@.name=="runtime-input")].configMap.name']
    extra_keys = ('controller-pod-uid', 'organization-hash', 'project-hash')
    extra_paths += ['.metadata.annotations.runtime\\.kodex\\.dev/' + key for key in extra_keys]
    extra_paths += ['.metadata.labels.runtime\\.kodex\\.dev/execution-hash']
    path = '{"|"}'.join('{' + item + '}' for item in extra_paths)
    raw = read(['get', 'pod', pod['name'], '-n', ACK.NAMESPACE, '-o', 'jsonpath=' + path])
    require(len(raw) <= 2048, 'WORKLOAD_POD_METADATA_INVALID')
    parts = raw.decode('utf-8').split('|')
    require(len(parts) == 8 and ACK.matches(r'[1-9][0-9]{0,19}', parts[0]) and
            parts[2] == 'Never' and ACK.matches(ACK.UUID, parts[4]) and
            all(ACK.matches(r'[a-f0-9]{16}', value) for value in parts[5:]),
            'WORKLOAD_POD_METADATA_INVALID')
    lease = pod['annotations']['lease-ref']
    workload_timestamp(parts[1])
    short = ACK.sha(lease)[:16]
    require(pod['name'] == 'runtime-turn-' + short and parts[3] == 'runtime-projection-' + short and
            parts[7] == short and parts[6] == ACK.sha(options.expected_project_ref)[:16],
            'WORKLOAD_LEASE_BINDING_INVALID')
    fields = ['.metadata.namespace', '.metadata.name', '.metadata.uid',
              '.metadata.resourceVersion', '.immutable',
              '.metadata.labels.runtime\\.kodex\\.dev/managed',
              '.metadata.labels.runtime\\.kodex\\.dev/mode', '.metadata.creationTimestamp']
    keys = ACK.ANNOTATIONS[:-1] + extra_keys + ('pod-name',)
    fields += ['.metadata.annotations.runtime\\.kodex\\.dev/' + key for key in keys]
    path = '{"|"}'.join('{' + field + '}' for field in fields) + '{"\\n"}{.data.results\\.json}'
    raw = read(['get', 'configmap', parts[3], '-n', ACK.NAMESPACE, '-o', 'jsonpath=' + path])
    require(len(raw) <= 16384, 'WORKLOAD_PROJECTION_LIMIT')
    metadata, separator, data = raw.decode('utf-8').partition('\n')
    values = metadata.split('|')
    require(separator and len(values) == len(fields) and values[0] == ACK.NAMESPACE and
            values[1] == parts[3] and ACK.matches(ACK.UUID, values[2]) and
            ACK.matches(r'[1-9][0-9]{0,19}', values[3]) and values[4:7] == ['true', 'true', 'turn'],
            'WORKLOAD_PROJECTION_METADATA_INVALID')
    workload_timestamp(values[7])
    require(workload_timestamp(values[7]) <= workload_timestamp(parts[1]),
            'WORKLOAD_PROJECTION_CREATED_AFTER_POD')
    annotations = dict(zip(keys, values[8:]))
    require(all(annotations[key] == pod['annotations'][key] for key in ACK.ANNOTATIONS[:-1]) and
            annotations['controller-pod-uid'] == parts[4] and
            annotations['organization-hash'] == parts[5] and
            annotations['project-hash'] == parts[6] and annotations['pod-name'] == pod['name'],
            'WORKLOAD_PROJECTION_BINDING_INVALID')
    try:
        result = json.loads(data, object_pairs_hook=ACK.unique)
    except (ValueError, UnicodeError):
        raise Failure('WORKLOAD_PROJECTION_JSON_INVALID') from None
    require(isinstance(result, dict) and set(result) ==
            {'identity', 'root', 'maximum_writable_bytes', 'maximum_file_count'} and
            isinstance(result['root'], str) and 0 < len(result['root']) <= 4096 and
            '\x00' not in result['root'] and
            all(type(result[key]) is int and 0 < result[key] <= (1 << 53) - 1
                for key in ('maximum_writable_bytes', 'maximum_file_count')),
            'WORKLOAD_PROJECTION_SHAPE_INVALID')
    identity = result['identity']
    refs = ('organization_ref', 'project_ref', 'run_ref', 'node_ref', 'session_ref',
            'turn_ref', 'runtime_revision_ref')
    numbers = ('attempt', 'runtime_revision_version')
    hashes = ('runtime_revision_digest', 'input_digest')
    require(isinstance(identity, dict) and set(identity) == set(refs + numbers + hashes) and
            all(ACK.matches(ACK.REF, identity[key]) for key in refs) and
            all(type(identity[key]) is int and 0 < identity[key] <= (1 << 53) - 1 for key in numbers) and
            all(ACK.matches(ACK.HASH, identity[key]) for key in hashes),
            'WORKLOAD_IDENTITY_INVALID')
    require(all(identity[key] == getattr(options, key) for key in
                ('run_ref', 'node_ref', 'session_ref', 'turn_ref', 'attempt')) and
            identity['project_ref'] == options.expected_project_ref and
            identity['runtime_revision_digest'] == annotations['revision-digest'] and
            ACK.sha(identity['organization_ref'])[:16] == parts[5] and
            ACK.sha(identity['project_ref'])[:16] == parts[6] and
            (options.expected_input_digest is None or
             identity['input_digest'] == options.expected_input_digest),
            'WORKLOAD_IDENTITY_BINDING_INVALID')
    # Raw results root/limits не сохраняются даже в приватном receipt.
    return {'pod': {key: value for key, value in pod.items() if key != 'phase'},
            'pod_created_at': parts[1], 'controller_uid': parts[4],
            'projection': {'name': values[1], 'uid': values[2], 'resource_version': values[3],
                           'created_at': values[7]},
            'identity': identity}


def admission_expression(value):
    """Render whitespace не меняет CEL tokens или байты строковых литералов."""
    require(isinstance(value, str), 'WORKLOAD_ADMISSION_SHAPE_INVALID')
    segments = re.split(r'''('(?:\\.|[^'\\])*'|"(?:\\.|[^"\\])*")''', value)
    normalized = []
    for index, segment in enumerate(segments):
        if index % 2:
            normalized.append(segment)
        else:
            require(not any(marker in segment for marker in ("'", '"', '//', '/*', '*/')),
                    'WORKLOAD_ADMISSION_EXPRESSION_INVALID')
            normalized.append(re.sub(r'[ \t\r\n]+', ' ', segment))
    return ''.join(normalized).strip(' \t\r\n')


def admission_spec(spec, kind):
    """Только документированные API defaults; неизвестные поля остаются при сравнении."""
    require(isinstance(spec, dict), 'WORKLOAD_ADMISSION_SHAPE_INVALID')
    normalized = copy.deepcopy(spec)
    key = 'matchConstraints' if kind == 'ValidatingAdmissionPolicy' else 'matchResources'
    match = normalized.get(key)
    require(isinstance(match, dict), 'WORKLOAD_ADMISSION_SHAPE_INVALID')
    for field, default in (('matchPolicy', 'Equivalent'), ('namespaceSelector', {}),
                           ('objectSelector', {})):
        match.setdefault(field, default)
    for field in ('resourceRules', 'excludeResourceRules'):
        require(isinstance(match.get(field, []), list), 'WORKLOAD_ADMISSION_SHAPE_INVALID')
        for rule in match.get(field, []):
            require(isinstance(rule, dict), 'WORKLOAD_ADMISSION_SHAPE_INVALID')
            rule.setdefault('scope', '*')
    for field in ('validations', 'matchConditions', 'variables'):
        require(isinstance(normalized.get(field, []), list), 'WORKLOAD_ADMISSION_SHAPE_INVALID')
        for item in normalized.get(field, []):
            require(isinstance(item, dict) and 'expression' in item,
                    'WORKLOAD_ADMISSION_SHAPE_INVALID')
            item['expression'] = admission_expression(item['expression'])
    return normalized


def require_workload_admission(read, projection_created_at):
    """Доверенный API сверяет фактические Fail/Deny guards с repo-owned source."""
    try:
        import yaml
        source = Path(__file__).resolve().parents[2] / 'deploy/k8s/base/runtime-controller/runtime-materialization-admission.yaml'
        try:
            objects = list(yaml.safe_load_all(source.read_text()))
        except yaml.YAMLError:
            raise Failure('WORKLOAD_ADMISSION_SOURCE_INVALID') from None
    except (ImportError, OSError, ValueError):
        raise Failure('WORKLOAD_ADMISSION_SOURCE_UNAVAILABLE') from None
    names = (('ValidatingAdmissionPolicy', 'runtime-revision-exact-configmap-projection'),
             ('ValidatingAdmissionPolicyBinding', 'runtime-revision-exact-configmap-projection'))
    pins = []
    for kind, name in names:
        expected = [item for item in objects if item and item.get('kind') == kind and
                    item.get('metadata', {}).get('name') == name]
        require(len(expected) == 1, 'WORKLOAD_ADMISSION_SOURCE_INVALID')
        raw = read(['get', kind, name, '-o', 'json'])
        require(len(raw) <= MAX_BYTES, 'WORKLOAD_ADMISSION_LIMIT')
        try:
            actual = json.loads(raw, object_pairs_hook=ACK.unique)
        except (ValueError, UnicodeError):
            raise Failure('WORKLOAD_ADMISSION_JSON_INVALID') from None
        require(isinstance(actual, dict) and isinstance(actual.get('metadata'), dict),
                'WORKLOAD_ADMISSION_SHAPE_INVALID')
        metadata = actual['metadata']
        require(actual.get('kind') == kind and actual.get('apiVersion') == expected[0]['apiVersion'] and
                metadata.get('name') == name and
                ACK.matches(ACK.UUID, metadata.get('uid')) and
                ACK.matches(r'[1-9][0-9]{0,19}', metadata.get('resourceVersion')) and
                not metadata.get('deletionTimestamp') and
                admission_spec(actual.get('spec'), kind) == admission_spec(expected[0]['spec'], kind),
                'WORKLOAD_ADMISSION_BINDING_INVALID')
        require(type(metadata.get('generation')) is int and metadata['generation'] == 1 and
                workload_timestamp(metadata.get('creationTimestamp')) <=
                workload_timestamp(projection_created_at), 'WORKLOAD_ADMISSION_ORIGIN_UNPROVEN')
        if kind == 'ValidatingAdmissionPolicy':
            status = actual.get('status', {})
            require(isinstance(status, dict) and
                    isinstance(status.get('typeChecking', {}), dict) and
                    type(metadata.get('generation')) is int and metadata['generation'] > 0 and
                    status.get('observedGeneration') == metadata['generation'] and
                    type(status.get('observedGeneration')) is int and
                    not status.get('typeChecking', {}).get('expressionWarnings'),
                    'WORKLOAD_ADMISSION_NOT_READY')
        pins.append({'kind': kind, 'name': name, 'uid': metadata['uid'],
                     'resource_version': metadata['resourceVersion']})
    return pins


def capture_workload(options, read, follow, now=time.monotonic, sleep=time.sleep):
    validate_workload_options(options)
    deadline = now() + options.timeout_seconds
    bound = None
    while now() < deadline:
        bound = workload_projection(read, options)
        if bound is not None:
            break
        sleep(min(0.5, max(0, deadline - now())))
    require(bound is not None, 'WORKLOAD_POD_NOT_OBSERVED')
    admission = require_workload_admission(read, bound['projection']['created_at'])
    require(workload_projection(read, options) == bound, 'WORKLOAD_CHANGED_BEFORE_LOG_READ')
    raw = read(['logs', bound['pod']['name'], '-n', ACK.NAMESPACE, '-c', 'provider-runtime',
                '--tail=256', '--limit-bytes=524288', '--timestamps=false'])
    try:
        diagnostic = diagnostic_from_chunks(iter([raw]), deadline=deadline, now=now)
    except Failure as error:
        if str(error) != 'FOLLOW_STREAM_ENDED':
            raise
        require(workload_projection(read, options) == bound, 'WORKLOAD_CHANGED_BEFORE_FOLLOW')
        stream = follow(bound['pod'])
        try:
            diagnostic = diagnostic_from_chunks(stream, deadline=deadline, now=now)
        finally:
            stream.close()
    # Нет повторов effects и нет присоединения к новому Pod/attempt.
    current = workload_projection(read, options)
    require(current is not None, 'WORKLOAD_REJOIN_UNAVAILABLE')
    require(current == bound, 'WORKLOAD_CHANGED_AFTER_CAPTURE')
    require(require_workload_admission(read, bound['projection']['created_at']) == admission,
            'WORKLOAD_ADMISSION_CHANGED')
    return {'version': 1, 'status': 'WORKLOAD_DIAGNOSTIC_CAPTURED',
            'proof_kind': 'TRUSTED_KUBERNETES_WORKLOAD', 'provider_ack': 'NOT_OBSERVED',
            'provider_input_acceptance': 'UNKNOWN', 'execution_binding_recomputed': 'NOT_RUN',
            'identity': bound['identity'], 'pod': {key: bound['pod'][key] for key in
                                                ('name', 'uid', 'namespace')},
            'projection': bound['projection'], 'image_manifest_digest': options.image_manifest,
            'lease_ref': bound['pod']['annotations']['lease-ref'],
            'execution_binding_digest': bound['pod']['annotations']['execution-binding-digest'],
            'mcp_binding_digest': bound['pod']['annotations']['mcp-binding-digest'],
            'rejoin': 'VERIFIED', 'diagnostic': diagnostic}


def stop_process(process):
    """Join собственного kubectl до закрытия private cache и stdout pipe."""
    try:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                process.kill()
        process.wait(timeout=2)
    finally:
        if process.stdout is not None:
            process.stdout.close()


def chunks(process, deadline, maximum=MAX_BYTES, now=time.monotonic):
    received = 0
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ)
        while now() < deadline:
            events = selector.select(min(0.25, max(0, deadline - now())))
            if not events:
                continue
            data = os.read(process.stdout.fileno(), 4096)
            if not data:
                return 'EOF'
            received += len(data)
            require(received <= maximum, 'PROVIDER_LOG_LIMIT')
            yield data
    return 'DEADLINE'


def diagnostic_from_chunks(stream, expected_ack=None, run_ref=None, scope='NONE',
                           deadline=None, now=time.monotonic, expected_project_ref=None):
    pending = bytearray()
    dropping = False
    acknowledged = expected_ack is None

    def consume(line):
        nonlocal acknowledged
        if expected_ack is not None and ACK.EVENT.encode() in line:
            proof = ACK.ack_from_logs(line, run_ref, scope, expected_project_ref)
            if proof is not None:
                require(proof == expected_ack, 'FOLLOW_ACK_BINDING_MISMATCH')
                acknowledged = True
        result = parse_line(line)
        if result is not None:
            require(acknowledged, 'FOLLOW_ACK_NOT_OBSERVED')
        return result

    for data in stream:
        for segment in data.splitlines(keepends=True):
            if not dropping:
                pending.extend(segment)
                if len(pending) > MAX_LINE:
                    # ACK и другие большие строки не являются diagnostic output.
                    stripped = STAMP.sub('', bytes(pending[:256]).decode('ascii', errors='ignore'), count=1)
                    require(not stripped.startswith((REQUEST_PREFIX, TERMINAL_PREFIX)),
                            'PROVIDER_DIAGNOSTIC_INVALID')
                if len(pending) > 65536:
                    dropping = True
                    pending.clear()
            if segment.endswith(b'\n'):
                if not dropping:
                    result = consume(bytes(pending))
                    if result is not None:
                        return result
                dropping = False
                pending.clear()
    if pending:
        result = consume(bytes(pending))
        if result is not None:
            return result
    if deadline is not None and now() >= deadline:
        raise Failure('FAILURE_NOT_OBSERVED_BEFORE_DEADLINE')
    require(acknowledged, 'FOLLOW_ACK_NOT_OBSERVED')
    raise Failure('FOLLOW_STREAM_ENDED')


class Kubectl:
    def __init__(self, cache, deadline):
        self.cache, self.deadline = cache, deadline

    def start(self, args):
        require(time.monotonic() < self.deadline, 'CAPTURE_DEADLINE')
        timeout = 5 if '--follow' not in args else max(1, int(self.deadline - time.monotonic()) + 1)
        try:
            return subprocess.Popen(
                ['kubectl', '--kubeconfig=' + ACK.KUBECONFIG, '--context=' + ACK.CONTEXT,
                 '--cache-dir=' + self.cache, '--request-timeout=' + str(timeout) + 's', *args],
                env={'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/home/s',
                     'KUBECONFIG': ACK.KUBECONFIG}, stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        except OSError:
            raise Failure('KUBECTL_UNAVAILABLE') from None

    def read(self, args):
        process = self.start(args)
        try:
            output = b''.join(chunks(process, min(self.deadline, time.monotonic() + 8)))
            require(process.wait(timeout=0.5) == 0, 'KUBECTL_READ_FAILED')
            return output
        except subprocess.TimeoutExpired:
            raise Failure('KUBECTL_READ_TIMEOUT') from None
        finally:
            stop_process(process)

    def follow(self, pod):
        process = self.start(['logs', pod['name'], '-n', ACK.NAMESPACE, '-c', 'provider-runtime',
                              '--follow', '--tail=256', '--timestamps=false'])
        try:
            ended = yield from chunks(process, self.deadline)
            if ended == 'EOF':
                code = process.poll()
                remaining = self.deadline - time.monotonic()
                if code is None and remaining > 0:
                    try:
                        code = process.wait(timeout=min(0.5, remaining))
                    except subprocess.TimeoutExpired:
                        if time.monotonic() < self.deadline:
                            raise Failure('KUBECTL_FOLLOW_EXIT_TIMEOUT') from None
                require(code in (None, 0), 'KUBECTL_FOLLOW_FAILED')
        finally:
            stop_process(process)


def capture(options, read, follow, now=time.monotonic, sleep=time.sleep):
    validate_options(options)
    expected_project_ref = getattr(options, 'expected_project_ref', None)
    deadline = now() + options.timeout_seconds
    pod, proof = None, None
    while now() < deadline:
        pod = selected_pod(read, options)
        if pod is not None:
            raw = read(['logs', pod['name'], '-n', ACK.NAMESPACE, '-c', 'provider-runtime',
                        '--tail=256', '--limit-bytes=524288', '--timestamps=false'])
            proof = ACK.ack_from_logs(raw, options.run_ref, options.assistant_scope, expected_project_ref)
            if proof is not None:
                require(proof['session_ref'] == options.session_ref and proof['turn_ref'] == options.turn_ref and
                        proof['attempt'] == options.attempt and proof['image_manifest_digest'] == options.image_manifest,
                        'ACK_REQUEST_TUPLE_MISMATCH')
                ACK.bind_pod(pod, proof)
                break
        sleep(min(0.5, max(0, deadline - now())))
    require(proof is not None, 'EXACT_ACK_NOT_OBSERVED')
    current = selected_pod(read, options, pending_identity=True)
    require(current is not None and current['uid'] == pod['uid'], 'POD_CHANGED_BEFORE_FOLLOW')
    ACK.bind_pod(current, proof)
    rejoins = 0
    while True:
        require(now() < deadline, 'FAILURE_NOT_OBSERVED_BEFORE_DEADLINE')
        stream = follow(current)
        try:
            diagnostic = diagnostic_from_chunks(stream, expected_ack=proof,
                                                run_ref=options.run_ref, scope=options.assistant_scope,
                                                deadline=deadline, now=now, expected_project_ref=expected_project_ref)
            break
        except Failure as error:
            if str(error) != 'FOLLOW_STREAM_ENDED':
                raise
        finally:
            stream.close()
        # Нормальный EOF допускает только ограниченное переподключение к тому
        # же Running Pod. Ошибка transport никогда не повторяется и не скрывается.
        current = selected_pod(read, options, pending_identity=True)
        require(current is not None, 'FOLLOW_POD_CLEANED')
        require(current['uid'] == pod['uid'], 'POD_CHANGED_BEFORE_FOLLOW')
        ACK.bind_pod(current, proof)
        require(current['phase'] not in ('Succeeded', 'Failed'), 'FOLLOW_POD_TERMINAL')
        require(current['phase'] == 'Running', 'FOLLOW_POD_NOT_RUNNING')
        require(rejoins < MAX_FOLLOW_REJOINS, 'FOLLOW_STREAM_ENDED')
        raw = read(['logs', current['name'], '-n', ACK.NAMESPACE, '-c', 'provider-runtime',
                    '--tail=256', '--limit-bytes=524288', '--timestamps=false'])
        rejoined = ACK.ack_from_logs(raw, options.run_ref, options.assistant_scope, expected_project_ref)
        require(rejoined is not None, 'FOLLOW_ACK_NOT_OBSERVED')
        require(rejoined == proof, 'FOLLOW_ACK_BINDING_MISMATCH')
        rejoins += 1
        sleep(min(0.5, max(0, deadline - now())))
    try:
        current = selected_pod(read, options, pending_identity=True)
    except Failure as error:
        # Только недоступность transport не подменяет mismatch/коррупцию.
        if str(error) not in ('KUBECTL_READ_FAILED', 'KUBECTL_READ_TIMEOUT', 'KUBECTL_UNAVAILABLE'):
            raise
        rejoin = 'READ_UNAVAILABLE_AFTER_CAPTURE'
    else:
        if current is None:
            rejoin = 'POD_CLEANED_AFTER_CAPTURE'
        else:
            require(current['uid'] == pod['uid'], 'POD_CHANGED_AFTER_CAPTURE')
            ACK.bind_pod(current, proof)
            rejoin = 'VERIFIED'
    result = {'status': 'CAPTURED', 'run_ref': options.run_ref, 'session_ref': options.session_ref,
            'turn_ref': options.turn_ref, 'attempt': options.attempt, 'assistant_scope': options.assistant_scope,
            'pod': {'name': pod['name'], 'uid': pod['uid'], 'namespace': ACK.NAMESPACE},
            'image_manifest_digest': options.image_manifest, 'rejoin': rejoin, 'diagnostic': diagnostic}
    if expected_project_ref is not None:
        result['project_ref'] = proof['project_ref']
    return result


class PrivateParser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure('ARGUMENTS_INVALID')


def cancel_capture(signum, frame):
    raise Failure('CAPTURE_CANCELLED')


def main(argv=None):
    parser = PrivateParser(description='Bounded exact local provider failure capture')
    for field in ('run-ref', 'session-ref', 'turn-ref', 'image-manifest'):
        parser.add_argument('--' + field, required=True)
    parser.add_argument('--attempt', type=int, required=True)
    parser.add_argument('--assistant-scope', choices=('NONE', 'PROJECT', 'SYSTEM'), default='NONE')
    parser.add_argument('--expected-project-ref',
                        help='Exact project context pin; SYSTEM without this flag requires no project')
    parser.add_argument('--timeout-seconds', type=int, default=120)
    parser.add_argument('--pod-name')
    parser.add_argument('--pod-uid')
    parser.add_argument('--capture-mode', choices=('ACK', 'WORKLOAD'), default='ACK')
    parser.add_argument('--node-ref')
    parser.add_argument('--expected-input-digest')
    try:
        options = parser.parse_args(argv)
        validate_options(options)
        if options.capture_mode == 'WORKLOAD':
            validate_workload_options(options)
        with tempfile.TemporaryDirectory(prefix='provider-failure-', dir='/home/s/.cache') as private:
            os.chmod(private, 0o700)
            client = Kubectl(private, time.monotonic() + options.timeout_seconds)
            operation = capture_workload if options.capture_mode == 'WORKLOAD' else capture
            result = operation(options, client.read, client.follow)
        print(json.dumps(result, sort_keys=True))
        return 0
    except Failure as error:
        print(json.dumps({'status': 'NOT_CAPTURED', 'code': str(error)}, sort_keys=True))
        return 1
    except (OSError, subprocess.SubprocessError, UnicodeError, ValueError, KeyboardInterrupt):
        print(json.dumps({'status': 'NOT_CAPTURED', 'code': 'CAPTURE_IO_FAILED'}, sort_keys=True))
        return 1


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, cancel_capture)
    sys.exit(main())
