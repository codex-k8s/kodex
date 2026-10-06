#!/usr/bin/env python3
"""Узкое применение/readback SSO policy без bootstrap пользователей и ролей."""

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.parse

SESSION_SECONDS = 43200
POLICY = {
    "ssoSessionIdleTimeout": SESSION_SECONDS,
    "ssoSessionMaxLifespan": SESSION_SECONDS,
    "ssoSessionIdleTimeoutRememberMe": SESSION_SECONDS,
    "ssoSessionMaxLifespanRememberMe": SESSION_SECONDS,
    "rememberMe": True,
}
REALM_FIELDS = tuple(POLICY) + (
    "clientSessionIdleTimeout", "clientSessionMaxLifespan",
    "accessTokenLifespan", "revokeRefreshToken", "refreshTokenMaxReuse",
)
CLIENT_FIELDS = (
    "client.session.idle.timeout", "client.session.max.lifespan",
    "access.token.lifespan",
)


class PolicyError(Exception):
    pass


def validate_inherited_timeouts(realm, client):
    for key in ("clientSessionIdleTimeout", "clientSessionMaxLifespan"):
        value = realm.get(key, 0)
        if type(value) is not int or value not in (0, SESSION_SECONDS):
            raise PolicyError("Realm client timeout must inherit or equal 12 hours")
    for key in CLIENT_FIELDS[:2]:
        value = client.get("attributes", {}).get(key)
        if value not in (None, "", "0", str(SESSION_SECONDS)):
            raise PolicyError("Control Center client timeout must inherit or equal 12 hours")


def safe_projection(realm, client):
    return {
        "realm": {key: realm.get(key) for key in REALM_FIELDS},
        "client": "kodex-control-center",
        "clientTimeoutAttributes": {
            key: client.get("attributes", {}).get(key) for key in CLIENT_FIELDS
        },
    }


class API:
    def __init__(self, origin, ca_file, username, password):
        self.origin = origin
        self.ca_file = ca_file
        response = self.request(
            "/realms/master/protocol/openid-connect/token",
            data=urllib.parse.urlencode({
                "grant_type": "password", "client_id": "admin-cli",
                "username": username, "password": password,
            }).encode(),
        )
        self.token = response.get("access_token")
        if not isinstance(self.token, str) or not re.fullmatch(r"[A-Za-z0-9._~-]{1,32768}", self.token):
            raise PolicyError("Keycloak authentication response is invalid")

    def request(self, path, data=None, method=None, authenticated=False):
        headers = {}
        if data is not None:
            headers["Content-Type"] = (
                "application/json" if authenticated else "application/x-www-form-urlencoded"
            )
        if authenticated:
            headers["Authorization"] = "Bearer " + self.token
        # Credentials и bearer передаются curl только через stdin, не argv/env/config-файл.
        config = ["url = " + json.dumps(self.origin + path)]
        config.append("request = " + json.dumps(method or ("POST" if data is not None else "GET")))
        for key, value in headers.items():
            config.append("header = " + json.dumps(key + ": " + value))
        if data is not None:
            config.append("data = " + json.dumps(data.decode()))
        command = ["curl", "--disable", "--silent", "--noproxy", "*", "--proto", "=https",
                   "--max-time", "20", "--write-out", "\n%{http_code}", "--config", "-"]
        if self.ca_file:
            command.extend(["--cacert", self.ca_file])
        try:
            response = subprocess.run(command, input="\n".join(config) + "\n",
                                      text=True, capture_output=True, timeout=25, check=False,
                                      env={"PATH": os.environ.get("PATH", "/usr/bin:/bin")})
            if response.returncode:
                raise PolicyError("Keycloak HTTPS transport failed, curl exit " + str(response.returncode))
            body, status = response.stdout.rsplit("\n", 1)
            if status not in ("200", "204"):
                raise PolicyError("Keycloak HTTP status " + status)
            return None if status == "204" else json.loads(body)
        except (subprocess.SubprocessError, OSError):
            raise PolicyError("Keycloak HTTPS transport failed") from None
        except (ValueError, KeyError):
            raise PolicyError("Keycloak response is invalid") from None

    def read(self, realm_name):
        realm = self.request("/admin/realms/" + realm_name, authenticated=True)
        clients = self.request(
            "/admin/realms/" + realm_name + "/clients?clientId=kodex-control-center",
            authenticated=True,
        )
        if not isinstance(realm, dict) or not isinstance(clients, list) or len(clients) != 1:
            raise PolicyError("Exact realm or client readback is invalid")
        client = clients[0]
        if not isinstance(client, dict) or client.get("clientId") != "kodex-control-center":
            raise PolicyError("Exact Control Center client readback is invalid")
        validate_inherited_timeouts(realm, client)
        return realm, client

    def execute(self, realm_name, mode):
        before, client = self.read(realm_name)
        if mode == "apply":
            self.request(
                "/admin/realms/" + realm_name, data=json.dumps(POLICY).encode(),
                method="PUT", authenticated=True,
            )
        after, fresh_client = self.read(realm_name)
        if any(after.get(key) != value for key, value in POLICY.items()):
            raise PolicyError("SSO session policy readback mismatch")
        # Узкая операция не должна менять rotation, token TTL или client attributes.
        if safe_projection(before, client)["clientTimeoutAttributes"] != safe_projection(after, fresh_client)["clientTimeoutAttributes"]:
            raise PolicyError("Client timeout changed during realm policy operation")
        for key in REALM_FIELDS[len(POLICY):]:
            if before.get(key) != after.get(key):
                raise PolicyError("Unrelated realm timeout or rotation changed")
        return safe_projection(after, fresh_client)


