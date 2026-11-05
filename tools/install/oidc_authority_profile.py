"""Материализация явного Git-owned OIDC authority profile в новый immutable release render.

Модуль не читает environment или credentials, не обращается к IdP/БД/Kubernetes
и не меняет выбор bundled/external identity либо TLS-профиль.
"""

import argparse
import copy
import hashlib
import json
import uuid
from pathlib import Path

from deployment_profile import exact_keys, https_endpoint, require, Rejected


def validate(profile):
    """Отклоняет неизвестные modes/fields, произвольные authority selectors и неограниченные owner inputs."""
    require(isinstance(profile, dict), "OIDC_AUTHORITY_PROFILE_REJECTED")
    keys = ("schema", "managed_by", "mode")
    exact_keys(profile, keys if profile.get("mode") == "claims" else keys + ("policy",))
    require(profile["schema"] == "kodex.oidc-authority-profile/v1" and
            profile["managed_by"] == "git" and profile["mode"] in ("claims", "platform-managed"),
            "OIDC_AUTHORITY_PROFILE_REJECTED")
    if profile["mode"] == "claims":
        return copy.deepcopy(profile)
    policy = profile["policy"]
    exact_keys(policy, ("schema", "revision", "issuer", "bootstrap_subjects", "session_lifetime_seconds"))
    require(policy["schema"] == "kodex.oidc-identity-policy/v1" and
            type(policy["revision"]) is int and 1 <= policy["revision"] <= 2 ** 53 - 1 and
            type(policy["session_lifetime_seconds"]) is int and 60 <= policy["session_lifetime_seconds"] <= 43200,
            "OIDC_IDENTITY_POLICY_REJECTED")
    https_endpoint(policy["issuer"])
    subjects = policy["bootstrap_subjects"]
    require(isinstance(subjects, list) and 1 <= len(subjects) <= 16 and all(isinstance(v, str) for v in subjects),
            "OIDC_BOOTSTRAP_MAPPING_REJECTED")
    for subject in subjects:
        try:
            parsed = uuid.UUID(subject)
        except ValueError:
            raise Rejected("OIDC_BOOTSTRAP_MAPPING_REJECTED") from None
        require(parsed.int != 0 and str(parsed) == subject, "OIDC_BOOTSTRAP_MAPPING_REJECTED")
    require(len(set(subjects)) == len(subjects), "OIDC_BOOTSTRAP_MAPPING_REJECTED")
    result = copy.deepcopy(profile)
    result["policy"]["bootstrap_subjects"] = sorted(subjects)
    require(len(policy_bytes(result["policy"])) <= 16384, "OIDC_IDENTITY_POLICY_REJECTED")
    return result


def policy_bytes(policy):
    """Сохраняет порядок полей и HTML escaping Go json.Marshal; digest совпадает с domain OIDCIdentityPolicy.Digest."""
    ordered = {name: policy[name] for name in
               ("schema", "revision", "issuer", "bootstrap_subjects", "session_lifetime_seconds")}
    raw = json.dumps(ordered, ensure_ascii=False, separators=(",", ":"))
    for before, after in (("&", "\\u0026"), ("<", "\\u003c"), (">", "\\u003e"),
                          ("\u2028", "\\u2028"), ("\u2029", "\\u2029")):
        raw = raw.replace(before, after)
    return raw.encode()


