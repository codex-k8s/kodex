#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Any


class MattermostError(RuntimeError):
    pass


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default)


def require_env(name: str) -> str:
    value = env(name)
    if not value:
        raise MattermostError(f"не задан env: {name}")
    return value


def api_url(path: str) -> str:
    site_url = require_env("MATTERCODEX_MATTERMOST_SITE_URL").rstrip("/")
    return f"{site_url}/api/v4{path}"


def request_json(method: str, path: str, payload: dict[str, Any] | None = None) -> tuple[int, Any]:
    data = None
    headers = {
        "Authorization": f"Bearer {require_env('MATTERCODEX_MATTERMOST_BOT_TOKEN')}",
        "Accept": "application/json",
    }
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(api_url(path), data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            raw = response.read().decode("utf-8")
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")
        try:
            body = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            body = {"message": raw}
        return exc.code, body


def ensure_success(status: int, body: Any, action: str, allowed: tuple[int, ...] = (200, 201)) -> Any:
    if status not in allowed:
        message = body.get("message", body) if isinstance(body, dict) else body
        raise MattermostError(f"{action}: Mattermost API вернул HTTP {status}: {message}")
    return body


def get_or_create_team() -> dict[str, Any]:
    team_name = require_env("MATTERCODEX_DEFAULT_TEAM_NAME")
    status, body = request_json("GET", f"/teams/name/{urllib.parse.quote(team_name)}")
    if status == 200:
        print("team: exists")
        return body
    if status != 404:
        ensure_success(status, body, "получение team")

    payload = {
        "name": team_name,
        "display_name": env("MATTERCODEX_DEFAULT_TEAM_DISPLAY_NAME", team_name),
        "type": "O",
    }
    status, body = request_json("POST", "/teams", payload)
    team = ensure_success(status, body, "создание team", allowed=(201,))
    print("team: created")
    return team


def parse_channels() -> list[tuple[str, str]]:
    channels: list[tuple[str, str]] = []
    for item in env("MATTERCODEX_DEFAULT_CHANNELS").split(","):
        if not item.strip():
            continue
        name, _, display_name = item.partition(":")
        name = name.strip()
        display_name = display_name.strip() or name
        if name:
            channels.append((name, display_name))
    return channels


def ensure_channel(team_id: str, name: str, display_name: str) -> None:
    path = f"/teams/{team_id}/channels/name/{urllib.parse.quote(name)}"
    status, body = request_json("GET", path)
    if status == 200:
        print(f"channel {name}: exists")
        return
    if status != 404:
        ensure_success(status, body, f"получение channel {name}")

    payload = {
        "team_id": team_id,
        "name": name,
        "display_name": display_name,
        "type": "O",
    }
    status, body = request_json("POST", "/channels", payload)
    ensure_success(status, body, f"создание channel {name}", allowed=(201,))
    print(f"channel {name}: created")


def command_payload(team_id: str) -> dict[str, Any]:
    callback_base = env("MATTERCODEX_BOT_SERVICE_INTERNAL_URL") or require_env("MATTERCODEX_BOT_SERVICE_SITE_URL")
    return {
        "team_id": team_id,
        "trigger": "agents",
        "url": f"{callback_base.rstrip('/')}/mattermost/slash/agents",
        "method": "P",
        "display_name": "matter-codex agents",
        "description": "Matter Codex agent control command",
        "auto_complete": True,
        "auto_complete_desc": "Показать статус matter-codex",
        "auto_complete_hint": "status",
    }


def find_agents_command(team_id: str) -> dict[str, Any] | None:
    query = urllib.parse.urlencode({"team_id": team_id, "custom_only": "true"})
    status, body = request_json("GET", f"/commands?{query}")
    commands = ensure_success(status, body, "получение slash commands")
    for command in commands:
        if command.get("trigger") == "agents":
            return command
    return None


def ensure_agents_command(team_id: str) -> str:
    payload = command_payload(team_id)
    current = find_agents_command(team_id)
    if current is None:
        status, body = request_json("POST", "/commands", payload)
        command = ensure_success(status, body, "создание slash command", allowed=(201,))
        print("slash command /agents: created")
    else:
        command_id = current["id"]
        merged = dict(current)
        merged.update(payload)
        status, body = request_json("PUT", f"/commands/{command_id}", merged)
        command = ensure_success(status, body, "обновление slash command")
        print("slash command /agents: updated")

    token = command.get("token", "")
    if token:
        return token

    status, body = request_json("PUT", f"/commands/{command['id']}/regen_token")
    token_body = ensure_success(status, body, "генерация slash command token")
    token = token_body.get("token", "")
    if not token:
        raise MattermostError("Mattermost API не вернул slash command token")
    print("slash command /agents token: generated")
    return token


def write_token(token: str) -> None:
    output_path = env("MATTERCODEX_SLASH_TOKEN_OUTPUT")
    if not output_path:
        return
    with open(output_path, "w", encoding="utf-8") as output:
        output.write(token)


def main() -> int:
    team = get_or_create_team()
    team_id = team["id"]
    for name, display_name in parse_channels():
        ensure_channel(team_id, name, display_name)
    token = ensure_agents_command(team_id)
    write_token(token)
    print("Mattermost control surface: ready")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except MattermostError as exc:
        print(f"[matter-codex] ОШИБКА: {exc}", file=sys.stderr)
        raise SystemExit(1)