def trusted_origin(context, namespace, configmap, realm):
    def kubectl(*arguments):
        try:
            return subprocess.check_output(
                ["kubectl", "--context", context, *arguments],
                stderr=subprocess.DEVNULL, text=True, timeout=20,
                env={key: os.environ[key] for key in ("PATH", "KUBECONFIG") if key in os.environ},
            ).strip()
        except (subprocess.SubprocessError, OSError):
            raise PolicyError("Trusted Kubernetes metadata read failed") from None

    current = kubectl("config", "current-context")
    if current != context:
        raise PolicyError("Current Kubernetes context mismatch")
    issuer = kubectl(
        "-n", namespace, "get", "configmap", configmap,
        "-o", "jsonpath={.data.oidcIssuer}",
    )
    parsed = urllib.parse.urlsplit(issuer)
    if (
        parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password
        or parsed.query or parsed.fragment or parsed.path != "/realms/" + realm
    ):
        raise PolicyError("Trusted Keycloak HTTPS issuer is invalid")
    return parsed.scheme + "://" + parsed.netloc


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", required=True)
    parser.add_argument("--mode", choices=("apply", "readback"), required=True)
    parser.add_argument("--realm", default="kodex")
    parser.add_argument("--namespace", default="kodex-system")
    parser.add_argument("--endpoints-configmap", default="kodex-platform-endpoints")
    parser.add_argument("--ca-file")
    args = parser.parse_args()
    if not os.environ.get("KUBECONFIG"):
        raise PolicyError("KUBECONFIG")
    for name in (args.realm, args.namespace, args.endpoints_configmap):
        if not re.fullmatch(r"[a-z0-9][a-z0-9.-]*", name):
            raise PolicyError("Resource name is invalid")
    keys = ("KODEX_LOCAL_ADMIN_USERNAME", "KODEX_LOCAL_ADMIN_PASSWORD")
    for key in keys:
        if not os.environ.get(key):
            raise PolicyError(key)
    origin = trusted_origin(args.context, args.namespace, args.endpoints_configmap, args.realm)
    api = API(origin, args.ca_file, os.environ[keys[0]], os.environ[keys[1]])
    print(json.dumps(api.execute(args.realm, args.mode), sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (PolicyError, ValueError, OSError) as error:
        message = str(error) if isinstance(error, PolicyError) else "Session policy configuration failed"
        print("Keycloak session policy failed: " + message, file=sys.stderr)
        sys.exit(1)
