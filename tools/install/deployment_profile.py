"""Проверка и чистое преобразование опционального профиля установки #1979.

Модуль не обращается к сети, Kubernetes, environment или хранилищу секретов.
Выбор внешней инфраструктуры не меняет прикладные правила OIDC и полномочий.
"""

import copy
import argparse
import hashlib
import ipaddress
import json
import re
from pathlib import Path
from urllib.parse import urlsplit


class Rejected(ValueError):
    pass


def require(condition, code):
    if not condition:
        raise Rejected(code)


def exact_keys(value, keys):
    require(isinstance(value, dict) and set(value) == set(keys), "PROFILE_FIELDS_REJECTED")


def https_endpoint(value):
    require(isinstance(value, str) and 0 < len(value) <= 2048, "OIDC_URL_REJECTED")
    require(not re.search(r"[\s\\%]", value), "OIDC_URL_REJECTED")
    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError:
        raise Rejected("OIDC_URL_REJECTED") from None
    require(parsed.scheme == "https" and parsed.hostname and "." in parsed.hostname
            and not parsed.username and not parsed.password and not parsed.query
            and not parsed.fragment and "?" not in value and "#" not in value
            and port is None and parsed.netloc == parsed.hostname and parsed.hostname == parsed.hostname.lower()
            and not any(p in (".", "..") for p in parsed.path.split("/")),
            "OIDC_URL_REJECTED")
    return parsed


def exact_hosts(values):
    require(isinstance(values, list) and 0 < len(values) <= 16, "EXACT_HOST_CIDRS_REQUIRED")
    result = []
    for value in values:
        require(isinstance(value, str) and "/" in value, "EXACT_HOST_CIDRS_REQUIRED")
        try:
            network = ipaddress.ip_network(value, strict=True)
        except ValueError:
            raise Rejected("EXACT_HOST_CIDRS_REQUIRED") from None
        require(network.prefixlen == network.max_prefixlen and str(network) == value
                and not network.network_address.is_unspecified
                and not network.network_address.is_multicast
                and not network.network_address.is_loopback, "EXACT_HOST_CIDRS_REQUIRED")
        result.append(value)
    require(len(set(result)) == len(result), "DUPLICATE_HOST_CIDR")
    return result