def transform(resources, profile):
    """Проецирует policy только в CP и mode в CP/gateway, не меняя credentials, CA, proxy или другие workloads."""
    profile = validate(profile)
    require(isinstance(resources, list) and all(isinstance(v, dict) for v in resources), "OIDC_RENDER_REJECTED")
    result = copy.deepcopy(resources)
    if profile["mode"] == "claims":
        return result
    raw = policy_bytes(profile["policy"])
    digest = hashlib.sha256(raw).hexdigest()
    name = "kodex-oidc-policy-" + digest[:24]
    require(not any(r.get("kind") == "ConfigMap" and r.get("metadata", {}).get("name") == name for r in result),
            "OIDC_POLICY_CONFIG_COLLISION")
    for component, prefix in (("control-plane", "CONTROL_PLANE"), ("control-api-gateway", "CONTROL_API_GATEWAY")):
        deployments = [r for r in result if r.get("kind") == "Deployment" and
                       r.get("metadata", {}).get("namespace") == "kodex-system" and r["metadata"].get("name") == component]
        require(len(deployments) == 1, "OIDC_AUTHORITY_WORKLOAD_REJECTED")
        template = deployments[0]["spec"]["template"]
        pod = template["spec"]
        containers = [c for c in pod["containers"] if c.get("name") == component]
        require(len(containers) == 1, "OIDC_AUTHORITY_CONTAINER_REJECTED")
        container = containers[0]
        env = container.setdefault("env", [])
        issuer = [v for v in env if v.get("name") == prefix + "_OIDC_ISSUER"]
        require(len(issuer) == 1 and issuer[0].get("value") == profile["policy"]["issuer"], "OIDC_POLICY_ISSUER_MISMATCH")
        additions = {prefix + "_OIDC_AUTHORITY_MODE": "platform-managed"}
        if component == "control-plane":
            additions[prefix + "_OIDC_IDENTITY_POLICY_FILE"] = "/etc/kodex/oidc-identity-policy/policy.json"
            volumes = pod.setdefault("volumes", [])
            mounts = container.setdefault("volumeMounts", [])
            require(not any(v.get("name") == "oidc-identity-policy" for v in volumes + mounts) and
                    not any(v.get("mountPath") == "/etc/kodex/oidc-identity-policy" for v in mounts), "OIDC_POLICY_CONFIG_COLLISION")
            volumes.append({"name": "oidc-identity-policy", "configMap": {"name": name, "defaultMode": 292}})
            mounts.append({"name": "oidc-identity-policy", "mountPath": "/etc/kodex/oidc-identity-policy", "readOnly": True})
        require(not any(v.get("name") in additions for v in env), "OIDC_POLICY_CONFIG_COLLISION")
        env.extend({"name": key, "value": value} for key, value in additions.items())
        annotations = template.setdefault("metadata", {}).setdefault("annotations", {})
        require("kodex.dev/oidc-identity-policy-sha256" not in annotations, "OIDC_POLICY_CONFIG_COLLISION")
        annotations["kodex.dev/oidc-identity-policy-sha256"] = digest
    result.append({"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": name, "namespace": "kodex-system",
                   "labels": {"app.kubernetes.io/part-of": "kodex", "kodex.dev/managed-by": "git"},
                   "annotations": {"kodex.dev/oidc-identity-policy-sha256": digest}}, "immutable": True,
                   "data": {"policy.json": raw.decode()}})
    return result


def read_profile(path):
    """Читает ровно переданный bounded JSON profile; credentials и environment не используются."""
    with Path(path).open("rb") as file:
        raw = file.read(16385)
    require(0 < len(raw) <= 16384, "OIDC_AUTHORITY_PROFILE_REJECTED")
    return validate(json.loads(raw, object_pairs_hook=unique_object))


def unique_object(pairs):
    """Не допускает неоднозначные повторные JSON keys в owner policy или profile envelope."""
    result = {}
    for key, value in pairs:
        require(key not in result, "OIDC_AUTHORITY_PROFILE_REJECTED")
        result[key] = value
    return result


def main():
    """Проверяет profile либо создаёт новый YAML render; ошибка не печатает значения входного файла."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("validate", "render"))
    parser.add_argument("--profile", required=True)
    parser.add_argument("--input")
    parser.add_argument("--output")
    args = parser.parse_args()
    try:
        profile = read_profile(args.profile)
        if args.mode == "render":
            import yaml
            require(bool(args.input and args.output), "OIDC_RENDER_ARGUMENTS_REJECTED")
            try:
                resources = [v for v in yaml.safe_load_all(Path(args.input).read_text()) if v is not None]
            except yaml.YAMLError:
                raise Rejected("OIDC_RENDER_REJECTED") from None
            rendered = transform(resources, profile)
            Path(args.output).write_text(yaml.safe_dump_all(rendered, sort_keys=False))
    except Rejected as error:
        raise SystemExit("OIDC authority profile rejected: " + str(error)) from None
    except (ValueError, OSError, TypeError, KeyError, RecursionError):
        raise SystemExit("OIDC authority profile rejected") from None


if __name__ == "__main__":
    main()
