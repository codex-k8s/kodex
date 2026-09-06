#!/usr/bin/env python3
"""Проверяет pinned app-server wire в disposable контейнере без сети."""

import base64
import json
import os
from pathlib import Path
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
        "--cpus", "2", "--memory", "1g", "--pids-limit", "128",
        "--user", "10001:10001", "--tmpfs", "/workspace:rw,size=256m,uid=10001,gid=10001,mode=0700",
        "--workdir", "/workspace", "--env", "HOME=/workspace", "--env", "CODEX_HOME=/workspace",
        "--entrypoint", "/usr/local/bin/codex",
    ]
    version = subprocess.run(common + [image, "--version"], capture_output=True, timeout=20, check=True)
    if version.stdout.strip() != b"codex-cli 0.153.4":
        raise RuntimeError("pinned Codex version mismatch")
    process = subprocess.Popen(common + ["--name", name, "-i", image, "app-server", "--strict-config", "--listen", "stdio://"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    buffer = b""
    deadline = time.monotonic() + 120
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
            if "error" in value:
                raise RuntimeError("Offline fixture rejected " + method)
            if value.get("id") != next_id or "result" not in value:
                raise RuntimeError("Codex response correlation failed for " + method)
            return value["result"]
        raise RuntimeError("Codex wire deadline exceeded")

    try:
        call("initialize", {"clientInfo": {"name": "kodex-catalog-wire", "version": "1"}, "capabilities": {"experimentalApi": True}})
        send({"method": "initialized"})
        models, cursor, seen = [], None, set()
        for _ in range(8):
            params = {"limit": 32, "includeHidden": True}
            if cursor is not None:
                params["cursor"] = cursor
            result = call("model/list", params)
            page = result.get("data")
            if not isinstance(page, list) or len(page) > 32:
                raise RuntimeError("Codex offline capabilities are invalid")
            models.extend(page)
            cursor = result.get("nextCursor")
            if cursor is None:
                break
            if not isinstance(cursor, str) or not cursor or cursor in seen:
                raise RuntimeError("Codex model pagination is invalid")
            seen.add(cursor)
        else:
            raise RuntimeError("Codex model pagination exceeded limit")
        for model in models:
            if model.get("id") != model.get("model") or not isinstance(model.get("supportedReasoningEfforts"), list) or not isinstance(model.get("defaultReasoningEffort"), str):
                raise RuntimeError("Codex capability wire changed")
        source_path = Path(__file__).resolve().parents[2] / "services/internal/secret-broker/internal/providercredential/api_model_capabilities.json"
        capabilities = json.loads(source_path.read_text())["models"]
        available = {model["model"] for model in models}
        if len(capabilities) != 7 or any(model["id"] not in available for model in capabilities):
            raise RuntimeError("Pinned runtime lacks a required API model")
        # Проверяется explicit selection, не значения Codex picker и не inference.
        # Никакого turn/start, credential или сетевого provider запроса.
        selections = 0
        for model in capabilities:
            for effort in model["reasoningEfforts"]:
                thread = call("thread/start", {
                    "model": model["id"], "cwd": "/workspace", "ephemeral": True,
                    "approvalPolicy": "never", "sandbox": "read-only",
                    "config": {"model_reasoning_effort": effort, "history.persistence": "none"},
                })
                if thread.get("model") != model["id"] or thread.get("reasoningEffort") != effort:
                    raise RuntimeError("Pinned runtime changed explicit API selection: " + model["id"] + "/" + effort)
                selections += 1
        # Только синтетический JWT; сеть контейнера физически отсутствует.
        def segment(value):
            return base64.urlsafe_b64encode(json.dumps(value).encode()).rstrip(b"=").decode()
        token = segment({"alg": "none"}) + "." + segment({"email": "fixture@example.invalid", "exp": int(time.time()) + 60, "https://api.openai.com/auth": {"chatgpt_account_id": "fixture-account", "chatgpt_plan_type": "plus"}}) + ".fixture"
        login = call("account/login/start", {"type": "chatgptAuthTokens", "accessToken": token, "chatgptAccountId": "fixture-account"})
        if login != {"type": "chatgptAuthTokens"}:
            raise RuntimeError("Codex external token login wire changed")
        print(f"Pinned Codex version, seven models, {selections} explicit API selections and external token wire passed without network; inference not run")
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
    except RuntimeError as error:
        # Только именованные локальные ошибки; provider payload не включается.
        raise SystemExit(str(error)) from None
    except Exception:
        raise SystemExit("Pinned Codex offline wire check failed") from None