def validate(profile):
    exact_keys(profile, ("version", "identity", "public_tls"))
    require(type(profile["version"]) is int and profile["version"] == 1, "PROFILE_VERSION_REJECTED")
    identity = profile["identity"]
    require(isinstance(identity, dict), "IDENTITY_MODE_REJECTED")
    if identity.get("mode") == "bundled":
        exact_keys(identity, ("mode",))
    else:
        exact_keys(identity, ("mode", "issuer", "jwks_url", "connect_host", "endpoint_cidrs",
                              "client_id", "audience", "scope", "authorization_url", "token_url"))
        require(identity["mode"] == "external", "IDENTITY_MODE_REJECTED")
        issuer, jwks = https_endpoint(identity["issuer"]), https_endpoint(identity["jwks_url"])
        require(issuer.netloc == jwks.netloc and jwks.path not in ("", "/"), "OIDC_AUTHORITY_MISMATCH")
        for key in ("authorization_url", "token_url"):
            require(https_endpoint(identity[key]).netloc == issuer.netloc, "OIDC_AUTHORITY_MISMATCH")
        host = identity["connect_host"]
        require(isinstance(host, str) and re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", host)
                and "." in host, "OIDC_CONNECT_HOST_REJECTED")
        try:
            ipaddress.ip_address(host)
        except ValueError:
            pass
        else:
            raise Rejected("OIDC_CONNECT_HOST_REJECTED")
        exact_hosts(identity["endpoint_cidrs"])
        require(isinstance(identity["client_id"], str) and
                re.fullmatch(r"[A-Za-z0-9_.-]{1,128}", identity["client_id"]), "OIDC_CLIENT_REJECTED")
        # Audience принадлежит общему контракту приложения, не произвольному IdP.
        require(identity["audience"] == "kodex-control-api", "OIDC_AUDIENCE_REJECTED")
        scope = identity["scope"]
        require(isinstance(scope, str) and re.fullmatch(r"[A-Za-z0-9_.: -]{1,256}", scope)
                and "openid" in scope.split() and scope == " ".join(scope.split())
                and len(scope.split()) == len(set(scope.split())),
                "OIDC_SCOPE_REJECTED")
    tls = profile["public_tls"]
    require(isinstance(tls, dict), "PUBLIC_TLS_MODE_REJECTED")
    if tls.get("mode") == "managed":
        exact_keys(tls, ("mode",))
    else:
        exact_keys(tls, ("mode", "trusted_proxy_cidrs"))
        require(tls["mode"] == "external", "PUBLIC_TLS_MODE_REJECTED")
        exact_hosts(tls["trusted_proxy_cidrs"])
    return copy.deepcopy(profile)


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def transform(resources, profile, public_host):
    """Преобразует новый release render; не служит live patch или сменой режима.

    Внешние endpoints остаются HTTPS, внутренние transport и secret refs сохраняются.
    Повторный запуск получает исходный render и даёт тот же результат.
    """
    profile = validate(profile)
    require(isinstance(resources, list) and all(isinstance(r, dict) for r in resources), "RENDER_REJECTED")
    require(isinstance(public_host, str) and
            re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", public_host)
            and "." in public_host, "PUBLIC_HOST_REJECTED")
    result = copy.deepcopy(resources)
    identity = profile["identity"]
    routes = [r for r in result if r.get("kind") == "Ingress" and
              r.get("metadata", {}).get("name") in
              ("staff-control-center", "staff-control-center-api", "staff-control-center-public-assets")]
    require(len(routes) == 3 and len({r["metadata"]["name"] for r in routes}) == 3, "FRONTEND_ROUTES_REQUIRED")
    for route in routes:
        require(route["metadata"].get("namespace") == "kodex-system" and
                len(route.get("spec", {}).get("rules", [])) == 1 and
                route["spec"]["rules"][0].get("host") == public_host, "PUBLIC_HOST_MISMATCH")
    if identity["mode"] == "external":
        # Разрешается только замена двух exact OIDC egress edges, не общая сеть.
        policies = [r for r in result if r.get("kind") == "NetworkPolicy" and
                    r["metadata"]["name"] in ("control-plane-exact-runtime-paths", "control-api-gateway-exact-runtime-paths")]
        require(len(policies) == 2, "OIDC_POLICY_SET_REJECTED")
        for policy in policies:
            matches = [edge for edge in policy["spec"]["egress"] if any(
                peer.get("podSelector", {}).get("matchLabels", {}).get("app.kubernetes.io/component")
                in ("identity-provider", "__KODEX_OIDC_POD_COMPONENT__") for peer in edge.get("to", []))]
            require(len(matches) == 1, "OIDC_POLICY_EDGE_REJECTED")
            matches[0].clear()
            matches[0].update(to=[{"ipBlock": {"cidr": c}} for c in identity["endpoint_cidrs"]],
                              ports=[{"protocol": "TCP", "port": 443}])
        gateways = [r for r in result if r.get("kind") == "Deployment" and
                    r["metadata"]["name"] == "control-api-gateway" and r["metadata"].get("namespace") == "kodex-system"]
        require(len(gateways) == 1, "OIDC_GATEWAY_REQUIRED")
        containers = [c for c in gateways[0]["spec"]["template"]["spec"]["containers"] if c["name"] == "control-api-gateway"]
        require(len(containers) == 1, "OIDC_GATEWAY_CONTAINER_REQUIRED")
        additions = {
            "CONTROL_API_GATEWAY_OIDC_BROWSER_CLIENT_ID": identity["client_id"],
            "CONTROL_API_GATEWAY_OIDC_AUTHORIZATION_URL": identity["authorization_url"],
            "CONTROL_API_GATEWAY_OIDC_TOKEN_URL": identity["token_url"],
            "CONTROL_API_GATEWAY_OIDC_BROWSER_SCOPE": identity["scope"],
        }
        env = containers[0].setdefault("env", [])
        require(not any(e.get("name") in additions for e in env), "OIDC_GATEWAY_CONFIG_COLLISION")
        env.extend({"name": name, "value": value} for name, value in additions.items())
        # Direct PKCE browser OIDC не зависит от bundled OAuth2 Proxy.
        for route in routes:
            route["metadata"].setdefault("annotations", {}).pop(
                "traefik.ingress.kubernetes.io/router.middlewares", None)
        runtime = [r for r in result if r.get("kind") == "ConfigMap" and
                   "runtime-config.json" in r.get("data", {}) and
                   r["metadata"]["name"].startswith("staff-control-center-runtime")]
        require(len(runtime) == 1, "FRONTEND_RUNTIME_REQUIRED")
        cm = runtime[0]
        config = json.loads(cm["data"]["runtime-config.json"])
        config["oidc"].update(authority=identity["issuer"], clientId=identity["client_id"], scope=identity["scope"])
        config.pop("revision", None)
        config["revision"] = fingerprint(config)
        cm["data"]["runtime-config.json"] = json.dumps(config, separators=(",", ":")) + "\n"
        readiness = json.loads(cm["data"]["readiness.json"])
        readiness["runtimeConfigRevision"] = config["revision"]
        cm["data"]["readiness.json"] = json.dumps(readiness, separators=(",", ":")) + "\n"
        old_name = cm["metadata"]["name"]
        new_name = "staff-control-center-runtime-profile-" + fingerprint(cm["data"])[:16]
        cm["metadata"]["name"] = new_name
        cm["immutable"] = True
        references = 0
        for resource in result:
            if resource.get("kind") == "Deployment" and resource["metadata"]["name"] == "staff-control-center":
                for volume in resource["spec"]["template"]["spec"].get("volumes", []):
                    if volume.get("configMap", {}).get("name") == old_name:
                        volume["configMap"]["name"] = new_name
                        references += 1
        require(references == 1, "FRONTEND_RUNTIME_REFERENCE_REJECTED")
    if profile["public_tls"]["mode"] == "external":
        certs = [r for r in result if r.get("kind") == "Certificate" and
                 r["metadata"]["name"] == "staff-control-center-public"]
        require(len(certs) == 1 and certs[0]["metadata"].get("namespace") == "kodex-system",
                "PUBLIC_CERTIFICATE_SET_REJECTED")
        result.remove(certs[0])
        proxies = profile["public_tls"]["trusted_proxy_cidrs"]
        for name, spec in (
            ("external-proxy-source", {"ipAllowList": {"sourceRange": proxies}}),
            ("external-proxy-origin", {"headers": {"customRequestHeaders": {
                "X-Forwarded-Proto": "https", "X-Forwarded-Host": public_host}}}),
        ):
            require(not any(r.get("kind") == "Middleware" and r["metadata"]["name"] == name
                            for r in result), "PROXY_MIDDLEWARE_COLLISION")
            result.append({"apiVersion": "traefik.io/v1alpha1", "kind": "Middleware",
                           "metadata": {"name": name, "namespace": "kodex-system",
                                        "labels": {"app.kubernetes.io/part-of": "kodex"}}, "spec": spec})
        for route in routes:
            annotations = route["metadata"].setdefault("annotations", {})
            require(annotations.get("traefik.ingress.kubernetes.io/router.tls") == "true",
                    "PUBLIC_ROUTE_TLS_PREDECESSOR_REJECTED")
            annotations.pop("traefik.ingress.kubernetes.io/router.tls")
            annotations["traefik.ingress.kubernetes.io/router.entrypoints"] = "web"
            previous = annotations.get("traefik.ingress.kubernetes.io/router.middlewares")
            chain = "kodex-system-external-proxy-source@kubernetescrd,kodex-system-external-proxy-origin@kubernetescrd"
            annotations["traefik.ingress.kubernetes.io/router.middlewares"] = chain + ("," + previous if previous else "")
            route["spec"].pop("tls", None)
    require(not any(r.get("kind") == "ConfigMap" and r["metadata"]["name"] == "kodex-installation-profile"
                    for r in result), "INSTALLATION_PROFILE_COLLISION")
    result.append({"apiVersion": "v1", "kind": "ConfigMap", "metadata": {
        "name": "kodex-installation-profile", "namespace": "kodex-system",
        "labels": {"app.kubernetes.io/part-of": "kodex", "app.kubernetes.io/managed-by": "kodex-install"}},
        "data": {"profile.json": json.dumps(profile, sort_keys=True, separators=(",", ":")),
                 "profile.sha256": fingerprint(profile)}})
    return result


