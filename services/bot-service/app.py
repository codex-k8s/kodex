#!/usr/bin/env python3

from __future__ import annotations

import hmac
import json
import os
import sys
from dataclasses import dataclass
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Mapping
from urllib.parse import parse_qs


SERVICE_NAME = "matter-codex-bot-service"
SERVICE_VERSION = "0.1.0"


@dataclass(frozen=True)
class BotConfig:
    host: str
    port: int
    mattermost_site_url: str
    bot_service_site_url: str
    default_team_name: str
    default_channels: tuple[str, ...]
    bot_token_configured: bool
    slash_token: str

    @property
    def slash_token_configured(self) -> bool:
        return bool(self.slash_token)


def _split_channels(value: str) -> tuple[str, ...]:
    channels: list[str] = []
    for item in value.split(","):
        name = item.split(":", 1)[0].strip()
        if name:
            channels.append(name)
    return tuple(channels)


def load_config(env: Mapping[str, str] | None = None) -> BotConfig:
    source = os.environ if env is None else env
    port_raw = source.get("MATTERCODEX_BOT_SERVICE_PORT", "8080")
    try:
        port = int(port_raw)
    except ValueError as exc:
        raise ValueError("MATTERCODEX_BOT_SERVICE_PORT должен быть числом") from exc

    mattermost_site_url = source.get("MATTERCODEX_MATTERMOST_SITE_URL") or source.get("PUBLIC_BASE_URL", "")
    bot_service_site_url = source.get("MATTERCODEX_BOT_SERVICE_SITE_URL", "")
    default_channels = _split_channels(
        source.get(
            "MATTERCODEX_DEFAULT_CHANNELS",
            "agents-control:Agents Control,agents-runs:Agents Runs,agent-alerts:Agent Alerts,agents-audit:Agents Audit",
        )
    )

    return BotConfig(
        host=source.get("MATTERCODEX_BOT_SERVICE_BIND", "0.0.0.0"),
        port=port,
        mattermost_site_url=mattermost_site_url,
        bot_service_site_url=bot_service_site_url,
        default_team_name=source.get("MATTERCODEX_DEFAULT_TEAM_NAME", "agents"),
        default_channels=default_channels,
        bot_token_configured=bool(source.get("MATTERCODEX_MATTERMOST_BOT_TOKEN")),
        slash_token=source.get("MATTERCODEX_MATTERMOST_SLASH_TOKEN", ""),
    )


def health_payload(config: BotConfig) -> dict[str, object]:
    return {
        "status": "ok",
        "service": SERVICE_NAME,
        "version": SERVICE_VERSION,
        "mattermost_configured": bool(config.mattermost_site_url),
        "bot_token_configured": config.bot_token_configured,
        "slash_token_configured": config.slash_token_configured,
        "default_team": config.default_team_name,
        "default_channels": list(config.default_channels),
    }


def agents_status_text(config: BotConfig) -> str:
    token_status = "configured" if config.bot_token_configured else "missing"
    slash_status = "configured" if config.slash_token_configured else "missing"
    channels = ", ".join(config.default_channels) if config.default_channels else "none"
    return (
        "matter-codex: online\n"
        f"service: {SERVICE_NAME} {SERVICE_VERSION}\n"
        f"mattermost: {'configured' if config.mattermost_site_url else 'missing'}\n"
        f"bot token: {token_status}\n"
        f"slash token: {slash_status}\n"
        f"default team: {config.default_team_name}\n"
        f"default channels: {channels}"
    )


def slash_response(config: BotConfig, form: Mapping[str, str]) -> tuple[int, dict[str, object]]:
    token = form.get("token", "")
    if not config.slash_token_configured:
        return (
            HTTPStatus.SERVICE_UNAVAILABLE,
            {
                "response_type": "ephemeral",
                "text": "matter-codex bot-service запущен, но slash token еще не настроен.",
            },
        )
    if not hmac.compare_digest(token, config.slash_token):
        return (
            HTTPStatus.UNAUTHORIZED,
            {
                "response_type": "ephemeral",
                "text": "matter-codex: slash token не прошел проверку.",
            },
        )

    text = form.get("text", "").strip()
    if text in ("", "status"):
        return (
            HTTPStatus.OK,
            {
                "response_type": "ephemeral",
                "text": agents_status_text(config),
            },
        )

    return (
        HTTPStatus.OK,
        {
            "response_type": "ephemeral",
            "text": "matter-codex: доступна команда `/agents status`.",
        },
    )


class BotRequestHandler(BaseHTTPRequestHandler):
    server: "BotHTTPServer"

    def _write_json(self, status: int, payload: Mapping[str, object]) -> None:
        body = json.dumps(payload, ensure_ascii=False, sort_keys=True).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_form(self) -> dict[str, str]:
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length).decode("utf-8") if length else ""
        parsed = parse_qs(body, keep_blank_values=True)
        return {key: values[-1] if values else "" for key, values in parsed.items()}

    def do_GET(self) -> None:
        if self.path == "/healthz":
            self._write_json(HTTPStatus.OK, health_payload(self.server.config))
            return
        if self.path == "/readyz":
            self._write_json(HTTPStatus.OK, {"status": "ready", "service": SERVICE_NAME})
            return
        self._write_json(HTTPStatus.NOT_FOUND, {"error": "not_found"})

    def do_POST(self) -> None:
        if self.path != "/mattermost/slash/agents":
            self._write_json(HTTPStatus.NOT_FOUND, {"error": "not_found"})
            return
        status, payload = slash_response(self.server.config, self._read_form())
        self._write_json(status, payload)

    def log_message(self, format: str, *args: object) -> None:
        path = self.path.split("?", 1)[0]
        sys.stderr.write(f"{self.command} {path} {args[1] if len(args) > 1 else ''}\n")


class BotHTTPServer(ThreadingHTTPServer):
    def __init__(self, server_address: tuple[str, int], handler_class: type[BaseHTTPRequestHandler], config: BotConfig):
        super().__init__(server_address, handler_class)
        self.config = config


def main() -> int:
    config = load_config()
    server = BotHTTPServer((config.host, config.port), BotRequestHandler, config)
    print(f"{SERVICE_NAME} listening on {config.host}:{config.port}", flush=True)
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
