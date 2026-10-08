#!/usr/bin/env python3
"""Ограниченный local read-only capture закрытой диагностики provider-runtime.

Run связывается существующим exact provider ACK, а не locator из payload.
Сырые строки, stderr и provider input никогда не становятся результатом.
Источники enum: agent-runner/internal/codex/{broker,process,parser}.go.
"""
import argparse
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
                    'NOTIFICATION_INVALID REQUEST_REJECTED RESPONSE_CORRELATION MESSAGE_KIND RPC_ERROR'.split())
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
                           deadline=None, now=time.monotonic):
    pending = bytearray()
    dropping = False
    acknowledged = expected_ack is None

    def consume(line):
        nonlocal acknowledged
        if expected_ack is not None and ACK.EVENT.encode() in line:
            proof = ACK.ack_from_logs(line, run_ref, scope)
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
    deadline = now() + options.timeout_seconds
    pod, proof = None, None
    while now() < deadline:
        pod = selected_pod(read, options)
        if pod is not None:
            raw = read(['logs', pod['name'], '-n', ACK.NAMESPACE, '-c', 'provider-runtime',
                        '--tail=256', '--limit-bytes=524288', '--timestamps=false'])
            proof = ACK.ack_from_logs(raw, options.run_ref, options.assistant_scope)
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
                                                deadline=deadline, now=now)
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
        rejoined = ACK.ack_from_logs(raw, options.run_ref, options.assistant_scope)
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
    return {'status': 'CAPTURED', 'run_ref': options.run_ref, 'session_ref': options.session_ref,
            'turn_ref': options.turn_ref, 'attempt': options.attempt, 'assistant_scope': options.assistant_scope,
            'pod': {'name': pod['name'], 'uid': pod['uid'], 'namespace': ACK.NAMESPACE},
            'image_manifest_digest': options.image_manifest, 'rejoin': rejoin, 'diagnostic': diagnostic}


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
    parser.add_argument('--timeout-seconds', type=int, default=120)
    parser.add_argument('--pod-name')
    parser.add_argument('--pod-uid')
    try:
        options = parser.parse_args(argv)
        validate_options(options)
        with tempfile.TemporaryDirectory(prefix='provider-failure-', dir='/home/s/.cache') as private:
            os.chmod(private, 0o700)
            client = Kubectl(private, time.monotonic() + options.timeout_seconds)
            result = capture(options, client.read, client.follow)
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