def read_render_profile(resources):
    matches = [r for r in resources if r.get("kind") == "ConfigMap" and
               r.get("metadata", {}).get("name") == "kodex-installation-profile"]
    require(len(matches) == 1 and matches[0]["metadata"].get("namespace") == "kodex-system",
            "INSTALLATION_PROFILE_REQUIRED")
    data = matches[0]["data"]
    require(set(data) == {"profile.json", "profile.sha256", "source.sha", "release-lock.sha256", "schema-heads.json"},
            "INSTALLATION_PINS_REQUIRED")
    require(re.fullmatch(r"[a-f0-9]{40}", data["source.sha"]) and
            re.fullmatch(r"[a-f0-9]{64}", data["release-lock.sha256"]), "INSTALLATION_PINS_REJECTED")
    heads = json.loads(data["schema-heads.json"])
    require(isinstance(heads, dict) and set(heads) == {"control-plane", "internal-rpc-authority", "email-bridge"}
            and all(type(v) is int and v > 0 for v in heads.values()), "SCHEMA_PIN_SET_REJECTED")
    profile = validate(json.loads(matches[0]["data"]["profile.json"]))
    require(fingerprint(profile) == matches[0]["data"].get("profile.sha256"), "INSTALLATION_PROFILE_DRIFT")
    return profile


