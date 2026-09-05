#!/usr/bin/env python3
"""Проверяет pinned app-server wire в disposable контейнере без сети."""

import base64
import json
import os
import selectors
import subprocess
import sys
import time
import uuid


def run(image):
    name = "kodex-catalog-wire-" + uuid.uuid4().hex[:16]
    common = [
        "docker", "run", "--rm", "--network", "none", "--read-only",
        "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
        "--user", "10001:10001", "--tmpfs", "/workspace:rw,uid=10001,gid=10001,mode=0700",
        "--workdir", "/workspace", "--env", "HOME=/workspace", "--env", "CODEX_HOME=/workspace",
        "--entrypoint", "/usr/local/bin/codex",
    ]
    version = subprocess.run(common + [image, "--version"], capture_output=True, timeout=20, check=True)
    if version.stdout.strip() != b"codex-cli 0.152.0":
        raise RuntimeError("pinned Codex version mismatch")
    process = subprocess.Popen(common + ["--name", name, "-i", image, "app-server", "--strict-config", "--listen", "stdio://"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    buffer = b""
    deadline = time.monotonic() + 30
    total = 0
    next_id = 0

    def send(value):
        process.stdin.write(json.dumps(value).encode() + b"\n")
        process.stdin.flush()

    def call(method, params):
        nonlocal buffer, total, next_id
        next_id += 1
        send({"id": next_id, "method": method, "params": params})
        while time.monotonic() < deadline:
            if b"\n" not in buffer:
                if not selector.select(max(0, deadline - time.monotonic())):
                    break
                chunk = os.read(process.stdout.fileno(), 65536)
                total += len(chunk)
                if not chunk or total > 4 * 1024 * 1024:
                    raise RuntimeError("Codex response stream bound failed")
                buffer += chunk
                continue
            line, buffer = buffer.split(b"\n", 1)
            value = json.loads(line)
            if "method" in value:
                if "id" in value:
                    send({"id": value["id"], "error": {"code": -32000, "message": "Server requests are not authorized"}})
                    raise RuntimeError("offline Codex requested authority")
                continue
            if value.get("id") != next_id or "error" in value or "result" not in value:
                raise RuntimeError("Codex response correlation failed")
            return value["result"]
        raise RuntimeError("Codex wire deadline exceeded")

    try:
        call("initialize", {"clientInfo": {"name": "kodex-catalog-wire", "version": "1"}, "capabilities": {"experimentalApi": True}})
        send({"method": "initialized"})
        result = call("model/list", {"limit": 32, "includeHidden": True})
        models = result.get("data")
        if not isinstance(models, list) or not models or len(models) > 32:
            raise RuntimeError("Codex offline capabilities are invalid")
        for model in models:
            if model.get("id") != model.get("model") or not isinstance(model.get("supportedReasoningEfforts"), list) or not isinstance(model.get("defaultReasoningEffort"), str):
                raise RuntimeError("Codex capability wire changed")
        # Только синтетический JWT; сеть контейнера физически отсутствует.
        def segment(value):
            return base64.urlsafe_b64encode(json.dumps(value).encode()).rstrip(b"=").decode()
        token = segment({"alg": "none"}) + "." + segment({"email": "fixture@example.invalid", "exp": int(time.time()) + 60, "https://api.openai.com/auth": {"chatgpt_account_id": "fixture-account", "chatgpt_plan_type": "plus"}}) + ".fixture"
        login = call("account/login/start", {"type": "chatgptAuthTokens", "accessToken": token, "chatgptAccountId": "fixture-account"})
        if login != {"type": "chatgptAuthTokens"}:
            raise RuntimeError("Codex external token login wire changed")
        print("Pinned Codex version, model/list and external token wire passed without network")
    finally:
        selector.close()
        process.stdin.close()
        subprocess.run(["docker", "rm", "--force", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15, check=False)
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
        process.stdout.close()


if __name__ == "__main__":
    if len(sys.argv) != 2 or not sys.argv[1] or sys.argv[1].startswith("-"):
        raise SystemExit("Usage: provider-model-catalog-codex-test.py IMAGE")
    try:
        run(sys.argv[1])
    except Exception:
        raise SystemExit("Pinned Codex offline wire check failed") from None
