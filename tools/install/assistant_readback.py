#!/usr/bin/env python3
"""Адресная проверка готовности лидера через закрытый loopback port-forward."""
import argparse
import json
import re
import signal
import subprocess
import tempfile
import time
import urllib.request
import uuid


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, stream, code, message, headers, new_url):
        return None


def exact_leader(command, pod, uid):
    def read(arguments):
        result = subprocess.run(command + arguments, capture_output=True, timeout=12)
        if result.returncode or len(result.stdout) > 1 << 20:
            raise ValueError('bounded Kubernetes readback required')
        return json.loads(result.stdout)
    current = read(['get', 'pod', pod, '-o', 'json'])
    lease = read(['get', 'lease', 'runtime-controller-leader', '-o', 'json'])
    metadata = current['metadata']
    return (metadata['name'] == pod and metadata['uid'] == uid
            and metadata['namespace'] == 'kodex-system' and not metadata.get('deletionTimestamp')
            and metadata.get('labels', {}).get('app.kubernetes.io/name') == 'runtime-controller'
            and lease['spec']['holderIdentity'] == uid
            and any(c.get('type') == 'Ready' and c.get('status') == 'True'
                    for c in current.get('status', {}).get('conditions', [])))


def check(context, pod, uid):
    if not context or not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?', pod):
        return False
    if str(uuid.UUID(uid)) != uid:
        return False
    command = ['kubectl', '--context', context, '--request-timeout=10s', '-n', 'kodex-system']
    if not exact_leader(command, pod, uid):
        return False
    with tempfile.TemporaryFile() as log:
        child = subprocess.Popen(command + ['port-forward', 'pod/' + pod, ':9090',
                                           '--address', '127.0.0.1'], stdout=log, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                if log.seek(0, 2) > 8192 or child.poll() is not None:
                    return False
                log.seek(0)
                match = re.search(rb'(?m)^Forwarding from 127\.0\.0\.1:([0-9]{1,5}) -> 9090\r?$', log.read(8193))
                if match:
                    port = int(match[1])
                    if not 1 <= port <= 65535:
                        return False
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
                    with opener.open('http://127.0.0.1:' + str(port) + '/assistant/readyz', timeout=5) as response:
                        if response.status != 204:
                            return False
                    return child.poll() is None and exact_leader(command, pod, uid)
                time.sleep(.1)
            return False
        finally:
            child.terminate()
            try:
                child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=3)


def main():
    parser = argparse.ArgumentParser(description='Check the exact assistant leader over a loopback Kubernetes port-forward')
    parser.add_argument('--context', required=True)
    parser.add_argument('--pod', required=True)
    parser.add_argument('--pod-uid', required=True)
    arguments = parser.parse_args()
    def interrupted(_signal, _frame):
        raise KeyboardInterrupt()
    previous = {s: signal.signal(s, interrupted) for s in (signal.SIGTERM, signal.SIGINT)}
    try:
        ready = check(arguments.context, arguments.pod, arguments.pod_uid)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, KeyboardInterrupt):
        ready = False
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)
    if not ready:
        raise SystemExit('System assistant exact leader readback failed')


if __name__ == '__main__':
    main()