def verify_proxy_controller(deployment, profile):
    """Read-only проверка выделенного Traefik; общий controller не изменяется."""
    tls = validate(profile)["public_tls"]
    require(tls["mode"] == "external", "EXTERNAL_PROXY_PROFILE_REQUIRED")
    require(deployment.get("kind") == "Deployment", "PROXY_CONTROLLER_REQUIRED")
    containers = deployment["spec"]["template"]["spec"]["containers"]
    controllers = [c for c in containers if c.get("name") == "traefik"]
    require(len(controllers) == 1, "EXACT_TRAEFIK_CONTAINER_REQUIRED")
    controller = controllers[0]
    require(controller.get("command", []) in ([], ["traefik"], ["/usr/local/bin/traefik"]),
            "AMBIGUOUS_TRAEFIK_COMMAND")
    require(not any(e.get("name", "").startswith("TRAEFIK_") for e in controller.get("env", []))
            and not controller.get("envFrom"), "AMBIGUOUS_TRAEFIK_CONFIGURATION")
    options = {}
    for raw in controller.get("args", []):
        require(isinstance(raw, str) and raw.startswith("--"), "TRAEFIK_ARGUMENT_REJECTED")
        key, _, value = raw[2:].partition("=")
        key = key.lower()
        require(key not in options, "DUPLICATE_TRAEFIK_OPTION")
        options[key] = value
    require("configfile" not in options, "AMBIGUOUS_TRAEFIK_CONFIGURATION")
    require(options.get("entrypoints.web.address") == ":80", "HTTP_80_ENTRYPOINT_REQUIRED")
    for key, value in options.items():
        if key.startswith("entrypoints.web."):
            require("redirections" not in key and "proxyprotocol" not in key, "PROXY_ENTRYPOINT_REJECTED")
            if key.endswith("forwardedheaders.insecure"):
                require(value == "false", "INSECURE_FORWARDED_HEADERS_REJECTED")
    trusted = options.get("entrypoints.web.forwardedheaders.trustedips", "").split(",")
    require(set(exact_hosts(trusted)) == set(tls["trusted_proxy_cidrs"]), "TRUSTED_PROXY_CONFIG_DRIFT")
    desired = deployment["spec"].get("replicas", 1)
    status = deployment.get("status", {})
    require(type(desired) is int and desired >= 1 and
            status.get("observedGeneration", 0) >= deployment.get("metadata", {}).get("generation", 1) and
            all(status.get(field, 0) == desired for field in
                ("replicas", "updatedReplicas", "readyReplicas", "availableReplicas")) and
            status.get("unavailableReplicas", 0) == 0, "PROXY_CONTROLLER_NOT_READY")


def verify_proxy_pods(deployment, pods, replica_sets, service, slices):
    """Проверяется реально обслуживающий состав, а не только новый Deployment spec."""
    desired = deployment["spec"].get("replicas", 1)
    uid = deployment["metadata"]["uid"]
    current = deployment["spec"]["template"]["spec"]["containers"]
    selectors = service.get("spec", {}).get("selector", {})
    require(selectors and all(deployment["spec"]["template"]["metadata"]["labels"].get(k) == v
                              for k, v in selectors.items()), "PROXY_SERVICE_SELECTOR_REJECTED")
    web_ports = [p for p in service["spec"].get("ports", []) if p.get("port") == 80 and p.get("protocol", "TCP") == "TCP"]
    require(len(web_ports) == 1, "PROXY_SERVICE_HTTP_80_REQUIRED")
    target = web_ports[0].get("targetPort", 80)
    traefik = [c for c in current if c.get("name") == "traefik"]
    require(len(traefik) == 1 and (type(target) is int and target == 80 or
            isinstance(target, str) and any(p.get("name") == target and p.get("containerPort") == 80 and
                p.get("protocol", "TCP") == "TCP" for p in traefik[0].get("ports", []))),
            "PROXY_SERVICE_TARGET_PORT_REJECTED")
    require(len(pods) == desired, "PROXY_SERVING_POD_SET_REJECTED")
    rs_uids = {r["metadata"]["uid"] for r in replica_sets if any(
        o.get("uid") == uid and o.get("kind") == "Deployment" and o.get("controller") is True
        for o in r["metadata"].get("ownerReferences", []))}
    pod_uids = set()
    for pod in pods:
        require(not pod["metadata"].get("deletionTimestamp") and pod.get("status", {}).get("phase") == "Running"
                and any(c.get("type") == "Ready" and c.get("status") == "True" for c in pod["status"].get("conditions", []))
                and any(o.get("kind") == "ReplicaSet" and o.get("uid") in rs_uids and o.get("controller") is True
                        for o in pod["metadata"].get("ownerReferences", [])), "PROXY_POD_OWNER_OR_READINESS_REJECTED")
        actual = pod["spec"]["containers"]
        require(len(actual) == len(current), "PROXY_POD_CONFIG_DRIFT")
        for expected, live in zip(current, actual):
            require(all(live.get(k, []) == expected.get(k, []) for k in ("name", "image", "command", "args", "env", "envFrom")),
                    "PROXY_POD_CONFIG_DRIFT")
        pod_uids.add(pod["metadata"]["uid"])
    endpoints = set()
    for group in slices:
        require(group["metadata"].get("labels", {}).get("kubernetes.io/service-name") == service["metadata"]["name"],
                "PROXY_ENDPOINT_SERVICE_DRIFT")
        mapped = [p for p in group.get("ports", []) if p.get("name") == web_ports[0].get("name")]
        require(len(mapped) == 1 and mapped[0].get("port") == 80 and mapped[0].get("protocol", "TCP") == "TCP",
                "PROXY_ENDPOINT_PORT_REJECTED")
        for endpoint in group.get("endpoints", []):
            ref = endpoint.get("targetRef", {})
            require(endpoint.get("conditions", {}).get("ready") is True and
                    ref.get("kind") == "Pod" and ref.get("uid") in pod_uids, "PROXY_ENDPOINT_POD_DRIFT")
            endpoints.add(ref["uid"])
    require(endpoints == pod_uids and endpoints, "PROXY_ENDPOINT_SET_DRIFT")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("validate", "render"))
    parser.add_argument("--profile", required=True)
    parser.add_argument("--input")
    parser.add_argument("--output")
    parser.add_argument("--public-host")
    parser.add_argument("--release-lock")
    args = parser.parse_args()
    try:
        raw = Path(args.profile).read_bytes()
        require(len(raw) <= 32768, "PROFILE_SIZE_REJECTED")
        def unique_object(pairs):
            result = {}
            for key, value in pairs:
                require(key not in result, "DUPLICATE_PROFILE_FIELD")
                result[key] = value
            return result
        profile = validate(json.loads(raw, object_pairs_hook=unique_object))
        if args.command == "render":
            import yaml
            require(args.input and args.output and args.public_host, "RENDER_ARGUMENTS_REQUIRED")
            resources = list(filter(None, yaml.safe_load_all(Path(args.input).read_text())))
            result = transform(resources, profile, args.public_host)
            require(args.release_lock, "RELEASE_LOCK_REQUIRED")
            lock = json.loads(Path(args.release_lock).read_text())
            require(isinstance(lock.get("schema_heads"), dict) and
                    set(lock["schema_heads"]) == {"control-plane", "internal-rpc-authority", "email-bridge"} and
                    all(type(v) is int and v > 0 for v in lock["schema_heads"].values()), "SCHEMA_PIN_SET_REJECTED")
            result[-1]["data"]["source.sha"] = lock["source_sha"]
            result[-1]["data"]["schema-heads.json"] = json.dumps(lock["schema_heads"], sort_keys=True)
            result[-1]["data"]["release-lock.sha256"] = hashlib.sha256(Path(args.release_lock).read_bytes()).hexdigest()
            Path(args.output).write_text(yaml.safe_dump_all(result, sort_keys=False))
    except (Rejected, OSError, ValueError, KeyError, TypeError):
        raise SystemExit("Deployment profile rejected") from None


if __name__ == "__main__":
    main()
